// Subtraction audit. Stage 9 requires every surviving surface to map to one
// owner — an exercised retained journey, a required foundation invariant, or a
// named external contract — and requires every unowned surface to block the
// release until a separate scoped task deletes it.
//
// The live inventory is derived here, from the repository itself (go/ast) and
// from the canonical registries (operation registry, record codec, generated
// guidance template, host adapter). Nothing is hand-typed, so
// release/surface-inventory.md cannot quietly drift away from the code: a new
// package, command, flag, record kind, generated instruction, or adapter hook
// fails this test until it is given an owner, and a removed one fails it until
// its inventory row is removed.
//
// This test deletes nothing. It only names what must be deleted, and by which
// separately approved task.
package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/0xkhdr/specd-cli/internal/core"
)

// repoDir is the module root relative to this package's test working directory.
const repoDir = "../.."

// inventoryDoc is the one ownership mapping. It is data for this test, never a
// second source of truth about what exists.
const inventoryDoc = "release/surface-inventory.md"

// protectedInvariants are the foundation invariants stage 9 forbids trading for
// surface reduction. They are the only legal `invariant:` owners, so an audit
// cannot invent a comfortable one.
var protectedInvariants = []string{
	"validation", "approval", "authority", "scope",
	"evidence", "staleness", "atomicity", "fail-closed",
}

// deferredDomainWords are the deferred-domain names no live surface may carry.
// The allowance is exact: `Migration` is the D10 REMOVED-requirement field of a
// delta document, which is a parsed external contract and not a deferred
// domain entered early.
var (
	deferredDomainWords = []string{
		"Telemetry", "Orchestrat", "Delivery", "Maintenance", "MultiRoot",
		"Plugin", "Remote", "Network", "Deprecat", "Migrat",
	}
	deferredDomainAllowed = map[string]string{
		"internal/plan.Migration": "D10 REMOVED requirement field of a delta document",
	}
)

// singleOwnerRoles challenges the duplicate-implementation patterns: each role
// may be declared in exactly one package, so a second parser, envelope, report
// model, or adapter abstraction cannot appear unnoticed.
var singleOwnerRoles = map[string]string{
	"Envelope": "internal/agentjson",
	"Adapter":  "internal/host",
	"Delta":    "internal/plan",
	"Report":   "internal/core/report",
}

type surfaceDecl struct {
	pkg, name, kind string
}

type inventoryRow struct {
	key, detail, owner string
}

func TestSurfaceOwnership(t *testing.T) {
	fset := token.NewFileSet()
	files := parseRepository(t, fset)
	decls, uses := declaredSurface(files)

	doc := readInventory(t)
	journeys := retainedJourneys(t)

	checkPackages(t, decls, doc["Go package surface"], journeys)
	checkRegistry(t, registrySurface(t, files), doc["Registry surface"], journeys)
	checkUnowned(t, decls, uses, doc["Pending deletion"])
	checkSpeculativePatterns(t, decls)
}

// ---------------------------------------------------------------- inventory

// parseRepository parses every Go file this module owns. Test fixtures are
// other people's surface and are excluded.
func parseRepository(t *testing.T, fset *token.FileSet) map[string]*ast.File {
	t.Helper()
	files := map[string]*ast.File{}
	err := filepath.WalkDir(repoDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "testdata", ".git", ".specd":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		files[path] = parsed
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("inventory found no Go files; the audit cannot claim an empty surface")
	}
	return files
}

// declaredSurface returns every exported declaration of the shipped (non-test)
// packages, plus how often each identifier appears anywhere in the module.
//
// ponytail: use counting is by identifier name, so two packages declaring the
// same exported name share a count. That direction is safe — it can only keep a
// dead symbol alive, never delete a live one — and go/types is the upgrade path
// if a false survivor is ever observed.
func declaredSurface(files map[string]*ast.File) ([]surfaceDecl, map[string]int) {
	var decls []surfaceDecl
	uses := map[string]int{}
	paths := slices.Sorted(maps.Keys(files))
	for _, path := range paths {
		file := files[path]
		if !strings.HasSuffix(path, "_test.go") {
			pkg := packageOf(path)
			ast.Inspect(file, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.FuncDecl:
					if node.Name.IsExported() {
						kind := "symbol"
						if node.Recv != nil {
							kind = "method"
						}
						decls = append(decls, surfaceDecl{pkg, node.Name.Name, kind})
					}
					return false
				case *ast.TypeSpec:
					if node.Name.IsExported() {
						decls = append(decls, surfaceDecl{pkg, node.Name.Name, typeKind(node)})
					}
					structure, ok := node.Type.(*ast.StructType)
					if !ok {
						return true
					}
					for _, field := range structure.Fields.List {
						for _, name := range field.Names {
							if !name.IsExported() {
								continue
							}
							decls = append(decls, surfaceDecl{pkg, name.Name, fieldKind(field)})
						}
					}
				case *ast.ValueSpec:
					for _, name := range node.Names {
						if name.IsExported() {
							decls = append(decls, surfaceDecl{pkg, name.Name, "symbol"})
						}
					}
				}
				return true
			})
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok {
				uses[identifier.Name]++
			}
			return true
		})
	}
	return decls, uses
}

