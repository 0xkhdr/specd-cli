// Package transaction commits a bounded set of managed file writes inside one
// selected root as a single recoverable unit.
//
// The filesystem gives no multi-file atomicity, so this package adds the
// smallest mechanism that keeps the promise the sync and archive owners make:
// after any interruption every declared output holds either all of its old
// bytes or all of its planned bytes, and exactly one deterministic recovery
// action exists.
//
// The mechanism is a write-ahead manifest. Staged bytes are written and synced
// first; the manifest write is the commit point; replacement is a replayable
// projection of the manifest. A manifest that exists is committed and rolls
// forward. Staging without a manifest was never committed and rolls back.
//
// Everything here is local deterministic filesystem logic: no network, no LLM,
// no clock of its own — the caller injects the time recovery judges against.
package transaction

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
	"github.com/0xkhdr/specd-cli/internal/core/lock"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/core/persist"
	"github.com/0xkhdr/specd-cli/internal/core/record"
)

// SchemaVersion versions the manifest contract. A manifest from any other
// version is refused rather than reinterpreted.
const SchemaVersion = 1

const (
	prefix         = ".txn-"
	manifestSuffix = ".json"
)

// Stable refusal codes. Each names one operator action that is never a manual
// edit of state, history, evidence, or a manifest.
const (
	CodeInvalid   = "transaction_invalid"
	CodeConflict  = "transaction_conflict"
	CodeDrift     = "transaction_drift"
	CodeAmbiguous = "transaction_ambiguous"
	CodeIO        = "transaction_io"
)

// Recovery actions. A pending transaction resolves to exactly one of them.
const (
	ActionRollForward = "roll_forward"
	ActionRollback    = "rollback"
)

// Write is one planned output. Path is managed-relative with `/` separators,
// so a manifest never embeds an absolute machine path.
type Write struct {
	Path   string
	Before string
	Bytes  []byte
}

// Output is the committed description of one planned write. Before is empty
// exactly when the target did not exist when the plan was built.
type Output struct {
	Path   string `json:"path"`
	Before string `json:"before,omitempty"`
	After  string `json:"after"`
}

// Move is one managed directory relocation. Both paths are managed-relative.
// A move is old-or-new by rename, so it needs no staged bytes.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Manifest is the write-ahead record. ID is derived from every other field, so
// a manifest cannot be edited without losing its identity.
//
// Record is the one history record this transaction owes. Carrying it inside
// the manifest is what makes an interrupted history append recoverable: the
// transaction identity proves the exact committed outputs before the missing
// record is appended, and the record's own content-derived id makes the append
// idempotent.
type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Operation     string          `json:"operation"`
	Change        string          `json:"change"`
	Revision      uint64          `json:"revision"`
	Timestamp     string          `json:"timestamp"`
	Outputs       []Output        `json:"outputs"`
	Moves         []Move          `json:"moves,omitempty"`
	Record        json.RawMessage `json:"record,omitempty"`
}

// Request is one commit attempt. Hook is nil in production; tests use it to
// interrupt the sequence at a stable boundary.
type Request struct {
	Operation string
	Change    string
	Revision  uint64
	Now       time.Time
	Outputs   []Write
	Moves     []Move
	Record    *record.Record
	Hook      persist.Hook
}

// Result is one committed transaction. NoOp is true when every target already
// held the planned bytes, so nothing was written and no manifest existed.
type Result struct {
	ID       string   `json:"id"`
	Manifest Manifest `json:"manifest"`
	NoOp     bool     `json:"no_op"`
}

// Pending is one interrupted transaction and its sole legal recovery action.
type Pending struct {
	ID       string    `json:"id"`
	Action   string    `json:"action"`
	Manifest *Manifest `json:"manifest,omitempty"`
}

func refuse(code, path, reason, next string) error {
	return failure.New(code, "", path, reason, next)
}

// Commit stages, commits, and applies every declared output as one unit. It
// first resolves any interrupted transaction, so a retry after a crash is the
// same call rather than a separate repair verb.
func Commit(owner *corepath.Owner, request Request) (Result, error) {
	var result Result
	err := lock.With(owner.RootLock(), func() error {
		var commitErr error
		result, commitErr = CommitUnderRootLock(owner, request)
		return commitErr
	})
	return result, err
}

