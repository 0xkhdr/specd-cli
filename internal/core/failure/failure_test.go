package failure

import (
	"errors"
	"fmt"
	"testing"
)

// TestNewRejectsIncompleteRefusal proves the recovery-action invariant is total
// rather than spot-checked per package: no refusal can exist without a code, a
// reason, and one legal next action.
func TestNewRejectsIncompleteRefusal(t *testing.T) {
	cases := map[string]struct{ code, reason, next string }{
		"empty code":   {"", "reason", "next"},
		"empty reason": {"lock_busy", "", "next"},
		"empty next":   {"lock_busy", "reason", ""},
	}
	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("incomplete refusal accepted")
				}
			}()
			New(item.code, "/root", "/root/path", item.reason, item.next)
		})
	}
}

func TestNewKeepsFieldsAndUnwraps(t *testing.T) {
	refusal := New("lock_busy", "/root", "/root/lock", "held too long", "retry the operation")
	if !IsCode(fmt.Errorf("wrapped: %w", refusal), "lock_busy") {
		t.Fatal("wrapped refusal did not match its code")
	}
	if IsCode(errors.New("plain"), "lock_busy") {
		t.Fatal("plain error matched a refusal code")
	}
	if got := refusal.Error(); got != "lock_busy: /root/lock: held too long; next: retry the operation" {
		t.Fatalf("unexpected message: %q", got)
	}
	pathless := New("lock_busy", "/root", "", "held too long", "retry the operation")
	if got := pathless.Error(); got != "lock_busy: held too long; next: retry the operation" {
		t.Fatalf("unexpected pathless message: %q", got)
	}
}