// fieldKind separates a persisted JSON field from a purely in-memory one: a
// json tag makes the field part of a durable external document.
func fieldKind(field *ast.Field) string {
	if field.Tag == nil {
		return "field"
	}
	value, ok := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Lookup("json")
	if !ok || value == "-" {
		return "field"
	}
	return "json field"
}

func packageOf(path string) string {
	directory := filepath.ToSlash(filepath.Dir(filepath.Clean(path)))
	directory = strings.TrimPrefix(directory, filepath.ToSlash(repoDir)+"/")
	if directory == filepath.ToSlash(repoDir) {
		return "."
	}
	return directory
}

// typeKind separates an interface from a concrete type: an interface is the
// stage 9 one-implementation pattern and must be visible in the inventory.
func typeKind(spec *ast.TypeSpec) string {
	if _, ok := spec.Type.(*ast.InterfaceType); ok {
		return "interface"
	}
	return "type"
}

// registrySurface projects the canonical registries into inventory items. Every
// item comes from the registry that already owns it: no second list exists.
func registrySurface(t *testing.T, files map[string]*ast.File) []inventoryRow {
	t.Helper()
	var rows []inventoryRow
	rows = append(rows,
		inventoryRow{key: "config key", detail: "global --root"},
		inventoryRow{key: "config key", detail: "global --json"})
	for _, operation := range core.Operations() {
		rows = append(rows, inventoryRow{key: "command", detail: operation.ID})
		for _, flag := range operation.Flags {
			if flag.Name == "--root" || flag.Name == "--json" {
				continue
			}
			rows = append(rows, inventoryRow{key: "config key", detail: operation.ID + " " + flag.Name})
		}
	}
	for _, kind := range recordKinds(t, files) {
		rows = append(rows, inventoryRow{key: "record kind", detail: kind})
	}
	for _, instruction := range generatedInstructions(t) {
		rows = append(rows, inventoryRow{key: "generated instruction", detail: instruction})
	}
	for _, hook := range adapterHooks(files) {
		rows = append(rows, inventoryRow{key: "adapter hook", detail: hook})
	}
	return rows
}

// recordKinds reads the declared record kinds from the codec that validates
// them, so a new kind cannot ship without an owner.
func recordKinds(t *testing.T, files map[string]*ast.File) []string {
	t.Helper()
	var kinds []string
	for path, file := range files {
		if packageOf(path) != "internal/core/record" {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			value, ok := node.(*ast.ValueSpec)
			if !ok || len(value.Values) != 1 {
				return true
			}
			named, ok := value.Type.(*ast.Ident)
			if !ok || named.Name != "Kind" {
				return true
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			kinds = append(kinds, strings.Trim(literal.Value, `"`))
			return true
		})
	}
	if len(kinds) == 0 {
		t.Fatal("no record kinds found; the record codec is the only owner of that list")
	}
	sort.Strings(kinds)
	return kinds
}

// generatedInstructions reads the section headings of the one guidance
// template. Each heading is one instruction a fresh agent is told to follow.
func generatedInstructions(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoDir, "internal/generate/agents.tmpl"))
	if err != nil {
		t.Fatalf("read guidance template: %v", err)
	}
	var headings []string
	for _, line := range strings.Split(string(raw), "\n") {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			headings = append(headings, strings.TrimSpace(heading))
		}
	}
	if len(headings) == 0 {
		t.Fatal("guidance template declares no instructions")
	}
	return headings
}

// adapterHooks are the exported entry points of the one host adapter.
func adapterHooks(files map[string]*ast.File) []string {
	var hooks []string
	for path, file := range files {
		if packageOf(path) != "internal/host" || strings.HasSuffix(path, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || !function.Name.IsExported() {
				continue
			}
			name := function.Name.Name
			if function.Recv != nil {
				name = receiverName(function.Recv.List[0].Type) + "." + name
			}
			hooks = append(hooks, name)
		}
	}
	sort.Strings(hooks)
	return hooks
}

func receiverName(expression ast.Expr) string {
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return "?"
}

// ---------------------------------------------------------------- doc input

