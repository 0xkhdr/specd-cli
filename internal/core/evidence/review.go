package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/record"
)

// ReviewState is the deterministic status of the review class for one binding.
// It stays separate from human approval and from runnable proof: an approved
// review authorizes nothing on its own and satisfies no other class.
type ReviewState string

const (
	ReviewApproved ReviewState = "approved"
	ReviewRejected ReviewState = "rejected"
	ReviewStale    ReviewState = "stale"
	ReviewMissing  ReviewState = "missing"
)

// ReviewBinding is the exact identity one verdict is bound to: the production
// subject that already owns change, task, attempt, current HEAD, artifact
// hashes, state revision, check, and policy digest, plus the two actors a
// reviewer may never be. Separateness is data, not prose, so a shared or
// unknown identity fails closed instead of counting as review.
type ReviewBinding struct {
	ProductionSubject
	Approver    string
	Implementer string
}

// ReviewVerdict is the projected review outcome for one binding. It carries
// facts only; the recovery action belongs to the caller that refuses.
type ReviewVerdict struct {
	State       ReviewState `json:"state"`
	Reviewer    string      `json:"reviewer,omitempty"`
	Findings    string      `json:"findings,omitempty"`
	RecordID    string      `json:"record,omitempty"`
	ObservedAt  string      `json:"observed_at,omitempty"`
	PriorDigest string      `json:"prior_policy_digest,omitempty"`
	Reason      string      `json:"reason,omitempty"`
}

// checkReviewer refuses every identity that cannot be a separate reviewer of
// this binding: unknown, the human who approved the artifacts, or the actor
// that implemented the task.
func (binding ReviewBinding) checkReviewer(reviewer string) error {
	reviewer = strings.TrimSpace(reviewer)
	switch {
	case reviewer == "":
		return errors.New("reviewer identity is unknown")
	case strings.TrimSpace(binding.Approver) == "" || strings.TrimSpace(binding.Implementer) == "":
		return errors.New("the approver and implementing actor must be resolvable to review")
	case sameActor(reviewer, binding.Approver):
		return errors.New("the approver of a change cannot review it")
	case sameActor(reviewer, binding.Implementer):
		return errors.New("the implementing actor cannot review its own task")
	}
	return nil
}

func sameActor(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

// EvidenceSetHash identifies the exact runnable evidence a reviewer read for
// one change. Review records are excluded on purpose: a verdict would otherwise
// change the set it binds and stale itself the moment it was written. Unknown
// or malformed records fail closed rather than silently shrinking the set.
func EvidenceSetHash(items []record.Record, change string) (string, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Change != change {
			continue
		}
		class, err := ClassOf(item.Payload)
		if err != nil {
			return "", err
		}
		if class == ClassReview {
			continue
		}
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	digest := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(digest[:]), nil
}

// RecordReview appends one reviewer verdict. It is the only review writer: it
// reuses the single review payload and appender, checks identity separation
// before anything is persisted, requires the full binding, and refuses a reject
// that names no finding. Findings are redacted and sampled by the caller's
// bounded-output owner; the record keeps that bounded text durably.
func RecordReview(path, actor string, binding ReviewBinding, reviewer, findings string, approved bool, observed time.Time) (Production, record.Record, error) {
	if binding.Check.Class != ClassReview {
		return Production{}, record.Record{}, errors.New("a verdict binds the review class only")
	}
	if binding.EvidenceSet == "" || binding.PacketHash == "" {
		return Production{}, record.Record{},
			errors.New("a verdict binds the evidence set and packet it was taken against")
	}
	if err := binding.checkReviewer(reviewer); err != nil {
		return Production{}, record.Record{}, err
	}
	if !approved && strings.TrimSpace(findings) == "" {
		return Production{}, record.Record{}, errors.New("a reject verdict requires actionable findings")
	}
	value, err := NewReview(binding.ProductionSubject, reviewer, approved, observed)
	if err != nil {
		return Production{}, record.Record{}, err
	}
	value.Findings = findings
	if err := value.Validate(); err != nil {
		return Production{}, record.Record{}, err
	}
	item, err := AppendProduction(path, actor, value)
	if err != nil {
		return Production{}, record.Record{}, err
	}
	return value, item, nil
}

// CurrentReview projects the review state of one binding from the evidence
// ledger. A verdict is current only when every stored binding still matches:
// code, artifact, evidence-set, and policy drift each break it on their own.
// Malformed, future, cross-class, and unknown-actor records fail closed and
// never count as review.
func CurrentReview(items []record.Record, binding ReviewBinding) (ReviewVerdict, error) {
	if binding.Check.Class != ClassReview {
		return ReviewVerdict{}, errors.New("a verdict binds the review class only")
	}
	now := time.Now().UTC()
	verdict := ReviewVerdict{State: ReviewMissing}
	found, drifted := false, false
	for _, item := range items {
		if item.Change != binding.Change {
			continue
		}
		class, err := ClassOf(item.Payload)
		if err != nil {
			return ReviewVerdict{}, err
		}
		if class != ClassReview {
			continue
		}
		value, err := DecodeProduction(item.Payload)
		if err != nil || item.Change != value.Change || item.Timestamp != value.EndedAt {
			return ReviewVerdict{}, errors.New("review evidence record is malformed")
		}
		ended, err := utc(value.EndedAt)
		if err != nil || ended.After(now) {
			return ReviewVerdict{}, errors.New("review evidence is dated in the future")
		}
		if !value.Matches(binding.ProductionSubject) {
			if priorVerdict(value, binding) {
				verdict.PriorDigest, verdict.Reason = value.PolicyDigest, "the bound identity changed"
				found, drifted = false, true
			}
			continue
		}
		if err := binding.checkReviewer(value.Reviewer); err != nil {
			return ReviewVerdict{}, err
		}
		verdict = ReviewVerdict{
			State: ReviewApproved, Reviewer: value.Reviewer,
			RecordID: item.ID, ObservedAt: value.EndedAt, Findings: value.Findings,
		}
		if !value.Passed {
			verdict.State, verdict.Reason = ReviewRejected, "the reviewer rejected this subject"
		}
		found, drifted = true, false
	}
	switch {
	case found && drifted:
		verdict.State, verdict.Reason = ReviewStale, "evidence was recorded after the verdict"
	case !found && drifted:
		verdict.State = ReviewStale
	}
	return verdict, nil
}

// priorVerdict reports whether a non-matching record is an earlier verdict on
// this same task and check, which is drift rather than an unrelated subject.
func priorVerdict(value Production, binding ReviewBinding) bool {
	return value.TaskID == binding.TaskID && value.CheckID == binding.Check.CheckID
}