// CommitUnderRootLock is Commit for a caller that already holds the root lock.
// It exists so an operation can take the root lock before the change lock —
// the one global lock order — instead of inverting it here.
func CommitUnderRootLock(owner *corepath.Owner, request Request) (Result, error) {
	manifest, err := plan(request)
	if err != nil {
		return Result{}, err
	}
	if _, err := recoverLocked(owner, request.Now); err != nil {
		return Result{}, err
	}
	return commitLocked(owner, request, manifest)
}

// Inspect reports every interrupted transaction and its one legal action. It
// reads only; a malformed or ambiguous transaction fails closed here too, so a
// caller cannot proceed past one by not recovering.
func Inspect(owner *corepath.Owner, now time.Time) ([]Pending, error) {
	var pending []Pending
	err := lock.With(owner.RootLock(), func() error {
		var err error
		pending, err = inspectLocked(owner, now)
		return err
	})
	return pending, err
}

// Recover performs the one legal action for every interrupted transaction and
// returns what it resolved. Repeating it is safe: the same manifest identity
// yields the same action, and a fully applied transaction resolves to nothing.
func Recover(owner *corepath.Owner, now time.Time) ([]Pending, error) {
	var resolved []Pending
	err := lock.With(owner.RootLock(), func() error {
		var err error
		resolved, err = recoverLocked(owner, now)
		return err
	})
	return resolved, err
}

// plan turns a request into the exact manifest it would commit. It touches no
// filesystem, so an invalid request is refused before anything is staged.
func plan(request Request) (Manifest, error) {
	if strings.TrimSpace(request.Operation) == "" || strings.TrimSpace(request.Change) == "" ||
		request.Revision == 0 {
		return Manifest{}, refuse(CodeInvalid, "",
			"a transaction declares an operation, a change, and a state revision",
			"retry the operation from a resolved change state")
	}
	if request.Now.IsZero() {
		return Manifest{}, refuse(CodeInvalid, "", "a transaction requires an injected clock",
			"retry the operation from a resolved change state")
	}
	if len(request.Outputs) == 0 && len(request.Moves) == 0 {
		return Manifest{}, refuse(CodeInvalid, "", "a transaction declares at least one output",
			"retry the operation from a resolved change state")
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion, Operation: request.Operation, Change: request.Change,
		Revision: request.Revision, Timestamp: request.Now.UTC().Format(time.RFC3339Nano),
	}
	seen := make(map[string]bool, len(request.Outputs))
	for _, write := range request.Outputs {
		if err := validRelative(write.Path); err != nil {
			return Manifest{}, err
		}
		if seen[write.Path] {
			return Manifest{}, refuse(CodeInvalid, write.Path,
				"output path is declared more than once",
				"declare each managed output exactly once")
		}
		seen[write.Path] = true
		if write.Before != "" && !validHash(write.Before) {
			return Manifest{}, refuse(CodeInvalid, write.Path,
				"declared source hash is not a sha256 digest",
				"rebuild the plan and retry the operation")
		}
		if write.Bytes == nil {
			return Manifest{}, refuse(CodeInvalid, write.Path,
				"output bytes are required", "rebuild the plan and retry the operation")
		}
		manifest.Outputs = append(manifest.Outputs, Output{
			Path: write.Path, Before: write.Before, After: hash(write.Bytes),
		})
	}
	// One deterministic order so the same plan always yields the same identity
	// and the same replay sequence.
	slices.SortFunc(manifest.Outputs, func(a, b Output) int { return cmp.Compare(a.Path, b.Path) })
	for _, move := range request.Moves {
		if err := validRelative(move.From); err != nil {
			return Manifest{}, err
		}
		if err := validRelative(move.To); err != nil {
			return Manifest{}, err
		}
		if move.From == move.To || under(move.To, move.From) || under(move.From, move.To) {
			return Manifest{}, refuse(CodeInvalid, move.From,
				"a move source and destination may not overlap",
				"declare one move between two distinct managed paths")
		}
		manifest.Moves = append(manifest.Moves, move)
	}
	slices.SortFunc(manifest.Moves, func(a, b Move) int { return cmp.Compare(a.From, b.From) })
	if request.Record != nil {
		if err := request.Record.Validate(); err != nil {
			return Manifest{}, refuse(CodeInvalid, "", "bound history record is invalid: "+err.Error(),
				"rebuild the plan and retry the operation")
		}
		if request.Record.Change != request.Change {
			return Manifest{}, refuse(CodeInvalid, "",
				"bound history record belongs to another change",
				"rebuild the plan and retry the operation")
		}
		encoded, err := json.Marshal(request.Record)
		if err != nil {
			return Manifest{}, refuse(CodeInvalid, "", err.Error(),
				"rebuild the plan and retry the operation")
		}
		manifest.Record = encoded
	}
	manifest.ID = identity(manifest)
	return manifest, manifest.validate()
}

