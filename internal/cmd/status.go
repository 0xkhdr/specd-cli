package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/report"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

type Root struct {
	Path string `json:"path"`
}

type ActivityCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"inProgress"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Blocked    int `json:"blocked"`
}

type StatusNextAction struct {
	Kind      string `json:"kind"`
	Operation string `json:"operation,omitempty"`
	Owner     string `json:"owner,omitempty"`
	Action    string `json:"action"`
}

// StatusProduction is the stage-8 extension of canonical status: the policy
// the projection ran under, the assurance level the harness can honestly
// claim, whether the change presents a reviewable packet, and mechanical
// deferred-domain eligibility. Eligibility is a fact for a root owner to weigh;
// it authorizes no domain and unblocks no operation.
type StatusProduction struct {
	Profile             string                     `json:"profile"`
	PolicyDigest        string                     `json:"policyDigest"`
	Assurance           string                     `json:"assurance"`
	ReviewApprovable    bool                       `json:"reviewApprovable"`
	ReviewBlockers      []core.ReadinessBlocker    `json:"reviewBlockers"`
	FrictionEligibility []core.FrictionEligibility `json:"frictionEligibility"`
}

type StatusResult struct {
	Root             Root                  `json:"root"`
	Approval         *core.ApprovalHandoff `json:"approval,omitempty"`
	ApprovalStatus   core.ApprovalStatus   `json:"approvalStatus"`
	Counts           ActivityCounts        `json:"counts"`
	Tasks            []core.TaskReadiness  `json:"tasks"`
	Frontier         []string              `json:"frontier"`
	Next             StatusNextAction      `json:"next"`
	AllTasksComplete bool                  `json:"allTasksComplete"`
	Production       StatusProduction      `json:"production"`
	state.Projection
}

// Status projects canonical status under the default policy. It loads truth
// once through the report owner, so status and every report read the same
// snapshot rather than two parsers of the same files.
func Status(root, change string) (StatusResult, error) {
	policy := core.DefaultPolicy()
	truth, err := report.Load(root, change, policy)
	if err != nil {
		return StatusResult{}, err
	}
	snapshot := truth.Snapshot
	result := StatusResult{
		Root: Root{Path: snapshot.Root()}, Approval: snapshot.Handoff(),
		ApprovalStatus: snapshot.Approval(), Projection: snapshot.Projection(),
	}
	packet, err := report.ProjectReviewPacket(truth)
	if err != nil {
		return StatusResult{}, err
	}
	eligibility, err := core.ProjectFrictionEligibility(snapshot.Root())
	if err != nil {
		return StatusResult{}, err
	}
	result.Production = StatusProduction{
		Profile: string(policy.Profile), PolicyDigest: policy.Digest(),
		Assurance: core.AttemptAssurance, ReviewApprovable: packet.Approvable,
		ReviewBlockers: packet.Blockers, FrictionEligibility: eligibility,
	}
	readiness := snapshot.Model()
	result.Tasks = readiness.Tasks()
	result.Frontier = readiness.Frontier()
	result.AllTasksComplete = readiness.AllComplete()
	result.Counts = countActivities(result.Tasks)
	result.Next = statusNextAction(change, readiness, result.Approval)
	return result, nil
}

func RenderStatusText(result StatusResult) string {
	var output strings.Builder
	fmt.Fprintf(&output, "root: %s\nchange: %s\nlifecycle: %s\nrevision: %d\n",
		result.Root.Path, result.Change, result.Stage, result.Revision)
	fmt.Fprintf(&output, "approval-current: %t\n", result.ApprovalStatus.Current)
	fmt.Fprintf(&output, "activity: pending=%d in_progress=%d completed=%d failed=%d blocked=%d\n",
		result.Counts.Pending, result.Counts.InProgress, result.Counts.Completed,
		result.Counts.Failed, result.Counts.Blocked)
	for _, task := range result.Tasks {
		fmt.Fprintf(&output, "task %s: activity=%s readiness=%s wave=%d dependencies=%s",
			task.ID, task.Activity, task.Readiness, task.Wave, strings.Join(task.Dependencies, ","))
		if task.Blocker != nil {
			fmt.Fprintf(&output, " blocker=%s owner=%s action=%s",
				task.Blocker.Code, task.Blocker.Owner, task.Blocker.Action)
		}
		output.WriteByte('\n')
	}
	fmt.Fprintf(&output, "frontier: %s\n", strings.Join(result.Frontier, ","))
	fmt.Fprintf(&output, "next: kind=%s", result.Next.Kind)
	if result.Next.Operation != "" {
		fmt.Fprintf(&output, " operation=%s", result.Next.Operation)
	}
	if result.Next.Owner != "" {
		fmt.Fprintf(&output, " owner=%s", result.Next.Owner)
	}
	fmt.Fprintf(&output, " action=%s\n", result.Next.Action)
	production := result.Production
	fmt.Fprintf(&output, "profile: %s\npolicy-digest: %s\nassurance: %s\n",
		production.Profile, production.PolicyDigest, production.Assurance)
	fmt.Fprintf(&output, "review-approvable: %t\n", production.ReviewApprovable)
	for _, blocker := range production.ReviewBlockers {
		fmt.Fprintf(&output, "review-blocker %s: owner=%s action=%s\n",
			blocker.Code, blocker.Owner, blocker.Action)
	}
	for _, domain := range production.FrictionEligibility {
		fmt.Fprintf(&output, "friction %s: records=%d eligible=%t\n",
			domain.Domain, domain.Records, domain.Eligible)
	}
	return output.String()
}

func RenderStatusJSON(result StatusResult) ([]byte, error) {
	return json.Marshal(result)
}

func countActivities(tasks []core.TaskReadiness) ActivityCounts {
	var counts ActivityCounts
	for _, task := range tasks {
		switch task.Activity {
		case core.TaskPending:
			counts.Pending++
		case core.TaskInProgress:
			counts.InProgress++
		case core.TaskCompleted:
			counts.Completed++
		case core.TaskFailed:
			counts.Failed++
		case core.TaskBlocked:
			counts.Blocked++
		}
	}
	return counts
}

func statusNextAction(change string, readiness core.ReadinessModel, handoff *core.ApprovalHandoff) StatusNextAction {
	outcome := readiness.Outcome()
	if outcome.Classification == "frontier" {
		return StatusNextAction{
			Kind: "operation", Operation: "next",
			Action: fmt.Sprintf("run specd next %s", change),
		}
	}
	if outcome.Classification == "all_complete" {
		return StatusNextAction{Kind: "terminal", Action: "all task work is complete"}
	}
	blocker := outcome.Blocker
	if blocker == nil {
		return StatusNextAction{Kind: "blocked", Owner: "author", Action: "repair tasks and run check"}
	}
	if blocker.Code == "approval_stale" || blocker.Code == "change_not_approved" {
		action := blocker.Action
		if handoff != nil && handoff.HumanInstruction != "" {
			action = handoff.HumanInstruction
		}
		return StatusNextAction{Kind: "human_handoff", Owner: "human", Action: action}
	}
	return StatusNextAction{Kind: "blocked", Owner: blocker.Owner, Action: blocker.Action}
}
