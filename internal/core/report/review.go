package report

import (
	"bytes"
	"cmp"
	"path/filepath"
	"slices"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	"github.com/0xkhdr/specd-cli/internal/core"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/plan"
	"github.com/0xkhdr/specd-cli/internal/reconcile"
)

// designSections are the two design contracts a reviewer reads against the
// diff. The remaining sections stay in the authored artifact: the packet is a
// review aid, never a second copy of design.md.
var designSections = []string{"Invariants", "Failure behavior"}

// DesignSection is one bounded design contract. Content is sampled and
// redacted by the existing bounded-output owner, so an oversized or
// secret-shaped artifact can only ever produce a short, safe excerpt.
type DesignSection struct {
	Name      string `json:"name"`
	Excerpt   string `json:"excerpt,omitempty"`
	Truncated bool   `json:"truncated"`
	Present   bool   `json:"present"`
}

// TaskDiff is one task's diff identity: the commit its recorded attempt was
// baselined at and the exact file scope that attempt declared. The packet
// carries identity only; no patch text, no source body.
type TaskDiff struct {
	TaskID       string   `json:"task"`
	HEAD         string   `json:"head,omitempty"`
	Files        []string `json:"files"`
	ContractHash string   `json:"contractHash,omitempty"`
	ApprovalHash string   `json:"approvalHash,omitempty"`
}

// DiffIdentity is the change-wide diff identity. HEAD is set only when every
// attempted task agrees on one baseline commit; disagreement is reported as
// ambiguity rather than resolved by picking one.
type DiffIdentity struct {
	HEAD        string     `json:"head,omitempty"`
	Tasks       []TaskDiff `json:"tasks"`
	Unattempted []string   `json:"unattempted"`
	Ambiguous   bool       `json:"ambiguous"`
}

// CapabilityEffect is the expected sync effect on one accepted spec: which
// document changes, from which bytes to which bytes, identified by hash.
type CapabilityEffect struct {
	Capability   string `json:"capability"`
	AcceptedPath string `json:"acceptedPath"`
	BeforeHash   string `json:"beforeHash,omitempty"`
	AfterHash    string `json:"afterHash,omitempty"`
	Created      bool   `json:"created"`
	NoOp         bool   `json:"noOp"`
}

// ReconcileEffect is the expected sync/archive effect projected from the one
// reconciliation owner. Rebuilt documents are deliberately absent.
type ReconcileEffect struct {
	Capabilities []CapabilityEffect `json:"capabilities"`
	Diagnostics  []string           `json:"diagnostics"`
	Applicable   bool               `json:"applicable"`
	NoOp         bool               `json:"noOp"`
}

// ReviewPacket is the one bounded review packet: approved intent and deltas,
// design contracts, task DAG and declared scope, diff identity, proof
// coverage, and the expected reconciliation effect. It resolves nothing and
// authorizes nothing: Approvable means no blocker was found, never that a
// human may be skipped.
type ReviewPacket struct {
	Change        string                  `json:"change"`
	PolicyDigest  string                  `json:"policyDigest"`
	Review        ReviewModel             `json:"review"`
	Design        []DesignSection         `json:"design"`
	Diff          DiffIdentity            `json:"diff"`
	Effect        ReconcileEffect         `json:"effect"`
	Blockers      []core.ReadinessBlocker `json:"blockers"`
	Approvable    bool                    `json:"approvable"`
	Empty         bool                    `json:"empty"`
	Inconsistency *Inconsistency          `json:"inconsistency,omitempty"`
}

// ProjectReviewPacket assembles the packet from the canonical owners alone:
// the review, proof, and readiness projections above, the recorded attempts
// that own diff identity, and the reconciliation plan. It reads accepted specs
// and deltas through the reconciliation owner and writes, transitions, and
// executes nothing.
func ProjectReviewPacket(truth Truth) (ReviewPacket, error) {
	review, err := ProjectReview(truth)
	if err != nil {
		return ReviewPacket{}, err
	}
	attempts, err := truth.attempts()
	if err != nil {
		return ReviewPacket{}, err
	}
	owner, err := corepath.New(truth.Snapshot.Root())
	if err != nil {
		return ReviewPacket{}, err
	}
	packet := ReviewPacket{
		Change: review.Change, PolicyDigest: review.PolicyDigest, Review: review,
		Design: designContracts(truth.Snapshot.Plan().Design),
		Diff:   diffIdentity(truth.Snapshot.Plan().Tasks.Tasks, attempts),
		Effect: reconcileEffect(owner, reconcile.Build(owner, review.Change)),
		Empty:  review.Empty, Inconsistency: review.Inconsistency,
	}
	packet.Blockers = packetBlockers(packet)
	packet.Approvable = len(packet.Blockers) == 0
	return packet, nil
}

// designContracts bounds the two design contracts a reviewer needs. A missing,
// placeholder, or empty section is reported as absent instead of invented.
func designContracts(design plan.Design) []DesignSection {
	contracts := make([]DesignSection, 0, len(designSections))
	for _, name := range designSections {
		contract := DesignSection{Name: name}
		for _, section := range design.Sections {
			if section.Name != name {
				continue
			}
			content := bytes.TrimSpace(section.Content)
			if section.Placeholder || len(content) == 0 {
				break
			}
			bounded := agentjson.Bound(string(content), "", "")
			contract.Excerpt, contract.Truncated = bounded.Excerpt, bounded.Truncated
			contract.Present = true
			break
		}
		contracts = append(contracts, contract)
	}
	return contracts
}

