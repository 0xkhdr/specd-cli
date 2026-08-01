package core

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/gates"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/record"
	"github.com/0xkhdr/specd-cli/internal/core/state"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

// deferredDomains is the D14 vocabulary, copied from the deferred rows of the
// foundation domain tables.
// It is a fixed list rather than configuration: a domain leaves it only through
// a dated foundation decision by the root owner, never through a friction
// record and never through a runtime setting.
var deferredDomains = []string{
	"delivery",
	"maintenance",
	"multi-root",
	"orchestration",
	"references",
	"security-scanners",
}

// DeferredDomains is the D14 vocabulary as an independent copy. The operation
// registry projects it as the friction domain enum, so the flag and the
// recorder can never disagree about which domains exist.
func DeferredDomains() []string { return slices.Clone(deferredDomains) }

// Friction is one recorded observation that a real current blocker was caused
// by a missing deferred domain. It is evidence for D14 and never authority.
type Friction = record.FrictionPayload

type FrictionRequest struct {
	TaskID           string
	Operation        string
	Domain           string
	Consequence      string
	Actor            string
	ExpectedRevision uint64
}

// FrictionEligibility is the derived D14 projection for one deferred domain.
// Records counts distinct change/task observations; Eligible means the root
// owner may now decide, not that anything is unblocked or authorized.
type FrictionEligibility struct {
	Domain   string `json:"domain"`
	Records  int    `json:"records"`
	Eligible bool   `json:"eligible"`
}

// RecordFriction appends one friction observation for a task the readiness
// owner currently reports as blocked. It writes no state: the record carries no
// revision transition, so recording friction can never move lifecycle,
// approval, authority, or task activity.
func RecordFriction(root, change string, request FrictionRequest) (Friction, error) {
	actor := strings.TrimSpace(request.Actor)
	if actor == "" {
		return Friction{}, frictionFailure(
			"friction_unauthorized", "", "friction actor is unauthorized",
			"record friction through an authorized harness operation",
		)
	}
	if strings.TrimSpace(request.Operation) == "" || strings.TrimSpace(request.Consequence) == "" {
		return Friction{}, frictionFailure(
			"friction_invalid", "", "friction names one blocked operation and its observed consequence",
			"name the blocked operation and what it prevented, then retry",
		)
	}
	if err := requireDeferredDomain(request.Domain); err != nil {
		return Friction{}, err
	}
	owner, err := corepath.New(root)
	if err != nil {
		return Friction{}, err
	}
	lockPath, err := owner.ChangeLock(change)
	if err != nil {
		return Friction{}, err
	}
	var recorded Friction
	err = lock.With(lockPath, func() error {
		statePath, err := owner.ChangeState(change)
		if err != nil {
			return err
		}
		raw, err := readCheckState(statePath)
		if err != nil {
			return err
		}
		current, err := state.Decode(raw, change)
		if err != nil {
			return frictionFailure("plan_invalid", statePath, err.Error(), "run status and repair state")
		}
		if current.Revision != request.ExpectedRevision {
			return frictionFailure(
				"stale_revision", statePath,
				fmt.Sprintf("expected revision %d, current %d", request.ExpectedRevision, current.Revision),
				fmt.Sprintf("run status and retry from revision %d", current.Revision),
			)
		}
		authored := plan.ParseChange(owner, change)
		task, found := canonicalTask(authored.Tasks.Tasks, request.TaskID)
		if !found {
			return frictionFailure(
				"friction_unknown", authored.Tasks.Source.Path,
				fmt.Sprintf("task %q is not canonical", request.TaskID),
				"record friction against a task reported by status",
			)
		}
		registry, err := gates.PlanningRegistry()
		if err != nil {
			return err
		}
		approval, err := projectApprovalStatusLocked(owner, change, registry.Version(), DefaultPolicyDigest())
		if err != nil {
			return err
		}
		// The readiness owner decides what "blocked" means; friction only
		// observes it. A ready, active, or terminal task is a hypothetical:
		// work that can proceed, is proceeding, or is done is stopped by
		// nothing. Every other readiness carries the blocker that stopped the
		// observer, which is exactly what the observation is about.
		row, known := ProjectReadiness(authored.Tasks, current.Tasks, current.Stage, approval).Task(task.ID)
		if !known || !frictionObservable(row.Readiness) || row.Blocker == nil {
			return frictionFailure(
				"friction_hypothetical", statePath,
				fmt.Sprintf("task %q is not currently blocked", task.ID),
				"record friction only for a task status reports as blocked",
			)
		}
		encoded, err := state.Encode(current)
		if err != nil {
			return err
		}
		payload, err := record.NewFrictionPayload(Friction{
			Change: change, TaskID: task.ID, Operation: strings.TrimSpace(request.Operation),
			Domain: strings.TrimSpace(request.Domain), Blocker: row.Blocker.Code,
			Consequence: strings.TrimSpace(request.Consequence), Actor: actor,
			ObservedRevision: current.Revision, ContractHash: taskContractHash(task),
			StateHash: hashBytes(encoded), EvidenceSet: evidenceSetHash(current),
		})
		if err != nil {
			return frictionFailure("friction_invalid", statePath, err.Error(),
				"name the blocked operation and what it prevented, then retry")
		}
		encodedFriction, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		item, err := record.New(record.Record{
			Family: record.FamilyHistory, Kind: record.KindFriction, Change: change,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Actor: actor,
			Payload: encodedFriction,
		})
		if err != nil {
			return err
		}
		if err := record.Append(owner.History(), record.FamilyHistory, item); err != nil {
			return err
		}
		recorded = payload
		return nil
	})
	return recorded, err
}

