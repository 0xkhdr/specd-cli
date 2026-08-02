package gates

import (
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/evidence"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

const (
	ProductionPolicyGate     GateID = "production_policy"
	ProductionDesignGate     GateID = "production_design"
	ProductionTraceGate      GateID = "production_trace"
	ProductionAcceptanceGate GateID = "production_acceptance"
	ProductionCheckGate      GateID = "production_checks"
)

// acceptancePathPattern matches a repository path token inside acceptance text:
// it contains a directory separator and ends in an extension. Prose with
// slashes such as `build/compile` or `and/or` has no extension and is ignored.
var acceptancePathPattern = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+\.[A-Za-z0-9]+`)

// Production evaluates the production-only planning rules over the canonical
// planning projection. It is additive by construction: it never runs, replaces,
// or suppresses a core gate, and the caller keeps evaluating the core registry
// exactly as the default profile does. It is pure: no filesystem, clock,
// network, or LLM access.
func Production(snapshot Snapshot, required []evidence.RequiredCheck) []Finding {
	var findings []Finding
	appendIssues := func(id GateID, issues []Issue) {
		for _, issue := range issues {
			findings = append(findings, Finding{
				Gate: id, Severity: Error, Location: issue.Location,
				Problem: issue.Problem, Repair: issue.Repair,
			})
		}
	}
	if strings.TrimSpace(snapshot.PolicyDigest) == "" {
		appendIssues(ProductionPolicyGate, []Issue{{
			Problem: "production policy digest is unavailable",
			Repair:  "resolve the production policy before evaluating production gates",
		}})
		return findings
	}
	appendIssues(ProductionCheckGate, productionCheckIssues(required))
	appendIssues(ProductionDesignGate, productionDesignIssues(snapshot.Plan))
	appendIssues(ProductionTraceGate, productionTraceIssues(snapshot.Plan))
	appendIssues(ProductionAcceptanceGate, productionAcceptanceIssues(snapshot.Plan))
	return findings
}

// productionCheckIssues refuses missing, malformed, duplicate, or unknown proof
// declarations. An empty declaration set is a refusal, never a silent pass.
func productionCheckIssues(required []evidence.RequiredCheck) []Issue {
	if len(required) == 0 {
		return []Issue{{
			Problem: "production policy declares no required proof",
			Repair:  "declare at least one runnable production check",
		}}
	}
	var issues []Issue
	seen := map[string]bool{}
	runnable := false
	for _, check := range required {
		switch {
		case !check.Valid():
			issues = append(issues, Issue{
				Problem: "production check " + check.String() + " has an unknown class or malformed check id",
				Repair:  "declare build, lint, or review with one lowercase check id",
			})
			continue
		case check.Class == evidence.ClassTestRun:
			issues = append(issues, Issue{
				Problem: "production check " + check.String() + " redeclares test-run proof",
				Repair:  "leave test-run proof to the approved task verification command",
			})
			continue
		case seen[check.String()]:
			issues = append(issues, Issue{
				Problem: "production check " + check.String() + " is declared more than once",
				Repair:  "declare each production check exactly once",
			})
			continue
		}
		seen[check.String()] = true
		runnable = runnable || evidence.Runnable(check.Class)
	}
	if !runnable && len(issues) == 0 {
		issues = append(issues, Issue{
			Problem: "production policy declares no runnable proof",
			Repair:  "declare at least one build or lint check beside review",
		})
	}
	return issues
}

// productionDesignIssues completes the design contract: every requirement the
// change declares must be cited by design, not only by a task.
func productionDesignIssues(authored plan.Change) []Issue {
	if !authored.Design.Source.Present {
		return nil
	}
	cited := map[string]bool{}
	for _, reference := range authored.Design.References {
		cited[referenceKey(reference.Capability, reference.Requirement)] = true
	}
	var issues []Issue
	for _, requirement := range authored.Trace.Requirements {
		name := requirement.Capability + "/Requirement: " + requirement.Requirement
		if !cited[referenceKey(requirement.Capability, requirement.Requirement)] {
			issues = append(issues, Issue{
				Location: requirement.Location,
				Problem:  "design does not cite " + name,
				Repair:   "cite " + name + " in design.md",
			})
		}
	}
	return issues
}

// productionTraceIssues closes the trace in both directions with executable
// scope: every task cites a requirement, and every requirement is implemented
// by a task that declares at least one file.
func productionTraceIssues(authored plan.Change) []Issue {
	files := map[string]int{}
	for _, task := range authored.Tasks.Tasks {
		if task.Valid {
			files[task.ID] = len(task.Files)
		}
	}
	var issues []Issue
	for _, task := range authored.Tasks.Tasks {
		if task.Valid && len(task.References) == 0 {
			issues = append(issues, Issue{
				Location: task.Location,
				Problem:  "task " + task.ID + " cites no requirement",
				Repair:   "cite one <capability>/Requirement: <name> in the task refs column",
			})
		}
	}
	for _, requirement := range authored.Trace.Requirements {
		name := requirement.Capability + "/Requirement: " + requirement.Requirement
		implemented := false
		for _, id := range requirement.Tasks {
			if files[id] > 0 {
				implemented = true
				break
			}
		}
		if !implemented && len(requirement.Tasks) != 0 {
			issues = append(issues, Issue{
				Location: requirement.Location,
				Problem:  name + " has no implementing task with declared files",
				Repair:   "declare the files one implementing task writes",
			})
		}
	}
	return issues
}

// productionAcceptanceIssues keeps acceptance reachable inside declared scope: a
// path named by acceptance must be producible by a declared file. Pure over the
// plan; the filesystem is never read, so a prospective output still passes.
func productionAcceptanceIssues(authored plan.Change) []Issue {
	var issues []Issue
	for _, task := range authored.Tasks.Tasks {
		if !task.Valid {
			continue
		}
		var unreachable []string
		seen := map[string]bool{}
		for _, token := range acceptancePathPattern.FindAllString(task.Acceptance, -1) {
			if seen[token] || declaredCanProduce(token, task.Files) {
				continue
			}
			seen[token] = true
			unreachable = append(unreachable, token)
		}
		slices.Sort(unreachable)
		for _, token := range unreachable {
			issues = append(issues, Issue{
				Location: task.Location,
				Problem:  "task " + task.ID + " acceptance names " + token + ", which no declared file can produce",
				Repair:   "declare " + token + " in the task files column or state acceptance within declared scope",
			})
		}
	}
	return issues
}

func declaredCanProduce(token string, declared []string) bool {
	return slices.ContainsFunc(declared, func(file string) bool {
		return file == token || path.Dir(file) == path.Dir(token)
	})
}

func referenceKey(capability, requirement string) string {
	return capability + "\x00" + plan.NormalizeRequirementIdentity(requirement)
}