// under reports whether child is value or a path inside it.
func under(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func commitLocked(owner *corepath.Owner, request Request, manifest Manifest) (Result, error) {
	staged := make(map[string][]byte, len(request.Outputs))
	for _, write := range request.Outputs {
		staged[write.Path] = write.Bytes
	}
	if err := owner.CheckWriteTarget(owner.Managed()); err != nil {
		return Result{}, err
	}
	noop := true
	for _, output := range manifest.Outputs {
		target, err := resolve(owner, output.Path)
		if err != nil {
			return Result{}, err
		}
		current, err := readTarget(target)
		if err != nil {
			return Result{}, err
		}
		switch current {
		case output.After:
		case output.Before:
			noop = false
		default:
			return Result{}, refuse(CodeDrift, target,
				"the source bytes changed after the plan was built",
				"rebuild the plan, review it again, and retry the operation")
		}
	}
	if noop && len(manifest.Moves) == 0 && len(manifest.Record) == 0 {
		// Nothing to write means nothing to recover: a no-op leaves no
		// manifest and therefore no transaction for the next command to see.
		return Result{ID: manifest.ID, Manifest: manifest, NoOp: true}, nil
	}

	stage := filepath.Join(owner.Managed(), prefix+manifest.ID)
	if err := os.RemoveAll(stage); err != nil {
		return Result{}, ioRefusal(stage, err)
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return Result{}, ioRefusal(stage, err)
	}
	for index, output := range manifest.Outputs {
		if err := writeSynced(stagedPath(stage, index), staged[output.Path]); err != nil {
			return Result{}, err
		}
	}
	if err := syncDir(stage); err != nil {
		return Result{}, err
	}
	if err := call(request.Hook, "after-stage"); err != nil {
		return Result{}, interrupted(stage, err)
	}

	// The manifest write is the commit point. Before it the transaction does
	// not exist; after it every output is guaranteed old-or-new by replay.
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Result{}, ioRefusal(stage, err)
	}
	journal := filepath.Join(owner.Managed(), prefix+manifest.ID+manifestSuffix)
	if err := persist.AtomicReplace(journal, append(encoded, '\n'), nil); err != nil {
		_ = os.RemoveAll(stage)
		return Result{}, ioRefusal(journal, err)
	}
	if err := call(request.Hook, "after-manifest"); err != nil {
		return Result{}, interrupted(journal, err)
	}
	if err := apply(owner, manifest, request.Hook); err != nil {
		return Result{}, err
	}
	return Result{ID: manifest.ID, Manifest: manifest}, nil
}