// readInventory parses the ownership tables. Rows are `| key | detail | owner |`
// except the deletion table, which is `| item | required task |`.
func readInventory(t *testing.T) map[string][]inventoryRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoDir, inventoryDoc))
	if err != nil {
		t.Fatalf("read %s: %v", inventoryDoc, err)
	}
	sections := map[string][]inventoryRow{}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			section = strings.TrimSpace(heading)
			continue
		}
		if !strings.HasPrefix(line, "|") || section == "" {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for index := range cells {
			cells[index] = strings.Trim(strings.TrimSpace(cells[index]), "`")
		}
		if len(cells) < 2 || strings.HasPrefix(cells[0], "---") ||
			slices.Contains([]string{"package", "class", "item", "pattern"}, cells[0]) {
			continue
		}
		row := inventoryRow{key: cells[0], detail: cells[1]}
		if len(cells) > 2 {
			row.owner = strings.TrimSpace(cells[2])
		}
		sections[section] = append(sections[section], row)
	}
	return sections
}

// retainedJourneys reads the retained journey numbers from the runner itself,
// so an inventory row cannot cite a journey that is not actually replayed.
func retainedJourneys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("release_journeys_test.go")
	if err != nil {
		t.Fatalf("read release journeys: %v", err)
	}
	var numbers []string
	for _, match := range regexp.MustCompile(`t\.Run\("(\d\d) `).FindAllStringSubmatch(string(raw), -1) {
		numbers = append(numbers, match[1])
	}
	if len(numbers) != 14 {
		t.Fatalf("retained journeys = %d, want the 14 required stage 9 journeys", len(numbers))
	}
	return numbers
}

// ---------------------------------------------------------------- ownership

func checkPackages(t *testing.T, decls []surfaceDecl, rows []inventoryRow, journeys []string) {
	t.Helper()
	live := map[string]map[string]bool{}
	for _, decl := range decls {
		if live[decl.pkg] == nil {
			live[decl.pkg] = map[string]bool{"package": true}
		}
		live[decl.pkg][decl.kind] = true
	}
	// A package with no exported surface still exists and still needs an owner.
	// The entry point declares none by design, and its path is the one a user
	// installs (`go install .../cmd/specd@latest`), so it is named here.
	if live["cmd/specd"] == nil {
		live["cmd/specd"] = map[string]bool{"package": true}
	}

	want := map[string]string{}
	for pkg, classes := range live {
		want[pkg] = strings.Join(slices.Sorted(maps.Keys(classes)), ", ")
	}
	compare(t, "Go package surface", want, rows, journeys)
}

func checkRegistry(t *testing.T, live []inventoryRow, rows []inventoryRow, journeys []string) {
	t.Helper()
	want := map[string]string{}
	for _, item := range live {
		want[item.key+": "+item.detail] = item.detail
	}
	documented := make([]inventoryRow, 0, len(rows))
	for _, row := range rows {
		documented = append(documented, inventoryRow{
			key: row.key + ": " + row.detail, detail: row.detail, owner: row.owner,
		})
	}
	compare(t, "Registry surface", want, documented, journeys)
}

// compare fails on any drift in either direction: live surface with no row is
// undocumented, and a row with no live surface is a stale claim.
func compare(t *testing.T, section string, want map[string]string, rows []inventoryRow, journeys []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, row := range rows {
		detail, exists := want[row.key]
		if !exists {
			t.Errorf("%s: %s: inventory row describes surface that no longer exists; delete the row",
				section, row.key)
			continue
		}
		if seen[row.key] {
			t.Errorf("%s: %s: duplicate inventory row; exactly one owner is allowed", section, row.key)
			continue
		}
		seen[row.key] = true
		if row.detail != detail {
			t.Errorf("%s: %s: inventory records %q, live surface is %q",
				section, row.key, row.detail, detail)
		}
		checkOwner(t, section+": "+row.key, row.owner, journeys)
	}
	for _, key := range slices.Sorted(maps.Keys(want)) {
		if !seen[key] {
			t.Errorf("BLOCKER %s: %s has no owner in %s: map it to an exercised journey, "+
				"a required invariant, or a named external contract, or delete it through a "+
				"separate approved task", section, key, inventoryDoc)
		}
	}
}

