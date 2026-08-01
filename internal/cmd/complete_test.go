package cmd

import (
	"os"
	"testing"
)

func TestCompleteRefusesUnauthorizedWithoutMutation(t *testing.T) {
	root := statusReadinessRoot(t)
	path := mustStatePath(t, root, "safe-change")
	before, _ := os.ReadFile(path)
	if _, err := Complete(root, "safe-change", "active", 2, CompleteOptions{}); err == nil {
		t.Fatal("unauthorized completion passed")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("unauthorized completion mutated state")
	}
}
