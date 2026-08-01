// Package integration proves the stage-6 agent contract end to end: every
// public surface is the same registry projection, and one task can be driven to
// completion by a headless agent through JSON and by a human through the
// terminal, with the human gate reachable from exactly one of them.
package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	"github.com/0xkhdr/specd-cli/internal/cli"
	"github.com/0xkhdr/specd-cli/internal/cmd"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/generate"
	"github.com/0xkhdr/specd-cli/internal/host"
)

const (
	change   = "sample-loop"
	taskID   = "edit-sample"
	approver = "human@example.com"
)

func TestAgentContract(t *testing.T) {
	t.Run("surfaces agree with the registry", testSurfaceParity)
	t.Run("journeys agree and only the human passes the gate", testJourneys)
}

// ---------------------------------------------------------------- parity

func testSurfaceParity(t *testing.T) {
	registry := core.Operations()
	ids := make([]string, 0, len(registry))
	agentIDs := make([]string, 0, len(registry))
	for _, operation := range registry {
		ids = append(ids, operation.ID)
		if operation.AgentVisible && operation.Executable {
			agentIDs = append(agentIDs, operation.ID)
		}
	}

	t.Run("docs and help project every registry fact", func(t *testing.T) {
		docs := readFile(t, filepath.Join("..", "..", "docs", "operations.md"))
		help := core.RenderOperationHelp(core.ProjectOperations())
		headings := regexp.MustCompile(`(?m)^## (.+)$`).FindAllStringSubmatch(docs, -1)
		documented := make([]string, 0, len(headings))
		for _, heading := range headings {
			documented = append(documented, heading[1])
		}
		if !slices.Equal(documented, ids) {
			t.Fatalf("docs operations %v, registry %v", documented, ids)
		}
		for _, operation := range registry {
			for _, fact := range operation.Facts() {
				if !strings.Contains(docs, "| "+fact.Field+" | "+fact.Value+" |") {
					t.Errorf("docs omit %s %s", operation.ID, fact.Field)
				}
				if !strings.Contains(help, fact.Value) {
					t.Errorf("help omits %s %s", operation.ID, fact.Field)
				}
			}
		}
	})

	t.Run("the JSON schema surface is the same projection", func(t *testing.T) {
		raw, err := core.OperationFactsJSON(core.ProjectOperations())
		if err != nil {
			t.Fatal(err)
		}
		var facts core.OperationFacts
		if err := json.Unmarshal(raw, &facts); err != nil {
			t.Fatal(err)
		}
		if facts.SchemaVersion != core.OperationSchemaVersion {
			t.Fatalf("schema version = %d", facts.SchemaVersion)
		}
		for index, operation := range facts.Operations {
			if !slices.Equal(operation.Facts(), registry[index].Facts()) {
				t.Fatalf("%s round-trips to different facts", operation.ID)
			}
		}
	})

	t.Run("guidance and adapter expose exactly the agent palette", func(t *testing.T) {
		guidance, err := generate.Render()
		if err != nil {
			t.Fatal(err)
		}
		adapter, err := host.Local(tempRoot(t), host.LocalCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(adapter.Callable(), agentIDs) {
			t.Fatalf("adapter callable %v, registry agent palette %v", adapter.Callable(), agentIDs)
		}
		for _, operation := range registry {
			mentioned := strings.Contains(guidance.Body, "`"+operation.Usage()+"`")
			if want := slices.Contains(agentIDs, operation.ID); mentioned != want {
				t.Fatalf("guidance mentions %s = %v, want %v", operation.ID, mentioned, want)
			}
			if slices.Contains(agentIDs, operation.ID) {
				continue
			}
			if _, err := adapter.Invoke(operation.ID, "x"); err == nil {
				t.Fatalf("adapter exposes %s", operation.ID)
			}
		}
		// The human gate is absent from every agent-facing surface, and the
		// envelope refuses to offer it as a next action.
		if strings.Contains(guidance.Body, "approve") {
			t.Fatal("guidance names the human gate as a command")
		}
		err = agentjson.Validate(agentjson.Envelope{
			Schema: agentjson.Schema, OK: true, Operation: "status",
			Root:    &agentjson.Root{Path: adapter.Root()},
			Subject: &agentjson.Subject{Change: change},
			Next: agentjson.Next{
				Kind: agentjson.NextOperation, Operation: "approve",
				Arguments: map[string]string{"change": change}, Instruction: "approve it",
			},
			Exit: agentjson.Exit{Code: 0, Class: core.ExitClassSuccess},
		})
		if !failure.IsCode(err, "next_human_only") {
			t.Fatalf("envelope offered the human gate: %v", err)
		}
	})
}

// ---------------------------------------------------------------- journeys

// opaqueID matches the harness-generated identifiers whose bytes differ between
// two runs of the same journey; their meaning, not their value, is compared.
var opaqueID = regexp.MustCompile(`\b[0-9a-f]{16,}\b`)

// step is one observed invocation, reduced to what both surfaces must agree on.
type step struct {
	operation, kind, instruction string
	exit                         int
}

func testJourneys(t *testing.T) {
	stdin := regularStdin(t)
	original := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() { os.Stdin = original })

	agent := headlessJourney(t)
	human := humanJourney(t)
	if len(agent) != len(human) {
		t.Fatalf("agent ran %d steps, human ran %d", len(agent), len(human))
	}
	for index, observed := range agent {
		if observed != human[index] {
			t.Fatalf("step %d: agent %#v, human %#v", index, observed, human[index])
		}
	}
}

