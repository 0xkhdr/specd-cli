// Package generate renders the one managed agent guidance region from canonical
// operation metadata. It owns no workflow semantics of its own: commands come
// from the operation registry, and everything else is one shared prose template.
package generate

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"text/template"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

//go:embed agents.tmpl
var guidanceTemplate string

var guidance = template.Must(template.New("agents").Parse(guidanceTemplate))

// Guidance is one rendered managed region. Hash identifies Body exactly, so a
// consumer can prove freshness without re-rendering and a hand edit is visible.
type Guidance struct {
	Version int
	Hash    string
	Body    string
}

type guidanceOperation struct {
	ID, Summary, Usage, Effect, Example string
}

// Render projects the agent-visible registry through the shared prose template.
// It is deterministic: registry order, no clock, no environment, no network.
func Render() (Guidance, error) {
	facts := core.ProjectAgentOperations()
	rows := make([]guidanceOperation, 0, len(facts.Operations))
	for _, operation := range facts.Operations {
		// A registered contract with no handler is not callable, so it is not
		// advertised. Visibility alone never puts a verb in the palette.
		if !operation.Executable {
			continue
		}
		rows = append(rows, guidanceOperation{
			ID: operation.ID, Summary: operation.Summary, Usage: operation.Usage(),
			Effect: string(operation.Effect), Example: operation.Example,
		})
	}
	var out bytes.Buffer
	if err := guidance.Execute(&out, struct {
		Version    int
		Operations []guidanceOperation
	}{Version: facts.SchemaVersion, Operations: rows}); err != nil {
		return Guidance{}, failure.New("guidance_render", "", "", err.Error(),
			"repair internal/generate/agents.tmpl and regenerate the guidance")
	}
	body := out.String()
	return Guidance{Version: facts.SchemaVersion, Hash: hash(body), Body: body}, nil
}

func hash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
