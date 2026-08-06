package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
)

// commandMention finds every `specd <verb>` the guidance prints, so an invented
// or human-only verb cannot hide inside prose.
var commandMention = regexp.MustCompile("`specd ([a-z][a-z0-9-]*)")

func TestAgentGuidance(t *testing.T) {
	first, err := Render()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("deterministic and hash identified", func(t *testing.T) {
		second, err := Render()
		if err != nil {
			t.Fatal(err)
		}
		if first.Body != second.Body || first.Hash != second.Hash || first.Version != second.Version {
			t.Fatal("guidance is not reproducible")
		}
		sum := sha256.Sum256([]byte(first.Body))
		if first.Hash != hex.EncodeToString(sum[:]) {
			t.Fatalf("hash %s does not identify the body", first.Hash)
		}
		if first.Version != core.OperationSchemaVersion {
			t.Fatalf("version = %d, want %d", first.Version, core.OperationSchemaVersion)
		}
	})

	t.Run("teaches every required boundary", func(t *testing.T) {
		required := map[string]string{
			"explicit root selection":    "--root <path>",
			"harness-owned artifacts":    "state.json",
			"human approval boundary":    "human_handoff",
			"loop order":                 "`next` -> `context` -> `start`",
			"declared scope":             "the task may write",
			"managed path prohibition":   "harness-owned path",
			"verify is not completion":   "It never completes",
			"stale and refusal recovery": "fail closed",
			"truthful host assurance":    "advisory",
		}
		for name, text := range required {
			if !strings.Contains(first.Body, text) {
				t.Errorf("guidance does not teach %s (missing %q)", name, text)
			}
		}
	})

	t.Run("projects every maturity claim", func(t *testing.T) {
		for _, claim := range core.MaturityClaims() {
			level := string(claim.Maturity)
			if level == "" {
				level = string(claim.Assurance)
			}
			for _, text := range []string{claim.Category + "/" + claim.Subject, level, claim.Observed, claim.Evidence} {
				if !strings.Contains(first.Body, text) {
					t.Errorf("guidance omits %s claim value %q", claim.Subject, text)
				}
			}
		}
	})

	t.Run("mentions only agent callable operations", func(t *testing.T) {
		callable := map[string]bool{}
		for _, operation := range core.Operations() {
			if operation.AgentVisible && operation.Executable {
				callable[operation.ID] = true
				continue
			}
			// Human-only and reserved ids may not appear at all: not as a
			// command, not as prose.
			if strings.Contains(first.Body, operation.ID) {
				t.Errorf("guidance names non-callable operation %q", operation.ID)
			}
		}
		for _, match := range commandMention.FindAllStringSubmatch(first.Body, -1) {
			if !callable[match[1]] {
				t.Errorf("guidance invents command %q", match[1])
			}
		}
		for id := range callable {
			operation, _ := core.OperationByID(id)
			if !strings.Contains(first.Body, operation.Usage()) ||
				!strings.Contains(first.Body, operation.Example) {
				t.Errorf("guidance omits the usage or example of %q", id)
			}
		}
	})
}