// headlessJourney drives one task from next to complete through the adapter and
// the JSON envelope only, taking each result's own next action.
func headlessJourney(t *testing.T) []step {
	root := fixture(t)
	adapter, err := host.Local(root, host.LocalCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	if adapter.Assurance() != core.AttemptAssurance {
		t.Fatalf("adapter claims assurance %q", adapter.Assurance())
	}
	// The adapter refuses to build the human verb at all, so a headless agent
	// has no argv for it to run.
	if _, err := adapter.Invoke("approve", change); !failure.IsCode(err, "human_approval_required") {
		t.Fatalf("adapter built an approval invocation: %v", err)
	}

	invoke := func(operation string, arguments ...string) (map[string]any, step) {
		t.Helper()
		request, err := adapter.Invoke(operation, arguments...)
		if err != nil {
			t.Fatalf("invoke %s: %v", operation, err)
		}
		if request.Argv[0] != "specd" || request.Dir != root {
			t.Fatalf("invoke %s produced %#v", operation, request)
		}
		code, stdout, stderr := runCLI(t, cmd.RouteAgent, append(request.Argv[1:], "--json")...)
		if stderr != "" {
			t.Fatalf("%s wrote to stderr: %s", operation, stderr)
		}
		var document map[string]any
		if err := json.Unmarshal([]byte(stdout), &document); err != nil {
			t.Fatalf("%s: %v: %s", operation, err, stdout)
		}
		next := document["next"].(map[string]any)
		return document, step{
			operation: operation, exit: code,
			kind:        next["kind"].(string),
			instruction: opaqueID.ReplaceAllString(next["instruction"].(string), "<id>"),
		}
	}

	steps := []step{}
	record := func(observed step) { steps = append(steps, observed) }

	checked, observed := invoke("check", change)
	if observed.exit != 0 || observed.kind != agentjson.NextHumanHandoff {
		t.Fatalf("check = %#v", observed)
	}
	record(observed)
	// The agent route cannot pass the gate even when it names the verb itself.
	if code, stdout, _ := runCLI(t, cmd.RouteAgent, "approve", change, "--root", root, "--json"); code != 2 ||
		!strings.Contains(stdout, agentjson.NextHumanHandoff) {
		t.Fatalf("agent approval: code=%d out=%s", code, stdout)
	}
	if checked["ok"] != true {
		t.Fatalf("authored plan failed check: %v", checked)
	}
	humanApproval(t, root)

	_, observed = invoke("next", change)
	if observed.kind != agentjson.NextOperation {
		t.Fatalf("next = %#v", observed)
	}
	record(observed)

	manifest, observed := invoke("context", change, taskID)
	record(observed)
	revision := revisionOf(t, manifest)
	declared := stringsOf(manifest, "allowed_write_paths")
	if !slices.Equal(declared, []string{"sample.go"}) {
		t.Fatalf("declared write paths = %v", declared)
	}

	started, observed := invoke("start", change, taskID, "--revision", revision)
	record(observed)
	if got := stringsOf(started, "declared_files"); !slices.Equal(got, declared) {
		t.Fatalf("start declared %v, context declared %v", got, declared)
	}
	attempt := dataString(t, started, "attempt")

	// Edit exactly the declared file, then prove verification alone is not
	// completion: completing before any evidence exists is refused.
	editDeclaredFile(t, root, declared)
	if code, stdout, _ := runCLI(t, cmd.RouteAgent, "complete", change, taskID,
		"--revision", revisionOf(t, started), "--root", root, "--json"); code == 0 {
		t.Fatalf("completion succeeded without evidence: %s", stdout)
	}

	verified, observed := invoke("verify", change, taskID, attempt)
	record(observed)
	if verified["ok"] != true || dataOf(verified)["passed"] != true {
		t.Fatalf("verify = %v", verified)
	}
	if observed.kind != agentjson.NextOperation {
		t.Fatalf("verify offered %q, want the explicit completion transition", observed.kind)
	}

	completed, observed := invoke("complete", change, taskID, "--revision", revisionOf(t, verified))
	record(observed)
	if completed["ok"] != true {
		t.Fatalf("complete = %v", completed)
	}
	return steps
}

// humanJourney drives the same task through rendered terminal output, and is
// the only route that may pass the approval gate.
func humanJourney(t *testing.T) []step {
	root := fixture(t)
	steps := []step{}
	terminal := func(operation string, arguments ...string) step {
		t.Helper()
		code, stdout, stderr := runCLI(t, cmd.RouteHumanTerminal,
			append(append([]string{operation}, arguments...), "--root", root)...)
		if stderr != "" {
			t.Fatalf("%s wrote to stderr: %s", operation, stderr)
		}
		observed := parseText(t, stdout)
		observed.operation, observed.exit = operation, code
		steps = append(steps, observed)
		return observed
	}
	fields := func(stdout string, key string) string {
		t.Helper()
		for _, line := range strings.Split(stdout, "\n") {
			if value, found := strings.CutPrefix(line, key+": "); found {
				return value
			}
		}
		t.Fatalf("%q is absent from %s", key, stdout)
		return ""
	}

	terminal("check", change)
	humanApproval(t, root)
	terminal("next", change)

	contextCode, contextOut, _ := runCLI(t, cmd.RouteHumanTerminal, "context", change, taskID, "--root", root)
	steps = append(steps, withOperation(parseText(t, contextOut), "context", contextCode))
	revision := fields(contextOut, "revision")

	startCode, startOut, _ := runCLI(t, cmd.RouteHumanTerminal,
		"start", change, taskID, "--revision", revision, "--root", root)
	steps = append(steps, withOperation(parseText(t, startOut), "start", startCode))
	attempt := fields(startOut, "attempt")
	editDeclaredFile(t, root, []string{"sample.go"})

	verifyCode, verifyOut, _ := runCLI(t, cmd.RouteHumanTerminal,
		"verify", change, taskID, attempt, "--root", root)
	steps = append(steps, withOperation(parseText(t, verifyOut), "verify", verifyCode))

	terminal("complete", change, taskID, "--revision", fields(verifyOut, "revision"))
	return steps
}

func withOperation(observed step, operation string, exit int) step {
	observed.operation, observed.exit = operation, exit
	return observed
}

// humanApproval passes the gate the way a human must: through the human route,
// with an explicitly named approver and reason.
func humanApproval(t *testing.T, root string) {
	t.Helper()
	code, stdout, stderr := runCLI(t, cmd.RouteHumanTerminal, "approve", change,
		"--approver", approver, "--reason", "reviewed the plan", "--root", root)
	if code != 0 {
		t.Fatalf("human approval exited %d: %s%s", code, stdout, stderr)
	}
	git(t, root, []string{"add", "-A"}, []string{"commit", "-m", "plan", "--no-gpg-sign"})
}

// ---------------------------------------------------------------- helpers

func runCLI(t *testing.T, route cmd.Route, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv(cli.RouteVariable, string(route))
	var stdout, stderr strings.Builder
	code := cli.Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// parseText reads the rendered terminal document back into the same facts the
// envelope carries, which is what makes the two journeys comparable.
func parseText(t *testing.T, rendered string) step {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		value, found := strings.CutPrefix(line, "next: ")
		if !found {
			continue
		}
		head, instruction, split := strings.Cut(value, "; ")
		if !split {
			t.Fatalf("next line names no instruction: %q", line)
		}
		kind, _, _ := strings.Cut(head, " ")
		return step{kind: kind, instruction: opaqueID.ReplaceAllString(instruction, "<id>")}
	}
	t.Fatalf("rendered document names no next action: %s", rendered)
	return step{}
}

func dataOf(document map[string]any) map[string]any {
	data, _ := document["data"].(map[string]any)
	return data
}

func dataString(t *testing.T, document map[string]any, key string) string {
	t.Helper()
	value, ok := dataOf(document)[key].(string)
	if !ok {
		t.Fatalf("%q is absent from %v", key, document)
	}
	return value
}

func revisionOf(t *testing.T, document map[string]any) string {
	t.Helper()
	state, ok := document["state"].(map[string]any)
	if !ok {
		t.Fatalf("document carries no state: %v", document)
	}
	return strings.TrimSuffix(strings.TrimSpace(jsonNumber(state["revision"])), ".0")
}

func jsonNumber(value any) string {
	if number, ok := value.(float64); ok {
		return strings.TrimSuffix(strings.TrimRight(formatFloat(number), "0"), ".")
	}
	return ""
}

func formatFloat(value float64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func stringsOf(document map[string]any, key string) []string {
	raw, _ := dataOf(document)[key].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, _ := item.(string)
		out = append(out, text)
	}
	return out
}

// editDeclaredFile writes only the files the task declared.
func editDeclaredFile(t *testing.T, root string, declared []string) {
	t.Helper()
	if !slices.Contains(declared, "sample.go") {
		t.Fatalf("declared files %v do not include the file the task edits", declared)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.go"),
		[]byte("package sample\n\nfunc Sample() string { return \"after\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fixture builds a project that is a Git worktree and a Go module, initializes
// the managed root, creates the change, and installs the authored plan.
func fixture(t *testing.T) string {
	t.Helper()
	root := tempRoot(t)
	copyTree(t, filepath.Join("testdata", "agent-contract", "project"), root)
	git(t, root,
		[]string{"init"},
		[]string{"config", "user.email", approver},
		[]string{"config", "user.name", "Human"},
		[]string{"add", "."},
		[]string{"commit", "-m", "baseline", "--no-gpg-sign"},
	)
	for _, args := range [][]string{{"init"}, {"new", change, "--capability", "sample"}} {
		if code, stdout, stderr := runCLI(t, cmd.RouteAgent, append(args, "--root", root)...); code != 0 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
	}
	copyTree(t, filepath.Join("testdata", "agent-contract", "plan"),
		filepath.Join(root, ".specd", "changes", change))
	return root
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, commands ...[]string) {
	t.Helper()
	for _, arguments := range commands {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, raw)
		}
	}
}

// regularStdin gives approval a non-terminal stream, so the confirmation path
// stays the non-interactive one that requires an explicit approver and reason.
func regularStdin(t *testing.T) *os.File {
	t.Helper()
	file, err := os.Create(filepath.Join(tempRoot(t), "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
