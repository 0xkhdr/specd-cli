package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	contextmodel "github.com/0xkhdr/specd-cli/internal/context"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// Outcome is one dispatched invocation as the renderer sees it: the canonical
// result or its error, plus the selectors resolved before the handler ran. The
// renderer projects it; it decides nothing about lifecycle, evidence, or scope.
type Outcome struct {
	Operation string
	Root      string
	Change    string
	Task      string
	Value     any
	Err       error
	Exit      int
}

// RenderJSON is the one machine surface: one document and its exit code.
func RenderJSON(outcome Outcome) ([]byte, int, error) {
	envelope, err := Envelope(outcome)
	if err != nil {
		return nil, 2, err
	}
	encoded, err := agentjson.Encode(envelope)
	if err != nil {
		return nil, 2, err
	}
	return encoded, envelope.Exit.Code, nil
}

// RenderText is the one human surface. It renders the same envelope, so the
// terminal cannot disagree with JSON about facts, diagnostics, exit, or the
// legal next action.
func RenderText(envelope agentjson.Envelope) string {
	var out strings.Builder
	fmt.Fprintf(&out, "operation: %s\nok: %t\n", envelope.Operation, envelope.OK)
	if envelope.Root != nil {
		fmt.Fprintf(&out, "root: %s\n", envelope.Root.Path)
	}
	if envelope.Subject != nil {
		fmt.Fprintf(&out, "change: %s\n", envelope.Subject.Change)
		if envelope.Subject.Task != "" {
			fmt.Fprintf(&out, "task: %s\n", envelope.Subject.Task)
		}
	}
	if envelope.State != nil {
		fmt.Fprintf(&out, "revision: %d\n", envelope.State.Revision)
	}
	keys := make([]string, 0, len(envelope.Data))
	for key := range envelope.Data {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		fmt.Fprintf(&out, "%s: %s\n", key, text(envelope.Data[key]))
	}
	for _, diagnostic := range envelope.Diagnostics {
		location := diagnostic.Path
		if diagnostic.Line > 0 {
			location += fmt.Sprintf(":%d", diagnostic.Line)
		}
		if location != "" {
			location = " " + location
		}
		fmt.Fprintf(&out, "%s %s:%s %s; fix: %s\n",
			diagnostic.Severity, diagnostic.Code, location, diagnostic.Message, diagnostic.Fix)
	}
	fmt.Fprintf(&out, "next: %s", envelope.Next.Kind)
	if envelope.Next.Operation != "" {
		fmt.Fprintf(&out, " %s", envelope.Next.Operation)
	}
	if envelope.Next.Owner != "" {
		fmt.Fprintf(&out, " owner=%s", envelope.Next.Owner)
	}
	if envelope.Next.Reason != "" {
		fmt.Fprintf(&out, " reason=%s", envelope.Next.Reason)
	}
	fmt.Fprintf(&out, "; %s\nexit: %d %s\n",
		envelope.Next.Instruction, envelope.Exit.Code, envelope.Exit.Class)
	return out.String()
}

func text(value any) string {
	if list, ok := value.([]string); ok {
		return strings.Join(list, ",")
	}
	return fmt.Sprint(value)
}

// Envelope projects one canonical result or refusal into the agent document.
func Envelope(outcome Outcome) (agentjson.Envelope, error) {
	envelope := agentjson.Envelope{Schema: agentjson.Schema, Operation: outcome.Operation}
	view := view{root: outcome.Root, change: outcome.Change, task: outcome.Task}
	exit := outcome.Exit
	if outcome.Err != nil {
		exit = ExitCode(outcome.Err)
		projectError(&view, outcome.Err)
	} else {
		exit = projectValue(&view, outcome.Value, exit)
	}
	class, err := agentjson.ExitFor(exit)
	if err != nil {
		return agentjson.Envelope{}, err
	}
	envelope.Exit, envelope.OK = class, exit == 0
	if view.root != "" {
		envelope.Root = &agentjson.Root{Path: view.root}
	}
	if view.change != "" {
		envelope.Subject = &agentjson.Subject{Change: view.change, Task: view.task}
		if view.revision != 0 {
			envelope.State = &agentjson.State{Revision: view.revision}
		}
	}
	envelope.Data, envelope.Diagnostics, envelope.Next = view.data, view.diagnostics, view.next
	return envelope, agentjson.Validate(envelope)
}

// view accumulates the projected facts of one result.
type view struct {
	root, change, task string
	revision           uint64
	data               map[string]any
	diagnostics        []agentjson.Diagnostic
	next               agentjson.Next
}

