package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/record"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
)

// Stage 8 adds three proof classes beside the stage-5 test-run class. Every
// class stays distinct: no record of one class can satisfy a requirement for
// another, and review never executes anything.
const (
	ClassBuild  = "build"
	ClassLint   = "lint"
	ClassReview = "review"
)

const maximumCheckID = 64
const maximumReviewer = 320

// maximumFindings bounds the reviewer's findings inside the record. Callers
// redact and sample through the one bounded-output owner before recording;
// this is the durable ceiling, not the redactor.
const maximumFindings = 2048

var checkIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// RequiredCheck is one exact proof declaration. It is the single check identity
// shared by policy, production gates, verification, and completion.
type RequiredCheck struct {
	Class   string `json:"class"`
	CheckID string `json:"check"`
}

func (check RequiredCheck) String() string { return check.Class + "/" + check.CheckID }

func (check RequiredCheck) Valid() bool {
	return knownClass(check.Class) && len(check.CheckID) <= maximumCheckID &&
		checkIDPattern.MatchString(check.CheckID)
}

func knownClass(class string) bool {
	switch class {
	case ClassTestRun, ClassBuild, ClassLint, ClassReview:
		return true
	}
	return false
}

// Runnable reports whether a class is proven by executing a command through the
// stage-5 runner. Review is authored by a reviewer and is never runnable.
func Runnable(class string) bool {
	return class == ClassTestRun || class == ClassBuild || class == ClassLint
}

// Production is the stage-8 evidence payload for the build, lint, and review
// classes. The test-run class keeps the unchanged stage-5 TestRun payload, so a
// default-profile ledger is byte-identical to what stage 5 wrote.
type Production struct {
	SchemaVersion int    `json:"schema_version"`
	Class         string `json:"class"`
	CheckID       string `json:"check"`
	PolicyDigest  string `json:"policy_digest"`
	Change        string `json:"change"`
	TaskID        string `json:"task"`
	AttemptID     string `json:"attempt"`
	HEAD          string `json:"head"`
	TaskHash      string `json:"task_hash"`
	CommandHash   string `json:"command_hash"`
	Reviewer      string `json:"reviewer"`
	// Findings, EvidenceSet, and PacketHash belong to the review class alone.
	// They are the reviewer's actionable findings and the two bindings a
	// runnable observation has no use for: the evidence set the reviewer read
	// and the bounded packet they read it in. A runnable record never carries
	// them, and an absent field is never treated as a match.
	Findings      string `json:"findings,omitempty"`
	EvidenceSet   string `json:"evidence_set,omitempty"`
	PacketHash    string `json:"packet_hash,omitempty"`
	ApprovalHash  string `json:"approval_hash"`
	StateRevision uint64 `json:"state_revision"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at"`
	ExitCode      int    `json:"exit_code"`
	Passed        bool   `json:"passed"`
	TimedOut      bool   `json:"timed_out"`
	Interrupted   bool   `json:"interrupted"`
	StdoutDigest  string `json:"stdout_digest"`
	StderrDigest  string `json:"stderr_digest"`
	StdoutCut     bool   `json:"stdout_truncated"`
	StderrCut     bool   `json:"stderr_truncated"`
}

// ProductionSubject is the exact identity a production record must match:
// stage-5 subject facts plus class, check id, and policy digest.
type ProductionSubject struct {
	Subject
	Check        RequiredCheck
	PolicyDigest string
	// EvidenceSet and PacketHash are the review-class bindings. They are empty
	// for every runnable class, which never reads them.
	EvidenceSet string
	PacketHash  string
}

// NewProduction records one runnable production observation using the sole
// stage-5 runner result. It never invents a pass: timeout and interruption
// stay recorded as bounded failures.
func NewProduction(subject ProductionSubject, result verifyexec.Result) (Production, error) {
	value := Production{
		SchemaVersion: SchemaVersion, Class: subject.Check.Class, CheckID: subject.Check.CheckID,
		PolicyDigest: subject.PolicyDigest, Change: subject.Change, TaskID: subject.TaskID,
		AttemptID: subject.AttemptID, HEAD: subject.HEAD, TaskHash: subject.TaskHash,
		CommandHash: subject.CommandHash, ApprovalHash: subject.ApprovalHash,
		StateRevision: subject.StateRevision, StartedAt: result.StartedAt, EndedAt: result.EndedAt,
		ExitCode: result.ExitCode, Passed: result.Passed, TimedOut: result.TimedOut,
		Interrupted: result.Interrupted, StdoutDigest: result.StdoutDigest,
		StderrDigest: result.StderrDigest, StdoutCut: result.StdoutTruncated,
		StderrCut: result.StderrTruncated,
	}
	if value.Passed && (result.TimedOut || result.Interrupted || !result.NonVacuous) {
		return Production{}, errors.New("interrupted or vacuous observation cannot pass")
	}
	return value, value.Validate()
}

