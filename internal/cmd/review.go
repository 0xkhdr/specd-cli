package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/report"
)

// ReviewerVariable names the second human. Reviewer identity is resolved from a
// trusted source exactly as approval identity is; a claim that disagrees with
// the trusted identity is refused rather than believed.
const ReviewerVariable = "SPECD_REVIEWER"

// The two verdicts. There is no third, and no default: an unknown verdict fails
// closed, and an absent one records nothing at all.
const (
	VerdictApprove = "approve"
	VerdictReject  = "reject"
)

type ReviewOptions struct {
	// Actor is the harness identity recording the verdict. It is provenance,
	// never the verdict itself: the reviewer must be someone else.
	Actor    string
	Reviewer string
	Verdict  string
	Findings string
	Now      time.Time
}

// ReviewResult is the canonical review outcome: the bounded packet identity the
// verdict was taken against, the projected verdict state, and the recorded
// findings excerpt. It carries no packet body, no patch text, and no prose.
type ReviewResult struct {
	Change            string                  `json:"change"`
	TaskID            string                  `json:"task"`
	AttemptID         string                  `json:"attempt"`
	PolicyDigest      string                  `json:"policy_digest"`
	PacketHash        string                  `json:"packet_hash"`
	EvidenceSet       string                  `json:"evidence_set"`
	Approvable        bool                    `json:"approvable"`
	Blockers          []core.ReadinessBlocker `json:"blockers"`
	Verdict           evidence.ReviewVerdict  `json:"verdict"`
	RecordID          string                  `json:"record,omitempty"`
	Findings          string                  `json:"findings,omitempty"`
	FindingsTruncated bool                    `json:"findings_truncated,omitempty"`
}

// The handler is bound beside the operation it runs. Dispatch owns the
// registry-driven mapping; nothing here restates operation semantics.
func init() {
	handlers["review"] = func(_ context.Context, in invocation) (any, error) {
		return Review(in.root, in.arg("change"), in.arg("task"), in.arg("attempt"), ReviewOptions{
			Actor:    in.actor,
			Reviewer: value[string](in, "--reviewer"),
			Verdict:  value[string](in, "--verdict"),
			Findings: value[string](in, "--findings"),
		})
	}
}

// Review projects the bounded review packet and records or reports one separate
// reviewer verdict. An omitted verdict writes nothing: it reports whether the
// current review is approved, rejected, stale, or missing.
func Review(root, change, task, attempt string, options ReviewOptions) (ReviewResult, error) {
	return reviewWithSources(root, change, task, attempt, options, gitEmail(root), os.Getenv(ReviewerVariable))
}

