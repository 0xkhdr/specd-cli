package context

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/plan"
)

type Lane string

const (
	LaneRequiredInput          Lane = "required_input"
	LaneOptionalExistingOutput Lane = "optional_existing_output"
	LaneProspectiveOutput      Lane = "prospective_output"

	// RequiredInputSchemaMismatch records the stage-2 contract gap explicitly:
	// task rows have no required-input field. Callers must pass that lane
	// separately; task.Files is write scope and is never treated as input.
	RequiredInputSchemaMismatch = "stage-2 tasks have no required-input field; pass required inputs explicitly"
)

const (
	maxSelectorFiles = 128
	maxSelectorBytes = 4 << 20
	maxExactBytes    = 4 << 20
)

type SourceRef struct {
	Path     string
	Location plan.Location
}

type ResolvedSource struct {
	Path     string
	Lane     Lane
	Selector string
	Location plan.Location
	Digest   string
	Content  []byte
}

type ResolveError struct {
	Code   string
	Path   string
	Owner  string
	Reason string
	Next   string
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("%s: %s: %s; owner: %s; next: %s",
		e.Code, e.Path, e.Reason, e.Owner, e.Next)
}

// resolver contains only a test seam for proving read/digest drift refusal.
type resolver struct {
	root      string
	afterRead func(string)
}

// ResolveContextLanes resolves explicit required read inputs and declared write
// outputs. It returns no partial result on any refusal.
func ResolveContextLanes(root string, requiredInputs, declaredOutputs []SourceRef) ([]ResolvedSource, error) {
	canonical, err := canonicalRoot(root)
	if err != nil {
		return nil, resolveFailure("context_root_invalid", root, err, "resolve the named path")
	}
	return (&resolver{root: canonical}).resolve(requiredInputs, declaredOutputs)
}

func (r *resolver) resolve(requiredInputs, declaredOutputs []SourceRef) ([]ResolvedSource, error) {
	var items []ResolvedSource
	for _, source := range requiredInputs {
		resolved, err := r.resolveRequired(source)
		if err != nil {
			return nil, err
		}
		items = append(items, resolved...)
	}
	for _, source := range declaredOutputs {
		item, err := r.resolveOutput(source)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *resolver) resolveRequired(source SourceRef) ([]ResolvedSource, error) {
	if strings.ContainsAny(source.Path, "*?[") {
		return r.resolveSelector(source)
	}
	relative, target, err := boundedPath(r.root, source.Path)
	if err != nil {
		return nil, resolveFailure("context_path_unsafe", source.Path, err, "resolve the named path")
	}
	content, digest, _, err := r.readStable(target)
	if err != nil {
		code := "context_required_missing"
		if !errors.Is(err, os.ErrNotExist) {
			code = "context_required_unsafe"
		}
		return nil, resolveFailure(code, source.Path, err, "resolve the named path")
	}
	return []ResolvedSource{{
		Path: relative, Lane: LaneRequiredInput, Location: source.Location,
		Digest: digest, Content: content,
	}}, nil
}

func (r *resolver) resolveOutput(source SourceRef) (ResolvedSource, error) {
	if strings.ContainsAny(source.Path, "*?[") {
		return ResolvedSource{}, resolveFailure("context_output_unsafe", source.Path,
			errors.New("declared output must be one exact path"), "revise plan then reapprove")
	}
	relative, target, err := boundedPath(r.root, source.Path)
	if err != nil {
		return ResolvedSource{}, resolveFailure("context_output_unsafe", source.Path, err, "revise plan then reapprove")
	}
	content, digest, _, err := r.readStable(target)
	if errors.Is(err, os.ErrNotExist) {
		return ResolvedSource{
			Path: relative, Lane: LaneProspectiveOutput, Location: source.Location,
		}, nil
	}
	if err != nil {
		return ResolvedSource{}, resolveFailure("context_output_unsafe", source.Path, err, "revise plan then reapprove")
	}
	return ResolvedSource{
		Path: relative, Lane: LaneOptionalExistingOutput, Location: source.Location,
		Digest: digest, Content: content,
	}, nil
}

func (r *resolver) resolveSelector(source SourceRef) ([]ResolvedSource, error) {
	base, suffix, recursive, err := boundedSelector(source.Path)
	if err != nil {
		return nil, resolveFailure("context_selector_unbounded", source.Path, err, "revise plan then reapprove")
	}
	_, basePath, err := boundedPath(r.root, base)
	if err != nil {
		return nil, resolveFailure("context_selector_unsafe", source.Path, err, "resolve the named path")
	}
	matches, err := enumerateSelector(r.root, basePath, suffix, recursive)
	if err != nil {
		return nil, resolveFailure("context_selector_unsafe", source.Path, err, "resolve the named path")
	}
	if len(matches) == 0 {
		return nil, resolveFailure("context_selector_empty", source.Path,
			errors.New("selector matched no files"), "revise plan then reapprove")
	}
	if len(matches) > maxSelectorFiles {
		return nil, resolveFailure("context_selector_unbounded", source.Path,
			fmt.Errorf("selector matched %d files; maximum is %d", len(matches), maxSelectorFiles),
			"revise plan then reapprove")
	}
	items := make([]ResolvedSource, 0, len(matches))
	total := 0
	for _, match := range matches {
		_, target, err := boundedPath(r.root, match.path)
		if err != nil {
			return nil, resolveFailure("context_selector_unsafe", match.path, err, "resolve the named path")
		}
		content, digest, opened, err := r.readStable(target)
		if err != nil || !os.SameFile(match.info, opened) {
			if err == nil {
				err = errors.New("selector match identity changed during assembly")
			}
			return nil, resolveFailure("context_required_unsafe", match.path, err, "resolve the named path")
		}
		total += len(content)
		if total > maxSelectorBytes {
			return nil, resolveFailure("context_selector_unbounded", source.Path,
				fmt.Errorf("selector exceeds %d bytes", maxSelectorBytes), "revise plan then reapprove")
		}
		items = append(items, ResolvedSource{
			Path: match.path, Lane: LaneRequiredInput, Selector: source.Path,
			Location: source.Location, Digest: digest, Content: content,
		})
	}
	after, err := enumerateSelector(r.root, basePath, suffix, recursive)
	if err != nil || !sameSelectorMatches(matches, after) {
		if err == nil {
			err = errors.New("selector membership changed during assembly")
		}
		return nil, resolveFailure("context_snapshot_stale", source.Path, err, "regenerate context")
	}
	return items, nil
}

type selectorMatch struct {
	path string
	info os.FileInfo
}

func enumerateSelector(root, basePath, suffix string, recursive bool) ([]selectorMatch, error) {
	var matches []selectorMatch
	err := filepath.WalkDir(basePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == basePath {
			return nil
		}
		relativeToBase, err := filepath.Rel(basePath, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("selector encountered symlink %s", relativeToBase)
		}
		if entry.IsDir() {
			if !recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("selector encountered non-regular file %s", relativeToBase)
		}
		if !recursive && strings.Contains(relativeToBase, string(filepath.Separator)) {
			return nil
		}
		if strings.HasSuffix(filepath.ToSlash(relativeToBase), suffix) {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			matches = append(matches, selectorMatch{path: filepath.ToSlash(relative), info: info})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(matches, func(a, b selectorMatch) int { return cmp.Compare(a.path, b.path) })
	return matches, nil
}

func sameSelectorMatches(first, second []selectorMatch) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index].path != second[index].path || !os.SameFile(first[index].info, second[index].info) {
			return false
		}
	}
	return true
}

