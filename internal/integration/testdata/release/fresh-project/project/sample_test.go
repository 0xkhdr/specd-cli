package sample

import "testing"

func TestSampleLoop(t *testing.T) {
	if Sample() != "after" {
		t.Fatalf("greeting = %q", Sample())
	}
}
