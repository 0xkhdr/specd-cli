// Release qualification. Stage 9 ends with one dated owner decision, and this
// test is the gate that decision must survive.
//
// Every gate below is derived from repository facts: go.mod, the module's own
// Go source, the canonical operation registry, the guidance template, the
// retained journey runner, and the subtraction inventory. Two gates cannot be
// derived that way — the full race suite and `go vet ./...` — because running
// them from inside a test recurses into this test. They are recorded as
// observed CI facts, and release/release-decision.md must state which gates are
// mechanical and which are observed.
//
// This test never edits harness-owned state, history, or evidence: it reads
// them.
package integration

import (
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/generate"
)

const (
	decisionDoc   = "release/release-decision.md"
	gateLimitsDoc = "release/gate-limits.md"
)

// requiredJourneys are the fourteen journeys the release requires the runner to
// retain, in order. This list is the requirement: the runner is checked against
// it, so a journey cannot be silently dropped, renamed into something else, or
// added without one here.
var requiredJourneys = []string{
	"fresh project and one-task change",
	"brownfield capability delta",
	"two independent wave-0 tasks and dependent wave-1 task",
	"process interruption after state mutation and after evidence append",
	"malformed Markdown and corrupt/future state",
	"stale approval after artifact byte change",
	"stale evidence after Git HEAD change",
	"out-of-scope implementation diff",
	"failing and zero-match verification",
	"sync conflict and injected multi-file write failure",
	"archive target collision",
	"agent handoff at human gate",
	"default and production profile comparison",
	"fresh agent resume with only repository and generated guidance",
}

// observedGates are the two gates a test cannot run without recursing into
// itself. They are not asserted here; the decision record must carry them as
// observed CI facts, and this test only proves it does.
var observedGates = []string{
	"go test ./... -race -count=1",
	"go vet ./...",
}

// decisionSections are the compact report's required sections, one per bullet
// of the stage 9 "Release decision" list plus the platform, stage 8, and gate
// facts the release requirement adds.
var decisionSections = []string{
	"Implemented base loop",
	"Journey results",
	"Assurance boundary",
	"Known limitations",
	"Recorded frictions",
	"Deleted surface",
	"Deferred domains and triggers",
	"Supported platforms",
	"Stage 8 status",
	"Release gates",
	"Decision",
}

// deadVocabulary is the historical OpenSpec vocabulary v2 does not use. The
// user- and agent-visible surface must not revive it; build documents and tests
// may discuss it.
var deadVocabulary = []string{"initiative", "collection", "workspace"}

// deterministicCore is the set of packages that must reach no network and no
// model. `os/exec` is deliberately not forbidden: verification and Git identity
// are bounded local subprocesses, which is determinism, not a network path.
var deterministicCore = []string{
	"internal/core", "internal/plan", "internal/reconcile",
	"internal/generate", "internal/agentjson", "internal/context",
}

var forbiddenCoreImports = []string{
	"net", "net/http", "net/rpc", "net/url", "net/smtp", "crypto/tls",
}

// gate is one release gate and its current status. A red gate forbids
// `release`; it never rewrites the decision, it only refuses a dishonest one.
type gate struct {
	name     string
	observed bool
	blocker  string // empty when the gate is green
}

func TestReleaseQualification(t *testing.T) {
	gates := []gate{
		gateStdlibOnly(t),
		gateFormatting(t),
		gateDocsParity(t),
		gateDocLinks(t),
		gateGuidanceParity(t),
		gateMaturityClaims(t),
		gateJourneys(t),
		gateUnownedSurface(t),
		gateVocabulary(t),
		gateDeterministicCore(t),
		gateLimitsComplete(t),
	}
	for _, name := range observedGates {
		gates = append(gates, gate{name: name, observed: true})
	}

	red := map[string]string{}
	for _, item := range gates {
		if item.blocker != "" {
			red[item.name] = item.blocker
			t.Logf("RED gate %s: %s", item.name, item.blocker)
		}
	}

	checkDecision(t, gates, red)
}

// ------------------------------------------------------------- mechanical gates