// apply replays a committed manifest. It is idempotent by construction: an
// output already holding the planned bytes is skipped, so an interrupted apply
// resumes rather than restarts.
func apply(owner *corepath.Owner, manifest Manifest, hook persist.Hook) error {
	stage := filepath.Join(owner.Managed(), prefix+manifest.ID)
	journal := filepath.Join(owner.Managed(), prefix+manifest.ID+manifestSuffix)
	for index, output := range manifest.Outputs {
		target, err := locate(owner, manifest, output.Path)
		if err != nil {
			return err
		}
		current, err := readTarget(target)
		if err != nil {
			return err
		}
		if current == output.After {
			continue
		}
		if current != output.Before {
			return refuse(CodeAmbiguous, target,
				"the output holds neither the recorded old nor the recorded new bytes",
				"restore the managed file from version control and retry the operation")
		}
		source := stagedPath(stage, index)
		raw, err := readStaged(source)
		if err != nil {
			return err
		}
		if hash(raw) != output.After {
			return refuse(CodeAmbiguous, source,
				"staged bytes do not match the committed output hash",
				"restore the managed file from version control and retry the operation")
		}
		if err := call(hook, "before-replace:"+output.Path); err != nil {
			return interrupted(target, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return ioRefusal(target, err)
		}
		if err := os.Rename(source, target); err != nil {
			return ioRefusal(target, err)
		}
		if err := syncDir(filepath.Dir(target)); err != nil {
			return err
		}
		if err := call(hook, "after-replace:"+output.Path); err != nil {
			return interrupted(target, err)
		}
	}
	for _, move := range manifest.Moves {
		if err := applyMove(owner, move, hook); err != nil {
			return err
		}
	}
	if err := appendBoundRecord(owner, manifest); err != nil {
		return err
	}
	if err := call(hook, "before-cleanup"); err != nil {
		return interrupted(journal, err)
	}
	// The manifest is removed before the staging directory: staging without a
	// manifest is unambiguously an uncommitted remnant.
	if err := removeDurable(journal); err != nil {
		return err
	}
	if err := os.RemoveAll(stage); err != nil {
		return ioRefusal(stage, err)
	}
	return syncDir(owner.Managed())
}

// applyMove relocates one managed directory. Rename is the whole mechanism: it
// is atomic within one filesystem, which is why a cross-filesystem copy-delete
// fallback is refused rather than emulated.
func applyMove(owner *corepath.Owner, move Move, hook persist.Hook) error {
	from, err := resolve(owner, move.From)
	if err != nil {
		return err
	}
	to, err := resolve(owner, move.To)
	if err != nil {
		return err
	}
	source, sourceErr := os.Lstat(from)
	_, destErr := os.Lstat(to)
	switch {
	case errors.Is(sourceErr, os.ErrNotExist) && destErr == nil:
		return nil
	case sourceErr != nil:
		return refuse(CodeAmbiguous, from, "move source is missing or unreadable",
			"restore .specd from version control and retry the operation")
	case destErr == nil:
		return refuse(CodeAmbiguous, to, "move source and destination both exist",
			"restore .specd from version control and retry the operation")
	case !errors.Is(destErr, os.ErrNotExist):
		return ioRefusal(to, destErr)
	case !source.IsDir() || source.Mode()&os.ModeSymlink != 0:
		return refuse(CodeInvalid, from, "move source is not a regular directory",
			"restore .specd from version control and retry the operation")
	}
	if err := call(hook, "before-move:"+move.From); err != nil {
		return interrupted(from, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return ioRefusal(to, err)
	}
	if err := os.Rename(from, to); err != nil {
		return ioRefusal(to, err)
	}
	if err := syncDir(filepath.Dir(to)); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(from)); err != nil {
		return err
	}
	return call(hook, "after-move:"+move.From)
}

// appendBoundRecord appends the manifest's history record exactly once. A
// duplicate id means a previous attempt already appended it, which is success
// on replay, not a second semantic record.
func appendBoundRecord(owner *corepath.Owner, manifest Manifest) error {
	if len(manifest.Record) == 0 {
		return nil
	}
	var item record.Record
	if err := json.Unmarshal(manifest.Record, &item); err != nil {
		return refuse(CodeInvalid, owner.History(), "bound history record is malformed",
			"restore .specd from version control and retry the operation")
	}
	err := record.Append(owner.History(), record.FamilyHistory, item)
	if err == nil || failure.IsCode(err, "record-duplicate") {
		return nil
	}
	return err
}

// locate resolves one output path, following any move of this transaction that
// has already been applied. Without it a replayed file output that lives inside
// a moved folder would look absent and read as ambiguous.
func locate(owner *corepath.Owner, manifest Manifest, relative string) (string, error) {
	target, err := resolve(owner, relative)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil || !errors.Is(err, os.ErrNotExist) {
		return target, nil
	}
	for _, move := range manifest.Moves {
		if !under(relative, move.From) {
			continue
		}
		moved := move.To + strings.TrimPrefix(relative, move.From)
		if resolved, err := resolve(owner, moved); err == nil {
			if _, statErr := os.Lstat(resolved); statErr == nil {
				return resolved, nil
			}
		}
	}
	return target, nil
}

func inspectLocked(owner *corepath.Owner, now time.Time) ([]Pending, error) {
	entries, err := os.ReadDir(owner.Managed())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ioRefusal(owner.Managed(), err)
	}
	journals := map[string]*Manifest{}
	stages := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if !strings.HasSuffix(name, manifestSuffix) {
			if entry.IsDir() {
				stages[strings.TrimPrefix(name, prefix)] = true
			}
			continue
		}
		full := filepath.Join(owner.Managed(), name)
		if !entry.Type().IsRegular() {
			return nil, refuse(CodeInvalid, full, "transaction manifest is not a regular file",
				"remove the unsafe path from version control and retry the operation")
		}
		manifest, err := readManifest(full, now)
		if err != nil {
			return nil, err
		}
		if name != prefix+manifest.ID+manifestSuffix {
			return nil, refuse(CodeInvalid, full,
				"transaction manifest name does not match its identity",
				"restore .specd from version control and retry the operation")
		}
		journals[manifest.ID] = manifest
	}

	var pending []Pending
	for id, manifest := range journals {
		pending = append(pending, Pending{ID: id, Action: ActionRollForward, Manifest: manifest})
	}
	for id := range stages {
		if _, committed := journals[id]; committed {
			continue
		}
		pending = append(pending, Pending{ID: id, Action: ActionRollback})
	}
	slices.SortFunc(pending, func(a, b Pending) int { return cmp.Compare(a.ID, b.ID) })
	return pending, nil
}

