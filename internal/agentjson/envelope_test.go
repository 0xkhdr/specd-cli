package agentjson

import (
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func success() Envelope {
	return Envelope{
		Schema: Schema, OK: true, Operation: "context",
		Root:    &Root{Path: "/project"},
		Subject: &Subject{Change: "safe-create", Task: "S1-01"},
		State:   &State{Revision: 4},
		Data:    map[string]any{"item_count": 3, "allowed_write_paths": []string{"internal/core/x.go"}},
		Next: Next{
			Kind: NextOperation, Operation: "start",
			Arguments:   map[string]string{"change": "safe-create", "task": "S1-01"},
			Instruction: "run specd start safe-create S1-01 --revision 4",
		},
		Exit: Exit{Code: 0, Class: "success"},
	}
}

func refusal() Envelope {
	return Envelope{
		Schema: Schema, OK: false, Operation: "complete",
		Root:    &Root{Path: "/project"},
		Subject: &Subject{Change: "safe-create", Task: "S1-01"},
		State:   &State{Revision: 6},
		Diagnostics: []Diagnostic{{
			Code: "evidence_stale", Severity: "error",
			Message: "recorded evidence does not match current HEAD",
			Fix:     "run specd verify safe-create S1-01 A1",
		}},
		Next: Next{
			Kind: NextOperation, Operation: "verify",
			Arguments: map[string]string{
				"change": "safe-create", "task": "S1-01", "attempt": "A1",
			},
			Instruction: "run specd verify safe-create S1-01 A1",
		},
		Exit: Exit{Code: 2, Class: "refusal"},
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	for name, envelope := range map[string]Envelope{"success": success(), "refusal": refusal()} {
		encoded, err := Encode(envelope)
		if err != nil {
			t.Fatalf("%s: encode: %v", name, err)
		}
		if !strings.HasPrefix(string(encoded), `{"schema":"`+Schema+`","ok":`) {
			t.Fatalf("%s: unexpected key order: %s", name, encoded)
		}
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		again, err := Encode(decoded)
		if err != nil {
			t.Fatalf("%s: re-encode: %v", name, err)
		}
		if string(again) != string(encoded) {
			t.Fatalf("%s: round trip drifted:\n%s\n%s", name, encoded, again)
		}
	}
}

func TestEnvelopeOmitsUnavailableValues(t *testing.T) {
	envelope := Envelope{
		Schema: Schema, OK: false, Operation: "status",
		Diagnostics: []Diagnostic{{
			Code: "root_selection", Severity: "error", Message: "no root selected",
			Fix: "run specd status <change> with an explicit --root",
		}},
		Next: Next{Kind: NextBlocked, Owner: "human",
			Instruction: "run specd status <change> with an explicit --root"},
		Exit: Exit{Code: 2, Class: "refusal"},
	}
	encoded, err := Encode(envelope)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, absent := range []string{`"root"`, `"subject"`, `"state"`, `"data"`, `null`} {
		if strings.Contains(string(encoded), absent) {
			t.Fatalf("unavailable value %s is present: %s", absent, encoded)
		}
	}
}

func TestEnvelopeFailsClosed(t *testing.T) {
	mutate := func(change func(*Envelope)) Envelope {
		envelope := success()
		change(&envelope)
		return envelope
	}
	cases := map[string]struct {
		envelope Envelope
		code     string
	}{
		"incompatible schema": {mutate(func(e *Envelope) {
			e.Schema = "specd.agent/v2"
		}), "schema_unsupported"},
		"unknown operation": {mutate(func(e *Envelope) {
			e.Operation = "drive"
		}), "operation_unknown"},
		"missing root after resolution": {mutate(func(e *Envelope) {
			e.Root = nil
		}), "root_missing"},
		"missing subject after resolution": {mutate(func(e *Envelope) {
			e.Subject = nil
		}), "subject_missing"},
		"missing task subject": {mutate(func(e *Envelope) {
			e.Subject = &Subject{Change: "safe-create"}
		}), "subject_missing"},
		"zero revision": {mutate(func(e *Envelope) {
			e.State = &State{Revision: 0}
		}), "state_missing"},
		"null data value": {mutate(func(e *Envelope) {
			e.Data = map[string]any{"selected": nil}
		}), "null_value"},
		"camelCase data key": {mutate(func(e *Envelope) {
			e.Data = map[string]any{"itemCount": 3}
		}), "key_casing"},
		"nested data": {mutate(func(e *Envelope) {
			e.Data = map[string]any{"items": map[string]any{"path": "x"}}
		}), "data_unsafe"},
		"unbounded data": {mutate(func(e *Envelope) {
			e.Data = map[string]any{"excerpt": strings.Repeat("x", MaxStringBytes+1)}
		}), "data_unbounded"},
		"control character": {mutate(func(e *Envelope) {
			e.Data = map[string]any{"excerpt": "before\x00after"}
		}), "data_unsafe"},
		"unknown next variant": {mutate(func(e *Envelope) {
			e.Next = Next{Kind: "maybe", Instruction: "guess"}
		}), "next_unknown"},
		"two next variants": {mutate(func(e *Envelope) {
			e.Next.Owner = "human"
		}), "next_ambiguous"},
		"terminal carrying an operation": {mutate(func(e *Envelope) {
			e.Next = Next{Kind: NextTerminal, Operation: "next", Instruction: "done"}
		}), "next_ambiguous"},
		"human-only next action": {mutate(func(e *Envelope) {
			e.Next = Next{Kind: NextOperation, Operation: "approve",
				Arguments: map[string]string{"change": "safe-create"}, Instruction: "run specd approve"}
		}), "next_human_only"},
		"unregistered next action": {mutate(func(e *Envelope) {
			e.Next = Next{Kind: NextOperation, Operation: "resume",
				Arguments: map[string]string{"change": "safe-create"}, Instruction: "run specd resume"}
		}), "next_unknown"},
		"missing required argument": {mutate(func(e *Envelope) {
			e.Next.Arguments = map[string]string{"change": "safe-create"}
		}), "next_incomplete"},
		"undeclared argument": {mutate(func(e *Envelope) {
			e.Next.Arguments["budget"] = "10"
		}), "next_incomplete"},
		"handoff without reason": {mutate(func(e *Envelope) {
			e.Next = Next{Kind: NextHumanHandoff, Owner: "human", Instruction: "ask a human"}
		}), "next_incomplete"},
		"blocked without owner": {mutate(func(e *Envelope) {
			e.Next = Next{Kind: NextBlocked, Instruction: "repair the plan"}
		}), "next_incomplete"},
		"success with error diagnostic": {mutate(func(e *Envelope) {
			e.Diagnostics = []Diagnostic{{Code: "x", Severity: "error", Message: "m", Fix: "f"}}
		}), "diagnostic_incomplete"},
		"exit class mismatch": {mutate(func(e *Envelope) {
			e.Exit = Exit{Code: 0, Class: "refusal"}
		}), "exit_class_mismatch"},
		"ok disagrees with exit": {mutate(func(e *Envelope) {
			e.Exit = Exit{Code: 1, Class: "failure"}
		}), "exit_class_mismatch"},
		"unknown exit code": {mutate(func(e *Envelope) {
			e.Exit = Exit{Code: 3, Class: "failure"}
		}), "exit_class_unknown"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			encoded, err := Encode(testCase.envelope)
			if err == nil {
				t.Fatalf("expected refusal, encoded %s", encoded)
			}
			if !failure.IsCode(err, testCase.code) {
				t.Fatalf("expected code %q, got %v", testCase.code, err)
			}
			var refusal *failure.Refusal
			if !errorsAs(err, &refusal) || strings.TrimSpace(refusal.Next) == "" {
				t.Fatalf("refusal %v names no next action", err)
			}
		})
	}
}

func TestEnvelopeFailureNeedsDiagnostic(t *testing.T) {
	envelope := success()
	envelope.OK, envelope.Exit = false, Exit{Code: 1, Class: "failure"}
	if _, err := Encode(envelope); !failure.IsCode(err, "diagnostic_incomplete") {
		t.Fatalf("expected diagnostic_incomplete, got %v", err)
	}
	envelope.Diagnostics = []Diagnostic{{
		Code: "gate_failed", Severity: "error", Message: "m", Line: 4,
	}}
	if _, err := Encode(envelope); !failure.IsCode(err, "diagnostic_incomplete") {
		t.Fatalf("expected missing fix to fail closed, got %v", err)
	}
	envelope.Diagnostics[0].Fix = "repair the plan"
	if _, err := Encode(envelope); !failure.IsCode(err, "diagnostic_incomplete") {
		t.Fatalf("expected line without path to fail closed, got %v", err)
	}
	envelope.Diagnostics[0].Path = "tasks.md"
	if _, err := Encode(envelope); err != nil {
		t.Fatalf("located diagnostic with one fix: %v", err)
	}
}

func TestEnvelopeDecodeRefusesDrift(t *testing.T) {
	cases := map[string]struct{ raw, code string }{
		"null root":    {`{"schema":"specd.agent/v1","ok":true,"operation":"status","root":null,"next":{"kind":"terminal","instruction":"done"},"exit":{"code":0,"class":"success"}}`, "null_value"},
		"unknown key":  {`{"schema":"specd.agent/v1","ok":true,"operation":"status","extra":1,"next":{"kind":"terminal","instruction":"done"},"exit":{"code":0,"class":"success"}}`, "envelope_malformed"},
		"two document": {`{"schema":"specd.agent/v1","ok":true,"operation":"status","next":{"kind":"terminal","instruction":"done"},"exit":{"code":0,"class":"success"}} {}`, "envelope_malformed"},
		"old schema":   {`{"schema":"specd/v0","ok":true,"operation":"status","next":{"kind":"terminal","instruction":"done"},"exit":{"code":0,"class":"success"}}`, "schema_unsupported"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(testCase.raw)); !failure.IsCode(err, testCase.code) {
				t.Fatalf("expected %q, got %v", testCase.code, err)
			}
		})
	}
}

// errorsAs keeps the assertion above readable without importing errors twice.
func errorsAs(err error, target **failure.Refusal) bool {
	refusal, ok := err.(*failure.Refusal)
	if ok {
		*target = refusal
	}
	return ok
}
