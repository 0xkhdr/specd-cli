package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/agentjson"
	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/report"
)

// ReportFact is one normalized report value, keyed by its field name. Both
// surfaces render this list and nothing else, so the terminal and the JSON
// document cannot disagree about a value or about the order it appears in.
type ReportFact struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// ReportResult is one projected report. It carries identities, counts, codes,
// and bounded facts only: no authored bodies, no command output, no patch text,
// no logs. Rendering owns no truth — every value comes from a canonical model.
type ReportResult struct {
	Root   Root         `json:"root"`
	Change string       `json:"change"`
	Kind   string       `json:"kind"`
	Facts  []ReportFact `json:"facts"`
}

// The handler is bound beside the operation it runs, exactly as review is.
func init() {
	handlers["report"] = func(_ context.Context, in invocation) (any, error) {
		return Report(in.root, in.arg("change"),
			value[string](in, "--kind"), value[string](in, "--profile"))
	}
}

// Report projects exactly one of the four canonical reports. It reads through
// the report owner alone: it writes nothing, transitions nothing, executes
// nothing, and reaches no network.
func Report(root, change, kind, profile string) (ReportResult, error) {
	policy, err := core.ResolveProfile(profile)
	if err != nil {
		return ReportResult{}, err
	}
	truth, err := report.Load(root, change, policy)
	if err != nil {
		return ReportResult{}, err
	}
	result := ReportResult{
		Root: Root{Path: truth.Snapshot.Root()}, Change: truth.Snapshot.Change(), Kind: kind,
	}
	switch kind {
	case report.KindStatus:
		model, err := report.ProjectStatus(truth)
		if err != nil {
			return ReportResult{}, err
		}
		eligibility, err := core.ProjectFrictionEligibility(root)
		if err != nil {
			return ReportResult{}, err
		}
		result.Facts = statusFacts(model, eligibility)
	case report.KindProof:
		model, err := report.ProjectProof(truth)
		if err != nil {
			return ReportResult{}, err
		}
		result.Facts = proofFacts(model)
	case report.KindHistory:
		model, err := report.ProjectHistory(truth)
		if err != nil {
			return ReportResult{}, err
		}
		result.Facts = historyFacts(model)
	case report.KindReview:
		packet, err := report.ProjectReviewPacket(truth)
		if err != nil {
			return ReportResult{}, err
		}
		eligibility, err := core.ProjectFrictionEligibility(root)
		if err != nil {
			return ReportResult{}, err
		}
		result.Facts = reviewFacts(packet, eligibility)
	default:
		return ReportResult{}, failure.New("report_kind", root, change,
			fmt.Sprintf("report kind %q is not one of %s, %s, %s, %s", kind,
				report.KindStatus, report.KindProof, report.KindHistory, report.KindReview),
			"run specd report "+change+" --kind status")
	}
	return result, nil
}

func statusFacts(model report.StatusModel, eligibility []core.FrictionEligibility) []ReportFact {
	return []ReportFact{
		fact("lifecycle", model.Lifecycle),
		fact("revision", fmt.Sprint(model.Revision)),
		fact("next_action", model.NextAction),
		fact("profile", model.Profile),
		fact("policy_digest", model.PolicyDigest),
		fact("assurance", model.Assurance),
		fact("maturity_claims", maturityRows(core.MaturityClaims())),
		fact("approval_current", fmt.Sprint(model.ApprovalCurrent)),
		fact("approval_reason", model.ApprovalReason),
		fact("artifacts", artifactRows(model.Artifacts)),
		fact("tasks", taskReadinessRows(model.Tasks)),
		fact("frontier", join(model.Frontier)),
		fact("outcome", model.Outcome.Classification),
		fact("blockers", blockerRows(model.Blockers)),
		fact("friction_eligibility", eligibilityRows(eligibility)),
		fact("empty", fmt.Sprint(model.Empty)),
		fact("inconsistency", inconsistencyRow(model.Inconsistency)),
	}
}

func maturityRows(claims []core.MaturityClaim) string {
	out := make([]string, 0, len(claims))
	for _, claim := range claims {
		level := string(claim.Maturity)
		if level == "" {
			level = string(claim.Assurance)
		}
		out = append(out, fmt.Sprintf("%s/%s=%s observed=%s evidence=%s",
			claim.Category, claim.Subject, level, claim.Observed, claim.Evidence))
	}
	return rows(out)
}