// NewReview records one review verdict. Review carries a reviewer identity
// instead of a command and can never satisfy a runnable class.
func NewReview(subject ProductionSubject, reviewer string, passed bool, observed time.Time) (Production, error) {
	stamp := observed.UTC().Format(time.RFC3339Nano)
	value := Production{
		SchemaVersion: SchemaVersion, Class: subject.Check.Class, CheckID: subject.Check.CheckID,
		PolicyDigest: subject.PolicyDigest, Change: subject.Change, TaskID: subject.TaskID,
		AttemptID: subject.AttemptID, HEAD: subject.HEAD, TaskHash: subject.TaskHash,
		Reviewer: strings.TrimSpace(reviewer), ApprovalHash: subject.ApprovalHash,
		EvidenceSet: subject.EvidenceSet, PacketHash: subject.PacketHash,
		StateRevision: subject.StateRevision, StartedAt: stamp, EndedAt: stamp, Passed: passed,
	}
	if !passed {
		value.ExitCode = 1
	}
	return value, value.Validate()
}

func (value Production) Validate() error {
	start, startErr := utc(value.StartedAt)
	end, endErr := utc(value.EndedAt)
	declared := RequiredCheck{Class: value.Class, CheckID: value.CheckID}
	switch {
	case value.SchemaVersion != SchemaVersion:
		return errors.New("unsupported evidence schema")
	case value.Class == ClassTestRun:
		return errors.New("test-run evidence uses the stage-5 payload")
	case !declared.Valid():
		return errors.New("evidence class or check id is unknown")
	case !hexValue(value.PolicyDigest, sha256.Size*2):
		return errors.New("evidence policy digest is malformed")
	case value.Change == "" || value.TaskID == "" || value.AttemptID == "":
		return errors.New("change, task, and attempt are required")
	case !hexValue(value.HEAD, 40, 64):
		return errors.New("HEAD is malformed")
	case !hexValue(value.TaskHash, sha256.Size*2) || !hexValue(value.ApprovalHash, sha256.Size*2):
		return errors.New("evidence digest is malformed")
	case value.StateRevision == 0 || startErr != nil || endErr != nil || end.Before(start):
		return errors.New("evidence revision or time is malformed")
	case value.Passed != (value.ExitCode == 0 && !value.TimedOut && !value.Interrupted):
		return errors.New("evidence result facts disagree")
	}
	if Runnable(value.Class) {
		switch {
		case value.Reviewer != "":
			return errors.New("runnable evidence cannot carry a reviewer")
		case value.Findings != "" || value.EvidenceSet != "" || value.PacketHash != "":
			return errors.New("runnable evidence cannot carry review bindings")
		case !hexValue(value.CommandHash, sha256.Size*2):
			return errors.New("runnable evidence requires a command digest")
		case !hexValue(value.StdoutDigest, sha256.Size*2) ||
			!hexValue(value.StderrDigest, sha256.Size*2):
			return errors.New("evidence digest is malformed")
		}
		return nil
	}
	switch {
	case value.CommandHash != "" || value.StdoutDigest != "" || value.StderrDigest != "" ||
		value.StdoutCut || value.StderrCut:
		return errors.New("review evidence never executes a command")
	case value.Reviewer == "" || len(value.Reviewer) > maximumReviewer ||
		strings.TrimSpace(value.Reviewer) != value.Reviewer ||
		strings.ContainsAny(value.Reviewer, "\n\r\t"):
		return errors.New("review evidence requires one bounded reviewer identity")
	case len(value.Findings) > maximumFindings || strings.ContainsAny(value.Findings, "\x00"):
		return errors.New("review findings are unbounded or malformed")
	case value.EvidenceSet != "" && !hexValue(value.EvidenceSet, sha256.Size*2),
		value.PacketHash != "" && !hexValue(value.PacketHash, sha256.Size*2):
		return errors.New("review binding digest is malformed")
	}
	return nil
}