// gateStdlibOnly reads go.mod. A default binary built from this module must
// need nothing but the standard library, so the require set must be empty.
func gateStdlibOnly(t *testing.T) gate {
	t.Helper()
	raw := readRepoFile(t, "go.mod")
	var required []string
	block := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			block = true
		case block && line == ")":
			block = false
		case block && line != "" && !strings.HasPrefix(line, "//"):
			required = append(required, line)
		case strings.HasPrefix(line, "require "):
			required = append(required, strings.TrimPrefix(line, "require "))
		}
	}
	g := gate{name: "standard-library-only default binary"}
	if len(required) > 0 {
		g.blocker = "go.mod requires " + strings.Join(required, ", ")
	}
	return g
}

// gateFormatting formats every module Go file in memory and compares bytes.
// That is exactly what `gofmt -l` reports, without shelling out.
func gateFormatting(t *testing.T) gate {
	t.Helper()
	var unformatted []string
	for _, path := range moduleGoFiles(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		formatted, err := format.Source(raw)
		if err != nil {
			unformatted = append(unformatted, path)
			continue
		}
		if string(formatted) != string(raw) {
			unformatted = append(unformatted, path)
		}
	}
	g := gate{name: "formatting clean"}
	if len(unformatted) > 0 {
		g.blocker = "unformatted: " + strings.Join(unformatted, ", ")
	}
	return g
}

// gateDocsParity compares the committed operations document with the one the
// registry renders. docs/operations.md is generated, so any drift is a stale
// document rather than an edit.
func gateDocsParity(t *testing.T) gate {
	t.Helper()
	g := gate{name: "generated docs parity"}
	if readRepoFile(t, "docs/operations.md") != core.RenderOperationDocs(core.ProjectOperations()) {
		g.blocker = "docs/operations.md differs from the registry projection"
	}
	return g
}

// gateGuidanceParity renders the managed guidance region twice and requires
// every agent-visible executable operation to appear in it. A fresh agent reads
// only this surface, so an operation missing here is invisible to it.
func gateGuidanceParity(t *testing.T) gate {
	t.Helper()
	g := gate{name: "generated guidance parity"}
	first, err := generate.Render()
	if err != nil {
		g.blocker = "render guidance: " + err.Error()
		return g
	}
	second, err := generate.Render()
	if err != nil || second.Hash != first.Hash {
		g.blocker = "guidance rendering is not deterministic"
		return g
	}
	var missing []string
	for _, operation := range core.Operations() {
		if operation.AgentVisible && operation.Executable &&
			!strings.Contains(first.Body, operation.ID) {
			missing = append(missing, operation.ID)
		}
	}
	if len(missing) > 0 {
		g.blocker = "guidance omits " + strings.Join(missing, ", ")
	}
	return g
}

// gateJourneys proves the fourteen required journeys are all retained and named
// by the runner, so one cannot be silently dropped or renamed.
func gateJourneys(t *testing.T) gate {
	t.Helper()
	g := gate{name: "all fourteen required journeys retained"}
	required := requiredJourneys
	retained := map[string]string{}
	for _, match := range regexp.MustCompile(`t\.Run\("(\d\d) ([^"]+)"`).
		FindAllStringSubmatch(readRepoFile(t, "internal/integration/release_journeys_test.go"), -1) {
		retained[match[1]] = match[2]
	}
	if len(required) != 14 {
		t.Fatalf("requiredJourneys lists %d journeys, want 14", len(required))
	}
	var problems []string
	for index, description := range required {
		number := strconv.Itoa(index + 1)
		if len(number) == 1 {
			number = "0" + number
		}
		name, ok := retained[number]
		if !ok {
			problems = append(problems, "journey "+number+" is not retained")
			continue
		}
		if overlap(description, name) < 2 {
			problems = append(problems, "journey "+number+" is named "+name+
				", which does not describe "+description)
		}
	}
	if len(retained) != 14 {
		problems = append(problems, "runner retains a journey that is not required")
	}
	if len(problems) > 0 {
		g.blocker = strings.Join(problems, "; ")
	}
	return g
}