func reviewWithSources(root, change, task, attempt string, options ReviewOptions, gitEmail, environment string) (ReviewResult, error) {
	verdict := strings.TrimSpace(options.Verdict)
	if verdict != "" && verdict != VerdictApprove && verdict != VerdictReject {
		return ReviewResult{}, reviewFailure(root, "review_verdict",
			"verdict "+verdict+" is not approve or reject",
			"record the verdict as approve or reject")
	}
	// Review is a production-only proof class: the default profile requires no
	// review, so a verdict is always bound to the production policy digest.
	policy := core.ProductionPolicy()
	check, declared := reviewCheck(policy)
	if !declared {
		return ReviewResult{}, reviewFailure(root, "review_policy",
			"the current policy declares no review check",
			"select a policy that declares a review check")
	}
	binding, err := bindVerification(root, change, task, attempt)
	if err != nil {
		return ReviewResult{}, err
	}
	truth, err := report.Load(root, change, policy)
	if err != nil {
		return ReviewResult{}, err
	}
	packet, err := report.ProjectReviewPacket(truth)
	if err != nil {
		return ReviewResult{}, err
	}
	implementer, err := implementingActor(truth.History, change, task, attempt)
	if err != nil {
		return ReviewResult{}, reviewFailure(root, "review_subject", err.Error(),
			"regenerate task context and start the task again")
	}
	evidenceSet, err := evidence.EvidenceSetHash(truth.Evidence, change)
	if err != nil {
		return ReviewResult{}, reviewFailure(root, "review_evidence", err.Error(),
			"repair the evidence ledger and rerun review")
	}
	approval := binding.snapshot.Approval()
	bound := evidence.ReviewBinding{
		ProductionSubject: evidence.ProductionSubject{
			Subject: evidence.Subject{
				Change: change, TaskID: task, AttemptID: attempt,
				HEAD: binding.scope.BaselineHEAD, TaskHash: core.TaskContractHash(binding.task),
				ApprovalHash: binding.approval, StateRevision: binding.snapshot.StateRevision(),
			},
			Check: check, PolicyDigest: policy.Digest(),
			EvidenceSet: evidenceSet, PacketHash: packetSubjectHash(packet),
		},
		Approver: approval.Approval.Approver, Implementer: implementer,
	}
	result := ReviewResult{
		Change: change, TaskID: task, AttemptID: attempt, PolicyDigest: policy.Digest(),
		PacketHash: bound.PacketHash, EvidenceSet: evidenceSet,
		Approvable: packet.Approvable, Blockers: packet.Blockers,
	}
	if verdict == "" {
		result.Verdict, err = evidence.CurrentReview(truth.Evidence, bound)
		if err != nil {
			return ReviewResult{}, reviewFailure(root, "review_evidence", err.Error(),
				"record a current verdict with specd review "+change+" "+task+" "+attempt)
		}
		return result, nil
	}
	reviewer, err := resolveReviewer(root, options.Reviewer, gitEmail, environment)
	if err != nil {
		return ReviewResult{}, err
	}
	// Findings are bounded and redacted by the one bounded-output owner before
	// they are reported anywhere.
	findings := agentjson.Bound(options.Findings, "", "")
	result.Findings, result.FindingsTruncated = findings.Excerpt, findings.Truncated
	owner, err := corepath.New(root)
	if err != nil {
		return ReviewResult{}, err
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	value, item, err := evidence.RecordReview(owner.Evidence(), options.Actor, bound,
		reviewer, result.Findings, verdict == VerdictApprove, now)
	if err != nil {
		return ReviewResult{}, reviewFailure(root, "review_verdict", err.Error(),
			"record the verdict of a reviewer who is neither the approver nor the implementer")
	}
	result.RecordID = item.ID
	result.Verdict = evidence.ReviewVerdict{
		State: evidence.ReviewApproved, Reviewer: value.Reviewer,
		RecordID: item.ID, ObservedAt: value.EndedAt, Findings: value.Findings,
	}
	if !value.Passed {
		result.Verdict.State = evidence.ReviewRejected
		result.Verdict.Reason = "the reviewer rejected this subject"
	}
	return result, nil
}

// reviewCheck resolves the one review check the policy declares.
func reviewCheck(policy core.Policy) (evidence.RequiredCheck, bool) {
	for _, required := range policy.RequiredChecks {
		if required.Class == evidence.ClassReview {
			return required, true
		}
	}
	return evidence.RequiredCheck{}, false
}

// resolveReviewer resolves the one reviewer identity this invocation may record.
// The environment names the second human; git identity is the fallback, and a
// claim that disagrees with the trusted identity is refused.
func resolveReviewer(root, claimed, gitEmail, environment string) (string, error) {
	trusted := strings.TrimSpace(environment)
	if trusted == "" {
		trusted = strings.TrimSpace(gitEmail)
	}
	if trusted == "" {
		return "", reviewFailure(root, "review_identity", "trusted reviewer identity is missing",
			"set "+ReviewerVariable+" to the reviewer identity")
	}
	if claimed = strings.TrimSpace(claimed); claimed != "" && claimed != trusted {
		return "", reviewFailure(root, "review_identity",
			"claimed reviewer differs from trusted identity",
			"supply --reviewer matching "+ReviewerVariable)
	}
	return trusted, nil
}

// implementingActor resolves who implemented the reviewed attempt, from the one
// history record that owns the attempt.
func implementingActor(history []record.Record, change, task, attempt string) (string, error) {
	for _, item := range history {
		if item.Kind != record.KindAttempt || item.Change != change {
			continue
		}
		payload, err := record.DecodeAttemptPayload(item.Payload)
		if err != nil {
			return "", err
		}
		if payload.TaskID == task && payload.ID == attempt {
			return payload.Actor, nil
		}
	}
	return "", errors.New("the reviewed attempt has no history record")
}

// packetSubjectHash identifies what the bounded packet is about: the change and
// policy it projects, the design contracts, the diff identity, and the expected
// reconciliation effect. The packet's proof coverage is deliberately excluded —
// evidence is bound separately by the evidence-set hash, and folding it in here
// would make every verdict stale itself the moment it was recorded.
func packetSubjectHash(packet report.ReviewPacket) string {
	encoded, err := json.Marshal(struct {
		Change       string                 `json:"change"`
		PolicyDigest string                 `json:"policyDigest"`
		Design       []report.DesignSection `json:"design"`
		Diff         report.DiffIdentity    `json:"diff"`
		Effect       report.ReconcileEffect `json:"effect"`
	}{
		Change: packet.Change, PolicyDigest: packet.PolicyDigest,
		Design: packet.Design, Diff: packet.Diff, Effect: packet.Effect,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func reviewFailure(root, code, reason, next string) error {
	return failure.New(code, root, "", reason, next)
}