func (v *view) set(key string, value any) {
	if v.data == nil {
		v.data = map[string]any{}
	}
	v.data[key] = value
}

func (v *view) selectors(root, change, task string, revision uint64) {
	if root != "" {
		v.root = root
	}
	if change != "" {
		v.change = change
	}
	if task != "" {
		v.task = task
	}
	if revision != 0 {
		v.revision = revision
	}
}

func operationNext(operation, instruction string, arguments map[string]string) agentjson.Next {
	return agentjson.Next{
		Kind: agentjson.NextOperation, Operation: operation,
		Arguments: arguments, Instruction: instruction,
	}
}

func blockedNext(owner, instruction string) agentjson.Next {
	return agentjson.Next{Kind: agentjson.NextBlocked, Owner: owner, Instruction: instruction}
}

func handoffNext(reason, instruction string) agentjson.Next {
	return agentjson.Next{
		Kind: agentjson.NextHumanHandoff, Owner: "human",
		Reason: reason, Instruction: instruction,
	}
}

// projectValue fills the view from one canonical result and returns the exit
// code the result implies. Gate findings and a failed verification are reported
// outcomes, so they carry the validation/verification failure class.
func projectValue(v *view, value any, exit int) int {
	switch result := value.(type) {
	case InitResult:
		v.selectors(result.Root.Path, "", "", 0)
		v.set("guidance", result.Guidance)
		v.next = blockedNext("author", "run specd new <change> to plan the first change")
	case NewResult:
		v.selectors(result.Root.Path, result.Change, "", result.Revision)
		v.set("stage", result.Stage)
		v.set("condition", result.Condition)
		v.next = operationNext("check", "run specd check "+result.Change,
			map[string]string{"change": result.Change})
	case core.CheckResult:
		return projectCheck(v, result)
	case core.ApprovalRecord:
		v.selectors("", result.Change, "", result.RevisionAfter)
		v.set("approver", result.Approver)
		v.set("gate", result.Gate)
		v.set("lifecycle_from", string(result.LifecycleFrom))
		v.set("lifecycle_to", string(result.LifecycleTo))
		v.set("aggregate_hash", result.AggregateHash)
		v.next = operationNext("next", "run specd next "+result.Change,
			map[string]string{"change": result.Change})
	case core.ReopenResult:
		v.selectors("", result.Change, "", result.RevisionAfter)
		v.set("actor", result.Actor)
		v.set("lifecycle_from", result.LifecycleFrom)
		v.set("lifecycle_to", result.LifecycleTo)
		v.set("approval_id", result.ApprovalID)
		if result.AttemptID != "" {
			v.set("attempt", result.AttemptID)
		}
		v.set("observed_head", result.ObservedHEAD)
		v.set("revision_before", result.RevisionBefore)
		v.set("history_id", result.HistoryID)
		v.next = operationNext("check", "repair the planning artifacts and run specd check "+result.Change,
			map[string]string{"change": result.Change})
	case StatusResult:
		projectStatus(v, result)
	case NextResult:
		projectNext(v, result)
	case contextmodel.Manifest:
		projectContext(v, result)
	case StartResult:
		v.selectors("", result.Change, result.TaskID, result.RevisionAfter)
		v.set("attempt", result.AttemptID)
		v.set("baseline_head", result.BaselineHEAD)
		v.set("assurance", result.Assurance)
		boundedList(v, "declared_files", result.DeclaredFiles)
		v.next = operationNext("verify",
			fmt.Sprintf("run specd verify %s %s %s", result.Change, result.TaskID, result.AttemptID),
			map[string]string{
				"change": result.Change, "task": result.TaskID, "attempt": result.AttemptID,
			})
	case VerifyResult:
		return projectVerify(v, result)
	case core.Completion:
		v.selectors("", result.Change, result.TaskID, result.RevisionAfter)
		v.set("attempt", result.AttemptID)
		v.set("evidence", result.EvidenceID)
		v.set("revision_before", result.RevisionBefore)
		v.set("history_id", result.HistoryID)
		v.next = operationNext("next", "run specd next "+result.Change,
			map[string]string{"change": result.Change})
	case core.SyncResult:
		v.selectors("", result.Change, "", result.RevisionAfter)
		v.set("approver", result.Approver)
		v.set("plan_hash", result.PlanHash)
		v.set("evidence_set", result.EvidenceSet)
		v.set("capability_count", len(result.Capabilities))
		v.set("history_id", result.HistoryID)
		v.set("archive_target", result.ArchiveTarget)
		v.set("no_op", result.NoOp)
		boundedList(v, "capabilities", capabilityNames(result.Capabilities))
		v.next = operationNext("archive", "run specd archive "+result.Change,
			map[string]string{"change": result.Change})
	case core.ArchiveResult:
		v.selectors("", result.Change, "", result.RevisionAfter)
		v.set("source", result.Source)
		v.set("target", result.Target)
		v.set("change_hash", result.ChangeHash)
		v.set("evidence_set", result.EvidenceSet)
		v.set("approver", result.Approver)
		v.set("history_id", result.HistoryID)
		boundedList(v, "accepted", result.Accepted)
		// Archive is the end of the local loop. It claims nothing beyond it:
		// no delivery, no deployment, no remote state.
		v.next = agentjson.Next{
			Kind:        agentjson.NextTerminal,
			Instruction: "the change is archived at " + result.Target + "; no further action is required",
		}
	case ReportResult:
		v.selectors(result.Root.Path, result.Change, "", 0)
		v.set("kind", result.Kind)
		for _, fact := range result.Facts {
			v.set(fact.Field, fact.Value)
		}
		// A report observes; it never advances the loop.
		v.next = operationNext("status", "run specd status "+result.Change,
			map[string]string{"change": result.Change})
	case ReviewResult:
		v.selectors("", result.Change, result.TaskID, 0)
		v.set("attempt", result.AttemptID)
		v.set("policy_digest", result.PolicyDigest)
		v.set("packet_hash", result.PacketHash)
		v.set("evidence_set", result.EvidenceSet)
		v.set("approvable", result.Approvable)
		v.set("state", string(result.Verdict.State))
		if result.Verdict.Reviewer != "" {
			v.set("reviewer", result.Verdict.Reviewer)
		}
		if result.RecordID != "" {
			v.set("record", result.RecordID)
		}
		if result.FindingsTruncated {
			v.set("findings_truncated", true)
		}
		boundedList(v, "blockers", blockerCodes(result.Blockers))
		switch {
		case len(result.Blockers) > 0:
			v.next = blockedNext(result.Blockers[0].Owner, result.Blockers[0].Action)
		case result.Approvable:
			v.next = operationNext("sync", "run specd sync "+result.Change,
				map[string]string{"change": result.Change})
		default:
			v.next = handoffNext("review verdict is human-only",
				"ask a separate human reviewer to record a verdict for "+result.Change)
		}
	case FrictionResult:
		v.selectors("", result.Change, result.TaskID, result.Revision)
		v.set("domain", result.Domain)
		v.set("blocked_operation", result.Operation)
		v.set("blocker", result.Blocker)
		v.set("state_hash", result.StateHash)
		v.set("evidence_set", result.EvidenceSet)
		v.set("record_count", eligibilityFor(result).Records)
		v.set("eligible", eligibilityFor(result).Eligible)
		// Recording friction unblocks nothing. Eligibility is a fact the root
		// owner may act on; the observer's next action is still the blocker.
		v.next = blockedNext("root owner",
			"the "+result.Domain+" block stands; run specd status "+result.Change+
				" for the current legal action")
	default:
		v.next = blockedNext("author", "run specd status <change> to observe canonical state")
	}
	return exit
}