func proofFacts(model report.ProofModel) []ReportFact {
	return []ReportFact{
		fact("policy_digest", model.PolicyDigest),
		fact("tasks", taskProofRows(model.Tasks)),
		fact("requirements", requirementProofRows(model.Requirements)),
		fact("extra_evidence", join(model.ExtraEvidence)),
		fact("empty", fmt.Sprint(model.Empty)),
		fact("inconsistency", inconsistencyRow(model.Inconsistency)),
	}
}

func historyFacts(model report.HistoryModel) []ReportFact {
	return []ReportFact{
		fact("events", eventRows(model.Events)),
		fact("observations", eventRows(model.Observations)),
		fact("empty", fmt.Sprint(model.Empty)),
		fact("inconsistency", inconsistencyRow(model.Inconsistency)),
	}
}

func reviewFacts(packet report.ReviewPacket, eligibility []core.FrictionEligibility) []ReportFact {
	review := packet.Review
	return []ReportFact{
		fact("lifecycle", review.Lifecycle),
		fact("revision", fmt.Sprint(review.Revision)),
		fact("profile", review.Profile),
		fact("policy_digest", packet.PolicyDigest),
		fact("assurance", review.Assurance),
		fact("approver", review.Approver),
		fact("approval_hash", review.ApprovalHash),
		fact("approval_current", fmt.Sprint(review.ApprovalCurrent)),
		fact("artifacts", approvalArtifactRows(review.Artifacts)),
		fact("capabilities", capabilityRows(review.Capabilities)),
		fact("tasks", taskReviewRows(review.Tasks)),
		fact("design", designRows(packet.Design)),
		fact("diff", diffRow(packet.Diff)),
		fact("effect", effectRow(packet.Effect)),
		fact("proof_tasks", taskProofRows(review.Proof.Tasks)),
		fact("proof_requirements", requirementProofRows(review.Proof.Requirements)),
		fact("extra_evidence", join(review.Proof.ExtraEvidence)),
		fact("synced", eventRow(review.Synced)),
		fact("archived", eventRow(review.Archived)),
		fact("readiness_blockers", blockerRows(review.Blockers)),
		fact("review_blockers", blockerRows(packet.Blockers)),
		// Eligibility is mechanical: it says a root owner may now decide, never
		// that a deferred domain is authorized or that anything is unblocked.
		fact("friction_eligibility", eligibilityRows(eligibility)),
		fact("approvable", fmt.Sprint(packet.Approvable)),
		fact("empty", fmt.Sprint(packet.Empty)),
		fact("inconsistency", inconsistencyRow(packet.Inconsistency)),
	}
}

// fact bounds every projected value through the one bounded-output owner, so a
// secret-shaped, oversized, or control-character value cannot reach either
// surface. An unavailable value is the explicit word rather than an empty row.
func fact(field, value string) ReportFact {
	if strings.TrimSpace(value) == "" {
		return ReportFact{Field: field, Value: factNone}
	}
	bounded := agentjson.Bound(value, "", "")
	if bounded.Truncated {
		return ReportFact{Field: field, Value: bounded.Excerpt + " [truncated]"}
	}
	return ReportFact{Field: field, Value: bounded.Excerpt}
}

// factNone is the explicit empty value. Both surfaces print it, so an absent
// fact is never confused with a fact that was left out.
const factNone = "none"

func join(values []string) string { return strings.Join(values, ", ") }

func rows(values []string) string {
	if len(values) == 0 {
		return factNone
	}
	return strings.Join(values, "; ")
}

func artifactRows(artifacts []report.Artifact) string {
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, fmt.Sprintf("%s present=%t", artifact.Name, artifact.Present))
	}
	return rows(out)
}

func taskReadinessRows(tasks []core.TaskReadiness) string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		row := fmt.Sprintf("%s activity=%s readiness=%s wave=%d", task.ID,
			task.Activity, task.Readiness, task.Wave)
		if task.Blocker != nil {
			row += " blocker=" + task.Blocker.Code
		}
		out = append(out, row)
	}
	return rows(out)
}

func blockerRows(blockers []core.ReadinessBlocker) string {
	out := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		out = append(out, fmt.Sprintf("%s owner=%s action=%s",
			blocker.Code, blocker.Owner, blocker.Action))
	}
	return rows(out)
}

func eligibilityRows(eligibility []core.FrictionEligibility) string {
	out := make([]string, 0, len(eligibility))
	for _, domain := range eligibility {
		out = append(out, fmt.Sprintf("%s records=%d eligible=%t",
			domain.Domain, domain.Records, domain.Eligible))
	}
	return rows(out)
}

