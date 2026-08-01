package generate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
)

// GuidanceFile is the only managed generation target. It is an allowlist of one:
// cleanup and refresh refuse every other name rather than deleting by pattern.
const GuidanceFile = "AGENTS.md"

// Managed region markers. The begin marker carries the schema version and the
// content hash, so drift between the marked bytes and their declared identity is
// detectable without re-rendering.
const (
	markerBegin  = "<!-- specd:begin schema="
	markerEnd    = "<!-- specd:end -->"
	markerFormat = markerBegin + "%d hash=%s -->"
)

// Result reports one refresh outcome.
type Result struct {
	Path    string
	Version int
	Hash    string
	// Changed is false when the file already held exactly this managed region,
	// which makes refresh idempotent and safe to run from a parity check.
	Changed bool
}

func refuse(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}

// Destination resolves one managed target inside the project. It refuses
// traversal, absolute names, and any symlink on the resolved path, so a
// generated file can never be written or removed outside the selected project.
func Destination(root, name string) (string, error) {
	if name != GuidanceFile {
		return "", refuse("generation_target_unknown", name,
			fmt.Sprintf("%q is not a managed generation target", name),
			"generate only "+GuidanceFile)
	}
	project, err := corepath.ResolveRoot(root, "")
	if err != nil {
		return "", err
	}
	target := filepath.Join(project, name)
	// filepath.Join already cleaned the name; anything that escaped the project
	// proves the name was traversal.
	if filepath.Dir(target) != project {
		return "", refuse("generation_path_escape", target,
			"managed target escapes the selected project", "generate inside the selected project")
	}
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return target, nil
	case err != nil:
		return "", refuse("generation_path_unreadable", target, err.Error(),
			"repair the project path and refresh again")
	case info.Mode()&os.ModeSymlink != 0:
		return "", refuse("generation_path_symlink", target,
			"managed target is a symlink", "remove the symlink and refresh again")
	case !info.Mode().IsRegular():
		return "", refuse("generation_path_unsafe", target,
			"managed target is not a regular file", "remove the target and refresh again")
	}
	return target, nil
}

// Refresh replaces exactly the managed region and preserves every surrounding
// byte. The write is atomic, so an interruption leaves the old complete region
// or the new one, never half of either.
func Refresh(root string) (Result, error) {
	target, err := Destination(root, GuidanceFile)
	if err != nil {
		return Result{}, err
	}
	rendered, err := Render()
	if err != nil {
		return Result{}, err
	}
	existing, err := read(target)
	if err != nil {
		return Result{}, err
	}
	region, err := locate(target, existing)
	if err != nil {
		return Result{}, err
	}
	block := wrap(rendered)
	updated := block
	if region.found {
		if existing[region.start:region.end] == block {
			return Result{Path: target, Version: rendered.Version, Hash: rendered.Hash}, nil
		}
		updated = existing[:region.start] + block + existing[region.end:]
	} else if strings.TrimSpace(existing) != "" {
		updated = existing + "\n" + block
	}
	if err := persist.AtomicReplace(target, []byte(updated), nil); err != nil {
		return Result{}, refuse("generation_write_failed", target, err.Error(),
			"repair the project path and refresh again")
	}
	return Result{Path: target, Version: rendered.Version, Hash: rendered.Hash, Changed: true}, nil
}

// Drift reports whether the installed managed region is the current one. It is
// the parity check that runs before refresh; its only repair is refresh.
func Drift(root string) (bool, string, error) {
	target, err := Destination(root, GuidanceFile)
	if err != nil {
		return false, "", err
	}
	rendered, err := Render()
	if err != nil {
		return false, "", err
	}
	existing, err := read(target)
	if err != nil {
		return false, "", err
	}
	region, err := locate(target, existing)
	if err != nil {
		return false, "", err
	}
	if region.found && existing[region.start:region.end] == wrap(rendered) {
		return false, "", nil
	}
	return true, "refresh the managed " + GuidanceFile + " region", nil
}

// Clean removes one managed file, and only when the file is nothing but a
// managed region whose declared hash still identifies its bytes. A file holding
// user text, or one whose managed bytes were edited, is left untouched: cleanup
// deletes proven managed content, never a guess.
func Clean(root, name string) error {
	target, err := Destination(root, name)
	if err != nil {
		return err
	}
	existing, err := read(target)
	if err != nil {
		return err
	}
	if existing == "" {
		return nil
	}
	region, err := locate(target, existing)
	if err != nil {
		return err
	}
	if !region.found || strings.TrimSpace(existing[:region.start]+existing[region.end:]) != "" {
		return refuse("cleanup_unproven", target,
			"file holds bytes this generator does not own",
			"remove the managed region by hand if it is no longer wanted")
	}
	if err := os.Remove(target); err != nil {
		return refuse("cleanup_failed", target, err.Error(),
			"repair the project path and clean again")
	}
	return nil
}

func read(target string) (string, error) {
	raw, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", refuse("generation_path_unreadable", target, err.Error(),
			"repair the project path and refresh again")
	}
	return string(raw), nil
}

func wrap(rendered Guidance) string {
	return fmt.Sprintf(markerFormat, rendered.Version, rendered.Hash) +
		"\n" + rendered.Body + markerEnd + "\n"
}

type region struct {
	found      bool
	start, end int
}

// locate finds the one managed region. Duplicate, unbalanced, or inverted
// markers, and managed bytes that no longer match their declared hash, are all
// refusals: the file is ambiguous and no mutation may follow.
func locate(target, content string) (region, error) {
	begins := strings.Count(content, markerBegin)
	ends := strings.Count(content, markerEnd)
	if begins == 0 && ends == 0 {
		return region{}, nil
	}
	if begins != 1 || ends != 1 {
		return region{}, refuse("managed_markers_ambiguous", target,
			fmt.Sprintf("file holds %d begin and %d end markers, want one of each", begins, ends),
			"leave exactly one managed region and refresh again")
	}
	start := strings.Index(content, markerBegin)
	end := strings.Index(content, markerEnd) + len(markerEnd)
	if end <= start {
		return region{}, refuse("managed_markers_ambiguous", target,
			"managed end marker precedes its begin marker",
			"leave exactly one managed region and refresh again")
	}
	if end < len(content) && content[end] == '\n' {
		end++
	}
	header, body, found := strings.Cut(content[start:end], "\n")
	if !found {
		return region{}, refuse("managed_markers_ambiguous", target,
			"managed region has no content", "leave exactly one managed region and refresh again")
	}
	var version int
	var declared string
	if _, err := fmt.Sscanf(strings.TrimSpace(header), markerFormat, &version, &declared); err != nil {
		return region{}, refuse("managed_markers_ambiguous", target,
			"managed begin marker declares no version and hash",
			"leave exactly one managed region and refresh again")
	}
	body = strings.TrimSuffix(strings.TrimSuffix(body, "\n"), markerEnd)
	if hash(body) != declared {
		return region{}, refuse("managed_hash_drift", target,
			"managed bytes no longer match their declared hash",
			"restore or delete the edited managed region, then refresh again")
	}
	return region{found: true, start: start, end: end}, nil
}
