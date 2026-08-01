package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const operationDocsPath = "../../docs/operations.md"

func TestOperationProjectionParity(t *testing.T) {
	facts := ProjectOperations()

	t.Run("projection covers every registry field", func(t *testing.T) {
		operationType := reflect.TypeOf(Operation{})
		for _, operation := range facts.Operations {
			projected := map[string]bool{}
			for _, fact := range operation.Facts() {
				if fact.Value == "" {
					t.Fatalf("%s: field %q projects an empty value", operation.ID, fact.Field)
				}
				projected[fact.Field] = true
			}
			for index := 0; index < operationType.NumField(); index++ {
				name, _, _ := strings.Cut(operationType.Field(index).Tag.Get("json"), ",")
				if name == "" || name == "-" {
					continue
				}
				if !projected[name] {
					t.Fatalf("%s: registry field %q is missing from the projection", operation.ID, name)
				}
			}
		}
	})

	t.Run("every consumer agrees field for field", func(t *testing.T) {
		encoded, err := OperationFactsJSON(facts)
		if err != nil {
			t.Fatalf("json projection: %v", err)
		}
		consumers := map[string]string{
			"terminal help": RenderOperationHelp(facts),
			"docs":          RenderOperationDocs(facts),
			"json schema":   string(encoded),
		}
		for name, rendered := range consumers {
			for _, operation := range facts.Operations {
				if !strings.Contains(rendered, operation.ID) {
					t.Fatalf("%s: operation %q is absent", name, operation.ID)
				}
				for _, fact := range operation.Facts() {
					if factVisible(name, rendered, operation, fact) {
						continue
					}
					t.Fatalf("%s: operation %q field %q value %q is absent",
						name, operation.ID, fact.Field, fact.Value)
				}
			}
		}
	})

	t.Run("schema version travels with the projection", func(t *testing.T) {
		if facts.SchemaVersion != OperationSchemaVersion {
			t.Fatalf("schema = %d, want %d", facts.SchemaVersion, OperationSchemaVersion)
		}
		if !strings.Contains(RenderOperationHelp(facts), "schema 1") {
			t.Fatal("help must carry the projection schema version")
		}
	})

	t.Run("agent projection stays narrower", func(t *testing.T) {
		agent := ProjectAgentOperations()
		if len(agent.Operations) >= len(facts.Operations) {
			t.Fatal("agent projection must be a strict subset")
		}
		for _, operation := range agent.Operations {
			if operation.Actor == ActorHuman || !operation.Executable || !operation.AgentVisible {
				t.Fatalf("%s must not appear in the agent projection", operation.ID)
			}
			registered, found := OperationByID(operation.ID)
			if !found || registered.Actor != operation.Actor ||
				registered.Effect != operation.Effect ||
				registered.ScopeSource != operation.ScopeSource {
				t.Fatalf("%s: agent projection widened actor, effect, or scope", operation.ID)
			}
		}
	})

	t.Run("docs are generated, never hand written", func(t *testing.T) {
		want := RenderOperationDocs(facts)
		if os.Getenv("SPECD_WRITE_OPERATION_DOCS") != "" {
			if err := os.MkdirAll(filepath.Dir(operationDocsPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := WriteOperationDocs(operationDocsPath); err != nil {
				t.Fatal(err)
			}
		}
		raw, err := os.ReadFile(operationDocsPath)
		if err != nil {
			t.Fatalf("%s: %v; regenerate with SPECD_WRITE_OPERATION_DOCS=1", operationDocsPath, err)
		}
		if string(raw) == want {
			return
		}
		for _, operation := range facts.Operations {
			for _, fact := range operation.Facts() {
				if !strings.Contains(string(raw), factRow(fact)) {
					t.Fatalf("%s: operation %q field %q drifted from the registry; "+
						"regenerate with SPECD_WRITE_OPERATION_DOCS=1",
						operationDocsPath, operation.ID, fact.Field)
				}
			}
		}
		t.Fatalf("%s drifted from the projection; regenerate with SPECD_WRITE_OPERATION_DOCS=1",
			operationDocsPath)
	})

	t.Run("projection copies do not alias the registry", func(t *testing.T) {
		mutated := ProjectOperations()
		mutated.Operations[0].Summary = "drifted"
		if ProjectOperations().Operations[0].Summary == "drifted" {
			t.Fatal("projection must return independent copies")
		}
	})
}

func factRow(fact OperationFact) string {
	return "| " + fact.Field + " | " + fact.Value + " |"
}

// factVisible checks one rendered surface for one projected fact. The JSON
// surface carries the structured field rather than the rendered row, so it is
// compared against the registry value it encodes.
func factVisible(consumer, rendered string, operation Operation, fact OperationFact) bool {
	if consumer != "json schema" {
		return strings.Contains(rendered, fact.Value)
	}
	switch fact.Field {
	case "usage", "lifecycles", "arguments", "flags", "exits", "resultType":
		// Composite rows: the JSON surface encodes the underlying fields, so
		// assert on those instead of the rendered summary.
		return jsonCarriesComposite(rendered, operation, fact.Field)
	default:
		return strings.Contains(rendered, fact.Value)
	}
}

func jsonCarriesComposite(rendered string, operation Operation, field string) bool {
	switch field {
	case "usage":
		return true // derived from arguments and flags, both asserted below
	case "resultType":
		return operation.ResultType == "" || strings.Contains(rendered, operation.ResultType)
	case "lifecycles":
		for _, lifecycle := range operation.Lifecycles {
			if !strings.Contains(rendered, string(lifecycle)) {
				return false
			}
		}
		return true
	case "arguments":
		for _, argument := range operation.Arguments {
			if !strings.Contains(rendered, argument.Name) ||
				!strings.Contains(rendered, argument.Description) {
				return false
			}
		}
		return true
	case "flags":
		for _, flag := range operation.Flags {
			if !strings.Contains(rendered, flag.Name) ||
				!strings.Contains(rendered, flag.Description) ||
				!strings.Contains(rendered, flag.Type) {
				return false
			}
		}
		return true
	case "exits":
		for _, exit := range operation.Exits {
			if !strings.Contains(rendered, exit.Class) || !strings.Contains(rendered, exit.Meaning) {
				return false
			}
		}
		return true
	}
	return false
}
