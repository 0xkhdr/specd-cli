package agentjson

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var credentialPattern = regexp.MustCompile(
	`(?i)(authorization:\s*(?:bearer|basic)\s+|(?:api[_-]?key|token|password|secret)\s*[=:]\s*)[^\s]+`)

// ExcerptLimit bounds any process output that reaches the envelope. The full
// stream stays in stage-5 evidence; the envelope carries a sample and a way
// back to the authoritative record.
const ExcerptLimit = 2048

// Bounded is process output as the envelope may carry it: sampled, redacted,
// and always paired with the digest and evidence reference that make the
// truncation traceable.
type Bounded struct {
	Excerpt   string
	Truncated bool
	Digest    string
	Evidence  string
}

// Bound samples one stream. Binary bytes, control characters, and secret-like
// text cannot survive it, and an oversized stream is cut, never widened.
//
// ponytail: verification already redacts what it records, so this is the second
// boundary rather than the only one; fold the two patterns into one owner if a
// third caller ever needs redaction.
func Bound(text, digest, evidence string) Bounded {
	bounded := Bounded{Digest: digest, Evidence: evidence}
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
		bounded.Truncated = true
	}
	text = redact(scrub(text))
	if len(text) > ExcerptLimit {
		cut := ExcerptLimit
		for cut > 0 && !utf8.ValidString(text[:cut]) {
			cut--
		}
		text, bounded.Truncated = text[:cut], true
	}
	bounded.Excerpt = text
	return bounded
}

// Fields projects the bounded stream into envelope data keys. An excerpt with
// no digest or evidence reference is refused: a sample that cannot be traced
// back to the authoritative record never ships.
func (bounded Bounded) Fields(prefix string) (map[string]any, error) {
	if strings.TrimSpace(bounded.Digest) == "" {
		return nil, refuse("output_unreferenced", prefix+" output has no digest",
			"record the full-stream digest with the evidence")
	}
	if strings.TrimSpace(bounded.Evidence) == "" {
		return nil, refuse("output_unreferenced", prefix+" output has no evidence reference",
			"record the evidence entry before encoding its output")
	}
	fields := map[string]any{
		prefix + "_digest":    bounded.Digest,
		prefix + "_evidence":  bounded.Evidence,
		prefix + "_truncated": bounded.Truncated,
	}
	if bounded.Excerpt != "" {
		fields[prefix+"_excerpt"] = bounded.Excerpt
	}
	return fields, nil
}

func scrub(text string) string {
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' {
			return character
		}
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, text)
}

// redact removes credential-shaped assignments. It reuses the shape stage 5
// already redacts so a value cannot become readable by changing surface.
func redact(text string) string {
	return credentialPattern.ReplaceAllString(text, "${1}[REDACTED]")
}
