package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/cmd"
	"github.com/0xkhdr/specd-cli/internal/core"
)

// run drives the CLI exactly as main does and returns the two streams.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return document
}

func TestStageOneOperations(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--root", root, "--json"},
		{"new", "cache-ttl", "--root", root, "--json"},
		{"status", "cache-ttl", "--root", root, "--json"},
	} {
		code, stdout, stderr := run(t, args...)
		if code != 0 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
		document := decode(t, stdout)
		if got := document["root"].(map[string]any)["path"]; got != root {
			t.Fatalf("root = %v, want %s", got, root)
		}
		if document["schema"] != "specd.agent/v1" {
			t.Fatalf("%v emitted %v", args, document["schema"])
		}
	}
	_, stdout, _ := run(t, "status", "cache-ttl", "--root", root, "--json")
	if !strings.Contains(stdout, `"revision":1`) {
		t.Fatalf("status = %s", stdout)
	}
}

// TestRefusalIsOneDocumentOnStdout pins the surface contract: success and
// failure both leave exactly one envelope on stdout, and stderr stays reserved
// for failures that prevent an envelope.
func TestRefusalIsOneDocumentOnStdout(t *testing.T) {
	root := t.TempDir()
	if code, _, stderr := run(t, "init", "--root", root); code != 0 {
		t.Fatal(stderr)
	}
	code, stdout, stderr := run(t, "status", "missing", "--root", root, "--json")
	if code != 1 {
		t.Fatalf("code = %d: %s%s", code, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("refusal wrote to stderr: %s", stderr)
	}
	document := decode(t, stdout)
	if document["ok"] != false || document["root"].(map[string]any)["path"] != root {
		t.Fatalf("refusal envelope = %s", stdout)
	}
	if next := document["next"].(map[string]any); next["instruction"] == "" {
		t.Fatalf("refusal named no next action: %s", stdout)
	}
	if diagnostics := document["diagnostics"].([]any); len(diagnostics) != 1 {
		t.Fatalf("refusal carried %d diagnostics", len(diagnostics))
	}
}

func scaffolded(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "--root", root}, {"new", "cache-ttl", "--root", root}} {
		if code, stdout, stderr := run(t, args...); code != 0 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
	}
	return root
}

func TestBaseLoopOperationsAreReachable(t *testing.T) {
	root := scaffolded(t)
	// A scaffolded change legitimately refuses most of the loop; what must hold
	// is that every verb reaches its handler through the one dispatcher.
	cases := [][]string{
		{"check", "cache-ttl"},
		{"next", "cache-ttl"},
		{"next", "cache-ttl", "T1"},
		{"context", "cache-ttl", "T1"},
		{"start", "cache-ttl", "T1", "--revision", "1"},
		{"verify", "cache-ttl", "T1", "A1"},
		{"complete", "cache-ttl", "T1", "--revision", "1"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			_, stdout, stderr := run(t, append(args, "--root", root)...)
			// Reaching the dispatcher is what is proven here: a scaffolded
			// change legitimately refuses on lifecycle or readiness grounds, but
			// the refusal is always one rendered envelope naming this operation.
			if stderr != "" || !strings.Contains(stdout, "operation: "+args[0]) {
				t.Fatalf("%v is not wired: out=%s err=%s", args, stdout, stderr)
			}
			if got := strings.Count(stdout, "next: "); got != 1 {
				t.Fatalf("%v named %d next actions: %s", args, got, stdout)
			}
		})
	}
}

// TestHumanGateRoutes proves the same binary refuses the human verb on an
// agent-capable route and reaches its handler on the declared human route.
func TestHumanGateRoutes(t *testing.T) {
	root := scaffolded(t)
	t.Setenv(RouteVariable, string(cmd.RouteAgent))
	code, stdout, stderr := run(t, "approve", "cache-ttl", "--root", root, "--json")
	if code != 2 || stderr != "" {
		t.Fatalf("agent approve: code=%d out=%s err=%s", code, stdout, stderr)
	}
	document := decode(t, stdout)
	next := document["next"].(map[string]any)
	if next["kind"] != "human_handoff" || next["owner"] != "human" {
		t.Fatalf("agent approve envelope = %s", stdout)
	}

	t.Setenv(RouteVariable, string(cmd.RouteHumanTerminal))
	// The scaffolded plan is not approvable, so the handler refuses on its own
	// terms — exit 1, not the usage class, which proves the route reached it.
	if code, stdout, _ := run(t, "approve", "cache-ttl", "--root", root,
		"--approver", "human@example.com", "--reason", "why"); code != 1 {
		t.Fatalf("human approve: code=%d out=%s", code, stdout)
	}
	t.Setenv(RouteVariable, "sudo")
	if code, _, stderr := run(t, "status", "cache-ttl", "--root", root); code != 2 ||
		!strings.Contains(stderr, "route_unknown") {
		t.Fatalf("unknown route: code=%d err=%s", code, stderr)
	}
}

