package plan

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/state"
)

//go:embed templates/*.md
var templateFiles embed.FS

type TemplateName string

const (
	ProposalTemplate TemplateName = "proposal"
	SpecTemplate     TemplateName = "spec"
	DesignTemplate   TemplateName = "design"
	TasksTemplate    TemplateName = "tasks"
)

var templatePaths = map[TemplateName]string{
	ProposalTemplate: "templates/proposal.md",
	SpecTemplate:     "templates/spec.md",
	DesignTemplate:   "templates/design.md",
	TasksTemplate:    "templates/tasks.md",
}

func Templates() map[TemplateName][]byte {
	result := make(map[TemplateName][]byte, len(templatePaths))
	for name := range templatePaths {
		raw, err := Template(name)
		if err != nil {
			panic(err) // Embedded source missing means broken binary, not runtime input.
		}
		result[name] = raw
	}
	return result
}

func Template(name TemplateName) ([]byte, error) {
	path, found := templatePaths[name]
	if !found {
		return nil, fmt.Errorf("unknown planning template %q", name)
	}
	raw, err := templateFiles.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded planning template %s: %w", name, err)
	}
	return append([]byte(nil), raw...), nil
}

type artifactTarget struct {
	path     string
	template TemplateName
}

// artifactTargets is the single owner of the artifact path/template table. An
// empty capability names no delta spec, so only the change-level artifacts are
// targeted.
func artifactTargets(dir, capability string) []artifactTarget {
	targets := []artifactTarget{
		{filepath.Join(dir, "proposal.md"), ProposalTemplate},
		{filepath.Join(dir, "design.md"), DesignTemplate},
		{filepath.Join(dir, "tasks.md"), TasksTemplate},
	}
	if capability == "" {
		return targets
	}
	return append(targets, artifactTarget{filepath.Join(dir, "specs", capability, "spec.md"), SpecTemplate})
}

// WriteArtifacts writes the absent planning artifacts under dir from embedded
// bytes and returns the created paths. It takes no lock and validates no state:
// callers own locking, state validation, and path-safety checks. Existing bytes
// are untouched.
func WriteArtifacts(dir, capability string) ([]string, error) {
	if capability != "" {
		if err := corepath.ValidateSegment(capability); err != nil {
			return nil, err
		}
	}
	var created []string
	for _, target := range artifactTargets(dir, capability) {
		if err := os.MkdirAll(filepath.Dir(target.path), 0o755); err != nil {
			return created, err
		}
		raw, err := Template(target.template)
		if err != nil {
			return created, err
		}
		err = writeAbsent(target.path, raw)
		switch {
		case err == nil:
			created = append(created, target.path)
		case errors.Is(err, fs.ErrExist):
			continue
		default:
			return created, err
		}
	}
	sort.Strings(created)
	return created, nil
}

// Scaffold creates only absent planning artifacts. Existing bytes are untouched.
func Scaffold(owner *corepath.Owner, change, capability string) ([]string, error) {
	if err := corepath.ValidateSegment(change); err != nil {
		return nil, err
	}
	if err := corepath.ValidateSegment(capability); err != nil {
		return nil, err
	}
	managedInfo, err := os.Lstat(owner.Managed())
	if err != nil || !managedInfo.IsDir() || managedInfo.Mode()&os.ModeSymlink != 0 {
		return nil, scaffoldFailure(owner, owner.Managed(), errors.New("project must be initialized through stage-1 init"))
	}
	proposal, err := owner.ChangeProposal(change)
	if err != nil {
		return nil, scaffoldFailure(owner, filepath.Join(owner.Changes(), change, "proposal.md"), err)
	}
	design, err := owner.ChangeDesign(change)
	if err != nil {
		return nil, scaffoldFailure(owner, filepath.Join(owner.Changes(), change, "design.md"), err)
	}
	tasks, err := owner.ChangeTasks(change)
	if err != nil {
		return nil, scaffoldFailure(owner, filepath.Join(owner.Changes(), change, "tasks.md"), err)
	}
	spec, err := owner.ChangeSpec(change, capability)
	if err != nil {
		return nil, scaffoldFailure(owner, filepath.Join(owner.Changes(), change, "specs", capability, "spec.md"), err)
	}
	targets := []string{proposal, design, tasks, spec}

	var created []string
	err = lock.With(owner.RootLock(), func() error {
		changePath, err := owner.Change(change)
		if err != nil {
			return err
		}
		info, err := os.Lstat(changePath)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return scaffoldFailure(owner, changePath, errors.New("change must exist through stage-1 new"))
		}
		statePath, err := owner.ChangeState(change)
		if err != nil {
			return err
		}
		stateSource, stateDiagnostics := readPlanSource(statePath, nil)
		if len(stateDiagnostics) != 0 || !stateSource.Present {
			return scaffoldFailure(owner, statePath, errors.New("valid stage-1 state is required"))
		}
		if _, err := state.Decode(stateSource.Bytes, change); err != nil {
			return scaffoldFailure(owner, statePath, err)
		}
		// Path safety is checked once per target under the root lock; every
		// directory WriteArtifacts creates below changePath is a real directory.
		for _, target := range targets {
			if err := owner.CheckWriteTarget(target); err != nil {
				return err
			}
		}
		written, err := WriteArtifacts(changePath, capability)
		created = written
		if err != nil {
			return scaffoldFailure(owner, changePath, err)
		}
		return nil
	})
	if err != nil {
		return created, err
	}
	sort.Strings(created)
	return created, nil
}

func writeAbsent(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func scaffoldFailure(owner *corepath.Owner, path string, err error) error {
	return failure.New("scaffold_failed", owner.Root(), path, err.Error(),
		"repair the path and retry scaffolding")
}