func taskProofRows(tasks []report.TaskProof) string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		row := fmt.Sprintf("%s activity=%s current=%t missing=%t verifyParsable=%t",
			task.TaskID, task.Activity, task.Current, task.Missing, task.VerifyParsable)
		if task.AttemptID != "" {
			row += " attempt=" + task.AttemptID
		}
		if task.ConsumedEvidence != "" {
			row += " consumed=" + task.ConsumedEvidence
		}
		if len(task.StaleEvidence) != 0 {
			row += " stale=" + join(task.StaleEvidence)
		}
		if len(task.MissingChecks) != 0 {
			checks := make([]string, 0, len(task.MissingChecks))
			for _, check := range task.MissingChecks {
				checks = append(checks, check.String())
			}
			row += " missingChecks=" + join(checks)
		}
		if task.PriorPolicyDigest != "" {
			row += " priorPolicyDigest=" + task.PriorPolicyDigest
		}
		out = append(out, row)
	}
	return rows(out)
}

func requirementProofRows(requirements []report.RequirementProof) string {
	out := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		out = append(out, fmt.Sprintf("%s/%s covered=%t tasks=%s", requirement.Capability,
			requirement.Requirement, requirement.Covered, join(requirement.Tasks)))
	}
	return rows(out)
}

func eventRows(events []report.Event) string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, eventRow(&event))
	}
	return rows(out)
}

// eventRow renders one ordered event. Sequence and revision decide order; the
// timestamp travels as a displayed fact and orders nothing.
func eventRow(event *report.Event) string {
	if event == nil {
		return factNone
	}
	return fmt.Sprintf("%d %s revision=%d->%d id=%s actor=%s at=%s", event.Sequence,
		event.Kind, event.From, event.Revision, event.ID, event.Actor, event.Timestamp)
}

func approvalArtifactRows(artifacts []core.ApprovalArtifact) string {
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, artifact.Path+" hash="+artifact.Hash)
	}
	return rows(out)
}

func capabilityRows(capabilities []report.CapabilityReview) string {
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		out = append(out, fmt.Sprintf("%s added=%d modified=%d removed=%d renamed=%d requirements=%s",
			capability.Capability, capability.Added, capability.Modified,
			capability.Removed, capability.Renamed, join(capability.Requirements)))
	}
	return rows(out)
}

func taskReviewRows(tasks []report.TaskReview) string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, fmt.Sprintf("%s wave=%d activity=%s dependsOn=%s files=%s requirements=%s",
			task.ID, task.Wave, task.Activity, join(task.DependsOn),
			join(task.Files), join(task.Requirements)))
	}
	return rows(out)
}

// designRows reports which design contracts a reviewer can rely on. The
// authored section body stays in the artifact: a report never dumps source.
func designRows(sections []report.DesignSection) string {
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		out = append(out, fmt.Sprintf("%s present=%t truncated=%t",
			section.Name, section.Present, section.Truncated))
	}
	return rows(out)
}

func diffRow(diff report.DiffIdentity) string {
	tasks := make([]string, 0, len(diff.Tasks))
	for _, task := range diff.Tasks {
		tasks = append(tasks, fmt.Sprintf("%s head=%s files=%s",
			task.TaskID, task.HEAD, join(task.Files)))
	}
	return fmt.Sprintf("head=%s ambiguous=%t tasks=[%s] unattempted=%s",
		or(diff.HEAD), diff.Ambiguous, strings.Join(tasks, "; "), join(diff.Unattempted))
}

func effectRow(effect report.ReconcileEffect) string {
	capabilities := make([]string, 0, len(effect.Capabilities))
	for _, capability := range effect.Capabilities {
		capabilities = append(capabilities, fmt.Sprintf("%s path=%s before=%s after=%s created=%t noOp=%t",
			capability.Capability, capability.AcceptedPath, or(capability.BeforeHash),
			or(capability.AfterHash), capability.Created, capability.NoOp))
	}
	return fmt.Sprintf("applicable=%t noOp=%t capabilities=[%s] diagnostics=%s",
		effect.Applicable, effect.NoOp, strings.Join(capabilities, "; "),
		join(effect.Diagnostics))
}

func inconsistencyRow(inconsistency *report.Inconsistency) string {
	if inconsistency == nil {
		return factNone
	}
	return fmt.Sprintf("%s subject=%s observed=%s action=%s", inconsistency.Code,
		inconsistency.Subject, inconsistency.Observed, inconsistency.Action)
}

func or(value string) string {
	if strings.TrimSpace(value) == "" {
		return factNone
	}
	return value
}