// eligibilityFor resolves the recorded domain's own D14 row. A domain always
// has one after a successful record, so a zero row means the projection and the
// recorder disagreed and nothing is claimed.
func eligibilityFor(result FrictionResult) core.FrictionEligibility {
	for _, row := range result.Eligibility {
		if row.Domain == result.Domain {
			return row
		}
	}
	return core.FrictionEligibility{Domain: result.Domain}
}

func blockerCodes(blockers []core.ReadinessBlocker) []string {
	codes := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		codes = append(codes, blocker.Code)
	}
	return codes
}

func capabilityNames(capabilities []core.SyncCapability) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, capability.Capability)
	}
	return names
}

func projectCheck(v *view, result core.CheckResult) int {
	v.selectors(result.Root, result.Change, "", result.StateRevision)
	v.set("registry_version", result.RegistryVersion)
	v.set("policy_digest", result.PolicyDigest)
	v.set("finding_count", len(result.Findings))
	for _, finding := range result.Findings {
		v.diagnostics = append(v.diagnostics, agentjson.Diagnostic{
			Code: string(finding.Gate), Severity: string(finding.Severity),
			Message: finding.Problem, Path: finding.Location.Path,
			Line: finding.Location.Line, Fix: finding.Repair,
		})
	}
	if !result.Success {
		v.next = blockedNext("author",
			"repair the reported findings and run specd check "+result.Change)
		return 1
	}
	// A valid plan is not an approved plan: the agent is handed the boundary,
	// never the verb.
	v.next = handoffNext("approval is human-only",
		fmt.Sprintf("ask a human to run specd approve %s in a human terminal", result.Change))
	return 0
}