// Matches is exact identity: class, check id, subject, current HEAD, and policy
// digest. The executed command is recorded but never widens applicability. A
// review record additionally binds the evidence set and packet it was taken
// against, so evidence drift invalidates it exactly like code or policy drift.
func (value Production) Matches(subject ProductionSubject) bool {
	// ponytail: a subject that declares no review bindings cannot check them,
	// so it only checks what it knows; the review path always declares both.
	// Give the completion path the same two bindings and this becomes an
	// unconditional comparison.
	if value.Class == ClassReview && subject.EvidenceSet != "" && subject.PacketHash != "" &&
		(value.EvidenceSet != subject.EvidenceSet || value.PacketHash != subject.PacketHash) {
		return false
	}
	return value.Validate() == nil &&
		value.Class == subject.Check.Class && value.CheckID == subject.Check.CheckID &&
		value.PolicyDigest == subject.PolicyDigest &&
		value.Change == subject.Change && value.TaskID == subject.TaskID &&
		value.AttemptID == subject.AttemptID && value.HEAD == subject.HEAD &&
		value.TaskHash == subject.TaskHash && value.ApprovalHash == subject.ApprovalHash &&
		value.StateRevision == subject.StateRevision
}

func (value Production) Applicable(subject ProductionSubject) bool {
	end, err := utc(value.EndedAt)
	return err == nil && !end.After(time.Now().UTC()) &&
		value.Matches(subject) && value.Passed && value.ExitCode == 0
}

// AppendProduction persists one production observation. A review record whose
// reviewer is the recording actor is refused: an agent cannot review itself.
func AppendProduction(path, actor string, value Production) (record.Record, error) {
	if err := value.Validate(); err != nil {
		return record.Record{}, err
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return record.Record{}, errors.New("evidence actor is required")
	}
	if value.Class == ClassReview && value.Reviewer == actor {
		return record.Record{}, errors.New("review evidence requires a separate reviewer identity")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return record.Record{}, err
	}
	item, err := record.New(record.Record{
		Family: record.FamilyEvidence, Kind: record.KindObserved,
		Change: value.Change, Timestamp: value.EndedAt, Actor: actor, Payload: payload,
	})
	if err != nil {
		return record.Record{}, err
	}
	if err := record.Append(path, record.FamilyEvidence, item); err != nil {
		return record.Record{}, err
	}
	return item, nil
}

// ClassOf reads the declared class of any evidence payload without decoding
// class-specific fields, so one class never has to parse another's record.
func ClassOf(raw []byte) (string, error) {
	var envelope struct {
		Class string `json:"class"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", errors.New("evidence payload is malformed")
	}
	if !knownClass(envelope.Class) {
		return "", fmt.Errorf("unknown evidence class %q", envelope.Class)
	}
	return envelope.Class, nil
}

func DecodeProduction(raw []byte) (Production, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return Production{}, err
	}
	var value Production
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Production{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Production{}, errors.New("evidence payload has trailing data")
	}
	return value, value.Validate()
}

// MissingProduction returns the required checks with no current applicable
// passing record, in declaration order. priorDigest names the policy digest of
// a record that matched everything except the current policy, which is policy
// drift rather than missing proof. Malformed or unknown records fail closed.
func MissingProduction(items []record.Record, required []RequiredCheck, subject Subject, policyDigest string) (missing []RequiredCheck, priorDigest string, err error) {
	for _, check := range required {
		switch {
		case check.Class == ClassTestRun:
			return nil, "", errors.New("test-run proof is owned by the core completion path")
		case !check.Valid():
			return nil, "", fmt.Errorf("unknown production check %q", check.String())
		}
	}
	values := make([]Production, 0, len(items))
	for _, item := range items {
		class, classErr := ClassOf(item.Payload)
		if classErr != nil {
			return nil, "", classErr
		}
		if class == ClassTestRun {
			continue
		}
		value, decodeErr := DecodeProduction(item.Payload)
		if decodeErr != nil || item.Change != value.Change || item.Timestamp != value.EndedAt {
			return nil, "", errors.New("malformed or unknown production evidence record")
		}
		values = append(values, value)
	}
	for _, check := range required {
		want := ProductionSubject{Subject: subject, Check: check, PolicyDigest: policyDigest}
		satisfied := false
		for _, value := range values {
			if value.Applicable(want) {
				satisfied = true
				break
			}
			if priorDigest == "" && value.PolicyDigest != policyDigest && value.Passed {
				drifted := want
				drifted.PolicyDigest = value.PolicyDigest
				if value.Applicable(drifted) {
					priorDigest = value.PolicyDigest
				}
			}
		}
		if !satisfied {
			missing = append(missing, check)
		}
	}
	return missing, priorDigest, nil
}
