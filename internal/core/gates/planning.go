package gates

import (
	"strings"
	"unicode"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

func PlanningRegistry() (Registry, error) {
	return CoreRegistry(map[GateID]Evaluator{
		ProposalGate: proposalIssues, DeltaGate: deltaIssues,
		DesignGate: designIssues, TaskGate: taskIssues,
		TraceGate:                traceIssues,
		VerificationGate:         verificationIssues,
		ApprovalPrerequisiteGate: approvalPrerequisiteIssues,
		LifecycleGate:            lifecycleIssues,
	})
}

func proposalIssues(snapshot Snapshot) []Issue {
	return diagnosticIssues(snapshot.Plan.Proposal.Diagnostics)
}

func deltaIssues(snapshot Snapshot) []Issue {
	prefixes := []string{
		"delta_", "capability_", "purpose_", "operation_", "requirement_", "scenario_", "rename_",
	}
	seen := make(map[plan.Diagnostic]struct{})
	var issues []Issue
	appendMatching := func(diagnostics []plan.Diagnostic) {
		for _, diagnostic := range diagnostics {
			if _, ok := seen[diagnostic]; ok {
				continue
			}
			for _, prefix := range prefixes {
				if strings.HasPrefix(diagnostic.Code, prefix) {
					seen[diagnostic] = struct{}{}
					issues = append(issues, issueFromDiagnostic(diagnostic))
					break
				}
			}
		}
	}
	for _, delta := range snapshot.Plan.Deltas {
		appendMatching(delta.Diagnostics)
	}
	appendMatching(snapshot.Plan.Diagnostics)
	return issues
}

func designIssues(snapshot Snapshot) []Issue {
	return diagnosticIssues(snapshot.Plan.Design.Diagnostics)
}

func taskIssues(snapshot Snapshot) []Issue {
	return diagnosticIssues(snapshot.Plan.Tasks.Diagnostics)
}

func traceIssues(snapshot Snapshot) []Issue {
	return matchingPlanIssues(snapshot.Plan.Diagnostics, "trace_")
}

func verificationIssues(snapshot Snapshot) []Issue {
	// An unreplaced scaffold placeholder is already reported by the canonical
	// parser; reporting it again as broken shell syntax hides the real repair.
	placeholder := map[plan.Location]bool{}
	for _, diagnostic := range snapshot.Plan.Diagnostics {
		if diagnostic.Code == "task_verify_placeholder" {
			placeholder[diagnostic.Location] = true
		}
	}
	var issues []Issue
	for _, task := range snapshot.Plan.Tasks.Tasks {
		if placeholder[task.Location] {
			continue
		}
		if problem := commandProblem(task.Verify); problem != "" {
			issues = append(issues, Issue{
				Location: task.Location,
				Problem:  "task " + task.ID + " verification command " + problem,
				Repair:   "use one non-empty simple executable command without shell control operators",
			})
		}
	}
	return issues
}

func approvalPrerequisiteIssues(snapshot Snapshot) []Issue {
	var issues []Issue
	if snapshot.StateRevision == 0 {
		issues = append(issues, Issue{
			Problem: "state revision is unavailable",
			Repair:  "reload valid change state before approval",
		})
	}
	if strings.TrimSpace(snapshot.PolicyDigest) == "" {
		issues = append(issues, Issue{
			Problem: "policy digest is unavailable",
			Repair:  "assemble the default policy digest before approval",
		})
	}
	for _, diagnostic := range snapshot.Plan.Diagnostics {
		switch diagnostic.Code {
		case "change_unsafe", "artifact_path", "artifact_unsafe", "artifact_unreadable":
			issues = append(issues, issueFromDiagnostic(diagnostic))
			continue
		}
		if classifiedDiagnostic(diagnostic.Code) {
			continue
		}
		issues = append(issues, issueFromDiagnostic(diagnostic))
	}
	return issues
}

func lifecycleIssues(snapshot Snapshot) []Issue {
	if snapshot.Transition != PlanningToApproved {
		return []Issue{{
			Problem: "requested lifecycle transition is not planning to approved",
			Repair:  "request the planning_to_approved transition",
		}}
	}
	if snapshot.Lifecycle != "planning" {
		return []Issue{{
			Problem: "change lifecycle is not planning",
			Repair:  "select a planning change for approval",
		}}
	}
	return nil
}

func diagnosticIssues(diagnostics []plan.Diagnostic) []Issue {
	issues := make([]Issue, len(diagnostics))
	for i, diagnostic := range diagnostics {
		issues[i] = issueFromDiagnostic(diagnostic)
	}
	return issues
}

func matchingPlanIssues(diagnostics []plan.Diagnostic, prefixes ...string) []Issue {
	var issues []Issue
	for _, diagnostic := range diagnostics {
		for _, prefix := range prefixes {
			if strings.HasPrefix(diagnostic.Code, prefix) {
				issues = append(issues, issueFromDiagnostic(diagnostic))
				break
			}
		}
	}
	return issues
}

func issueFromDiagnostic(diagnostic plan.Diagnostic) Issue {
	return Issue{
		Location: diagnostic.Location,
		Problem:  diagnostic.Message,
		Repair:   diagnostic.Repair,
	}
}

func classifiedDiagnostic(code string) bool {
	for _, prefix := range []string{
		"artifact_", "section_", "fence_",
		"delta_", "capability_", "purpose_", "operation_", "requirement_", "scenario_", "rename_",
		"task_", "trace_", "reference_",
	} {
		if strings.HasPrefix(code, prefix) {
			return true
		}
	}
	return false
}

func commandProblem(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "is empty"
	}
	var quote rune
	escaped := false
	tokenStarted := false
	var token strings.Builder
	var tokens []string
	flush := func() {
		if tokenStarted {
			tokens = append(tokens, token.String())
			token.Reset()
			tokenStarted = false
		}
	}
	for _, character := range command {
		switch {
		case character == 0 || character == '\n' || character == '\r':
			return "contains a line break or NUL"
		case escaped:
			escaped = false
			tokenStarted = true
			token.WriteRune(character)
		case character == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if character == quote {
				quote = 0
			} else {
				tokenStarted = true
				token.WriteRune(character)
			}
		case character == '\'' || character == '"':
			quote = character
			tokenStarted = true
		case strings.ContainsRune(";&|<>$()`", character):
			return "uses unsupported shell control syntax"
		case unicode.IsSpace(character):
			flush()
		default:
			tokenStarted = true
			token.WriteRune(character)
		}
	}
	if quote != 0 || escaped {
		return "has an unterminated quote or escape"
	}
	flush()
	for len(tokens) > 0 && shellAssignment(tokens[0]) {
		tokens = tokens[1:]
	}
	if len(tokens) == 0 || tokens[0] == "" || strings.HasPrefix(tokens[0], "#") {
		return "has no executable token"
	}
	return ""
}

func shellAssignment(token string) bool {
	name, _, found := strings.Cut(token, "=")
	if !found || name == "" {
		return false
	}
	for i, character := range name {
		if !(character == '_' || unicode.IsLetter(character) || i > 0 && unicode.IsDigit(character)) {
			return false
		}
	}
	return true
}
