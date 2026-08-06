package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

type Lifecycle string

const (
	LifecyclePlanning    Lifecycle = "planning"
	LifecycleApproved    Lifecycle = "approved"
	LifecycleExecuting   Lifecycle = "executing"
	LifecycleReconciling Lifecycle = "reconciling"
	LifecycleArchived    Lifecycle = "archived"
)

var lifecycleSuccessor = map[Lifecycle]Lifecycle{
	LifecyclePlanning:    LifecycleApproved,
	LifecycleApproved:    LifecycleExecuting,
	LifecycleExecuting:   LifecycleReconciling,
	LifecycleReconciling: LifecycleArchived,
}

func ValidateApprovalTransition(from, to Lifecycle) error {
	next, known := lifecycleSuccessor[from]
	if !known {
		if from == LifecycleArchived {
			return failure.New("lifecycle_transition", "", "", "archived lifecycle is terminal", "inspect the archived change")
		}
		return failure.New("lifecycle_transition", "", "", fmt.Sprintf("unknown lifecycle %q", from), "reload current lifecycle state")
	}
	if next != to {
		return failure.New("lifecycle_transition", "", "",
			fmt.Sprintf("lifecycle %q requires exact successor %q, got %q", from, next, to),
			"request approval for "+string(next))
	}
	return nil
}

type ApprovalArtifact struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

type ApprovalIdentity struct {
	Artifacts     []ApprovalArtifact `json:"artifacts"`
	AggregateHash string             `json:"aggregate_hash"`
}

type ApprovalRecord struct {
	SchemaVersion   int                `json:"schema_version"`
	ID              string             `json:"id"`
	Change          string             `json:"change"`
	Gate            string             `json:"gate"`
	LifecycleFrom   Lifecycle          `json:"lifecycle_from"`
	LifecycleTo     Lifecycle          `json:"lifecycle_to"`
	Approver        string             `json:"approver"`
	ActorClass      string             `json:"actor_class"`
	Artifacts       []ApprovalArtifact `json:"artifacts"`
	AggregateHash   string             `json:"aggregate_hash"`
	RegistryVersion string             `json:"registry_version"`
	PolicyDigest    string             `json:"policy_digest"`
	RevisionBefore  uint64             `json:"revision_before"`
	RevisionAfter   uint64             `json:"revision_after"`
	Timestamp       string             `json:"timestamp"`
	Reason          string             `json:"reason"`
	Assurance       string             `json:"assurance"`
}

const ApprovalSchemaVersion = 1

