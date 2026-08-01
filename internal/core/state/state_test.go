package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestInitialRoundTrip(t *testing.T) {
	want := Initial("cache-ttl", "create-1")
	data, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data, "cache-ttl")
	if err != nil {
		t.Fatal(err)
	}
	if got.Change != want.Change || got.Revision != 1 || got.Stage != "planning" ||
		got.Condition != "active" || len(got.Approvals) != 0 || len(got.Tasks) != 0 {
		t.Fatalf("unexpected state: %+v", got)
	}
}

func TestApproveTransitionStateStages(t *testing.T) {
	for _, stage := range []string{"planning", "approved", "executing", "reconciling", "archived"} {
		current := Initial("safe-change", "created")
		current.Stage = stage
		if err := current.Validate("safe-change"); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
	}
	current := Initial("safe-change", "created")
	current.Stage = "future"
	if err := current.Validate("safe-change"); err == nil {
		t.Fatal("future lifecycle accepted")
	}
}

func TestDecodeRefusesInvalidState(t *testing.T) {
	valid, _ := json.Marshal(Initial("cache-ttl", "create-1"))
	cases := map[string]string{
		"malformed":       `{`,
		"duplicate":       strings.Replace(string(valid), `"change":"cache-ttl"`, `"change":"cache-ttl","change":"other"`, 1),
		"future":          strings.Replace(string(valid), `"schemaVersion":1`, `"schemaVersion":2`, 1),
		"unknown":         strings.Replace(string(valid), `"revision":1`, `"revision":1,"mystery":true`, 1),
		"impossible":      strings.Replace(string(valid), `"condition":"active"`, `"condition":"archived"`, 1),
		"zero revision":   strings.Replace(string(valid), `"revision":1`, `"revision":0`, 1),
		"identity":        strings.Replace(string(valid), `"change":"cache-ttl"`, `"change":"other"`, 1),
		"trailing object": string(valid) + `{}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(data), "cache-ttl"); err == nil {
				t.Fatal("accepted invalid state")
			}
		})
	}
}

func TestDecodeRefusalNamesExternalRepair(t *testing.T) {
	valid, _ := json.Marshal(Initial("cache-ttl", "create-1"))
	cases := map[string]struct {
		data string
		code string
	}{
		"malformed": {`{`, CodeCorrupt},
		"duplicate": {strings.Replace(string(valid), `"change":"cache-ttl"`,
			`"change":"cache-ttl","change":"other"`, 1), CodeCorrupt},
		"future": {strings.Replace(string(valid), `"schemaVersion":1`,
			`"schemaVersion":2`, 1), CodeSchema},
		"identity": {strings.Replace(string(valid), `"change":"cache-ttl"`,
			`"change":"other"`, 1), CodeIdentity},
		"trailing": {string(valid) + `{}`, CodeCorrupt},
		"unknown": {strings.Replace(string(valid), `"revision":1`,
			`"revision":1,"mystery":true`, 1), CodeCorrupt},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(want.data), "cache-ttl")
			var refusal *failure.Refusal
			// Wrapping by callers must not hide the typed refusal.
			if !errors.As(fmt.Errorf("load state: %w", err), &refusal) {
				t.Fatalf("not a refusal: %v", err)
			}
			if refusal.Code != want.code {
				t.Fatalf("code %q, want %q", refusal.Code, want.code)
			}
			if refusal.Next != "repair corrupt state outside automated mutation" {
				t.Fatalf("next action %q", refusal.Next)
			}
			if refusal.Reason == "" {
				t.Fatal("empty reason")
			}
		})
	}
}

func TestDecodePreservesExtensions(t *testing.T) {
	s := Initial("cache-ttl", "create-1")
	s.Extensions = map[string]json.RawMessage{"tool": json.RawMessage(`{"x":1}`)}
	data, _ := Encode(s)
	got, err := Decode(data, s.Change)
	var extension map[string]int
	decodeErr := json.Unmarshal(got.Extensions["tool"], &extension)
	if err != nil || decodeErr != nil || extension["x"] != 1 {
		t.Fatalf("extension lost: %s, %v", got.Extensions["tool"], err)
	}
}