func recoverLocked(owner *corepath.Owner, now time.Time) ([]Pending, error) {
	pending, err := inspectLocked(owner, now)
	if err != nil {
		return nil, err
	}
	for _, item := range pending {
		if item.Action == ActionRollback {
			stage := filepath.Join(owner.Managed(), prefix+item.ID)
			if err := os.RemoveAll(stage); err != nil {
				return nil, ioRefusal(stage, err)
			}
			if err := syncDir(owner.Managed()); err != nil {
				return nil, err
			}
			continue
		}
		if err := apply(owner, *item.Manifest, nil); err != nil {
			return nil, err
		}
	}
	return pending, nil
}

func readManifest(path string, now time.Time) (*Manifest, error) {
	raw, err := readStaged(path)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, refuse(CodeInvalid, path, "transaction manifest is malformed: "+err.Error(),
			"restore .specd from version control and retry the operation")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, refuse(CodeInvalid, path, "transaction manifest has trailing data",
			"restore .specd from version control and retry the operation")
	}
	if err := manifest.validate(); err != nil {
		var refusal *failure.Refusal
		if errors.As(err, &refusal) {
			return nil, refuse(refusal.Code, path, refusal.Reason, refusal.Next)
		}
		return nil, err
	}
	// A duplicate JSON key is harmless here: identity is recomputed from the
	// decoded content, so the manifest is judged by what it decoded to.
	stamp, err := time.Parse(time.RFC3339Nano, manifest.Timestamp)
	if err != nil {
		return nil, refuse(CodeInvalid, path, "transaction timestamp is malformed",
			"restore .specd from version control and retry the operation")
	}
	if stamp.After(now.UTC()) {
		return nil, refuse(CodeInvalid, path, "transaction timestamp is in the future",
			"correct the system clock and retry the operation")
	}
	return &manifest, nil
}

func (manifest Manifest) validate() error {
	if manifest.SchemaVersion != SchemaVersion {
		return refuse(CodeInvalid, "",
			fmt.Sprintf("unsupported transaction schema version %d", manifest.SchemaVersion),
			"upgrade specd and retry the operation")
	}
	if strings.TrimSpace(manifest.Operation) == "" || strings.TrimSpace(manifest.Change) == "" ||
		manifest.Revision == 0 || strings.TrimSpace(manifest.Timestamp) == "" ||
		len(manifest.Outputs)+len(manifest.Moves) == 0 {
		return refuse(CodeInvalid, "", "transaction manifest is incomplete",
			"restore .specd from version control and retry the operation")
	}
	previousMove := ""
	for _, move := range manifest.Moves {
		if err := validRelative(move.From); err != nil {
			return err
		}
		if err := validRelative(move.To); err != nil {
			return err
		}
		if move.From <= previousMove || under(move.To, move.From) || under(move.From, move.To) {
			return refuse(CodeInvalid, move.From,
				"transaction moves are not in one deterministic order",
				"restore .specd from version control and retry the operation")
		}
		previousMove = move.From
	}
	if len(manifest.Record) != 0 {
		var item record.Record
		if json.Unmarshal(manifest.Record, &item) != nil || item.Validate() != nil ||
			item.Change != manifest.Change {
			return refuse(CodeInvalid, "", "bound history record is invalid",
				"restore .specd from version control and retry the operation")
		}
	}
	previous := ""
	for _, output := range manifest.Outputs {
		if err := validRelative(output.Path); err != nil {
			return err
		}
		if output.Path <= previous {
			return refuse(CodeInvalid, output.Path,
				"transaction outputs are not in one deterministic order",
				"restore .specd from version control and retry the operation")
		}
		previous = output.Path
		if !validHash(output.After) || output.Before != "" && !validHash(output.Before) {
			return refuse(CodeInvalid, output.Path, "transaction output hash is malformed",
				"restore .specd from version control and retry the operation")
		}
	}
	if manifest.ID != identity(manifest) {
		return refuse(CodeInvalid, "", "transaction identity does not match its content",
			"restore .specd from version control and retry the operation")
	}
	return nil
}