// ProjectFrictionEligibility derives D14 eligibility from history alone. It is
// a read-only projection: it confers no authority, changes no policy, and
// unblocks no operation. Two records count as independent only when they name
// both a different change and a different task.
func ProjectFrictionEligibility(root string) ([]FrictionEligibility, error) {
	owner, err := corepath.New(root)
	if err != nil {
		return nil, err
	}
	items, diagnostics, err := record.Replay(owner.History(), record.FamilyHistory)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) != 0 {
		return nil, frictionFailure("friction_history", owner.History(), "history has an incomplete tail",
			"repair history from a trusted source and run status")
	}
	now := time.Now().UTC()
	observed := map[string][][2]string{}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Kind != record.KindFriction {
			continue
		}
		payload, err := record.DecodeFrictionPayload(item.Payload)
		if err != nil {
			return nil, frictionFailure("friction_invalid", owner.History(), err.Error(),
				"repair history from a trusted source and run status")
		}
		if err := requireDeferredDomain(payload.Domain); err != nil {
			return nil, err
		}
		stamp, err := time.Parse(time.RFC3339Nano, item.Timestamp)
		if err != nil || stamp.After(now) {
			return nil, frictionFailure("friction_future", owner.History(),
				fmt.Sprintf("friction record %s is stamped ahead of the current clock", item.ID),
				"correct the system clock and run status")
		}
		// A replayed or duplicated observation of the same change and task is
		// one fact, so it can never manufacture the second independent record.
		key := payload.Domain + "\x00" + payload.Change + "\x00" + payload.TaskID
		if seen[key] {
			continue
		}
		seen[key] = true
		observed[payload.Domain] = append(observed[payload.Domain],
			[2]string{payload.Change, payload.TaskID})
	}
	result := make([]FrictionEligibility, 0, len(observed))
	for domain, pairs := range observed {
		result = append(result, FrictionEligibility{
			Domain: domain, Records: len(pairs), Eligible: independent(pairs),
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Domain < result[right].Domain })
	return result, nil
}

// independent reports whether two observations name both a different change and
// a different task, which is D14's whole eligibility rule.
//
// ponytail: pairwise scan, because a domain accumulates a handful of records in
// the life of the project. Upgrade path: index by change when a domain ever
// collects enough friction for this to matter.
func independent(pairs [][2]string) bool {
	for left := range pairs {
		for right := left + 1; right < len(pairs); right++ {
			if pairs[left][0] != pairs[right][0] && pairs[left][1] != pairs[right][1] {
				return true
			}
		}
	}
	return false
}

// frictionObservable reports whether a readiness is a real stop rather than a
// hypothetical. It is a closed switch over the readiness vocabulary, so a new
// readiness must be classified here instead of defaulting into observability.
func frictionObservable(readiness Readiness) bool {
	switch readiness {
	case ReadinessReady, ReadinessActive, ReadinessTerminal:
		return false
	case ReadinessBlocked, ReadinessWaitingDependency, ReadinessWaitingApproval:
		return true
	}
	return false
}

func requireDeferredDomain(domain string) error {
	for _, candidate := range deferredDomains {
		if candidate == strings.TrimSpace(domain) {
			return nil
		}
	}
	return frictionFailure(
		"friction_domain", "", fmt.Sprintf("domain %q is not a deferred domain", domain),
		"name one deferred domain: "+strings.Join(deferredDomains, ", "),
	)
}

func frictionFailure(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}