// diffIdentity projects what the diff is bound to: the recorded attempt owns
// the baseline commit and the declared scope, so the packet never re-derives a
// diff of its own.
func diffIdentity(tasks []plan.Task, attempts map[string]record.AttemptPayload) DiffIdentity {
	identity := DiffIdentity{Tasks: []TaskDiff{}, Unattempted: []string{}}
	heads := map[string]bool{}
	for _, task := range tasks {
		attempt, recorded := attempts[task.ID]
		if !recorded {
			identity.Unattempted = append(identity.Unattempted, task.ID)
			continue
		}
		heads[attempt.BaselineHEAD] = true
		identity.Tasks = append(identity.Tasks, TaskDiff{
			TaskID: task.ID, HEAD: attempt.BaselineHEAD,
			Files:        append([]string{}, attempt.DeclaredFiles...),
			ContractHash: attempt.ContractHash, ApprovalHash: attempt.ApprovalHash,
		})
	}
	if len(heads) == 1 {
		identity.HEAD = identity.Tasks[0].HEAD
	}
	identity.Ambiguous = len(heads) > 1
	return identity
}

// reconcileEffect projects the expected accepted-truth effect by hash. The
// rebuilt documents stay with the reconciliation owner and never enter the
// packet.
func reconcileEffect(owner *corepath.Owner, reconciliation reconcile.Plan) ReconcileEffect {
	effect := ReconcileEffect{
		Capabilities: []CapabilityEffect{}, Diagnostics: []string{},
		Applicable: reconciliation.Applicable, NoOp: reconciliation.NoOp,
	}
	for _, capability := range reconciliation.Capabilities {
		effect.Capabilities = append(effect.Capabilities, CapabilityEffect{
			Capability: capability.Capability, AcceptedPath: relative(owner, capability.AcceptedPath),
			BeforeHash: capability.AcceptedHash, AfterHash: capability.OutputHash,
			Created: capability.Created, NoOp: capability.NoOp,
		})
	}
	for _, diagnostic := range reconciliation.Diagnostics {
		effect.Diagnostics = append(effect.Diagnostics, diagnostic.Code)
	}
	return effect
}

// relative keeps absolute host paths out of the packet.
func relative(owner *corepath.Owner, path string) string {
	value, err := filepath.Rel(owner.Root(), path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(value)
}

// packetBlockers names every reason this packet may not present an approvable
// state. Readiness blockers describe the implementation frontier rather than
// review, so they stay visible under Review.Blockers and are not restated
// here: a completed task is not a review blocker, and an incomplete one is
// already named by its missing proof.
func packetBlockers(packet ReviewPacket) []core.ReadinessBlocker {
	change := packet.Change
	blockers := []core.ReadinessBlocker{}
	add := func(code, owner, action string) {
		blockers = append(blockers, core.ReadinessBlocker{Code: code, Owner: owner, Action: action})
	}
	if packet.Inconsistency != nil {
		add("review_disagreement", "author", packet.Inconsistency.Action)
	}
	if packet.Empty {
		add("review_empty", "author", "author the change artifacts and run specd check "+change)
	}
	if !packet.Review.ApprovalCurrent {
		add("review_approval_stale", "human",
			"ask a human to run specd approve "+change+" in a human terminal")
	}
	for _, section := range packet.Design {
		if !section.Present {
			add("review_design_incomplete", "author",
				"author the design "+section.Name+" section and run specd check "+change)
		}
	}
	if packet.Diff.Ambiguous {
		add("review_diff_ambiguous", "author",
			"regenerate task context so every attempt shares one baseline commit")
	}
	for _, task := range packet.Review.Proof.Tasks {
		if task.Missing || len(task.MissingChecks) != 0 {
			add("review_proof_missing", "author", "run specd verify "+change+" "+task.TaskID)
		}
		if len(task.StaleEvidence) != 0 {
			add("review_proof_stale", "author",
				"re-verify "+task.TaskID+" against the current attempt and HEAD")
		}
	}
	for _, requirement := range packet.Review.Proof.Requirements {
		if !requirement.Covered {
			add("review_requirement_uncovered", "author",
				"declare a task for "+requirement.Capability+"/Requirement: "+requirement.Requirement)
		}
	}
	if len(packet.Review.Proof.ExtraEvidence) != 0 {
		add("review_evidence_extra", "author",
			"reconcile the evidence ledger with the canonical task list of "+change)
	}
	if !packet.Effect.Applicable {
		add("review_plan_not_applicable", "author",
			"repair the reported delta or accepted spec and run specd check "+change)
	}
	return distinctBlockers(blockers)
}

// distinctBlockers keeps one blocker per code in canonical order, so the same
// truth always produces the same packet.
func distinctBlockers(blockers []core.ReadinessBlocker) []core.ReadinessBlocker {
	slices.SortStableFunc(blockers, func(a, b core.ReadinessBlocker) int {
		return cmp.Compare(a.Code, b.Code)
	})
	result := []core.ReadinessBlocker{}
	for index, blocker := range blockers {
		if index != 0 && blocker.Code == blockers[index-1].Code {
			continue
		}
		result = append(result, blocker)
	}
	return result
}
