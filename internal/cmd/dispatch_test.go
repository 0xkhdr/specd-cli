package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

func TestOperationDispatch(t *testing.T) {
	t.Run("registry and handlers are one to one", func(t *testing.T) {
		bound := map[string]bool{}
		for _, operation := range core.Operations() {
			handler, ok := handlers[operation.ID]
			switch {
			case operation.Executable && (!ok || handler == nil):
				t.Fatalf("executable operation %q has no handler", operation.ID)
			case !operation.Executable && ok:
				t.Fatalf("reserved operation %q must not resolve a handler", operation.ID)
			}
			bound[operation.ID] = true
		}
		for id := range handlers {
			if !bound[id] {
				t.Fatalf("handler %q has no registry entry", id)
			}
		}
	})

	t.Run("invalid invocations reach no handler", func(t *testing.T) {
		root := loopFixture(t)
		mustDispatch(t, root, "init")
		mustDispatch(t, root, "new", "sample-loop")
		before := revisionOf(t, root, "sample-loop")

		cases := map[string][]string{
			"no operation":          {},
			"unknown operation":     {"resume", "sample-loop"},
			"reserved operation":    {"sync", "sample-loop"},
			"unknown flag":          {"status", "sample-loop", "--verbose"},
			"flag without value":    {"status", "sample-loop", "--root"},
			"repeated flag":         {"status", "sample-loop", "--root", "a", "--root", "b"},
			"invalid flag value":    {"complete", "sample-loop", "edit-sample", "--revision", "soon"},
			"missing required":      {"complete", "sample-loop", "edit-sample"},
			"reopen missing reason": {"reopen", "sample-loop", "--revision", "1"},
			"too few arguments":     {"context", "sample-loop"},
			"too many arguments":    {"status", "sample-loop", "extra"},
			"empty selector":        {"status", "  "},
			"ambiguous root":        {"init", root, "--root", root},
			"lifecycle mismatch":    {"complete", "sample-loop", "edit-sample", "--revision", "1"},
			"agent asks for human":  {"approve", "sample-loop"},
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				request := Request{Args: args, Root: root, Actor: "agent:builder", Route: RouteAgent}
				result, err := Dispatch(context.Background(), request)
				if err == nil {
					t.Fatalf("%v dispatched to a handler: %#v", args, result)
				}
				var refusal *UsageRefusal
				if !errors.As(err, &refusal) {
					t.Fatalf("%v = %v, want a fail-closed usage refusal", args, err)
				}
				if ExitCode(err) != 2 {
					t.Fatalf("%v exit = %d, want 2", args, ExitCode(err))
				}
				if refusal.Next == "" || refusal.Reason == "" {
					t.Fatalf("%v refusal %#v must name one next action", args, refusal)
				}
			})
		}
		if after := revisionOf(t, root, "sample-loop"); after != before {
			t.Fatalf("revision moved from %d to %d during refusals", before, after)
		}
	})

	t.Run("human boundary refuses the agent route only", func(t *testing.T) {
		root := loopFixture(t)
		mustDispatch(t, root, "init")
		mustDispatch(t, root, "new", "sample-loop")

		_, err := Dispatch(context.Background(), Request{
			Args: []string{"approve", "sample-loop"}, Root: root, Route: RouteAgent,
		})
		var refusal *failure.Refusal
		if !errors.As(err, &refusal) || refusal.Code != "human_approval_required" {
			t.Fatalf("agent approve = %v, want the canonical human approval boundary", err)
		}
		// The same operation on a human route reaches its handler, which then
		// refuses on its own terms: exit class 1, not the usage class.
		_, err = Dispatch(context.Background(), Request{
			Args: []string{"approve", "sample-loop"}, Root: root, Route: RouteHumanTerminal,
		})
		if err == nil {
			t.Fatal("approve of a scaffolded plan must fail in the handler")
		}
		var usage *UsageRefusal
		if errors.As(err, &usage) {
			t.Fatalf("human route must reach the handler, got %v", err)
		}
		if ExitCode(err) != 1 {
			t.Fatalf("handler refusal exit = %d, want 1", ExitCode(err))
		}
	})

	t.Run("authority provenance is checked before the handler", func(t *testing.T) {
		root := loopFixture(t)
		mustDispatch(t, root, "init")
		mustDispatch(t, root, "new", "sample-loop")
		_, err := Dispatch(context.Background(), Request{
			Args: []string{"start", "sample-loop", "edit-sample", "--revision", "1"},
			Root: root, Route: RouteAgent,
		})
		var refusal *UsageRefusal
		if !errors.As(err, &refusal) || refusal.Code != "authority_missing" {
			t.Fatalf("start without an actor = %v, want an authority refusal", err)
		}
	})

	t.Run("declared enums and defaults bound flag values", func(t *testing.T) {
		operation, _ := core.OperationByID("status")
		operation.Flags = append(operation.Flags, core.OperationFlag{
			Name: "--mode", Type: "string", Enum: []string{"fast", "full"},
			Default: "fast", Description: "Test-only enum flag.",
		})
		registry := []core.Operation{operation}

		in, err := resolve(registry, Request{
			Args: []string{"status", "sample-loop"}, Root: "/tmp", Route: RouteAgent,
		})
		if err != nil {
			t.Fatalf("default value refused: %v", err)
		}
		if value[string](in, "--mode") != "fast" {
			t.Fatalf("declared default not applied: %#v", in.values)
		}
		if _, err := resolve(registry, Request{
			Args: []string{"status", "sample-loop", "--mode", "turbo"}, Root: "/tmp", Route: RouteAgent,
		}); !errors.As(err, new(*UsageRefusal)) {
			t.Fatalf("unknown enum value = %v, want a usage refusal", err)
		}
	})

	t.Run("valid invocations run exactly one handler", func(t *testing.T) {
		root := loopFixture(t)
		if _, ok := mustDispatch(t, root, "init").Value.(InitResult); !ok {
			t.Fatal("init must return its declared result type")
		}
		if _, ok := mustDispatch(t, root, "new", "sample-loop", "--capability", "sample").Value.(NewResult); !ok {
			t.Fatal("new must return its declared result type")
		}
		if _, ok := mustDispatch(t, root, "status", "sample-loop").Value.(StatusResult); !ok {
			t.Fatal("status must return its declared result type")
		}
		// A scaffolded plan reports blocking findings: the declared failure
		// exit class, not a refusal and not success.
		checked, err := Dispatch(context.Background(), Request{
			Args: []string{"check", "sample-loop"}, Root: root, Route: RouteAgent,
		})
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if checked.Exit != 1 {
			t.Fatalf("check of a scaffolded plan exit = %d, want 1", checked.Exit)
		}
	})
}

func mustDispatch(t *testing.T, root string, args ...string) Result {
	t.Helper()
	result, err := Dispatch(context.Background(), Request{
		Args: args, Root: root, Actor: "agent:builder", Route: RouteAgent,
	})
	if err != nil {
		t.Fatalf("dispatch %v: %v", args, err)
	}
	if result.Exit != 0 {
		t.Fatalf("dispatch %v exit = %d, want 0", args, result.Exit)
	}
	return result
}

func revisionOf(t *testing.T, root, change string) uint64 {
	t.Helper()
	snapshot, err := core.LoadReadinessSnapshot(root, change)
	if err != nil {
		t.Fatalf("load readiness: %v", err)
	}
	return snapshot.StateRevision()
}