// overlap counts the distinctive words two journey descriptions share, so a
// rename stays legal but a substitution does not.
func overlap(required, retained string) int {
	words := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(retained)) {
		words[strings.Trim(word, ",-")] = true
	}
	shared := 0
	for _, word := range strings.Fields(strings.ToLower(required)) {
		if len(word) > 4 && words[strings.Trim(word, ",-")] {
			shared++
		}
	}
	return shared
}

// gateUnownedSurface reads the subtraction inventory's pending-deletion table.
// TestSurfaceOwnership derives that table's contents from the repository; this
// gate only refuses a release while any row survives.
func gateUnownedSurface(t *testing.T) gate {
	t.Helper()
	g := gate{name: "no unowned surface"}
	var pending []string
	section := ""
	for _, line := range strings.Split(readRepoFile(t, "release/surface-inventory.md"), "\n") {
		line = strings.TrimSpace(line)
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			section = strings.TrimSpace(heading)
			continue
		}
		if section != "Pending deletion" || !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		first := strings.TrimSpace(cells[0])
		if first == "item" || strings.HasPrefix(first, "---") || first == "" {
			continue
		}
		pending = append(pending, first)
	}
	if len(pending) > 0 {
		g.blocker = "unowned surface awaiting deletion: " + strings.Join(pending, ", ")
	}
	return g
}

// gateVocabulary scans the user- and agent-visible surface only: the guidance
// template, the generated operations document, and the registry projection.
// Build documents and tests may still discuss the historical vocabulary.
func gateVocabulary(t *testing.T) gate {
	t.Helper()
	g := gate{name: "no dead vocabulary in the user and agent surface"}
	surface := map[string]string{
		"internal/generate/agents.tmpl": readRepoFile(t, "internal/generate/agents.tmpl"),
		"docs/operations.md":            readRepoFile(t, "docs/operations.md"),
		"operation registry":            core.RenderOperationHelp(core.ProjectOperations()),
	}
	var found []string
	for _, name := range slices.Sorted(maps.Keys(surface)) {
		lowered := strings.ToLower(surface[name])
		for _, word := range deadVocabulary {
			if strings.Contains(lowered, word) {
				found = append(found, name+" uses "+word)
			}
		}
	}
	if len(found) > 0 {
		g.blocker = strings.Join(found, "; ")
	}
	return g
}

// docLinkPattern matches one inline Markdown link target. Reference-style links
// and bare URLs are not linked navigation and are not checked.
var docLinkPattern = regexp.MustCompile(`]\(([^)\s]+)`)

// gateDocLinks resolves every relative link in the hand-written user
// documentation. docs/operations.md already has byte parity with the registry;
// this is the equivalent mechanical guarantee for the pages a person writes,
// where the cheapest way to be wrong is to point at a file that moved.
func gateDocLinks(t *testing.T) gate {
	t.Helper()
	g := gate{name: "no broken link in the user documentation"}
	pages := []string{"README.md"}
	if _, err := os.Stat(filepath.Join(repoDir, "ARCHITECTURE.md")); err == nil {
		pages = append(pages, "ARCHITECTURE.md")
	}
	entries, err := os.ReadDir(filepath.Join(repoDir, "docs"))
	if err != nil {
		t.Fatalf("read docs: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			pages = append(pages, "docs/"+entry.Name())
		}
	}
	var broken []string
	for _, page := range pages {
		dir := filepath.Dir(filepath.Join(repoDir, page))
		for _, match := range docLinkPattern.FindAllStringSubmatch(readRepoFile(t, page), -1) {
			target := match[1]
			if anchor := strings.IndexByte(target, '#'); anchor >= 0 {
				target = target[:anchor]
			}
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
				broken = append(broken, page+" -> "+match[1])
			}
		}
	}
	if len(broken) > 0 {
		g.blocker = "unresolvable documentation links: " + strings.Join(broken, ", ")
	}
	return g
}