var (
	recordIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
	registryPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*-v[1-9][0-9]*$`)
)

func ComputeApprovalIdentity(changeRoot string, covered []string) (ApprovalIdentity, error) {
	root, err := filepath.Abs(changeRoot)
	if err != nil {
		return ApprovalIdentity{}, approvalArtifactError(changeRoot, err)
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ApprovalIdentity{}, approvalArtifactError(root, errors.New("change root must be a safe directory"))
	}
	if len(covered) == 0 {
		return ApprovalIdentity{}, approvalArtifactError(root, errors.New("covered artifacts are required"))
	}

	paths := slices.Clone(covered)
	slices.Sort(paths)
	identity := ApprovalIdentity{Artifacts: make([]ApprovalArtifact, 0, len(paths))}
	seen := map[string]bool{}
	var seenFiles []os.FileInfo
	for _, relative := range paths {
		if !safeApprovalPath(relative) || seen[relative] {
			return ApprovalIdentity{}, approvalArtifactError(relative, errors.New("covered path is unsafe or aliases another path"))
		}
		seen[relative] = true
		target := filepath.Join(root, filepath.FromSlash(relative))
		raw, fileInfo, err := readApprovalArtifact(root, target)
		if err != nil {
			return ApprovalIdentity{}, approvalArtifactError(relative, err)
		}
		for _, prior := range seenFiles {
			if os.SameFile(prior, fileInfo) {
				return ApprovalIdentity{}, approvalArtifactError(relative, errors.New("covered path aliases another artifact"))
			}
		}
		seenFiles = append(seenFiles, fileInfo)
		hash := sha256.New()
		hashField(hash, []byte(relative))
		hashField(hash, raw)
		identity.Artifacts = append(identity.Artifacts, ApprovalArtifact{Path: relative, Hash: hex.EncodeToString(hash.Sum(nil))})
	}
	identity.AggregateHash = aggregateApprovalHash(identity.Artifacts)
	return identity, nil
}

func (record ApprovalRecord) Validate() error {
	switch {
	case record.SchemaVersion != ApprovalSchemaVersion:
		return fmt.Errorf("unsupported approval schema version %d", record.SchemaVersion)
	case !recordIDPattern.MatchString(record.ID):
		return errors.New("approval id must be one stable token")
	case corepath.ValidateSegment(record.Change) != nil:
		return errors.New("approval change must be lowercase kebab-case")
	case strings.TrimSpace(record.Approver) == "":
		return errors.New("approval approver is required")
	case record.ActorClass != "human":
		return errors.New("approval actor_class must be human")
	case ValidateApprovalTransition(record.LifecycleFrom, record.LifecycleTo) != nil:
		return errors.New("approval lifecycle transition is not legal")
	case record.Gate != approvalGate(record.LifecycleFrom, record.LifecycleTo):
		return errors.New("approval gate does not match lifecycle transition")
	case !registryPattern.MatchString(record.RegistryVersion):
		return errors.New("approval registry_version is malformed")
	case !validSHA256(record.PolicyDigest):
		return errors.New("approval policy_digest must be a SHA-256 digest")
	case record.RevisionBefore == 0 || record.RevisionAfter != record.RevisionBefore+1:
		return errors.New("approval revisions must describe one forward transition")
	case strings.TrimSpace(record.Reason) == "":
		return errors.New("approval reason is required")
	case record.Assurance != "advisory":
		return errors.New("approval assurance must be advisory without host proof")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.Timestamp); err != nil {
		return errors.New("approval timestamp must be RFC3339")
	}
	if len(record.Artifacts) == 0 {
		return errors.New("approval artifacts are required")
	}
	for index, artifact := range record.Artifacts {
		if !safeApprovalPath(artifact.Path) || len(artifact.Hash) != sha256.Size*2 {
			return fmt.Errorf("invalid approval artifact at index %d", index)
		}
		if _, err := hex.DecodeString(artifact.Hash); err != nil {
			return fmt.Errorf("invalid approval artifact hash at index %d", index)
		}
		if index > 0 && record.Artifacts[index-1].Path >= artifact.Path {
			return errors.New("approval artifacts must be unique and sorted by path")
		}
	}
	if record.AggregateHash != aggregateApprovalHash(record.Artifacts) {
		return errors.New("approval aggregate hash does not match artifacts")
	}
	return nil
}

func safeApprovalPath(value string) bool {
	native := filepath.FromSlash(value)
	drivePath := len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
	return value != "" && value != "." && !strings.Contains(value, "\\") && !drivePath &&
		!filepath.IsAbs(native) && filepath.VolumeName(native) == "" &&
		!strings.HasPrefix(value, "/") && path.Clean(value) == value &&
		value != ".." && !strings.HasPrefix(value, "../")
}

func readApprovalArtifact(root, target string) ([]byte, os.FileInfo, error) {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, errors.New("covered path escapes change root")
	}
	if err := validateApprovalPathComponents(root, relative); err != nil {
		return nil, nil, err
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return nil, nil, errors.New("covered artifact must be a regular file")
	}
	if err := validateApprovalPathComponents(root, relative); err != nil {
		return nil, nil, err
	}
	pathInfo, err := os.Lstat(target)
	if err != nil || !pathInfo.Mode().IsRegular() || !os.SameFile(opened, pathInfo) {
		return nil, nil, errors.New("covered artifact changed during read")
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() ||
		!opened.ModTime().Equal(after.ModTime()) || opened.Mode() != after.Mode() {
		return nil, nil, errors.New("covered artifact changed during read")
	}
	return raw, after, nil
}

func validateApprovalPathComponents(root, relative string) error {
	current := root
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("covered path contains a symlink")
		}
	}
	return nil
}

func aggregateApprovalHash(artifacts []ApprovalArtifact) string {
	hash := sha256.New()
	for _, artifact := range artifacts {
		hashField(hash, []byte(artifact.Path))
		decoded, _ := hex.DecodeString(artifact.Hash)
		hashField(hash, decoded)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func hashField(writer io.Writer, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write(value)
}

func approvalGate(from, to Lifecycle) string {
	return string(from) + "_to_" + string(to)
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func approvalArtifactError(path string, err error) error {
	return failure.New("approval_artifact", "", path, err.Error(), "repair covered artifacts and rerun check")
}
