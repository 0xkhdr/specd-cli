package core

import (
	"errors"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func assertActionableRefusal(t *testing.T, err error, code string) *failure.Refusal {
	t.Helper()
	var refusal *failure.Refusal
	if !errors.As(err, &refusal) || refusal.Code != code || refusal.Next == "" {
		t.Fatalf("refusal = %#v, want code %q and one legal next action", err, code)
	}
	return refusal
}