func (r *resolver) readStable(path string) ([]byte, string, os.FileInfo, error) {
	first, firstDigest, firstInfo, err := readNoFollow(path)
	if err != nil {
		return nil, "", nil, err
	}
	if r.afterRead != nil {
		r.afterRead(path)
	}
	_, secondDigest, secondInfo, err := readNoFollow(path)
	if err != nil {
		return nil, "", nil, fmt.Errorf("content changed during assembly: %w", err)
	}
	if firstDigest != secondDigest || !os.SameFile(firstInfo, secondInfo) {
		return nil, "", nil, errors.New("content or file identity changed during assembly")
	}
	return first, firstDigest, firstInfo, nil
}

func readNoFollow(path string) ([]byte, string, os.FileInfo, error) {
	if err := rejectSymlinkPrefix(path); err != nil {
		return nil, "", nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", nil, err
	}
	defer file.Close()
	opened, openedErr := file.Stat()
	named, namedErr := os.Lstat(path)
	if openedErr != nil || namedErr != nil {
		return nil, "", nil, fmt.Errorf("inspect source: %v", firstError(openedErr, namedErr))
	}
	if !opened.Mode().IsRegular() || !named.Mode().IsRegular() ||
		named.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, named) {
		return nil, "", nil, errors.New("source must be one regular non-symlink file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxExactBytes+1))
	if err != nil {
		return nil, "", nil, err
	}
	if len(raw) > maxExactBytes {
		return nil, "", nil, fmt.Errorf("source exceeds %d bytes", maxExactBytes)
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), opened, nil
}

func canonicalRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", errors.New("project root must be an existing directory")
	}
	return filepath.Clean(canonical), nil
}

func boundedPath(root, source string) (string, string, error) {
	if source == "" || filepath.IsAbs(source) || strings.Contains(source, "\\") {
		return "", "", errors.New("path must be non-empty project-relative slash syntax")
	}
	clean := filepath.Clean(filepath.FromSlash(source))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		filepath.ToSlash(clean) != source {
		return "", "", errors.New("path traverses or is not canonical")
	}
	target := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("path escapes project root")
	}
	return filepath.ToSlash(relative), target, nil
}

func boundedSelector(selector string) (base, suffix string, recursive bool, err error) {
	if selector == "" || filepath.IsAbs(selector) || strings.ContainsAny(selector, "?[\\") ||
		strings.Contains(selector, "..") {
		return "", "", false, errors.New("selector is empty, unsafe, or unsupported")
	}
	switch {
	case strings.Count(selector, "*") == 1:
		at := strings.Index(selector, "*")
		if at < 2 || selector[at-1] != '/' {
			return "", "", false, errors.New("selector must bind one explicit directory")
		}
		base, suffix = selector[:at-1], selector[at+1:]
	case strings.Count(selector, "*") == 3 && strings.Contains(selector, "/**/"):
		parts := strings.Split(selector, "/**/")
		if len(parts) != 2 || parts[0] == "" || !strings.HasPrefix(parts[1], "*") {
			return "", "", false, errors.New("recursive selector must be dir/**/*.suffix")
		}
		base, suffix, recursive = parts[0], strings.TrimPrefix(parts[1], "*"), true
	default:
		return "", "", false, errors.New("selector must be dir/*suffix or dir/**/*suffix")
	}
	if _, _, err := boundedPath(".", base); err != nil || suffix == "" || strings.Contains(suffix, "/") {
		return "", "", false, errors.New("selector base or suffix is unbounded")
	}
	return base, suffix, recursive, nil
}

func rejectSymlinkPrefix(path string) error {
	current := filepath.VolumeName(path) + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path contains symlink at %s", current)
		}
	}
	return nil
}

func resolveFailure(code, path string, err error, next string) error {
	return &ResolveError{Code: code, Path: path, Owner: "author", Reason: err.Error(), Next: next}
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