func identity(manifest Manifest) string {
	manifest.ID = ""
	raw, _ := json.Marshal(manifest)
	return hash(raw)
}

// validRelative keeps every managed output inside .specd, out of the dot-file
// namespace the harness reserves for locks and manifests, and free of any
// traversal or separator surprise.
func validRelative(value string) error {
	next := "declare managed outputs as relative paths inside .specd"
	if value == "" || value != path.Clean(value) || strings.HasPrefix(value, "/") ||
		strings.Contains(value, `\`) || value == ".." || strings.HasPrefix(value, "../") {
		return refuse(CodeInvalid, value, "output path is not a safe managed relative path", next)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return refuse(CodeInvalid, value, "output path segment is empty or reserved", next)
		}
	}
	return nil
}

func resolve(owner *corepath.Owner, relative string) (string, error) {
	if err := validRelative(relative); err != nil {
		return "", err
	}
	target := filepath.Join(owner.Managed(), filepath.FromSlash(relative))
	if err := owner.CheckWriteTarget(target); err != nil {
		return "", err
	}
	return target, nil
}

// readTarget returns the sha256 of an existing regular target, or "" when the
// target is absent. Anything else is unsafe and refuses.
//
// ponytail: this is an Lstat-then-read rather than an O_NOFOLLOW open, because
// the platform-split helper the command layer uses is not in this package's
// declared scope. It runs under the root lock inside .specd and refuses every
// non-regular entry. Upgrade path: share one no-follow reader across packages
// when a second caller needs it.
func readTarget(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", ioRefusal(path, err)
	}
	if !info.Mode().IsRegular() {
		return "", refuse(CodeInvalid, path, "managed output is not a regular file",
			"replace it with a regular file under version control and retry the operation")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ioRefusal(path, err)
	}
	return hash(raw), nil
}

func readStaged(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, refuse(CodeAmbiguous, path, "staged transaction bytes are missing",
			"restore .specd from version control and retry the operation")
	}
	if err != nil {
		return nil, ioRefusal(path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, refuse(CodeInvalid, path, "staged transaction path is not a regular file",
			"restore .specd from version control and retry the operation")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, ioRefusal(path, err)
	}
	return raw, nil
}

func stagedPath(stage string, index int) string {
	return filepath.Join(stage, fmt.Sprintf("%04d", index))
}

func writeSynced(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ioRefusal(path, err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return ioRefusal(path, err)
	}
	if err := file.Sync(); err != nil {
		return ioRefusal(path, err)
	}
	return nil
}

func syncDir(path string) error {
	if err := persist.SyncDir(path); err != nil && !errors.Is(err, os.ErrInvalid) {
		return ioRefusal(path, err)
	}
	return nil
}

func removeDurable(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ioRefusal(path, err)
	}
	return syncDir(filepath.Dir(path))
}

func call(hook persist.Hook, step string) error {
	if hook == nil {
		return nil
	}
	return hook(step)
}

func interrupted(path string, err error) error {
	var refusal *failure.Refusal
	if errors.As(err, &refusal) {
		return refusal
	}
	return refuse(CodeConflict, path, "transaction interrupted: "+err.Error(),
		"retry the operation so the pending transaction is resolved")
}

func ioRefusal(path string, err error) error {
	return refuse(CodeIO, path, err.Error(),
		"repair the managed path from version control and retry the operation")
}

func hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
