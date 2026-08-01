package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// nonTerminal returns a real file that is not a character device, so the
// derived interactivity is non-interactive without a pseudo-terminal.
func nonTerminal(t *testing.T) *os.File {
	t.Helper()
	file, err := os.Create(filepath.Join(tempRoot(t), "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestApproveIdentityIntentRefusesBeforeFilesystem(t *testing.T) {
	cases := []struct {
		name     string
		options  ApproveOptions
		git, env string
	}{
		{"missing", ApproveOptions{Approver: "a", Reason: "why"}, "", ""},
		{"ambiguous", ApproveOptions{Approver: "a", Reason: "why"}, "a", "b"},
		{"forged", ApproveOptions{Approver: "b", Reason: "why"}, "a", ""},
		{"missing reason", ApproveOptions{Approver: "a"}, "a", ""},
		{"non-interactive without approver", ApproveOptions{Reason: "why"}, "a", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var prompt bytes.Buffer
			options := test.options
			options.Input, options.Output = nonTerminal(t), &prompt
			if _, err := approveWithSources("/does/not/exist", "safe-change", options, test.git, test.env); err == nil {
				t.Fatal("invalid identity or intent reached filesystem")
			}
			if prompt.Len() != 0 {
				t.Fatalf("non-interactive call prompted: %q", prompt.String())
			}
		})
	}
}

func TestApproveDerivesInteractiveConfirmation(t *testing.T) {
	cases := []struct {
		answer   string
		refusing bool
	}{
		{"y\n", false},
		{"yes\n", false},
		{"n\n", true},
		{"\n", true},
		{"", true},
	}
	for _, test := range cases {
		t.Run(strings.TrimSpace(test.answer), func(t *testing.T) {
			var prompt bytes.Buffer
			// A non-file reader stands in for the terminal, so interactivity is
			// derived, and only the answer can confirm.
			options := ApproveOptions{Reason: "why", Input: strings.NewReader(test.answer), Output: &prompt}
			_, err := approveWithSources("/does/not/exist", "safe-change", options, "a", "")
			if err == nil {
				t.Fatal("missing root reached approval")
			}
			if got := failure.IsCode(err, "approval_intent"); got != test.refusing {
				t.Fatalf("answer %q: approval_intent=%v, err=%v", test.answer, got, err)
			}
			if prompt.Len() == 0 {
				t.Fatal("interactive call did not prompt")
			}
		})
	}
}

func TestApproveOptionsCannotClaimConfirmation(t *testing.T) {
	structure := reflect.TypeOf(ApproveOptions{})
	for index := 0; index < structure.NumField(); index++ {
		if field := structure.Field(index); field.Type.Kind() == reflect.Bool {
			t.Fatalf("ApproveOptions.%s lets a caller claim interactivity or confirmation", field.Name)
		}
	}
}