func projectStatus(v *view, result StatusResult) {
	v.selectors(result.Root.Path, result.Change, "", result.Revision)
	v.set("stage", result.Stage)
	v.set("condition", result.Condition)
	v.set("approval_current", result.ApprovalStatus.Current)
	v.set("pending", result.Counts.Pending)
	v.set("in_progress", result.Counts.InProgress)
	v.set("completed", result.Counts.Completed)
	v.set("failed", result.Counts.Failed)
	v.set("blocked", result.Counts.Blocked)
	v.set("all_tasks_complete", result.AllTasksComplete)
	boundedList(v, "frontier", result.Frontier)
	switch result.Next.Kind {
	case "operation":
		v.next = operationNext(result.Next.Operation, result.Next.Action,
			map[string]string{"change": result.Change})
	case "terminal":
		v.next = agentjson.Next{Kind: agentjson.NextTerminal, Instruction: result.Next.Action}
	case "human_handoff":
		v.next = handoffNext("approval is human-only", result.Next.Action)
	default:
		v.next = blockedNext(result.Next.Owner, result.Next.Action)
	}
}

func projectNext(v *view, result NextResult) {
	v.selectors(result.Root.Path, result.Change, "", result.Revision)
	v.set("classification", result.Classification)
	boundedList(v, "frontier", result.Frontier)
	if result.Selected != nil {
		v.task = result.Selected.ID
		v.set("wave", result.Selected.Wave)
		v.set("activity", string(result.Selected.Activity))
	}
	if result.Blocker != nil {
		v.set("blocker", result.Blocker.Code)
	}
	switch result.Classification {
	case "selected":
		v.next = operationNext("context",
			fmt.Sprintf("run specd context %s %s", result.Change, result.Selected.ID),
			map[string]string{"change": result.Change, "task": result.Selected.ID})
	case "frontier":
		v.next = operationNext("context",
			fmt.Sprintf("run specd next %s <task> to select one, then specd context %s <task>",
				result.Change, result.Change),
			map[string]string{"change": result.Change, "task": result.Frontier[0]})
	case "all_complete":
		v.next = agentjson.Next{Kind: agentjson.NextTerminal, Instruction: result.Action}
	case "waiting_approval":
		v.next = handoffNext("approval is human-only", result.Action)
	default:
		owner := "author"
		if result.Blocker != nil {
			owner = result.Blocker.Owner
		}
		v.next = blockedNext(owner, result.Action)
	}
}

// projectContext keeps bounded facts only. Manifest item content is source
// text and never enters the envelope.
func projectContext(v *view, result contextmodel.Manifest) {
	v.selectors(result.Root, result.Change, result.Task, result.StateRevision)
	v.set("role", result.Role)
	v.set("item_count", len(result.Items))
	v.set("omission_count", len(result.Omissions))
	v.set("required_bytes", result.RequiredBytes)
	v.set("optional_bytes", result.OptionalBytes)
	v.set("manifest_hash", result.ManifestHash)
	v.set("frontier_hash", result.FrontierHash)
	v.set("approval_hash", result.ApprovalHash)
	v.set("operation_class", result.Authority.OperationClass)
	v.set("assurance", result.Authority.Assurance)
	boundedList(v, "allowed_write_paths", result.Authority.AllowedWritePaths)
	v.next = operationNext("start",
		fmt.Sprintf("run specd start %s %s --revision %d",
			result.Change, result.Task, result.StateRevision),
		map[string]string{"change": result.Change, "task": result.Task})
}

