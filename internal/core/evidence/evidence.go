package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/record"
	verifyexec "github.com/0xkhdr/specd-cli/internal/core/verify"
)

const (
	SchemaVersion = 1
	ClassTestRun  = "test-run"
)

// TestRun is the sole stage-5 evidence payload. Output text is deliberately
// absent: only bounded execution facts and full-stream digests are durable.
type TestRun struct {
	SchemaVersion int    `json:"schema_version"`
	Class         string `json:"class"`
	Change        string `json:"change"`
	TaskID        string `json:"task"`
	AttemptID     string `json:"attempt"`
	HEAD          string `json:"head"`
	TaskHash      string `json:"task_hash"`
	CommandHash   string `json:"command_hash"`
	ApprovalHash  string `json:"approval_hash"`
	StateRevision uint64 `json:"state_revision"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at"`
	ExitCode      int    `json:"exit_code"`
	Passed        bool   `json:"passed"`
	TimedOut      bool   `json:"timed_out"`
	Interrupted   bool   `json:"interrupted"`
	NonVacuous    bool   `json:"non_vacuous"`
	ZeroMatch     bool   `json:"zero_match"`
	StdoutDigest  string `json:"stdout_digest"`
	StderrDigest  string `json:"stderr_digest"`
	StdoutCut     bool   `json:"stdout_truncated"`
	StderrCut     bool   `json:"stderr_truncated"`
}

type Subject struct {
	Change, TaskID, AttemptID, HEAD string
	TaskHash, CommandHash           string
	ApprovalHash                    string
	StateRevision                   uint64
}

func NewTestRun(subject Subject, result verifyexec.Result) (TestRun, error) {
	value := TestRun{
		SchemaVersion: SchemaVersion, Class: ClassTestRun,
		Change: subject.Change, TaskID: subject.TaskID, AttemptID: subject.AttemptID,
		HEAD: subject.HEAD, TaskHash: subject.TaskHash, CommandHash: subject.CommandHash,
		ApprovalHash: subject.ApprovalHash, StateRevision: subject.StateRevision,
		StartedAt: result.StartedAt, EndedAt: result.EndedAt, ExitCode: result.ExitCode,
		Passed: result.Passed, TimedOut: result.TimedOut, Interrupted: result.Interrupted,
		NonVacuous: result.NonVacuous, ZeroMatch: result.ZeroMatch,
		StdoutDigest: result.StdoutDigest, StderrDigest: result.StderrDigest,
		StdoutCut: result.StdoutTruncated, StderrCut: result.StderrTruncated,
	}
	return value, value.Validate()
}

func (value TestRun) Validate() error {
	start, startErr := utc(value.StartedAt)
	end, endErr := utc(value.EndedAt)
	switch {
	case value.SchemaVersion != SchemaVersion || value.Class != ClassTestRun:
		return errors.New("unsupported evidence schema or class")
	case value.Change == "" || value.TaskID == "" || value.AttemptID == "":
		return errors.New("change, task, and attempt are required")
	case !hexValue(value.HEAD, 40, 64):
		return errors.New("HEAD is malformed")
	case !hexValue(value.TaskHash, sha256.Size*2) ||
		!hexValue(value.CommandHash, sha256.Size*2) ||
		!hexValue(value.ApprovalHash, sha256.Size*2) ||
		!hexValue(value.StdoutDigest, sha256.Size*2) ||
		!hexValue(value.StderrDigest, sha256.Size*2):
		return errors.New("evidence digest is malformed")
	case value.StateRevision == 0 || startErr != nil || endErr != nil || end.Before(start):
		return errors.New("evidence revision or time is malformed")
	case value.Passed != (value.ExitCode == 0 && value.NonVacuous &&
		!value.TimedOut && !value.Interrupted && !value.ZeroMatch):
		return errors.New("evidence result facts disagree")
	case (value.TimedOut || value.Interrupted || value.ZeroMatch) && value.NonVacuous:
		return errors.New("failed observation cannot be non-vacuous")
	}
	return nil
}

func (value TestRun) Applicable(subject Subject) bool {
	end, err := utc(value.EndedAt)
	return err == nil && !end.After(time.Now().UTC()) &&
		value.Matches(subject) && value.Passed && value.ExitCode == 0 && value.NonVacuous
}

func (value TestRun) Matches(subject Subject) bool {
	return value.Validate() == nil && value.Class == ClassTestRun &&
		value.Change == subject.Change && value.TaskID == subject.TaskID &&
		value.AttemptID == subject.AttemptID && value.HEAD == subject.HEAD &&
		value.TaskHash == subject.TaskHash && value.CommandHash == subject.CommandHash &&
		value.ApprovalHash == subject.ApprovalHash &&
		value.StateRevision == subject.StateRevision
}

func Append(path, actor string, value TestRun) (record.Record, error) {
	if err := value.Validate(); err != nil {
		return record.Record{}, err
	}
	if strings.TrimSpace(actor) == "" {
		return record.Record{}, errors.New("evidence actor is required")
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

// Project returns all observations and the sole latest applicable pass.
// Malformed, unknown, future, duplicate, or ambiguous ledgers fail closed.
func Project(path string, subject Subject) ([]TestRun, *TestRun, error) {
	items, diagnostics, err := record.Replay(path, record.FamilyEvidence)
	if err != nil {
		return nil, nil, err
	}
	if len(diagnostics) != 0 {
		return nil, nil, fmt.Errorf("evidence has incomplete record at line %d", diagnostics[0].Line)
	}
	seen := map[string]bool{}
	values := make([]TestRun, 0, len(items))
	applicable := -1
	for _, item := range items {
		if seen[item.ID] {
			return values, nil, errors.New("duplicate evidence record")
		}
		seen[item.ID] = true
		value, decodeErr := Decode(item.Payload)
		if decodeErr != nil || item.Change != value.Change ||
			item.Timestamp != value.EndedAt {
			return values, nil, errors.New("malformed or unknown evidence record")
		}
		values = append(values, value)
		if value.Matches(subject) {
			applicable = len(values) - 1
		}
	}
	if applicable < 0 {
		return values, nil, nil
	}
	value := values[applicable]
	if !value.Applicable(subject) {
		return values, nil, nil
	}
	return values, &value, nil
}

func Decode(raw []byte) (TestRun, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return TestRun{}, err
	}
	var value TestRun
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return TestRun{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return TestRun{}, errors.New("evidence payload has trailing data")
	}
	return value, value.Validate()
}

func utc(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || !strings.HasSuffix(value, "Z") || parsed.Location() != time.UTC {
		return time.Time{}, errors.New("time must be RFC3339 UTC")
	}
	return parsed, nil
}

func hexValue(value string, lengths ...int) bool {
	for _, length := range lengths {
		if len(value) == length && strings.ToLower(value) == value {
			_, err := hex.DecodeString(value)
			return err == nil
		}
	}
	return false
}

func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name := key.(string)
				if seen[name] {
					return fmt.Errorf("duplicate JSON key %q", name)
				}
				seen[name] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		}
		_, err = decoder.Token()
		return err
	}
	return walk()
}