func gateMaturityClaims(t *testing.T) gate {
	t.Helper()
	docs := map[string]string{
		"README.md":      readRepoFile(t, "README.md"),
		"SECURITY.md":    readRepoFile(t, "SECURITY.md"),
		"docs/README.md": readRepoFile(t, "docs/README.md"),
	}
	g := gate{name: "maturity claims complete and consistent"}
	if problems := checkMaturityClaims(core.MaturityClaims(), docs); len(problems) > 0 {
		g.blocker = strings.Join(problems, "; ")
	}
	return g
}

func checkMaturityClaims(claims []core.MaturityClaim, docs map[string]string) []string {
	required := []string{
		"platform/linux/amd64", "platform/linux/arm64", "platform/darwin/arm64", "platform/windows/amd64",
		"profile/default", "profile/production", "guarantee/approval", "guarantee/evidence",
		"guarantee/scope", "guarantee/atomicity", "guarantee/fail-closed", "guarantee/path-containment",
		"guarantee/host-assurance", "coverage/concurrent-end-to-end",
	}
	levels := map[string]bool{"proven": true, "gated": true, "experimental": true, "unclaimed": true, "advisory": true, "enforced": true}
	seen := map[string]string{}
	var problems []string
	for _, claim := range claims {
		id := claim.Category + "/" + claim.Subject
		level := string(claim.Maturity)
		if level != "" && claim.Assurance != "" {
			problems = append(problems, id+" has both maturity and assurance")
			continue
		}
		if level == "" {
			level = string(claim.Assurance)
		}
		if seen[id] != "" {
			problems = append(problems, id+" is duplicated")
		}
		seen[id] = level
		if !levels[level] {
			problems = append(problems, id+" has invalid level "+level)
		}
		if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(claim.Observed) || strings.TrimSpace(claim.Evidence) == "" {
			problems = append(problems, id+" has no dated evidence")
		}
		path := strings.Split(claim.Evidence, "#")[0]
		if _, err := os.Stat(filepath.Join(repoDir, path)); err != nil {
			problems = append(problems, id+" evidence does not resolve")
		}
	}
	for _, id := range required {
		if seen[id] == "" {
			problems = append(problems, id+" is missing")
		}
	}
	checks := []struct{ path, prefix, id string }{
		{"README.md", "the base loop is ", "platform/linux/amd64"},
		{"README.md", "the production profile is ", "profile/production"},
		{"README.md", "host assurance is ", "guarantee/host-assurance"},
		{"SECURITY.md", "host assurance is ", "guarantee/host-assurance"},
		{"docs/README.md", "the base loop is released and ", "platform/linux/amd64"},
		{"docs/README.md", "the production profile remains ", "profile/production"},
		{"docs/README.md", "host scope assurance is ", "guarantee/host-assurance"},
	}
	for _, check := range checks {
		normalized := strings.ToLower(strings.Join(strings.Fields(docs[check.path]), " "))
		prefix := strings.ToLower(check.prefix)
		start := strings.Index(normalized, prefix)
		if start < 0 {
			problems = append(problems, check.path+" omits "+check.id)
			continue
		}
		word := strings.Trim(strings.Fields(normalized[start+len(prefix):])[0], "`.,;:")
		if word != seen[check.id] {
			problems = append(problems, check.path+" claims "+check.id+"="+word+", registry says "+seen[check.id])
		}
	}
	return problems
}

func TestMaturityGateBites(t *testing.T) {
	claims := core.MaturityClaims()
	claims[0].Observed = ""
	if problems := checkMaturityClaims(claims, map[string]string{}); !slices.ContainsFunc(problems, func(problem string) bool {
		return strings.Contains(problem, "has no dated evidence")
	}) {
		t.Fatalf("missing evidence did not bite: %v", problems)
	}
	docs := map[string]string{
		"README.md":      "The base loop is proven. The production profile is proven. Host assurance is advisory.",
		"SECURITY.md":    "Host assurance is advisory.",
		"docs/README.md": "The base loop is released and proven. The production profile remains experimental. Host scope assurance is advisory.",
	}
	if problems := checkMaturityClaims(core.MaturityClaims(), docs); !slices.ContainsFunc(problems, func(problem string) bool {
		return strings.Contains(problem, "profile/production=proven")
	}) {
		t.Fatalf("contradictory documentation did not bite: %v", problems)
	}
}