// TestDerivedRouteNeedsARealTerminal pins the derivation to a kernel fact. Every
// stdin an agent actually inherits must derive the agent route: /dev/null is the
// one that matters, because it is a character device and a character-device test
// let an agent shell derive the human route and reach the approval gate.
func TestDerivedRouteNeedsARealTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	regular, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readEnd.Close()
	defer writeEnd.Close()

	for name, stdin := range map[string]*os.File{
		"dev null": devNull, "regular file": regular, "pipe": readEnd, "closed": nil,
	} {
		t.Run(name, func(t *testing.T) {
			route, err := selectRoute("", stdin)
			if err != nil {
				t.Fatal(err)
			}
			if route != cmd.RouteAgent {
				t.Fatalf("%s derived %q, want %q", name, route, cmd.RouteAgent)
			}
		})
	}

	// A declared route is still provenance the host owns, and it stays honored.
	route, err := selectRoute(string(cmd.RouteHumanTerminal), devNull)
	if err != nil || route != cmd.RouteHumanTerminal {
		t.Fatalf("declared human route = %q, %v", route, err)
	}
}

func TestUsageRefusalsExitTwo(t *testing.T) {
	root := scaffolded(t)
	cases := [][]string{
		{"check"},
		{"context", "cache-ttl"},
		{"verify", "cache-ttl", "T1"},
		{"start", "cache-ttl", "T1"},
		{"complete", "cache-ttl", "T1", "--revision", "one"},
		{"check", "cache-ttl", "--force"},
		{"sync", "cache-ttl"},
	}
	for _, args := range cases {
		code, stdout, stderr := run(t, append(args, "--root", root)...)
		if code != 2 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
		if got := strings.Count(stdout, "next: "); got != 1 {
			t.Fatalf("%v named %d next actions: %s", args, got, stdout)
		}
		if stderr != "" {
			t.Fatalf("%v wrote to stderr: %s", args, stderr)
		}
	}
	// An unregistered verb has no envelope to be reported in: it is the one
	// refusal that goes to stderr, and it still names one next action.
	code, stdout, stderr := run(t, "resume", "cache-ttl", "--root", root)
	if code != 2 || stdout != "" || strings.Count(stderr, "next: ") != 1 {
		t.Fatalf("unknown operation: code=%d out=%s err=%s", code, stdout, stderr)
	}
}

func TestFailedCheckExitsOneOnBothSurfaces(t *testing.T) {
	root := scaffolded(t)
	code, stdout, _ := run(t, "check", "cache-ttl", "--root", root)
	if code != 1 || !strings.Contains(stdout, "exit: 1 failure") {
		t.Fatalf("text check: code=%d out=%s", code, stdout)
	}
	jsonCode, jsonOut, _ := run(t, "check", "cache-ttl", "--root", root, "--json")
	if jsonCode != code {
		t.Fatalf("json check exited %d, text exited %d", jsonCode, code)
	}
	if decode(t, jsonOut)["ok"] != false {
		t.Fatalf("json check = %s", jsonOut)
	}
}

func TestMainDefaultRoot(t *testing.T) {
	root := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if code, stdout, stderr := run(t, "init"); code != 0 {
		t.Fatalf("code=%d out=%s err=%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".specd")); err != nil {
		t.Fatal(err)
	}
}

// TestIntrinsicHelpAndVersion pins the two argv forms a caller reaches for
// before it has a root: they succeed, they go to stdout, help names every
// executable operation, and neither shadows a registered id.
func TestIntrinsicHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		code, stdout, stderr := run(t, args...)
		if code != 0 || stderr != "" {
			t.Fatalf("%v: code=%d err=%s", args, code, stderr)
		}
		for _, operation := range core.Operations() {
			if operation.Executable && !strings.Contains(stdout, operation.ID) {
				t.Fatalf("%v omitted operation %q", args, operation.ID)
			}
		}
	}
	for _, args := range [][]string{{"--version"}, {"-v"}, {"version"}} {
		code, stdout, stderr := run(t, args...)
		if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "specd ") {
			t.Fatalf("%v: code=%d out=%s err=%s", args, code, stdout, stderr)
		}
	}
	// A registered operation is never answered here, even when it takes no
	// argument: registering `help` or `version` later shadows the shortcut.
	root := scaffolded(t)
	if code, _, _ := run(t, "status", "cache-ttl", "--root", root); code != 0 {
		t.Fatalf("registered operation was intercepted: code=%d", code)
	}
}
