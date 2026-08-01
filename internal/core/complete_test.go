package core

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

func TestCompleteConsumesExactEvidenceOnce(t *testing.T) {
	root, _, evidenceID := completeRoot(t, true)
	result, err := CompleteTask(root, "safe-change", completeRequest(3))
	if err != nil || result.EvidenceID != evidenceID || result.RevisionAfter != 4 {
		t.Fatalf("completion = %#v, %v", result, err)
	}
	current := readTaskTransitionState(t, root)
	activity, activityErr := ProjectTaskActivity(current.Tasks, "T1")
	bindings, bindingErr := decodeCompletionBindings(current.Extensions[completionExtensionKey])
	if activityErr != nil || bindingErr != nil || activity != TaskCompleted ||
		current.Revision != 4 || bindings["T1"].EvidenceID != evidenceID ||
		bindings["T1"].HistoryID != current.LastTransition {
		t.Fatalf("state/bindings = %#v / %#v / %v / %v", current, bindings, activityErr, bindingErr)
	}
	if _, err := CompleteTask(root, "safe-change", completeRequest(3)); !failure.IsCode(err, "stale_revision") {
		t.Fatalf("replay = %v", err)
	}
}

func TestCompleteRequiresCurrentProofAndExactScope(t *testing.T) {
	t.Run("no proof", func(t *testing.T) {
		root, _, _ := completeRoot(t, false)
		if _, err := CompleteTask(root, "safe-change", completeRequest(3)); !failure.IsCode(err, "complete_evidence") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("outside scope", func(t *testing.T) {
		root, _, _ := completeRoot(t, true)
		if err := os.WriteFile(filepath.Join(root, "sibling.txt"), []byte("outside\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := CompleteTask(root, "safe-change", completeRequest(3)); !failure.IsCode(err, "scope_outside") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("scope drift at commit boundary", func(t *testing.T) {
		root, _, _ := completeRoot(t, true)
		request := completeRequest(3)
		request.BeforeHistory = func() error {
			return os.WriteFile(filepath.Join(root, "late-sibling.txt"), []byte("outside\n"), 0o600)
		}
		if _, err := CompleteTask(root, "safe-change", request); !failure.IsCode(err, "scope_outside") {
			t.Fatalf("error = %v", err)
		}
		history, _, err := record.Replay(
			filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory,
		)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range history {
			if item.Kind == record.KindCompletion {
				t.Fatal("completion history appended after late scope drift")
			}
		}
	})
	t.Run("HEAD drift at commit boundary", func(t *testing.T) {
		root, _, _ := completeRoot(t, true)
		request := completeRequest(3)
		request.BeforeHistory = func() error {
			command := exec.Command("git", "-C", root, "commit", "--allow-empty", "-m", "late drift")
			return command.Run()
		}
		if _, err := CompleteTask(root, "safe-change", request); !failure.IsCode(err, "complete_head") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestCompleteConcurrentCASOneWinner(t *testing.T) {
	root, _, _ := completeRoot(t, true)
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := CompleteTask(root, "safe-change", completeRequest(3)); err == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful completions = %d", successes.Load())
	}
}

func TestCompleteSerializesNewerFailedEvidence(t *testing.T) {
	root, _, _ := completeRoot(t, true)
	evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
	items, _, err := record.Replay(evidencePath, record.FamilyEvidence)
	if err != nil || len(items) != 1 {
		t.Fatalf("evidence = %#v, %v", items, err)
	}
	failed, err := evidence.Decode(items[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	failed.StartedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	failed.EndedAt = time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	failed.ExitCode, failed.Passed, failed.NonVacuous = 1, false, false

	started := make(chan struct{})
	finished := make(chan error, 1)
	request := completeRequest(3)
	request.BeforeHistory = func() error {
		go func() {
			close(started)
			_, err := evidence.Append(evidencePath, "agent:validator", failed)
			finished <- err
		}()
		<-started
		return nil
	}
	if _, err := CompleteTask(root, "safe-change", request); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	current := readTaskTransitionState(t, root)
	activity, _ := ProjectTaskActivity(current.Tasks, "T1")
	if activity != TaskCompleted {
		t.Fatalf("serialized completion activity = %q", activity)
	}
}

func TestCompleteInterruptedTransactionRecoversOnce(t *testing.T) {
	root, _, _ := completeRoot(t, true)
	request := completeRequest(3)
	request.AfterHistory = func() error { return errors.New("stop") }
	if _, err := CompleteTask(root, "safe-change", request); !failure.IsCode(err, "complete_interrupted") {
		t.Fatalf("interruption = %v", err)
	}
	if current := readTaskTransitionState(t, root); current.Revision != 3 {
		t.Fatalf("pending completion changed state: %#v", current)
	}
	evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
	items, _, err := record.Replay(evidencePath, record.FamilyEvidence)
	if err != nil || len(items) != 1 {
		t.Fatalf("evidence = %#v, %v", items, err)
	}
	failed, err := evidence.Decode(items[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	failed.StartedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	failed.EndedAt = time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	failed.ExitCode, failed.Passed, failed.NonVacuous = 1, false, false
	if _, err := evidence.Append(evidencePath, "agent:validator", failed); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", root, "commit", "--allow-empty", "-m", "post-decision drift").Run(); err != nil {
		t.Fatal(err)
	}
	request.AfterHistory = nil
	if _, err := CompleteTask(root, "safe-change", request); err != nil {
		t.Fatal(err)
	}
	history, _, err := record.Replay(filepath.Join(root, ".specd", "history.jsonl"), record.FamilyHistory)
	count := 0
	for _, item := range history {
		if item.Kind == record.KindCompletion {
			count++
		}
	}
	if err != nil || count != 1 {
		t.Fatalf("completion history count = %d, %v", count, err)
	}
}

func TestCompleteRequiresSealedAuthority(t *testing.T) {
	root, _, _ := completeRoot(t, true)
	request := completeRequest(3)
	request.Authority = CompletionAuthority{}
	if _, err := CompleteTask(root, "safe-change", request); !failure.IsCode(err, "complete_unauthorized") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompleteRefusesUnprovenTransitions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		code   string
	}{
		{"task not in progress", func(t *testing.T, root string) {
			rewriteCompleteState(t, root, func(current *state.State) {
				delete(current.Tasks, "T1")
			})
		}, "complete_activity"},
		{"dependency incomplete", func(t *testing.T, root string) {
			writeCompleteTasks(t, root,
				"| T0 | builder | internal/other.go | | sample/Requirement: Stable updates | `go test ./...` | Check passes |\n"+
					"| T1 | builder | internal/sample.go | T0 | sample/Requirement: Stable updates | `go test ./...` | Check passes |\n")
		}, "dependency_incomplete"},
		{"attempt missing", func(t *testing.T, root string) {
			rewriteCompleteState(t, root, func(current *state.State) {
				delete(current.Extensions, attemptExtensionKey)
			})
		}, "complete_attempt"},
		{"attempt contract drift", func(t *testing.T, root string) {
			rewriteAttemptTasks(t, root, "internal/other.go")
		}, "complete_attempt"},
		{"approval stale", func(t *testing.T, root string) {
			appendAttemptFile(t,
				filepath.Join(root, ".specd", "changes", "safe-change", "proposal.md"),
				[]byte("\ndrift\n"),
			)
		}, "complete_approval"},
		{"evidence already consumed", func(t *testing.T, root string) {
			consumeCompleteEvidenceElsewhere(t, root)
		}, "complete_replay"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _, _ := completeRoot(t, true)
			test.mutate(t, root)
			historyPath := filepath.Join(root, ".specd", "history.jsonl")
			beforeHistory, _ := os.ReadFile(historyPath)
			if _, err := CompleteTask(root, "safe-change", completeRequest(3)); !failure.IsCode(err, test.code) {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
			afterHistory, _ := os.ReadFile(historyPath)
			current := readTaskTransitionState(t, root)
			if current.Revision != 3 || string(beforeHistory) != string(afterHistory) {
				t.Fatalf("refusal committed completion: %#v", current)
			}
		})
	}
}

// Completion consumes evidence; it must never re-run the task command or write
// planning artifacts and the worktree.
func TestCompleteObservesNothingAndWritesNoArtifacts(t *testing.T) {
	root, _, _ := completeRoot(t, true)
	changeRoot := filepath.Join(root, ".specd", "changes", "safe-change")
	watched := []string{
		filepath.Join(changeRoot, "tasks.md"),
		filepath.Join(changeRoot, "proposal.md"),
		filepath.Join(changeRoot, "design.md"),
		filepath.Join(changeRoot, "specs", "sample", "spec.md"),
		filepath.Join(root, ".specd", "evidence.jsonl"),
	}
	before := make(map[string][]byte, len(watched))
	for _, path := range watched {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = raw
	}
	beforeTree := gitPorcelain(t, root)
	if _, err := CompleteTask(root, "safe-change", completeRequest(3)); err != nil {
		t.Fatal(err)
	}
	for _, path := range watched {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(before[path]) != string(after) {
			t.Fatalf("completion rewrote %s", path)
		}
	}
	if after := gitPorcelain(t, root); after != beforeTree {
		t.Fatalf("completion changed the worktree: %q -> %q", beforeTree, after)
	}
}

func gitPorcelain(t *testing.T, root string) string {
	t.Helper()
	output, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func rewriteCompleteState(t *testing.T, root string, mutate func(*state.State)) {
	t.Helper()
	current := readTaskTransitionState(t, root)
	mutate(&current)
	raw, err := state.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".specd", "changes", "safe-change", "state.json"), raw, 0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func writeCompleteTasks(t *testing.T, root, rows string) {
	t.Helper()
	raw := []byte(
		"| id | role | files | depends-on | refs | verify | acceptance |\n" +
			"|---|---|---|---|---|---|---|\n" + rows,
	)
	if err := os.WriteFile(
		filepath.Join(root, ".specd", "changes", "safe-change", "tasks.md"), raw, 0o600,
	); err != nil {
		t.Fatal(err)
	}
}

// consumeCompleteEvidenceElsewhere records that another change already consumed
// the current evidence, which must not be spendable twice.
func consumeCompleteEvidenceElsewhere(t *testing.T, root string) {
	t.Helper()
	historyPath := filepath.Join(root, ".specd", "history.jsonl")
	items, _, err := record.Replay(filepath.Join(root, ".specd", "evidence.jsonl"), record.FamilyEvidence)
	if err != nil || len(items) != 1 {
		t.Fatalf("evidence = %#v, %v", items, err)
	}
	current := readTaskTransitionState(t, root)
	attempt, found, err := CurrentAttempt(current, "T1")
	if err != nil || !found {
		t.Fatalf("attempt = %#v, %v, %v", attempt, found, err)
	}
	payload, err := record.NewCompletionPayload(record.CompletionPayload{
		Change: "other-change", TaskID: "T1", AttemptID: attempt.ID,
		EvidenceID: items[0].ID, Actor: "agent:builder",
		ActivityFrom: string(TaskInProgress), ActivityTo: string(TaskCompleted),
		RevisionBefore: 1, RevisionAfter: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	item, err := record.New(record.Record{
		Family: record.FamilyHistory, Kind: record.KindCompletion, Change: "other-change",
		ExpectedRevision: record.Revision(1), ResultingRevision: record.Revision(2),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Actor:     "agent:builder", Payload: encoded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := record.Append(historyPath, record.FamilyHistory, item); err != nil {
		t.Fatal(err)
	}
}

func completeRoot(t *testing.T, withEvidence bool) (string, Attempt, string) {
	t.Helper()
	root := attemptRoot(t)
	attempt, err := StartAttempt(root, "safe-change", attemptRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	if !withEvidence {
		return root, attempt, ""
	}
	authored := plan.ParseChange(mustOwner(t, root), "safe-change")
	task, found := canonicalTask(authored.Tasks.Tasks, "T1")
	if !found {
		t.Fatal("missing canonical task")
	}
	current := readTaskTransitionState(t, root)
	approval, err := CurrentApprovalStatus(root, "safe-change")
	if err != nil || approval.Approval == nil {
		t.Fatalf("approval = %#v, %v", approval, err)
	}
	now := time.Now().UTC()
	run := verifyexec.Result{
		StartedAt: now.Format(time.RFC3339Nano), EndedAt: now.Add(time.Millisecond).Format(time.RFC3339Nano),
		ExitCode: 0, Passed: true, NonVacuous: true,
		StdoutDigest: strings.Repeat("a", 64), StderrDigest: strings.Repeat("b", 64),
	}
	command, err := verifyexec.ParseCommand(task.Verify)
	if err != nil {
		t.Fatal(err)
	}
	commandHash, err := verifyexec.CommandDigest(command)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := evidence.NewTestRun(evidence.Subject{
		Change: "safe-change", TaskID: "T1", AttemptID: attempt.ID,
		HEAD: attempt.BaselineHEAD, TaskHash: taskContractHash(task),
		CommandHash: commandHash, ApprovalHash: approval.Approval.AggregateHash,
		StateRevision: current.Revision,
	}, run)
	if err != nil {
		t.Fatal(err)
	}
	item, err := evidence.Append(filepath.Join(root, ".specd", "evidence.jsonl"), "agent:validator", observation)
	if err != nil {
		t.Fatal(err)
	}
	return root, attempt, item.ID
}

func completeRequest(revision uint64) CompleteRequest {
	return CompleteRequest{
		TaskID: "T1", ExpectedRevision: revision,
		Authority: trustedCompletionAuthority("agent:builder"),
	}
}

func mustOwner(t *testing.T, root string) *corepath.Owner {
	t.Helper()
	owner, err := corepath.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func TestProductionCompletionAddsBlockersToEveryCoreGuard(t *testing.T) {
	root, attempt, evidenceID := completeRoot(t, true)
	policy := ProductionPolicy()
	// Every core guard already passes: only production proof is missing, and it
	// is demanded one check at a time with a stable code and one remediation.
	for _, check := range policy.RequiredChecks {
		err := productionComplete(root, nil)
		var refusal *failure.Refusal
		if !failure.IsCode(err, "production_evidence") || !errors.As(err, &refusal) {
			t.Fatalf("missing %s = %v", check.String(), err)
		}
		if !strings.Contains(refusal.Reason, check.String()) ||
			!strings.Contains(refusal.Reason, policy.Digest()) ||
			!strings.Contains(refusal.Reason, "T1") || refusal.Next == "" {
			t.Fatalf("refusal for %s = %#v", check.String(), refusal)
		}
		appendProductionProof(t, root, attempt, check, policy.Digest())
	}
	result, err := productionCompletion(root, nil)
	if err != nil || result.EvidenceID != evidenceID || result.RevisionAfter != 4 {
		t.Fatalf("production completion = %#v, %v", result, err)
	}
	// Production consumes the same core test-run proof once and no other route.
	if _, err := productionCompletion(root, nil); !failure.IsCode(err, "stale_revision") {
		t.Fatalf("replay = %v", err)
	}
}

func TestProductionCompletionCannotBypassCoreGuards(t *testing.T) {
	policy := ProductionPolicy()
	t.Run("no test-run proof", func(t *testing.T) {
		root, attempt, _ := completeRoot(t, false)
		for _, check := range policy.RequiredChecks {
			appendProductionProof(t, root, attempt, check, policy.Digest())
		}
		if err := productionComplete(root, nil); !failure.IsCode(err, "complete_evidence") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("outside scope", func(t *testing.T) {
		root, attempt, _ := completeRoot(t, true)
		for _, check := range policy.RequiredChecks {
			appendProductionProof(t, root, attempt, check, policy.Digest())
		}
		if err := os.WriteFile(filepath.Join(root, "sibling.txt"), []byte("outside\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := productionComplete(root, nil); !failure.IsCode(err, "scope_outside") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unknown profile", func(t *testing.T) {
		root, _, _ := completeRoot(t, true)
		request := completeRequest(3)
		request.Profile = "strict"
		if _, err := CompleteTask(root, "safe-change", request); !failure.IsCode(err, "policy_unknown") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestProductionCompletionLeavesDefaultProfileUnchanged(t *testing.T) {
	root, _, evidenceID := completeRoot(t, true)
	result, err := CompleteTask(root, "safe-change", completeRequest(3))
	if err != nil || result.EvidenceID != evidenceID || result.RevisionAfter != 4 {
		t.Fatalf("default completion = %#v, %v", result, err)
	}
	// Work completed under the default profile is never reinterpreted: a later
	// production attempt cannot reopen or re-judge it.
	before := readTaskTransitionState(t, root)
	retry := completeRequest(before.Revision)
	retry.Profile = string(ProfileProduction)
	if _, err := CompleteTask(root, "safe-change", retry); !failure.IsCode(err, "complete_activity") {
		t.Fatalf("retroactive production completion = %v", err)
	}
	after := readTaskTransitionState(t, root)
	activity, err := ProjectTaskActivity(after.Tasks, "T1")
	if err != nil || activity != TaskCompleted || after.Revision != before.Revision ||
		after.LastTransition != before.LastTransition {
		t.Fatalf("state changed: %#v -> %#v (%v, %v)", before, after, activity, err)
	}
}

func TestProductionCompletionRefusesDriftAndIncompletePlans(t *testing.T) {
	root, attempt, _ := completeRoot(t, true)
	policy := ProductionPolicy()
	prior := ProductionPolicy()
	prior.AcceptanceReach = false
	for _, check := range policy.RequiredChecks {
		appendProductionProof(t, root, attempt, check, prior.Digest())
	}
	if err := productionComplete(root, nil); !failure.IsCode(err, "policy_drift") {
		t.Fatalf("drift = %v", err)
	}
	// An explicit human transition authorizes the current policy; it never
	// substitutes for proof under it.
	transition := &PolicyTransition{
		From: prior.Digest(), To: policy.Digest(),
		Approver: "human@example.com", Reason: "adopt production policy",
	}
	if err := productionComplete(root, transition); !failure.IsCode(err, "production_evidence") {
		t.Fatalf("after transition = %v", err)
	}
	// An incomplete production plan blocks with the failing rule and one repair.
	owner := mustOwner(t, root)
	authored := plan.ParseChange(owner, "safe-change")
	authored.Design.References = nil
	err := validateProductionCompletion(owner, policy, completeRequest(3), gates.Snapshot{
		Plan: authored, Lifecycle: "planning", StateRevision: 3,
		Transition: gates.PlanningToApproved, RegistryVersion: gates.CoreRegistryVersion,
		PolicyDigest: policy.Digest(),
	}, evidence.Subject{})
	var refusal *failure.Refusal
	if !failure.IsCode(err, "production_plan") || !errors.As(err, &refusal) ||
		!strings.Contains(refusal.Reason, string(gates.ProductionDesignGate)) ||
		!strings.Contains(refusal.Reason, policy.Digest()) || refusal.Next == "" {
		t.Fatalf("plan refusal = %v", err)
	}
}

func TestProductionCompletionRefusesReviewBoundToMovedEvidenceSet(t *testing.T) {
	root, attempt, _ := completeRoot(t, true)
	policy := ProductionPolicy()
	for _, check := range policy.RequiredChecks {
		appendProductionProof(t, root, attempt, check, policy.Digest())
	}
	// The verdict still matches code, artifact, policy, and subject identity;
	// only the evidence set it was read against has moved.
	appendProductionProof(t, root, attempt,
		evidence.RequiredCheck{Class: evidence.ClassBuild, CheckID: "extra"}, policy.Digest())
	err := productionComplete(root, nil)
	var refusal *failure.Refusal
	if !failure.IsCode(err, "production_evidence") || !errors.As(err, &refusal) {
		t.Fatalf("moved evidence set = %v", err)
	}
	if !strings.Contains(refusal.Reason, "review/change-review") ||
		!strings.Contains(refusal.Reason, policy.Digest()) ||
		!strings.Contains(refusal.Reason, "T1") || refusal.Next == "" {
		t.Fatalf("refusal = %#v", refusal)
	}
	// A fresh verdict against the current set completes; nothing else changed.
	appendProductionProof(t, root, attempt,
		evidence.RequiredCheck{Class: evidence.ClassReview, CheckID: "change-review"}, policy.Digest())
	if result, err := productionCompletion(root, nil); err != nil || result.RevisionAfter != 4 {
		t.Fatalf("rebound review completion = %#v, %v", result, err)
	}
}

func productionCompletion(root string, transition *PolicyTransition) (Completion, error) {
	request := completeRequest(3)
	request.Profile = string(ProfileProduction)
	request.PolicyTransition = transition
	return CompleteTask(root, "safe-change", request)
}

func productionComplete(root string, transition *PolicyTransition) error {
	_, err := productionCompletion(root, transition)
	return err
}

func appendProductionProof(t *testing.T, root string, attempt Attempt, check evidence.RequiredCheck, digest string) {
	t.Helper()
	authored := plan.ParseChange(mustOwner(t, root), "safe-change")
	task, found := canonicalTask(authored.Tasks.Tasks, "T1")
	if !found {
		t.Fatal("missing canonical task")
	}
	current := readTaskTransitionState(t, root)
	subject := evidence.ProductionSubject{
		Subject: evidence.Subject{
			Change: "safe-change", TaskID: "T1", AttemptID: attempt.ID,
			HEAD: attempt.BaselineHEAD, TaskHash: taskContractHash(task),
			CommandHash: strings.Repeat("2", 64), ApprovalHash: attempt.ApprovalHash,
			StateRevision: current.Revision,
		},
		Check: check, PolicyDigest: digest,
	}
	evidencePath := filepath.Join(root, ".specd", "evidence.jsonl")
	if !evidence.Runnable(check.Class) {
		// A verdict binds the evidence set it was taken against, exactly as the
		// review command records it.
		items, _, err := record.Replay(evidencePath, record.FamilyEvidence)
		if err != nil {
			t.Fatal(err)
		}
		subject.EvidenceSet, err = evidence.EvidenceSetHash(items, "safe-change")
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Add(-time.Second)
	var observation evidence.Production
	var err error
	if evidence.Runnable(check.Class) {
		observation, err = evidence.NewProduction(subject, verifyexec.Result{
			StartedAt: now.Format(time.RFC3339Nano),
			EndedAt:   now.Add(time.Millisecond).Format(time.RFC3339Nano),
			Passed:    true, NonVacuous: true,
			StdoutDigest: strings.Repeat("a", 64), StderrDigest: strings.Repeat("b", 64),
		})
	} else {
		observation, err = evidence.NewReview(subject, "human@example.com", true, now)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evidence.AppendProduction(
		evidencePath, "agent:validator", observation,
	); err != nil {
		t.Fatal(err)
	}
}
