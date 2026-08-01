package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

const SchemaVersion = 1

// Stable refusal codes for state decode/validate. All name external repair as
// the sole next action; the split exists because a future schema and a
// mismatched identity are diagnosed differently by an operator.
const (
	CodeCorrupt  = "state_corrupt"
	CodeSchema   = "state_schema_unsupported"
	CodeIdentity = "state_identity_mismatch"
)

const repairNext = "repair corrupt state outside automated mutation"

func corrupt(format string, args ...any) *failure.Refusal {
	return failure.New(CodeCorrupt, "", "", fmt.Sprintf(format, args...), repairNext)
}

// reprefix keeps a nested refusal's code and next action while preserving the
// original reason text.
func reprefix(prefix string, err error) error {
	var refusal *failure.Refusal
	if errors.As(err, &refusal) {
		return failure.New(refusal.Code, refusal.Root, refusal.Path, prefix+refusal.Reason, refusal.Next)
	}
	return corrupt("%s%s", prefix, err.Error())
}

type State struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	Change         string                     `json:"change"`
	Stage          string                     `json:"stage"`
	Condition      string                     `json:"condition"`
	Revision       uint64                     `json:"revision"`
	Approvals      []json.RawMessage          `json:"approvals"`
	Tasks          map[string]json.RawMessage `json:"tasks"`
	CreatedBy      string                     `json:"createdBy"`
	LastTransition string                     `json:"lastTransition"`
	Extensions     map[string]json.RawMessage `json:"extensions,omitempty"`
}

type Projection struct {
	Change     string `json:"change"`
	Schema     int    `json:"schemaVersion"`
	Stage      string `json:"stage"`
	Condition  string `json:"condition"`
	Revision   uint64 `json:"revision"`
	NextAction string `json:"nextAction"`
}

func Initial(change, creationID string) State {
	return State{
		SchemaVersion: SchemaVersion,
		Change:        change, Stage: "planning", Condition: "active", Revision: 1,
		Approvals: []json.RawMessage{}, Tasks: map[string]json.RawMessage{},
		CreatedBy: creationID, LastTransition: creationID,
	}
}

func (s State) Validate(expectedChange string) error {
	switch {
	case s.SchemaVersion != SchemaVersion:
		return failure.New(CodeSchema, "", "",
			fmt.Sprintf("unsupported schema version %d", s.SchemaVersion), repairNext)
	case s.Change == "" || s.Change != expectedChange:
		return failure.New(CodeIdentity, "", "",
			fmt.Sprintf("state change %q does not match %q", s.Change, expectedChange), repairNext)
	case !validStage(s.Stage):
		return corrupt("invalid lifecycle stage %q", s.Stage)
	case s.Condition != "active":
		return corrupt("invalid lifecycle condition %q", s.Condition)
	case s.Revision == 0:
		return corrupt("revision must be positive")
	case s.Approvals == nil:
		return corrupt("approvals must be an array")
	case s.Tasks == nil:
		return corrupt("tasks must be an object")
	case s.CreatedBy == "" || s.LastTransition == "":
		return corrupt("creation and transition identities are required")
	}
	return nil
}

func (s State) Project() Projection {
	next := map[string]string{
		"planning": "author planning artifacts", "approved": "start execution",
		"executing": "complete tasks", "reconciling": "sync accepted specs",
		"archived": "inspect archived change",
	}[s.Stage]
	return Projection{s.Change, s.SchemaVersion, s.Stage, s.Condition, s.Revision, next}
}

func validStage(stage string) bool {
	switch stage {
	case "planning", "approved", "executing", "reconciling", "archived":
		return true
	}
	return false
}

func Encode(s State) ([]byte, error) {
	if err := s.Validate(s.Change); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func Decode(data []byte, expectedChange string) (State, error) {
	if err := rejectDuplicateKeys(data); err != nil {
		return State{}, err
	}
	var s State
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return State{}, corrupt("decode state: %s", err.Error())
	}
	if err := ensureEOF(d); err != nil {
		return State{}, err
	}
	if err := s.Validate(expectedChange); err != nil {
		return State{}, reprefix("validate state: ", err)
	}
	return s, nil
}

func rejectDuplicateKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch delim := tok.(type) {
		case json.Delim:
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return err
					}
					k := key.(string)
					if seen[k] {
						return corrupt("duplicate JSON key %q", k)
					}
					seen[k] = true
					if err := walk(); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			case '[':
				for d.More() {
					if err := walk(); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			}
		}
		return nil
	}
	if err := walk(); err != nil {
		return reprefix("decode state: ", err)
	}
	return ensureEOF(d)
}

func ensureEOF(d *json.Decoder) error {
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return corrupt("decode state: trailing JSON value")
		}
		return corrupt("decode state: %s", err.Error())
	}
	return nil
}