// checkOwner rejects a decorative owner. A journey owner must name a retained
// journey and an invariant owner must name a protected foundation invariant.
func checkOwner(t *testing.T, item, owner string, journeys []string) {
	t.Helper()
	label, prose, _ := strings.Cut(owner, " ")
	switch {
	case strings.HasPrefix(label, "journey:"):
		if !slices.Contains(journeys, strings.TrimPrefix(label, "journey:")) {
			t.Errorf("%s: owner %q names no retained journey", item, owner)
		}
	case strings.HasPrefix(label, "invariant:"):
		if !slices.Contains(protectedInvariants, strings.TrimPrefix(label, "invariant:")) {
			t.Errorf("%s: owner %q names no protected invariant", item, owner)
		}
	case strings.HasPrefix(label, "contract:"):
		if strings.TrimSpace(prose) == "" {
			t.Errorf("%s: a contract owner must name the external contract", item)
		}
	default:
		t.Errorf("%s: owner %q must start with journey:, invariant:, or contract:", item, owner)
	}
}

// ---------------------------------------------------------------- deletions

// checkUnowned names every exported declaration nothing references. Such a
// symbol cannot be owned by a journey, because no journey can reach it. It is a
// release blocker until a separately approved, exactly scoped task deletes it;
// this task deletes nothing.
func checkUnowned(t *testing.T, decls []surfaceDecl, uses map[string]int, rows []inventoryRow) {
	t.Helper()
	dead := map[string]bool{}
	for _, decl := range decls {
		if uses[decl.name] <= 1 {
			dead[decl.pkg+"."+decl.name] = true
		}
	}
	documented := map[string]string{}
	for _, row := range rows {
		documented[row.key] = row.detail
	}
	for item := range documented {
		if !dead[item] {
			t.Errorf("Pending deletion: %s is referenced again; remove its row from %s", item, inventoryDoc)
		}
	}
	for _, item := range slices.Sorted(maps.Keys(dead)) {
		task, declared := documented[item]
		if !declared {
			t.Errorf("BLOCKER %s is exported and never referenced, and %s does not record it. "+
				"Record it with the scoped deletion task that removes it.", item, inventoryDoc)
			continue
		}
		t.Errorf("BLOCKER %s is exported and never referenced. Release is blocked until "+
			"its scoped deletion task removes it with its metadata, projections, tests, "+
			"and docs: %s", item, task)
	}
}

// ------------------------------------------------------- speculative patterns

// checkSpeculativePatterns challenges the stage 9 list directly. Each check is
// mechanical: none of them can be satisfied by prose.
func checkSpeculativePatterns(t *testing.T, decls []surfaceDecl) {
	t.Helper()

	// Extra lifecycle phases: the five declared phases are the whole set, and
	// every one of them is reachable by a declared operation.
	phases := core.Lifecycles()
	if len(phases) != 5 {
		t.Errorf("lifecycle phases = %d, want the five declared phases", len(phases))
	}
	for _, phase := range phases {
		used := false
		for _, operation := range core.Operations() {
			used = used || operation.AppliesTo(string(phase))
		}
		if !used {
			t.Errorf("BLOCKER lifecycle phase %q is reachable by no operation; delete the phase", phase)
		}
	}

	// Persistent in_progress: `in_progress` exists only between one attempt and
	// its completion, and the vocabulary admits no further resting state. The
	// check reads the live vocabulary rather than counting matches of a name
	// list, which cannot fail when a sixth value is added.
	wantActivities := []core.TaskActivity{
		core.TaskPending, core.TaskInProgress, core.TaskCompleted,
		core.TaskFailed, core.TaskBlocked,
	}
	if got := core.TaskActivities(); !slices.Equal(got, wantActivities) {
		t.Errorf("task activity vocabulary = %v, want %v", got, wantActivities)
	}

	for _, decl := range decls {
		qualified := decl.pkg + "." + decl.name

		// One-implementation interfaces: an exported interface with a single
		// implementation is the pattern stage 9 forbids, so any exported
		// interface must be argued for explicitly rather than assumed.
		if decl.kind == "interface" {
			t.Errorf("BLOCKER %s is an exported interface; name its second implementation "+
				"or delete the abstraction", qualified)
		}

		// Second abstractions: each role model is declared by exactly one
		// package. Handlers and gates that consume a role are not duplicates of
		// it, so the match is on the declared model type itself.
		for role, owner := range singleOwnerRoles {
			if decl.name == role && decl.pkg != owner &&
				(decl.kind == "type" || decl.kind == "interface") {
				t.Errorf("BLOCKER %s duplicates the %s role owned by %s; keep one owner",
					qualified, role, owner)
			}
		}

		// Fields reserved for deferred domains.
		for _, word := range deferredDomainWords {
			if !strings.Contains(decl.name, word) {
				continue
			}
			if _, allowed := deferredDomainAllowed[qualified]; allowed {
				continue
			}
			t.Errorf("BLOCKER %s names deferred domain %q; no deferred domain may hold "+
				"surface before D14 authorizes it", qualified, word)
		}
	}
}