func projectVerify(v *view, result VerifyResult) int {
	observation := result.Evidence
	v.selectors("", observation.Change, observation.TaskID, observation.StateRevision)
	v.set("attempt", observation.AttemptID)
	v.set("head", observation.HEAD)
	v.set("exit_code", observation.ExitCode)
	v.set("passed", observation.Passed)
	v.set("non_vacuous", observation.NonVacuous)
	v.set("timed_out", observation.TimedOut)
	v.set("interrupted", observation.Interrupted)
	v.set("zero_match", observation.ZeroMatch)
	v.set("record_id", result.RecordID)
	stdout := agentjson.Bounded{
		Truncated: observation.StdoutCut, Digest: observation.StdoutDigest, Evidence: result.RecordID,
	}
	stderr := agentjson.Bounded{
		Truncated: observation.StderrCut, Digest: observation.StderrDigest, Evidence: result.RecordID,
	}
	if err := mergeBounded(v, "stdout", stdout); err != nil {
		return 2
	}
	if err := mergeBounded(v, "stderr", stderr); err != nil {
		return 2
	}
	if observation.Passed {
		// Verification never completes: the next action is the explicit
		// harness transition, not a claim that the task is done.
		v.next = operationNext("complete",
			fmt.Sprintf("run specd complete %s %s --revision %d",
				observation.Change, observation.TaskID, observation.StateRevision),
			map[string]string{"change": observation.Change, "task": observation.TaskID})
		return 0
	}
	v.diagnostics = append(v.diagnostics, agentjson.Diagnostic{
		Code: "verification_failed", Severity: "error",
		Message: fmt.Sprintf("verification exited %d for task %s", observation.ExitCode, observation.TaskID),
		Fix: fmt.Sprintf("repair the declared files and run specd verify %s %s %s",
			observation.Change, observation.TaskID, observation.AttemptID),
	})
	v.next = blockedNext("author",
		fmt.Sprintf("repair the declared files and run specd verify %s %s %s",
			observation.Change, observation.TaskID, observation.AttemptID))
	return 1
}

func mergeBounded(v *view, stream string, bounded agentjson.Bounded) error {
	fields, err := bounded.Fields(stream)
	if err != nil {
		v.diagnostics = append(v.diagnostics, agentjson.Diagnostic{
			Code: "output_unreferenced", Severity: "error", Message: err.Error(),
			Fix: "record the evidence digest and reference before encoding output",
		})
		v.next = blockedNext("author", "re-run verification so evidence is recorded")
		return err
	}
	for key, value := range fields {
		v.set(key, value)
	}
	return nil
}

// boundedList keeps a projected list inside the envelope bound and says so
// rather than silently dropping members.
func boundedList(v *view, key string, values []string) {
	// An empty list is an unavailable value: omitted, never encoded as null.
	if len(values) == 0 {
		return
	}
	if len(values) <= agentjson.MaxListItems {
		v.set(key, values)
		return
	}
	v.set(key, values[:agentjson.MaxListItems])
	v.set(key+"_truncated", true)
}

// projectError renders any refusal or failure as one diagnostic with exactly
// one recovery. The human approval boundary stays a handoff, never a callable.
func projectError(v *view, err error) {
	var manifest *contextmodel.ManifestError
	if errors.As(err, &manifest) {
		v.diagnostics = append(v.diagnostics, agentjson.Diagnostic{
			Code: manifest.Code, Severity: "error", Message: manifest.Reason, Fix: manifest.Next,
		})
		v.next = blockedNext(manifest.Owner, manifest.Next)
		return
	}
	refusal := asRefusal(err)
	v.selectors(refusal.Root, "", "", 0)
	v.diagnostics = append(v.diagnostics, agentjson.Diagnostic{
		Code: refusal.Code, Severity: "error", Message: refusal.Reason,
		Path: refusal.Path, Fix: refusal.Next,
	})
	if humanBoundaryCode(refusal.Code) {
		v.next = handoffNext("approval is human-only", refusal.Next)
		return
	}
	owner := "author"
	var blocked *NextRefusal
	if errors.As(err, &blocked) && blocked.Owner != "" {
		owner = blocked.Owner
		v.task = blocked.TaskID
	}
	v.next = blockedNext(owner, refusal.Next)
}

func humanBoundaryCode(code string) bool {
	switch code {
	case "human_approval_required", "human_operation_required",
		"approval_stale", "change_not_approved":
		return true
	}
	return false
}

// asRefusal keeps one refusal model: an error that already names its next
// action keeps it, so no envelope shows two next actions.
func asRefusal(err error) *failure.Refusal {
	var refusal *failure.Refusal
	if errors.As(err, &refusal) {
		return refusal
	}
	reason, next := err.Error(), "run specd status <change> to observe canonical state"
	if head, tail, found := strings.Cut(reason, "; next: "); found {
		reason, next = head, tail
	}
	return failure.New("operation_failed", "", "", reason, next)
}
