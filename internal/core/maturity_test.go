package core

import (
	"reflect"
	"testing"
)

func TestMaturityClaimsAreStableAndIsolated(t *testing.T) {
	first, second := MaturityClaims(), MaturityClaims()
	if len(first) == 0 || !reflect.DeepEqual(first, second) {
		t.Fatalf("maturity claims are not stable: %#v %#v", first, second)
	}
	first[0].Subject = "changed"
	if reflect.DeepEqual(first, MaturityClaims()) {
		t.Fatal("caller mutated the maturity registry")
	}
}
