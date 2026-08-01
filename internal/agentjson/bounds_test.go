package agentjson

import (
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

const (
	testDigest   = "b1946ac92492d2347c6235b4d2611184"
	testEvidence = "ev-000001"
)

func TestBoundedOutputStaysInsideLimits(t *testing.T) {
	oversized := strings.Repeat("a", ExcerptLimit*3)
	bounded := Bound(oversized, testDigest, testEvidence)
	if len(bounded.Excerpt) > ExcerptLimit || !bounded.Truncated {
		t.Fatalf("excerpt %d bytes truncated=%t", len(bounded.Excerpt), bounded.Truncated)
	}
	if bounded.Digest != testDigest || bounded.Evidence != testEvidence {
		t.Fatalf("truncation lost its reference: %+v", bounded)
	}
	binary := Bound("head\x00\x01\x02\xffbody", testDigest, testEvidence)
	if strings.ContainsAny(binary.Excerpt, "\x00\x01\x02") {
		t.Fatalf("binary bytes survived: %q", binary.Excerpt)
	}
	secret := Bound("token=hunter2 Authorization: Bearer abc.def", testDigest, testEvidence)
	if strings.Contains(secret.Excerpt, "hunter2") || strings.Contains(secret.Excerpt, "abc.def") {
		t.Fatalf("secret survived: %q", secret.Excerpt)
	}
	if !strings.Contains(secret.Excerpt, "[REDACTED]") {
		t.Fatalf("redaction is not visible: %q", secret.Excerpt)
	}
}

func TestBoundedOutputRequiresEvidenceReference(t *testing.T) {
	if _, err := Bound("out", "", testEvidence).Fields("stdout"); !failure.IsCode(err, "output_unreferenced") {
		t.Fatalf("expected refusal without digest, got %v", err)
	}
	if _, err := Bound("out", testDigest, "").Fields("stdout"); !failure.IsCode(err, "output_unreferenced") {
		t.Fatalf("expected refusal without evidence, got %v", err)
	}
	fields, err := Bound("out", testDigest, testEvidence).Fields("stdout")
	if err != nil {
		t.Fatalf("fields: %v", err)
	}
	for key, want := range map[string]any{
		"stdout_excerpt": "out", "stdout_digest": testDigest,
		"stdout_evidence": testEvidence, "stdout_truncated": false,
	} {
		if fields[key] != want {
			t.Fatalf("field %s = %v, want %v", key, fields[key], want)
		}
	}
	// Bounded output must survive the envelope's own data rules.
	if err := validateData(fields); err != nil {
		t.Fatalf("bounded fields rejected by the envelope: %v", err)
	}
}
