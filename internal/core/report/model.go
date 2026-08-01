// Package report projects the four canonical read-only reports over local
// truth. It owns no truth of its own: state, readiness, approval, history,
// evidence, and policy each keep exactly one existing owner, and every model
// here is a bounded projection of those owners. Nothing in this package writes,
// transitions, executes, or reaches the network.
package report

import (
	"fmt"
	"sort"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

// The four report kinds are the whole surface. A fifth report needs a
// requirement, not a constant.
const (
	KindStatus  = "status"
	KindProof   = "proof"
	KindHistory = "history"
	KindReview  = "review"
)

// Truth is the bounded canonical input every report projects from. Load
// gathers it once; a Truth built by hand is refused because its readiness
// snapshot can only come from the canonical loader.
type Truth struct {
	Snapshot core.ReadinessSnapshot
	History  []record.Record
	Evidence []record.Record
	Policy   core.Policy
}

// Inconsistency is an explicit blocking disagreement between sources. It is
// exposed instead of guessed, and it always suppresses "current" proof.
type Inconsistency struct {
	Code     string `json:"code"`
	Subject  string `json:"subject"`
	Observed string `json:"observed"`
	Action   string `json:"action"`
}

// Event is one ordered history fact. Sequence is the append position and
// Revision is the resulting state revision; together they decide order.
// Timestamp is carried as a fact and never orders anything.
type Event struct {
	Sequence  int    `json:"sequence"`
	From      uint64 `json:"revisionFrom"`
	Revision  uint64 `json:"revision"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Actor     string `json:"actor"`
	Timestamp string `json:"timestamp"`
}

// HistoryModel is the ordered lifecycle, approval, completion, sync, and
// archive record of one change, plus the observations appended beside it.
type HistoryModel struct {
	Change string  `json:"change"`
	Events []Event `json:"events"`
	// Observations are appended history facts that carry no lifecycle
	// transition — today only friction. They are ordered by append position
	// alone and are kept out of Events so they can never take part in revision
	// ordering or weaken its gap and ambiguity checks. Revision reports the
	// revision the observation was recorded against, as a fact.
	Observations  []Event        `json:"observations"`
	Empty         bool           `json:"empty"`
	Inconsistency *Inconsistency `json:"inconsistency,omitempty"`
}

// Artifact reports whether one authored planning artifact exists. A partially
// authored change is an explicit fact, never an error and never an omission.
type Artifact struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

// StatusModel is current lifecycle, full frontier, blockers, policy, and
// assurance.
type StatusModel struct {
	Change          string                  `json:"change"`
	Lifecycle       string                  `json:"lifecycle"`
	Revision        uint64                  `json:"revision"`
	NextAction      string                  `json:"nextAction"`
	Profile         string                  `json:"profile"`
	PolicyDigest    string                  `json:"policyDigest"`
	Assurance       string                  `json:"assurance"`
	ApprovalCurrent bool                    `json:"approvalCurrent"`
	ApprovalReason  string                  `json:"approvalReason,omitempty"`
	Artifacts       []Artifact              `json:"artifacts"`
	Tasks           []core.TaskReadiness    `json:"tasks"`
	Frontier        []string                `json:"frontier"`
	Outcome         core.ReadinessOutcome   `json:"outcome"`
	Blockers        []core.ReadinessBlocker `json:"blockers"`
	Empty           bool                    `json:"empty"`
	Inconsistency   *Inconsistency          `json:"inconsistency,omitempty"`
}

// TaskProof maps one task to its declared verification, its current attempt,
// and the applicability of every evidence record recorded against it.
type TaskProof struct {
	TaskID            string                   `json:"task"`
	Activity          core.TaskActivity        `json:"activity"`
	Requirements      []string                 `json:"requirements"`
	Verify            string                   `json:"verify"`
	VerifyParsable    bool                     `json:"verifyParsable"`
	AttemptID         string                   `json:"attempt,omitempty"`
	ConsumedEvidence  string                   `json:"consumedEvidence,omitempty"`
	Current           bool                     `json:"current"`
	StaleEvidence     []string                 `json:"staleEvidence"`
	MissingChecks     []evidence.RequiredCheck `json:"missingChecks"`
	PriorPolicyDigest string                   `json:"priorPolicyDigest,omitempty"`
	Missing           bool                     `json:"missing"`
}

// RequirementProof is the requirement side of the same trace, so a requirement
// with no task is as visible as a task with no proof.
type RequirementProof struct {
	Capability  string   `json:"capability"`
	Requirement string   `json:"requirement"`
	Tasks       []string `json:"tasks"`
	Covered     bool     `json:"covered"`
}

// ProofModel is task, requirement, and check applicability with stale, missing,
// and extra evidence made explicit.
type ProofModel struct {
	Change        string             `json:"change"`
	PolicyDigest  string             `json:"policyDigest"`
	Tasks         []TaskProof        `json:"tasks"`
	Requirements  []RequirementProof `json:"requirements"`
	ExtraEvidence []string           `json:"extraEvidence"`
	Empty         bool               `json:"empty"`
	Inconsistency *Inconsistency     `json:"inconsistency,omitempty"`
}

// CapabilityReview counts delta operations without carrying authored bodies.
type CapabilityReview struct {
	Capability   string   `json:"capability"`
	Added        int      `json:"added"`
	Modified     int      `json:"modified"`
	Removed      int      `json:"removed"`
	Renamed      int      `json:"renamed"`
	Requirements []string `json:"requirements"`
}

// TaskReview is the declared task contract a reviewer checks the diff against.
type TaskReview struct {
	ID           string            `json:"id"`
	Wave         int               `json:"wave"`
	Activity     core.TaskActivity `json:"activity"`
	DependsOn    []string          `json:"dependsOn"`
	Files        []string          `json:"files"`
	Requirements []string          `json:"requirements"`
	Verify       string            `json:"verify"`
}

// ReviewModel is the bounded review packet input: approved intent and deltas,
// declared scope, proof, and the sync/archive effect. Source bodies, command
// output, and unbounded logs are deliberately absent.
type ReviewModel struct {
	Change          string                  `json:"change"`
	Lifecycle       string                  `json:"lifecycle"`
	Revision        uint64                  `json:"revision"`
	Profile         string                  `json:"profile"`
	PolicyDigest    string                  `json:"policyDigest"`
	Assurance       string                  `json:"assurance"`
	Approver        string                  `json:"approver,omitempty"`
	ApprovalHash    string                  `json:"approvalHash,omitempty"`
	ApprovalCurrent bool                    `json:"approvalCurrent"`
	Artifacts       []core.ApprovalArtifact `json:"artifacts"`
	Capabilities    []CapabilityReview      `json:"capabilities"`
	Tasks           []TaskReview            `json:"tasks"`
	Proof           ProofModel              `json:"proof"`
	Synced          *Event                  `json:"synced,omitempty"`
	Archived        *Event                  `json:"archived,omitempty"`
	Blockers        []core.ReadinessBlocker `json:"blockers"`
	Empty           bool                    `json:"empty"`
	Inconsistency   *Inconsistency          `json:"inconsistency,omitempty"`
}

// Load gathers canonical local truth for one change. It reads through the
// existing owners only: readiness owns state, plan, and approval projection;
// the record ledgers own history and evidence.
func Load(root, change string, policy core.Policy) (Truth, error) {
	if err := policy.Validate(); err != nil {
		return Truth{}, err
	}
	snapshot, err := core.LoadReadinessSnapshot(root, change)
	if err != nil {
		return Truth{}, err
	}
	owner, err := corepath.New(snapshot.Root())
	if err != nil {
		return Truth{}, err
	}
	history, err := replay(owner.History(), record.FamilyHistory)
	if err != nil {
		return Truth{}, err
	}
	observations, err := replay(owner.Evidence(), record.FamilyEvidence)
	if err != nil {
		return Truth{}, err
	}
	return Truth{Snapshot: snapshot, History: history, Evidence: observations, Policy: policy}, nil
}

func replay(path string, family record.Family) ([]record.Record, error) {
	items, diagnostics, err := record.Replay(path, family)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) != 0 {
		return nil, refuse("report_ledger_torn", path,
			fmt.Sprintf("line %d: %s", diagnostics[0].Line, diagnostics[0].Reason),
			"repair the ledger tail and rerun the report")
	}
	return items, nil
}

// ProjectHistory orders every event of one change by resulting revision and
// append sequence. Timestamps are reported and never consulted for order.
func ProjectHistory(truth Truth) (HistoryModel, error) {
	events, err := truth.events()
	if err != nil {
		return HistoryModel{}, err
	}
	disagreement, err := truth.inconsistency(events)
	if err != nil {
		return HistoryModel{}, err
	}
	observations, err := truth.observations()
	if err != nil {
		return HistoryModel{}, err
	}
	return HistoryModel{
		Change: truth.Snapshot.Change(), Events: events, Observations: observations,
		Empty: len(events) == 0 && len(observations) == 0, Inconsistency: disagreement,
	}, nil
}

// ProjectStatus reports current lifecycle, the full frontier, blockers, policy,
// and assurance. An empty or partially authored change is explicit.
func ProjectStatus(truth Truth) (StatusModel, error) {
	events, err := truth.events()
	if err != nil {
		return StatusModel{}, err
	}
	disagreement, err := truth.inconsistency(events)
	if err != nil {
		return StatusModel{}, err
	}
	readiness := truth.Snapshot.Model()
	approval := truth.Snapshot.Approval()
	projection := truth.Snapshot.Projection()
	model := StatusModel{
		Change: truth.Snapshot.Change(), Lifecycle: projection.Stage,
		Revision: projection.Revision, NextAction: projection.NextAction,
		Profile: string(truth.Policy.Profile), PolicyDigest: truth.Policy.Digest(),
		Assurance: core.AttemptAssurance, ApprovalCurrent: approval.Current,
		ApprovalReason: approval.Reason, Artifacts: artifacts(truth.Snapshot.Plan()),
		Tasks: readiness.Tasks(), Frontier: readiness.Frontier(),
		Outcome: readiness.Outcome(), Inconsistency: disagreement,
	}
	model.Blockers = blockers(readiness)
	model.Empty = len(model.Tasks) == 0
	return model, nil
}

// ProjectProof maps requirements, tasks, and required checks to the evidence
// recorded against them. Applicability is decided by the evidence owner alone.
func ProjectProof(truth Truth) (ProofModel, error) {
	events, err := truth.events()
	if err != nil {
		return ProofModel{}, err
	}
	disagreement, err := truth.inconsistency(events)
	if err != nil {
		return ProofModel{}, err
	}
	change := truth.Snapshot.Change()
	authored := truth.Snapshot.Plan()
	digest := truth.Policy.Digest()
	consumed, err := truth.consumedEvidence()
	if err != nil {
		return ProofModel{}, err
	}
	attempts, err := truth.attempts()
	if err != nil {
		return ProofModel{}, err
	}
	activities := map[string]core.TaskActivity{}
	for _, row := range truth.Snapshot.Model().Tasks() {
		activities[row.ID] = row.Activity
	}
	model := ProofModel{
		Change: change, PolicyDigest: digest, Tasks: []TaskProof{},
		Requirements: []RequirementProof{}, ExtraEvidence: []string{},
		Inconsistency: disagreement,
	}
	canonical := map[string]bool{}
	for _, task := range authored.Tasks.Tasks {
		canonical[task.ID] = true
		row, err := truth.taskProof(task, activities[task.ID], attempts[task.ID], consumed, disagreement != nil)
		if err != nil {
			return ProofModel{}, err
		}
		model.Tasks = append(model.Tasks, row)
	}
	for _, trace := range authored.Trace.Requirements {
		model.Requirements = append(model.Requirements, RequirementProof{
			Capability: trace.Capability, Requirement: trace.Requirement,
			Tasks: append([]string{}, trace.Tasks...), Covered: len(trace.Tasks) != 0,
		})
	}
	for _, item := range truth.Evidence {
		if item.Change != change {
			continue
		}
		taskID, err := truth.evidenceTask(item)
		if err != nil {
			return ProofModel{}, err
		}
		if !canonical[taskID] {
			model.ExtraEvidence = append(model.ExtraEvidence, item.ID)
		}
	}
	model.Empty = len(model.Tasks) == 0
	return model, nil
}

// ProjectReview is the bounded review packet input. It carries identities,
// counts, declared scope, and proof; never authored bodies or command output.
func ProjectReview(truth Truth) (ReviewModel, error) {
	events, err := truth.events()
	if err != nil {
		return ReviewModel{}, err
	}
	disagreement, err := truth.inconsistency(events)
	if err != nil {
		return ReviewModel{}, err
	}
	proof, err := ProjectProof(truth)
	if err != nil {
		return ReviewModel{}, err
	}
	authored := truth.Snapshot.Plan()
	readiness := truth.Snapshot.Model()
	approval := truth.Snapshot.Approval()
	projection := truth.Snapshot.Projection()
	activities := map[string]core.TaskActivity{}
	for _, row := range readiness.Tasks() {
		activities[row.ID] = row.Activity
	}
	model := ReviewModel{
		Change: truth.Snapshot.Change(), Lifecycle: projection.Stage,
		Revision: projection.Revision, Profile: string(truth.Policy.Profile),
		PolicyDigest: truth.Policy.Digest(), Assurance: core.AttemptAssurance,
		ApprovalCurrent: approval.Current, Artifacts: []core.ApprovalArtifact{},
		Capabilities: []CapabilityReview{}, Tasks: []TaskReview{},
		Proof: proof, Blockers: blockers(readiness), Inconsistency: disagreement,
	}
	if approval.Approval != nil {
		model.Approver = approval.Approval.Approver
		model.ApprovalHash = approval.Approval.AggregateHash
		model.Artifacts = append(model.Artifacts, approval.Approval.Artifacts...)
	}
	for _, delta := range authored.Deltas {
		model.Capabilities = append(model.Capabilities, capabilityReview(delta))
	}
	for _, task := range authored.Tasks.Tasks {
		wave := 0
		if row, found := readiness.Task(task.ID); found {
			wave = row.Wave
		}
		model.Tasks = append(model.Tasks, TaskReview{
			ID: task.ID, Wave: wave, Activity: activities[task.ID],
			DependsOn:    append([]string{}, task.DependsOn...),
			Files:        append([]string{}, task.Files...),
			Requirements: requirementNames(task.References), Verify: task.Verify,
		})
	}
	for index := range events {
		event := events[index]
		switch record.Kind(event.Kind) {
		case record.KindSynced:
			model.Synced = &event
		case record.KindArchived:
			model.Archived = &event
		}
	}
	model.Empty = len(model.Tasks) == 0 && len(model.Capabilities) == 0
	return model, nil
}

// events orders one change's history by revision and append sequence. Unknown
// record versions, invalid transitions, duplicates, and gaps fail closed.
func (truth Truth) events() ([]Event, error) {
	if !truth.Snapshot.Valid() {
		return nil, refuse("report_source", "", "report truth has no canonical readiness snapshot",
			"load report truth through Load")
	}
	change := truth.Snapshot.Change()
	events := []Event{}
	revisions := map[uint64]bool{}
	ids := map[string]bool{}
	for _, item := range truth.History {
		if item.Change != change {
			continue
		}
		if item.SchemaVersion != record.SchemaVersion {
			return nil, refuse("report_event_unknown", change,
				fmt.Sprintf("record %s declares schema version %d", item.ID, item.SchemaVersion),
				"upgrade specd to a version that understands this history record")
		}
		// A non-transitional observation is projected by observations() and
		// never enters revision ordering; every other kind must transition.
		if item.Kind == record.KindFriction {
			continue
		}
		if item.Family != record.FamilyHistory ||
			item.ExpectedRevision == nil || item.ResultingRevision == nil ||
			*item.ResultingRevision != *item.ExpectedRevision+1 {
			return nil, refuse("report_event_invalid", change,
				fmt.Sprintf("record %s carries no single forward revision transition", item.ID),
				"repair history from a trusted source and rerun the report")
		}
		// Ambiguity is decided before authenticity so a duplicated sequence
		// identity is diagnosed as ambiguity rather than as a bad record.
		if revisions[*item.ResultingRevision] || ids[item.ID] {
			return nil, refuse("report_event_ambiguous", change,
				fmt.Sprintf("revision %d has more than one record", *item.ResultingRevision),
				"repair history from a trusted source and rerun the report")
		}
		if err := item.Validate(); err != nil {
			return nil, refuse("report_event_invalid", change, err.Error(),
				"repair history from a trusted source and rerun the report")
		}
		revisions[*item.ResultingRevision] = true
		ids[item.ID] = true
		events = append(events, Event{
			Sequence: len(events), From: *item.ExpectedRevision, Revision: *item.ResultingRevision,
			Kind: string(item.Kind), ID: item.ID, Actor: item.Actor, Timestamp: item.Timestamp,
		})
	}
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].Revision != events[right].Revision {
			return events[left].Revision < events[right].Revision
		}
		return events[left].Sequence < events[right].Sequence
	})
	for index, event := range events {
		want := uint64(0)
		if index > 0 {
			want = events[index-1].Revision
		}
		if event.From != want {
			return nil, refuse("report_event_gap", change,
				fmt.Sprintf("revision %d does not follow revision %d", event.Revision, want),
				"repair history from a trusted source and rerun the report")
		}
	}
	return events, nil
}

// observations projects the appended non-lifecycle facts of one change in
// append order. A malformed friction payload fails closed exactly like any
// other history record.
func (truth Truth) observations() ([]Event, error) {
	change := truth.Snapshot.Change()
	observations := []Event{}
	for _, item := range truth.History {
		if item.Change != change || item.Kind != record.KindFriction {
			continue
		}
		if err := item.Validate(); err != nil {
			return nil, refuse("report_event_invalid", change, err.Error(),
				"repair history from a trusted source and rerun the report")
		}
		friction, err := record.DecodeFrictionPayload(item.Payload)
		if err != nil {
			return nil, refuse("report_event_invalid", change, err.Error(),
				"repair history from a trusted source and rerun the report")
		}
		observations = append(observations, Event{
			Sequence: len(observations), Revision: friction.ObservedRevision,
			Kind: string(item.Kind), ID: item.ID, Actor: item.Actor, Timestamp: item.Timestamp,
		})
	}
	return observations, nil
}

// inconsistency compares history with the state revision the readiness owner
// observed. One appended record awaiting its state write is a known recoverable
// condition and is exposed; anything else is a refusal.
func (truth Truth) inconsistency(events []Event) (*Inconsistency, error) {
	revision := truth.Snapshot.StateRevision()
	if len(events) == 0 {
		if revision == 0 {
			return nil, nil
		}
		return nil, refuse("report_disagreement", truth.Snapshot.Change(),
			fmt.Sprintf("state revision %d has no history record", revision),
			"repair history from a trusted source and rerun the report")
	}
	last := events[len(events)-1]
	switch {
	case last.Revision == revision:
		return nil, nil
	case last.From == revision:
		return &Inconsistency{
			Code: "history_ahead_of_state", Subject: truth.Snapshot.Change(),
			Observed: fmt.Sprintf("history records revision %d, state holds revision %d",
				last.Revision, revision),
			Action: "run status to recover the pending transaction",
		}, nil
	}
	return nil, refuse("report_disagreement", truth.Snapshot.Change(),
		fmt.Sprintf("history records revision %d, state holds revision %d", last.Revision, revision),
		"repair history from a trusted source and rerun the report")
}

// attempts returns the latest recorded attempt per task. The attempt binds the
// evidence subject, so proof never invents an identity of its own.
func (truth Truth) attempts() (map[string]record.AttemptPayload, error) {
	change := truth.Snapshot.Change()
	result := map[string]record.AttemptPayload{}
	for _, item := range truth.History {
		if item.Change != change || item.Kind != record.KindAttempt {
			continue
		}
		attempt, err := record.DecodeAttemptPayload(item.Payload)
		if err != nil {
			return nil, refuse("report_event_invalid", change, err.Error(),
				"repair history from a trusted source and rerun the report")
		}
		result[attempt.TaskID] = attempt
	}
	return result, nil
}

// consumedEvidence maps every evidence record already consumed by a completion
// to the task that consumed it.
func (truth Truth) consumedEvidence() (map[string]string, error) {
	change := truth.Snapshot.Change()
	result := map[string]string{}
	for _, item := range truth.History {
		if item.Change != change || item.Kind != record.KindCompletion {
			continue
		}
		completion, err := record.DecodeCompletionPayload(item.Payload)
		if err != nil {
			return nil, refuse("report_event_invalid", change, err.Error(),
				"repair history from a trusted source and rerun the report")
		}
		result[completion.EvidenceID] = completion.TaskID
	}
	return result, nil
}

func (truth Truth) taskProof(task plan.Task, activity core.TaskActivity, attempt record.AttemptPayload, consumed map[string]string, blocked bool) (TaskProof, error) {
	row := TaskProof{
		TaskID: task.ID, Activity: activity, Requirements: requirementNames(task.References),
		Verify: task.Verify, AttemptID: attempt.ID, StaleEvidence: []string{},
		MissingChecks: []evidence.RequiredCheck{},
	}
	for id, taskID := range consumed {
		if taskID == task.ID {
			row.ConsumedEvidence = id
		}
	}
	subject, known := truth.subject(task, attempt)
	row.VerifyParsable = known
	for _, item := range truth.Evidence {
		if item.Change != truth.Snapshot.Change() {
			continue
		}
		class, err := evidence.ClassOf(item.Payload)
		if err != nil {
			return TaskProof{}, refuse("report_evidence_unknown", truth.Snapshot.Change(), err.Error(),
				"repair the evidence ledger from a trusted source and rerun the report")
		}
		if class != evidence.ClassTestRun {
			continue
		}
		value, err := evidence.Decode(item.Payload)
		if err != nil {
			return TaskProof{}, refuse("report_evidence_invalid", truth.Snapshot.Change(), err.Error(),
				"repair the evidence ledger from a trusted source and rerun the report")
		}
		if value.TaskID != task.ID {
			continue
		}
		if known && !blocked && value.Applicable(subject) {
			row.Current = true
			continue
		}
		row.StaleEvidence = append(row.StaleEvidence, item.ID)
	}
	if truth.Policy.Production() {
		missing, prior, err := productionProof(truth, subject, known && !blocked)
		if err != nil {
			return TaskProof{}, err
		}
		row.MissingChecks, row.PriorPolicyDigest = missing, prior
	}
	row.Missing = !row.Current && row.ConsumedEvidence == ""
	return row, nil
}

// productionProof delegates every applicability decision to the evidence owner.
func productionProof(truth Truth, subject evidence.Subject, known bool) ([]evidence.RequiredCheck, string, error) {
	required := truth.Policy.RequiredChecks
	if !known {
		return append([]evidence.RequiredCheck{}, required...), "", nil
	}
	missing, prior, err := evidence.MissingProduction(truth.Evidence, required, subject, truth.Policy.Digest())
	if err != nil {
		return nil, "", refuse("report_evidence_invalid", truth.Snapshot.Change(), err.Error(),
			"repair the evidence ledger from a trusted source and rerun the report")
	}
	if missing == nil {
		missing = []evidence.RequiredCheck{}
	}
	return missing, prior, nil
}

// subject rebuilds the exact evidence identity from its owners: the recorded
// attempt, the canonical task contract hash, and the parsed verify command.
func (truth Truth) subject(task plan.Task, attempt record.AttemptPayload) (evidence.Subject, bool) {
	if attempt.ID == "" || attempt.TaskID != task.ID {
		return evidence.Subject{}, false
	}
	command, err := verifyexec.ParseCommand(task.Verify)
	if err != nil {
		return evidence.Subject{}, false
	}
	commandHash, err := verifyexec.CommandDigest(command)
	if err != nil {
		return evidence.Subject{}, false
	}
	return evidence.Subject{
		Change: truth.Snapshot.Change(), TaskID: task.ID, AttemptID: attempt.ID,
		HEAD: attempt.BaselineHEAD, TaskHash: core.TaskContractHash(task),
		CommandHash: commandHash, ApprovalHash: attempt.ApprovalHash,
		StateRevision: truth.Snapshot.StateRevision(),
	}, true
}

func (truth Truth) evidenceTask(item record.Record) (string, error) {
	class, err := evidence.ClassOf(item.Payload)
	if err != nil {
		return "", refuse("report_evidence_unknown", truth.Snapshot.Change(), err.Error(),
			"repair the evidence ledger from a trusted source and rerun the report")
	}
	if class == evidence.ClassTestRun {
		value, err := evidence.Decode(item.Payload)
		if err != nil {
			return "", refuse("report_evidence_invalid", truth.Snapshot.Change(), err.Error(),
				"repair the evidence ledger from a trusted source and rerun the report")
		}
		return value.TaskID, nil
	}
	value, err := evidence.DecodeProduction(item.Payload)
	if err != nil {
		return "", refuse("report_evidence_invalid", truth.Snapshot.Change(), err.Error(),
			"repair the evidence ledger from a trusted source and rerun the report")
	}
	return value.TaskID, nil
}

func capabilityReview(delta plan.CapabilityDelta) CapabilityReview {
	review := CapabilityReview{Capability: delta.Capability, Requirements: []string{}}
	for _, operation := range delta.Operations {
		switch operation.Kind {
		case plan.Added:
			review.Added++
		case plan.Modified:
			review.Modified++
		case plan.Removed:
			review.Removed++
		case plan.Renamed:
			review.Renamed++
		}
		if operation.Requirement != nil {
			review.Requirements = append(review.Requirements, operation.Requirement.Name)
		}
	}
	return review
}

func artifacts(authored plan.Change) []Artifact {
	items := []Artifact{
		{Name: "proposal.md", Present: authored.Proposal.Source.Present},
		{Name: "design.md", Present: authored.Design.Source.Present},
		{Name: "tasks.md", Present: authored.Tasks.Source.Present},
	}
	for _, delta := range authored.Deltas {
		items = append(items, Artifact{
			Name: "specs/" + delta.Capability + "/spec.md", Present: delta.Source.Present,
		})
	}
	return items
}

// blockers lists the readiness outcome blocker first, then every distinct task
// blocker in authored order, so an empty list means nothing blocks.
func blockers(readiness core.ReadinessModel) []core.ReadinessBlocker {
	result := []core.ReadinessBlocker{}
	seen := map[string]bool{}
	if outcome := readiness.Outcome(); outcome.Blocker != nil {
		result = append(result, *outcome.Blocker)
		seen[outcome.Blocker.Code] = true
	}
	for _, row := range readiness.Tasks() {
		if row.Blocker == nil || seen[row.Blocker.Code] {
			continue
		}
		seen[row.Blocker.Code] = true
		result = append(result, *row.Blocker)
	}
	return result
}

func requirementNames(references []plan.RequirementReference) []string {
	names := []string{}
	for _, reference := range references {
		names = append(names, reference.Capability+"/Requirement: "+reference.Requirement)
	}
	return names
}

func refuse(code, subject, reason, next string) error {
	return failure.New(code, "", subject, reason, next)
}
