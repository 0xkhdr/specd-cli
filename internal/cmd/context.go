package cmd

import (
	"encoding/json"
	"slices"

	contextmodel "github.com/0xkhdr/specd-cli/internal/context"
	"github.com/0xkhdr/specd-cli/internal/core"
)

func Context(root, change, taskID string, budgetBytes int) (contextmodel.Manifest, error) {
	before, err := core.LoadReadinessSnapshot(root, change)
	if err != nil {
		return contextmodel.Manifest{}, err
	}
	manifest, err := contextmodel.BuildManifest(before, taskID, nil, budgetBytes)
	if err != nil {
		return contextmodel.Manifest{}, err
	}
	after, err := core.LoadReadinessSnapshot(root, change)
	if err != nil {
		return contextmodel.Manifest{}, err
	}
	if before.StateRevision() != after.StateRevision() ||
		before.Approval().Current != after.Approval().Current ||
		approvalHash(before) != approvalHash(after) ||
		!slices.Equal(before.Model().Frontier(), after.Model().Frontier()) {
		return contextmodel.Manifest{}, &contextmodel.ManifestError{
			Code: "context_snapshot_stale", Owner: "author",
			Reason: "status, approval, or frontier changed during assembly", Next: "regenerate context",
		}
	}
	return manifest, nil
}

func RenderContextJSON(manifest contextmodel.Manifest) ([]byte, error) {
	return json.Marshal(manifest)
}

func approvalHash(snapshot core.ReadinessSnapshot) string {
	status := snapshot.Approval()
	if status.Approval == nil {
		return ""
	}
	return status.Approval.AggregateHash
}
