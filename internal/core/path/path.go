package path

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

const managedDir = ".specd"

var (
	segmentPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	archivePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-(.+)$`)
	reserved       = map[string]struct{}{
		"archive": {}, "changes": {}, "evidence": {}, "history": {},
		"specs": {}, "state": {},
	}
)

type Owner struct {
	root    string
	managed string
}

func ResolveRoot(positional, flagged string) (string, error) {
	if positional == "" && flagged == "" || positional != "" && flagged != "" {
		return "", failure.New("root_selection", "", "", "exactly one project path is required", "supply one existing project path")
	}
	selected := positional
	if selected == "" {
		selected = flagged
	}
	absolute, err := filepath.Abs(selected)
	if err != nil {
		return "", failure.New("root_invalid", "", selected, err.Error(), "supply one existing project path")
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", failure.New("root_invalid", "", absolute, err.Error(), "supply one existing project path")
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		reason := "selected path is not a directory"
		if err != nil {
			reason = err.Error()
		}
		return "", failure.New("root_invalid", canonical, canonical, reason, "supply one existing project path")
	}
	return filepath.Clean(canonical), nil
}

func New(root string) (*Owner, error) {
	canonical, err := ResolveRoot(root, "")
	if err != nil {
		return nil, err
	}
	return &Owner{root: canonical, managed: filepath.Join(canonical, managedDir)}, nil
}

func ValidateSegment(segment string) error {
	if !segmentPattern.MatchString(segment) {
		return failure.New("unsafe_segment", "", segment, "name must be lowercase kebab-case", "choose another name")
	}
	if _, found := reserved[segment]; found || windowsDeviceName(segment) {
		return failure.New("reserved_segment", "", segment, "name is reserved for managed state", "choose another name")
	}
	return nil
}

func windowsDeviceName(segment string) bool {
	if segment == "con" || segment == "prn" || segment == "aux" || segment == "nul" {
		return true
	}
	return len(segment) == 4 && (strings.HasPrefix(segment, "com") || strings.HasPrefix(segment, "lpt")) &&
		segment[3] >= '1' && segment[3] <= '9'
}

func (o *Owner) Root() string     { return o.root }
func (o *Owner) Managed() string  { return o.managed }
func (o *Owner) Specs() string    { return filepath.Join(o.managed, "specs") }
func (o *Owner) Changes() string  { return filepath.Join(o.managed, "changes") }
func (o *Owner) Archive() string  { return filepath.Join(o.managed, "archive") }
func (o *Owner) History() string  { return filepath.Join(o.managed, "history.jsonl") }
func (o *Owner) Evidence() string { return filepath.Join(o.managed, "evidence.jsonl") }
func (o *Owner) RootLock() string { return filepath.Join(o.managed, ".root.lock") }

func (o *Owner) Change(name string) (string, error) {
	return o.segmentTarget(o.Changes(), name)
}

// ChangeLock refuses before any caller can create a lock file, so locking an
// absent change never materializes its directory inside the managed tree.
func (o *Owner) ChangeLock(name string) (string, error) {
	change, err := o.Change(name)
	if err != nil {
		return "", err
	}
	if err := o.requireChange(change); err != nil {
		return "", err
	}
	return ChangeLockFor(change), nil
}

// ChangeLockFor names the lock guarding one change folder. The lock sits beside
// that folder rather than inside it, because archive renames the folder while
// holding the lock and Windows refuses to move a directory that holds an open
// handle. This is the one derivation of that path: a state writer that has only
// the state file's folder resolves the same lock every other caller takes.
func ChangeLockFor(change string) string {
	change = filepath.Clean(change)
	return filepath.Join(filepath.Dir(change), filepath.Base(change)+".lock")
}

func (o *Owner) requireChange(change string) error {
	if _, err := os.Lstat(o.managed); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return failure.New("not_initialized", o.root, o.managed,
				"project has no managed root", "initialize the project")
		}
		return failure.New("path_unreadable", o.root, o.managed, err.Error(), "initialize the project")
	}
	info, err := os.Lstat(change)
	if errors.Is(err, os.ErrNotExist) {
		return failure.New("change_not_found", o.root, change,
			"change does not exist", "choose an existing change")
	}
	if err != nil {
		return failure.New("path_unreadable", o.root, change, err.Error(), "choose an existing change")
	}
	if !info.IsDir() {
		return failure.New("change_not_found", o.root, change,
			"change path is not a directory", "choose an existing change")
	}
	return nil
}

func (o *Owner) ChangeState(name string) (string, error) {
	change, err := o.Change(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(change, "state.json"), nil
}

func (o *Owner) ChangeProposal(name string) (string, error) {
	change, err := o.Change(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(change, "proposal.md"), nil
}

func (o *Owner) ChangeDesign(name string) (string, error) {
	change, err := o.Change(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(change, "design.md"), nil
}

func (o *Owner) ChangeTasks(name string) (string, error) {
	change, err := o.Change(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(change, "tasks.md"), nil
}

func (o *Owner) ChangeSpecs(name string) (string, error) {
	change, err := o.Change(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(change, "specs"), nil
}

func (o *Owner) AcceptedSpec(capability string) (string, error) {
	dir, err := o.segmentTarget(o.Specs(), capability)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spec.md"), nil
}

func (o *Owner) ChangeSpec(change, capability string) (string, error) {
	dir, err := o.ChangeSpecs(change)
	if err != nil {
		return "", err
	}
	dir, err = o.segmentTarget(dir, capability)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spec.md"), nil
}

func (o *Owner) EnsureUniqueChange(name string) error {
	active, err := o.Change(name)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(active); err == nil {
		return failure.New("duplicate_change", o.root, active, "active change already exists", "choose another name")
	} else if !errors.Is(err, os.ErrNotExist) {
		return failure.New("path_unreadable", o.root, active, err.Error(), "choose another name")
	}
	entries, err := os.ReadDir(o.Archive())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return failure.New("path_unreadable", o.root, o.Archive(), err.Error(), "choose another name")
	}
	for _, entry := range entries {
		match := archivePattern.FindStringSubmatch(entry.Name())
		if len(match) == 2 && match[1] == name {
			return failure.New("duplicate_change", o.root, filepath.Join(o.Archive(), entry.Name()), "archived change already exists", "choose another name")
		}
	}
	return nil
}

// CheckWriteTarget refuses escapes and every symlink in the existing prefix.
func (o *Owner) CheckWriteTarget(target string) error {
	target = filepath.Clean(target)
	relative, err := filepath.Rel(o.managed, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return failure.New("path_escape", o.root, target, "managed target escapes .specd", "choose another name")
	}
	current := o.managed
	parts := []string{}
	if relative != "." {
		parts = strings.Split(relative, string(filepath.Separator))
	}
	for _, part := range append([]string{managedDir}, parts...) {
		if part == managedDir {
			current = o.managed
		} else {
			current = filepath.Join(current, part)
		}
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return failure.New("path_unreadable", o.root, current, statErr.Error(), "choose another name")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return failure.New("path_symlink", o.root, current, "managed target contains a symlink", "choose another name")
		}
	}
	return nil
}

func (o *Owner) segmentTarget(parent, segment string) (string, error) {
	if err := ValidateSegment(segment); err != nil {
		return "", err
	}
	target := filepath.Join(parent, segment)
	if err := o.CheckWriteTarget(target); err != nil {
		return "", err
	}
	return target, nil
}

func (o *Owner) String() string {
	return fmt.Sprintf("root=%s managed=%s", o.root, o.managed)
}
