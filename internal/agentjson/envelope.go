// Package agentjson owns the one bounded, versioned document a headless agent
// reads. It projects canonical results; it decides no lifecycle, readiness,
// evidence, authority, or scope question of its own.
package agentjson

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// Schema is the only accepted envelope version. There is no alias and no
// compatibility reader: an incompatible client is refused, never reinterpreted.
const Schema = "specd.agent/v1"

// Next-action variants. Exactly one is legal per envelope.
const (
	NextOperation    = "operation"
	NextHumanHandoff = "human_handoff"
	NextTerminal     = "terminal"
	NextBlocked      = "blocked"
)

// Bounds keep the document small enough to read and impossible to use as a
// side channel for source text, transcripts, or unbounded process output.
const (
	MaxDataKeys    = 40
	MaxStringBytes = 4096
	MaxListItems   = 64
	MaxDiagnostics = 100
)

var (
	nextKinds = []string{NextOperation, NextHumanHandoff, NextTerminal, NextBlocked}
	snakeCase = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

type Root struct {
	Path string `json:"path"`
}

type Subject struct {
	Change string `json:"change"`
	Task   string `json:"task,omitempty"`
}

type State struct {
	Revision uint64 `json:"revision"`
}

// Exit is the process outcome class. The three classes stay distinct so a
// fail-closed refusal is never read as a failed gate or as success.
type Exit struct {
	Code  int    `json:"code"`
	Class string `json:"class"`
}

// Diagnostic locates one problem and supplies exactly one repair.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Fix      string `json:"fix"`
}

// Next is the single legal next action. Kind selects the variant; every field
// outside that variant must be absent, so no envelope offers two moves.
type Next struct {
	Kind        string            `json:"kind"`
	Operation   string            `json:"operation,omitempty"`
	Arguments   map[string]string `json:"arguments,omitempty"`
	Owner       string            `json:"owner,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Instruction string            `json:"instruction"`
}

// Envelope is the whole agent-facing document. Field order here is the encoded
// key order, so goldens stay byte-stable.
type Envelope struct {
	Schema      string         `json:"schema"`
	OK          bool           `json:"ok"`
	Operation   string         `json:"operation"`
	Root        *Root          `json:"root,omitempty"`
	Subject     *Subject       `json:"subject,omitempty"`
	State       *State         `json:"state,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	Next        Next           `json:"next"`
	Exit        Exit           `json:"exit"`
}

// ExitFor maps an exit code to its declared class.
func ExitFor(code int) (Exit, error) {
	switch code {
	case 0:
		return Exit{Code: 0, Class: core.ExitClassSuccess}, nil
	case 1:
		return Exit{Code: 1, Class: core.ExitClassFailure}, nil
	case 2:
		return Exit{Code: 2, Class: core.ExitClassRefusal}, nil
	}
	return Exit{}, refuse("exit_class_unknown",
		fmt.Sprintf("exit code %d has no declared class", code),
		"return one of the declared exit codes 0, 1, or 2")
}

// Encode validates and renders one document. An invalid envelope produces no
// bytes at all: a partial document is never presented as valid.
func Encode(envelope Envelope) ([]byte, error) {
	if err := Validate(envelope); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(envelope); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Decode reads one document strictly: unknown keys, JSON null, trailing data,
// and any invalid shape are refusals.
func Decode(raw []byte) (Envelope, error) {
	if err := rejectNull(raw); err != nil {
		return Envelope{}, err
	}
	var envelope Envelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, refuse("envelope_malformed", err.Error(),
			"emit one "+Schema+" document")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Envelope{}, refuse("envelope_malformed", "document has trailing data",
			"emit exactly one "+Schema+" document")
	}
	return envelope, Validate(envelope)
}