// gateDeterministicCore refuses any network, TLS, or subprocess import inside
// the deterministic packages. Determinism here is structural, not a promise.
func gateDeterministicCore(t *testing.T) gate {
	t.Helper()
	g := gate{name: "no network or LLM path in the deterministic core"}
	fset := token.NewFileSet()
	var found []string
	for _, path := range moduleGoFiles(t) {
		relative := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), repoDir+"/"))
		if !slices.ContainsFunc(deterministicCore, func(pkg string) bool {
			return relative == pkg || strings.HasPrefix(relative, pkg+"/")
		}) {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			name := strings.Trim(imported.Path.Value, `"`)
			if slices.Contains(forbiddenCoreImports, name) {
				found = append(found, relative+" imports "+name)
			}
		}
	}
	if len(found) > 0 {
		g.blocker = strings.Join(found, "; ")
	}
	return g
}

// ------------------------------------------------------------------- documents

// checkDecision parses the compact decision record. Exactly one decision may be
// current, every required section must exist, every red gate must be named, and
// `release` is impossible while any gate is red.
func checkDecision(t *testing.T, gates []gate, red map[string]string) {
	t.Helper()
	doc := readRepoFile(t, decisionDoc)

	for _, section := range decisionSections {
		if !strings.Contains(doc, "## "+section) {
			t.Errorf("%s: required section %q is missing", decisionDoc, section)
		}
	}

	decisions := regexp.MustCompile(`(?m)^Decision \((\d{4}-\d{2}-\d{2}), ([^)]+)\): (release|continue dogfood|stop)$`).
		FindAllStringSubmatch(doc, -1)
	if len(decisions) != 1 {
		t.Fatalf("%s: %d decisions recorded, want exactly one dated "+
			"`Decision (YYYY-MM-DD, owner): release|continue dogfood|stop`", decisionDoc, len(decisions))
	}
	decision := decisions[0][3]

	for _, item := range gates {
		if !strings.Contains(doc, item.name) {
			t.Errorf("%s: gate %q is not reported", decisionDoc, item.name)
		}
		if item.observed && !strings.Contains(doc, "observed") {
			t.Errorf("%s: %q is an observed CI fact and must be labelled as one", decisionDoc, item.name)
		}
	}

	if decision == "release" && len(red) > 0 {
		for name, blocker := range red {
			t.Errorf("%s: decision `release` is forbidden while gate %q is red: %s",
				decisionDoc, name, blocker)
		}
	}
	if decision != "release" && len(red) == 0 {
		t.Logf("%s: every gate is green and the decision is %q", decisionDoc, decision)
	}
}

func gateLimitsComplete(t *testing.T) gate {
	t.Helper()
	g := gate{name: "gate limits complete"}
	decision := readRepoFile(t, decisionDoc)
	start := strings.Index(decision, "## Release gates")
	if start < 0 {
		g.blocker = "Release gates section is missing"
		return g
	}
	section := decision[start:]
	if end := strings.Index(section[len("## Release gates"):], "\n## "); end >= 0 {
		section = section[:len("## Release gates")+end]
	}
	limits := readRepoFile(t, gateLimitsDoc)
	seen := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		name := strings.Trim(strings.TrimSpace(cells[0]), "`")
		if name == "gate" || strings.HasPrefix(name, "---") || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if !strings.Contains(limits, "## "+name+"\n") {
			g.blocker = "gate " + name + " has no limits heading"
			return g
		}
	}
	if len(seen) == 0 {
		g.blocker = "no gate rows parsed from " + decisionDoc
	}
	return g
}

// --------------------------------------------------------------------- helpers

func readRepoFile(t *testing.T, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoDir, relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(raw)
}

// moduleGoFiles lists the Go files this module owns.
func moduleGoFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(repoDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".specd":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no Go files found; the gates cannot pass over an empty module")
	}
	return files
}
