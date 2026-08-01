package core

import (
	"slices"
	"strings"
	"testing"
)

func TestOperationRegistry(t *testing.T) {
	t.Run("canonical set is exact and ordered", func(t *testing.T) {
		want := []string{
			"init", "new", "check", "approve", "status", "next",
			"context", "start", "verify", "complete", "review", "sync", "archive",
			"report", "friction",
		}
		var got []string
		for _, operation := range Operations() {
			got = append(got, operation.ID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("registry order = %v, want %v", got, want)
		}
	})

	t.Run("complete metadata validates", func(t *testing.T) {
		if err := ValidateOperations(); err != nil {
			t.Fatalf("canonical registry must validate: %v", err)
		}
	})

	t.Run("copies do not alias the registry", func(t *testing.T) {
		first := Operations()
		first[0].Flags[0].Name = "--mutated"
		if Operations()[0].Flags[0].Name != "--root" {
			t.Fatal("Operations() must return independent copies")
		}
	})

	t.Run("agent palette excludes the human boundary", func(t *testing.T) {
		for _, operation := range AgentOperations() {
			if operation.ID == "approve" {
				t.Fatal("approve must never appear in the agent palette")
			}
			if operation.Actor == ActorHuman || !operation.Executable {
				t.Fatalf("%s: agent palette must stay executable and non-human", operation.ID)
			}
		}
		approve, found := OperationByID("approve")
		if !found {
			t.Fatal("approve must stay registered")
		}
		if approve.Actor != ActorHuman || approve.AgentVisible {
			t.Fatalf("approve = %+v, want human-only and hidden", approve)
		}
	})

	t.Run("stage 7 contracts are callable and keep the human boundary", func(t *testing.T) {
		for _, id := range []string{"sync", "archive"} {
			operation, found := OperationByID(id)
			if !found {
				t.Fatalf("%s must be registered", id)
			}
			if !operation.Executable || operation.ResultType == "" {
				t.Fatalf("%s = %+v, want callable with a declared result type", id, operation)
			}
			success := false
			for _, exit := range operation.Exits {
				success = success || exit.Class == ExitClassSuccess
			}
			if !success {
				t.Fatalf("%s must advertise a success exit", id)
			}
		}
		// Accepted truth changes only under a human authorization, so sync is
		// never in the agent palette and never carries an agent-supplied
		// identity. Archive moves an already-authorized change and stays
		// agent-callable.
		sync, _ := OperationByID("sync")
		if sync.Actor != ActorHuman || sync.AgentVisible ||
			sync.AuthoritySource != AuthorityHumanID {
			t.Fatalf("sync = %+v, want a human-only content-bound gate", sync)
		}
		archive, _ := OperationByID("archive")
		if archive.Actor != ActorEither || !archive.AgentVisible {
			t.Fatalf("archive = %+v, want agent callable", archive)
		}
	})

	t.Run("unknown id resolves to nothing", func(t *testing.T) {
		if _, found := OperationByID("sync-all"); found {
			t.Fatal("a near miss must not resolve to a neighbour")
		}
	})

	t.Run("usage renders from metadata", func(t *testing.T) {
		start, _ := OperationByID("start")
		want := "specd start <change> <task> [--root <root>] [--json] --revision <revision>"
		if start.Usage() != want {
			t.Fatalf("usage = %q, want %q", start.Usage(), want)
		}
	})

	t.Run("lifecycle applicability is declared, not defaulted", func(t *testing.T) {
		approve, _ := OperationByID("approve")
		if !approve.AppliesTo("planning") || approve.AppliesTo("approved") {
			t.Fatal("approve applies to planning only")
		}
		complete, _ := OperationByID("complete")
		if !complete.AppliesTo("approved") || complete.AppliesTo("planning") {
			t.Fatal("complete applies to approved only")
		}
		init, _ := OperationByID("init")
		if !init.AppliesTo("archived") {
			t.Fatal("an operation resolving no change applies in every lifecycle")
		}
	})

	t.Run("malformed metadata fails closed", func(t *testing.T) {
		valid := func() Operation {
			operation, _ := OperationByID("status")
			return operation
		}
		cases := map[string]func(Operation) Operation{
			"missing summary": func(o Operation) Operation { o.Summary = ""; return o },
			"unknown actor":   func(o Operation) Operation { o.Actor = "operator"; return o },
			"unknown effect":  func(o Operation) Operation { o.Effect = "external"; return o },
			"unknown authority": func(o Operation) Operation {
				o.AuthoritySource = "trust_me"
				return o
			},
			"unknown scope": func(o Operation) Operation { o.ScopeSource = "everything"; return o },
			"unknown lifecycle": func(o Operation) Operation {
				o.Lifecycles = []Lifecycle{"shipped"}
				return o
			},
			"missing lifecycle for change": func(o Operation) Operation { o.Lifecycles = nil; return o },
			"missing result type":          func(o Operation) Operation { o.ResultType = ""; return o },
			"human but agent visible": func(o Operation) Operation {
				o.Actor = ActorHuman
				return o
			},
			"reserved but callable": func(o Operation) Operation {
				o.Executable = false
				return o
			},
			"unknown flag type": func(o Operation) Operation {
				o.Flags = append(slices.Clone(o.Flags), OperationFlag{
					Name: "--mode", Type: "enum", Description: "x",
				})
				return o
			},
			"default outside enum": func(o Operation) Operation {
				o.Flags = append(slices.Clone(o.Flags), OperationFlag{
					Name: "--mode", Type: "string", Enum: []string{"fast"},
					Default: "slow", Description: "x",
				})
				return o
			},
			"missing global flag":  func(o Operation) Operation { o.Flags = nil; return o },
			"missing exit classes": func(o Operation) Operation { o.Exits = nil; return o },
			"unknown exit class": func(o Operation) Operation {
				o.Exits = []OperationExit{{Code: 3, Class: "maybe", Meaning: "x"}}
				return o
			},
			"required after optional": func(o Operation) Operation {
				o.Arguments = []OperationArgument{
					{Name: "change", Description: "x"},
					{Name: "task", Required: true, Description: "x"},
				}
				return o
			},
			"unsafe example": func(o Operation) Operation {
				o.Example = "specd status safe-create; rm -rf /"
				return o
			},
			"example names another operation": func(o Operation) Operation {
				o.Example = "specd next safe-create"
				return o
			},
		}
		for name, mutate := range cases {
			t.Run(name, func(t *testing.T) {
				if err := validateOperations([]Operation{mutate(valid())}); err == nil {
					t.Fatalf("%s must fail registry validation", name)
				}
			})
		}
	})

	t.Run("duplicate identity fails closed", func(t *testing.T) {
		status, _ := OperationByID("status")
		if err := validateOperations([]Operation{status, status}); err == nil {
			t.Fatal("duplicate id must fail registry validation")
		}
		next, _ := OperationByID("next")
		next.ID = "peek"
		next.Example = "specd peek safe-create"
		next.ResultType = status.ResultType
		if err := validateOperations([]Operation{status, next}); err == nil {
			t.Fatal("duplicate result type must fail registry validation")
		}
		if err := validateOperations(nil); err == nil {
			t.Fatal("an empty registry must fail validation")
		}
	})

	t.Run("examples stay display data", func(t *testing.T) {
		for _, operation := range Operations() {
			if !strings.HasPrefix(operation.Example, "specd "+operation.ID) {
				t.Fatalf("%s: example %q must invoke its own operation", operation.ID, operation.Example)
			}
			if strings.ContainsAny(operation.Example, unsafeInExample) {
				t.Fatalf("%s: example %q must be shell safe", operation.ID, operation.Example)
			}
		}
	})
}