// Validate fails closed on every representable inconsistency.
func Validate(envelope Envelope) error {
	if envelope.Schema != Schema {
		return refuse("schema_unsupported",
			fmt.Sprintf("envelope schema %q is not supported", envelope.Schema),
			"request schema "+Schema)
	}
	operation, known := core.OperationByID(envelope.Operation)
	if !known {
		return refuse("operation_unknown",
			fmt.Sprintf("unknown operation %q", envelope.Operation),
			"name one registered operation")
	}
	if err := validateExit(envelope); err != nil {
		return err
	}
	if err := validateSubject(envelope, operation); err != nil {
		return err
	}
	if err := validateData(envelope.Data); err != nil {
		return err
	}
	if err := validateDiagnostics(envelope); err != nil {
		return err
	}
	return validateNext(envelope.Next)
}

func validateExit(envelope Envelope) error {
	expected, err := ExitFor(envelope.Exit.Code)
	if err != nil {
		return err
	}
	if expected.Class != envelope.Exit.Class {
		return refuse("exit_class_mismatch",
			fmt.Sprintf("exit %d is class %q, not %q",
				envelope.Exit.Code, expected.Class, envelope.Exit.Class),
			"report the declared class for this exit code")
	}
	if envelope.OK != (envelope.Exit.Code == 0) {
		return refuse("exit_class_mismatch",
			"ok and exit class disagree",
			"report success only with exit class "+core.ExitClassSuccess)
	}
	return nil
}

// validateSubject keeps resolution explicit: anything that proves a change was
// resolved obliges the envelope to name the root and the subject.
func validateSubject(envelope Envelope, operation core.Operation) error {
	if envelope.Root != nil && strings.TrimSpace(envelope.Root.Path) == "" {
		return refuse("root_missing", "resolved root is empty", "report the resolved root path")
	}
	if envelope.Subject != nil && strings.TrimSpace(envelope.Subject.Change) == "" {
		return refuse("subject_missing", "resolved subject has no change",
			"report the resolved change, or omit the subject")
	}
	if envelope.State != nil && envelope.Subject == nil {
		return refuse("subject_missing", "state revision without a resolved subject",
			"report the resolved change with its state revision")
	}
	if envelope.State != nil && envelope.State.Revision == 0 {
		return refuse("state_missing", "state revision is zero",
			"report the observed state revision, or omit state")
	}
	if (envelope.Subject != nil || envelope.State != nil) && envelope.Root == nil {
		return refuse("root_missing", "resolved subject without a resolved root",
			"report the resolved root path")
	}
	if !envelope.OK {
		return nil
	}
	switch {
	case envelope.Root == nil:
		return refuse("root_missing", "a successful operation resolved a root",
			"report the resolved root path")
	case operation.RequiresChange && envelope.Subject == nil:
		return refuse("subject_missing",
			operation.ID+" resolves a change", "report the resolved change")
	case operation.RequiresTask && (envelope.Subject == nil || envelope.Subject.Task == ""):
		return refuse("subject_missing",
			operation.ID+" resolves a task", "report the resolved task")
	}
	return nil
}

// validateData bounds the payload. Only scalars and string lists are legal:
// nesting is how source text, transcripts, and host detail leak.
func validateData(data map[string]any) error {
	if len(data) > MaxDataKeys {
		return refuse("data_unbounded",
			fmt.Sprintf("data has %d keys, limit %d", len(data), MaxDataKeys),
			"project only bounded operation facts")
	}
	for key, value := range data {
		if !snakeCase.MatchString(key) {
			return refuse("key_casing", fmt.Sprintf("data key %q is not snake_case", key),
				"encode every key as snake_case")
		}
		if err := validateValue(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(key string, value any) error {
	switch typed := value.(type) {
	case nil:
		return refuse("null_value", fmt.Sprintf("data key %q is null", key),
			"omit the key instead of encoding null")
	case string:
		return validateText(key, typed)
	case bool, int, int64, uint64, float64:
		return nil
	case []string:
		return validateList(key, len(typed), func(index int) any { return typed[index] })
	// A decoded document carries []any; every element must still be a bounded
	// string, so a decoded envelope is held to the encoded envelope's rules.
	case []any:
		return validateList(key, len(typed), func(index int) any { return typed[index] })
	}
	return refuse("data_unsafe", fmt.Sprintf("data key %q has unsupported type %T", key, value),
		"project scalars or string lists only")
}

func validateList(key string, length int, item func(int) any) error {
	if length > MaxListItems {
		return refuse("data_unbounded",
			fmt.Sprintf("data key %q has %d items, limit %d", key, length, MaxListItems),
			"project only bounded operation facts")
	}
	for index := 0; index < length; index++ {
		text, ok := item(index).(string)
		if !ok {
			return refuse("data_unsafe",
				fmt.Sprintf("data key %q holds a non-string item", key),
				"project scalars or string lists only")
		}
		if err := validateText(key, text); err != nil {
			return err
		}
	}
	return nil
}

func validateText(key, value string) error {
	if len(value) > MaxStringBytes {
		return refuse("data_unbounded",
			fmt.Sprintf("data key %q is %d bytes, limit %d", key, len(value), MaxStringBytes),
			"bound the value and reference the authoritative evidence")
	}
	if !utf8.ValidString(value) {
		return refuse("data_unsafe", fmt.Sprintf("data key %q is not valid UTF-8", key),
			"bound and sanitize the value before encoding")
	}
	for _, character := range value {
		if character < 0x20 && character != '\n' && character != '\t' {
			return refuse("data_unsafe",
				fmt.Sprintf("data key %q contains a control character", key),
				"bound and sanitize the value before encoding")
		}
	}
	return nil
}

func validateDiagnostics(envelope Envelope) error {
	if len(envelope.Diagnostics) > MaxDiagnostics {
		return refuse("data_unbounded",
			fmt.Sprintf("envelope has %d diagnostics, limit %d",
				len(envelope.Diagnostics), MaxDiagnostics),
			"report the bounded set of findings")
	}
	errorCount := 0
	for _, diagnostic := range envelope.Diagnostics {
		switch {
		case strings.TrimSpace(diagnostic.Code) == "":
			return refuse("diagnostic_incomplete", "diagnostic has no code",
				"give every diagnostic a stable code")
		case diagnostic.Severity != "error" && diagnostic.Severity != "warning":
			return refuse("diagnostic_incomplete",
				fmt.Sprintf("diagnostic %q has severity %q", diagnostic.Code, diagnostic.Severity),
				"report severity error or warning")
		case strings.TrimSpace(diagnostic.Message) == "":
			return refuse("diagnostic_incomplete",
				fmt.Sprintf("diagnostic %q has no message", diagnostic.Code),
				"describe the problem in the diagnostic message")
		case strings.TrimSpace(diagnostic.Fix) == "":
			return refuse("diagnostic_incomplete",
				fmt.Sprintf("diagnostic %q has no fix", diagnostic.Code),
				"give every diagnostic exactly one fix")
		case diagnostic.Line < 0 || (diagnostic.Line > 0 && diagnostic.Path == ""):
			return refuse("diagnostic_incomplete",
				fmt.Sprintf("diagnostic %q locates a line without a path", diagnostic.Code),
				"report the path the line belongs to")
		}
		if err := validateText("message", diagnostic.Message); err != nil {
			return err
		}
		if diagnostic.Severity == "error" {
			errorCount++
		}
	}
	if envelope.OK && errorCount != 0 {
		return refuse("diagnostic_incomplete", "successful result carries an error diagnostic",
			"report ok false when a diagnostic is an error")
	}
	if !envelope.OK && len(envelope.Diagnostics) == 0 {
		return refuse("diagnostic_incomplete", "failed result carries no diagnostic",
			"report at least one diagnostic with its fix")
	}
	return nil
}

// validateNext enforces exactly one variant. Fields belonging to another
// variant are a second next action, so they fail closed.
func validateNext(next Next) error {
	if !slices.Contains(nextKinds, next.Kind) {
		return refuse("next_unknown", fmt.Sprintf("unknown next action %q", next.Kind),
			"report one of "+strings.Join(nextKinds, ", "))
	}
	if strings.TrimSpace(next.Instruction) == "" {
		return refuse("next_incomplete", "next action has no instruction",
			"name exactly one legal next action")
	}
	if err := validateText("instruction", next.Instruction); err != nil {
		return err
	}
	forbid := func(condition bool, field string) error {
		if !condition {
			return nil
		}
		return refuse("next_ambiguous",
			fmt.Sprintf("next action %q must not carry %s", next.Kind, field),
			"report exactly one next-action variant")
	}
	if err := firstError(
		forbid(next.Kind != NextOperation && next.Operation != "", "an operation"),
		forbid(next.Kind != NextOperation && len(next.Arguments) != 0, "operation arguments"),
		forbid(next.Kind == NextOperation && next.Owner != "", "an owner"),
		forbid(next.Kind == NextTerminal && (next.Owner != "" || next.Reason != ""), "an owner or reason"),
		forbid(next.Kind == NextOperation && next.Reason != "", "a reason"),
	); err != nil {
		return err
	}
	switch next.Kind {
	case NextOperation:
		return validateNextOperation(next)
	case NextHumanHandoff:
		if next.Owner != "human" || strings.TrimSpace(next.Reason) == "" {
			return refuse("next_incomplete", "human handoff needs owner human and a reason",
				"report why a human must act and what they run")
		}
	case NextBlocked:
		if strings.TrimSpace(next.Owner) == "" {
			return refuse("next_incomplete", "blocked next action has no owner",
				"name the owner of the one legal recovery")
		}
	}
	return nil
}

func validateNextOperation(next Next) error {
	operation, known := core.OperationByID(next.Operation)
	switch {
	case !known:
		return refuse("next_unknown", fmt.Sprintf("unknown operation %q", next.Operation),
			"offer one registered operation")
	case !operation.Executable:
		return refuse("next_unknown",
			fmt.Sprintf("operation %q is reserved and not callable", next.Operation),
			"offer one callable operation")
	// The human boundary is metadata: an agent is never handed a human-only
	// verb, it is handed a handoff.
	case !operation.AgentVisible:
		return refuse("next_human_only",
			fmt.Sprintf("operation %q is human-only", next.Operation),
			"report a human handoff instead of a callable operation")
	}
	for name, value := range next.Arguments {
		if !snakeCase.MatchString(name) {
			return refuse("key_casing",
				fmt.Sprintf("argument %q is not snake_case", name),
				"encode every key as snake_case")
		}
		if !argumentDeclared(operation, name) {
			return refuse("next_incomplete",
				fmt.Sprintf("%s declares no argument %q", operation.ID, name),
				"pass only arguments the operation declares")
		}
		if err := validateText(name, value); err != nil {
			return err
		}
	}
	for _, argument := range operation.Arguments {
		if argument.Required && strings.TrimSpace(next.Arguments[argument.Name]) == "" {
			return refuse("next_incomplete",
				fmt.Sprintf("%s requires argument %q", operation.ID, argument.Name),
				"supply every required argument of the offered operation")
		}
	}
	return nil
}

func argumentDeclared(operation core.Operation, name string) bool {
	return slices.ContainsFunc(operation.Arguments, func(argument core.OperationArgument) bool {
		return argument.Name == name
	})
}

// firstError returns the first refusal, so a document is judged by the first
// rule it broke rather than by an aggregate nothing can act on.
func firstError(candidates ...error) error { return cmp.Or(candidates...) }

// rejectNull refuses documented-null drift anywhere in the document: an
// unavailable value is omitted, never encoded as null.
func rejectNull(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return refuse("envelope_malformed", err.Error(), "emit one "+Schema+" document")
		}
		if token == nil {
			return refuse("null_value", "document contains a null value",
				"omit unavailable values instead of encoding null")
		}
	}
}

func refuse(code, reason, next string) error {
	return failure.New(code, "", "", reason, next)
}
