package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

const SchemaVersion = 1
const AttemptSchemaVersion = 1
const CompletionSchemaVersion = 1
const SyncSchemaVersion = 1
const ArchiveSchemaVersion = 1
const FrictionSchemaVersion = 1

type Family string
type Kind string

const (
	FamilyHistory      Family = "history"
	FamilyEvidence     Family = "evidence"
	KindCreated        Kind   = "created"
	KindApproved       Kind   = "approved"
	KindTaskTransition Kind   = "task_transition"
	KindAttempt        Kind   = "attempt"
	KindCompletion     Kind   = "completion"
	KindObserved       Kind   = "observed"
	KindSynced         Kind   = "synced"
	KindArchived       Kind   = "archived"
	KindFriction       Kind   = "friction"
)

type Record struct {
	SchemaVersion     int             `json:"schema_version"`
	ID                string          `json:"id"`
	Family            Family          `json:"family"`
	Kind              Kind            `json:"kind"`
	Change            string          `json:"change"`
	ExpectedRevision  *uint64         `json:"expected_revision,omitempty"`
	ResultingRevision *uint64         `json:"resulting_revision,omitempty"`
	Timestamp         string          `json:"timestamp"`
	Actor             string          `json:"actor"`
	Payload           json.RawMessage `json:"payload"`
}

type Diagnostic struct {
	Code   string
	File   string
	Line   int
	Reason string
}

// AttemptPayload is the exact history/state binding for one mutable task
// attempt. It lives with the codec so KindAttempt cannot accept generic JSON.
type AttemptPayload struct {
	SchemaVersion  int      `json:"schema_version"`
	ID             string   `json:"id"`
	Change         string   `json:"change"`
	TaskID         string   `json:"task"`
	BaselineHEAD   string   `json:"baseline_head"`
	RevisionBefore uint64   `json:"revision_before"`
	RevisionAfter  uint64   `json:"revision_after"`
	ContractHash   string   `json:"contract_hash"`
	ApprovalHash   string   `json:"approval_hash"`
	StateGuardHash string   `json:"state_guard_hash"`
	DeclaredFiles  []string `json:"declared_files"`
	Actor          string   `json:"actor"`
	Assurance      string   `json:"assurance"`
	ActivityFrom   string   `json:"activity_from"`
	ActivityTo     string   `json:"activity_to"`
}

type CompletionPayload struct {
	SchemaVersion  int    `json:"schema_version"`
	Change         string `json:"change"`
	TaskID         string `json:"task"`
	AttemptID      string `json:"attempt"`
	EvidenceID     string `json:"evidence"`
	Actor          string `json:"actor"`
	ActivityFrom   string `json:"activity_from"`
	ActivityTo     string `json:"activity_to"`
	RevisionBefore uint64 `json:"revision_before"`
	RevisionAfter  uint64 `json:"revision_after"`
}

// SpecHash identifies one accepted capability document by its managed-relative
// path and content. Before is empty exactly when the capability was created.
type SpecHash struct {
	Capability string `json:"capability"`
	Path       string `json:"path"`
	Before     string `json:"before,omitempty"`
	After      string `json:"after"`
}

// SyncPayload is the exact record of one accepted-truth mutation. It binds the
// human who authorized it, the artifact hashes that authorization covered, the
// reconciliation plan, the consumed evidence set, and the exact output bytes,
// so history identifies inputs and outputs without replaying the filesystem.
type SyncPayload struct {
	SchemaVersion  int        `json:"schema_version"`
	Change         string     `json:"change"`
	Approver       string     `json:"approver"`
	ActorClass     string     `json:"actor_class"`
	Reason         string     `json:"reason"`
	ApprovalHash   string     `json:"approval_hash"`
	PlanHash       string     `json:"plan_hash"`
	EvidenceSet    string     `json:"evidence_set"`
	Transaction    string     `json:"transaction"`
	Outputs        []SpecHash `json:"outputs"`
	RevisionBefore uint64     `json:"revision_before"`
	RevisionAfter  uint64     `json:"revision_after"`
	Assurance      string     `json:"assurance"`
}

// ArchivePayload is the exact record of one change folder becoming immutable
// local history. It carries only local facts: no delivery, deployment, or
// remote identity may appear here.
type ArchivePayload struct {
	SchemaVersion  int        `json:"schema_version"`
	Change         string     `json:"change"`
	Actor          string     `json:"actor"`
	Approver       string     `json:"approver"`
	Source         string     `json:"source"`
	Target         string     `json:"target"`
	ChangeHash     string     `json:"change_hash"`
	Accepted       []SpecHash `json:"accepted"`
	EvidenceSet    string     `json:"evidence_set"`
	SyncRecord     string     `json:"sync_record"`
	Transaction    string     `json:"transaction"`
	RevisionBefore uint64     `json:"revision_before"`
	RevisionAfter  uint64     `json:"revision_after"`
}

// FrictionPayload is the exact record of one real blocker observed against a
// deferred domain (D14). It carries no revision transition: friction observes
// truth and never moves lifecycle, authority, or approval, so the observed
// revision and the hashes it was observed against live in the payload as facts.
type FrictionPayload struct {
	SchemaVersion    int    `json:"schema_version"`
	Change           string `json:"change"`
	TaskID           string `json:"task"`
	Operation        string `json:"operation"`
	Domain           string `json:"domain"`
	Blocker          string `json:"blocker"`
	Consequence      string `json:"consequence"`
	Actor            string `json:"actor"`
	ObservedRevision uint64 `json:"observed_revision"`
	ContractHash     string `json:"contract_hash"`
	StateHash        string `json:"state_hash"`
	EvidenceSet      string `json:"evidence_set"`
}

func Revision(value uint64) *uint64 { return &value }

func New(r Record) (Record, error) {
	r.SchemaVersion = SchemaVersion
	r.ID = ""
	payload, err := canonicalPayload(r.Payload)
	if err != nil {
		return Record{}, fmt.Errorf("canonical payload: %w", err)
	}
	r.Payload = payload
	id, err := Identity(r)
	if err != nil {
		return Record{}, err
	}
	r.ID = id
	return r, r.Validate()
}

func Identity(r Record) (string, error) {
	r.ID = ""
	payload, err := canonicalPayload(r.Payload)
	if err != nil {
		return "", fmt.Errorf("canonical payload: %w", err)
	}
	r.Payload = payload
	raw, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("canonical record: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", r.SchemaVersion)
	}
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Change) == "" ||
		strings.TrimSpace(r.Actor) == "" || strings.TrimSpace(r.Timestamp) == "" {
		return errors.New("id, change, timestamp, and actor are required")
	}
	if err := validateFamilyKind(r); err != nil {
		return err
	}
	timestamp, err := time.Parse(time.RFC3339Nano, r.Timestamp)
	if err != nil || !strings.HasSuffix(r.Timestamp, "Z") || timestamp.Location() != time.UTC {
		return errors.New("timestamp must be RFC3339 UTC")
	}
	if _, err := canonicalPayload(r.Payload); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	if r.Kind == KindAttempt {
		attempt, err := DecodeAttemptPayload(r.Payload)
		if err != nil {
			return fmt.Errorf("attempt payload: %w", err)
		}
		if attempt.Change != r.Change || attempt.Actor != r.Actor ||
			r.ExpectedRevision == nil || r.ResultingRevision == nil ||
			attempt.RevisionBefore != *r.ExpectedRevision ||
			attempt.RevisionAfter != *r.ResultingRevision {
			return errors.New("attempt payload does not match history envelope")
		}
	}
	if r.Kind == KindCompletion {
		completion, err := DecodeCompletionPayload(r.Payload)
		if err != nil {
			return fmt.Errorf("completion payload: %w", err)
		}
		if completion.Change != r.Change || completion.Actor != r.Actor ||
			r.ExpectedRevision == nil || r.ResultingRevision == nil ||
			completion.RevisionBefore != *r.ExpectedRevision ||
			completion.RevisionAfter != *r.ResultingRevision {
			return errors.New("completion payload does not match history envelope")
		}
	}
	if r.Kind == KindSynced {
		payload, err := DecodeSyncPayload(r.Payload)
		if err != nil {
			return fmt.Errorf("sync payload: %w", err)
		}
		// The actor of a sync record is the human who authorized it: the
		// record cannot claim one identity in its envelope and another in its
		// payload.
		if payload.Change != r.Change || payload.Approver != r.Actor ||
			r.ExpectedRevision == nil || r.ResultingRevision == nil ||
			payload.RevisionBefore != *r.ExpectedRevision ||
			payload.RevisionAfter != *r.ResultingRevision {
			return errors.New("sync payload does not match history envelope")
		}
	}
	if r.Kind == KindArchived {
		payload, err := DecodeArchivePayload(r.Payload)
		if err != nil {
			return fmt.Errorf("archive payload: %w", err)
		}
		if payload.Change != r.Change || payload.Actor != r.Actor ||
			r.ExpectedRevision == nil || r.ResultingRevision == nil ||
			payload.RevisionBefore != *r.ExpectedRevision ||
			payload.RevisionAfter != *r.ResultingRevision {
			return errors.New("archive payload does not match history envelope")
		}
	}
	if r.Kind == KindFriction {
		payload, err := DecodeFrictionPayload(r.Payload)
		if err != nil {
			return fmt.Errorf("friction payload: %w", err)
		}
		if payload.Change != r.Change || payload.Actor != r.Actor {
			return errors.New("friction payload does not match history envelope")
		}
	}
	want, err := Identity(r)
	if err != nil {
		return err
	}
	if r.ID != want {
		return errors.New("id does not match canonical record content")
	}
	return nil
}

func NewCompletionPayload(value CompletionPayload) (CompletionPayload, error) {
	value.SchemaVersion = CompletionSchemaVersion
	return value, value.Validate()
}

func (value CompletionPayload) Validate() error {
	if value.SchemaVersion != CompletionSchemaVersion ||
		value.Change == "" || value.TaskID == "" ||
		!validHex(value.AttemptID, sha256.Size*2) ||
		!validHex(value.EvidenceID, sha256.Size*2) ||
		strings.TrimSpace(value.Actor) == "" ||
		value.ActivityFrom != "in_progress" || value.ActivityTo != "completed" ||
		value.RevisionBefore == 0 || value.RevisionAfter != value.RevisionBefore+1 {
		return errors.New("completion binding is malformed")
	}
	return nil
}

func DecodeCompletionPayload(raw json.RawMessage) (CompletionPayload, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return CompletionPayload{}, err
	}
	var value CompletionPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return CompletionPayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CompletionPayload{}, errors.New("completion payload has trailing data")
	}
	return value, value.Validate()
}

func NewSyncPayload(value SyncPayload) (SyncPayload, error) {
	value.SchemaVersion = SyncSchemaVersion
	return value, value.Validate()
}

func (value SyncPayload) Validate() error {
	if value.SchemaVersion != SyncSchemaVersion || value.Change == "" ||
		strings.TrimSpace(value.Approver) == "" || value.ActorClass != "human" ||
		strings.TrimSpace(value.Reason) == "" ||
		!validHex(value.ApprovalHash, sha256.Size*2) ||
		!validHex(value.PlanHash, sha256.Size*2) ||
		!validHex(value.EvidenceSet, sha256.Size*2) ||
		!validHex(value.Transaction, sha256.Size*2) ||
		value.Assurance != "advisory" ||
		value.RevisionBefore == 0 || value.RevisionAfter != value.RevisionBefore+1 {
		return errors.New("sync binding is malformed")
	}
	return validSpecHashes(value.Outputs, true)
}

func DecodeSyncPayload(raw json.RawMessage) (SyncPayload, error) {
	var value SyncPayload
	if err := decodePayload(raw, &value); err != nil {
		return SyncPayload{}, err
	}
	return value, value.Validate()
}

func NewArchivePayload(value ArchivePayload) (ArchivePayload, error) {
	value.SchemaVersion = ArchiveSchemaVersion
	return value, value.Validate()
}

func (value ArchivePayload) Validate() error {
	if value.SchemaVersion != ArchiveSchemaVersion || value.Change == "" ||
		strings.TrimSpace(value.Actor) == "" || strings.TrimSpace(value.Approver) == "" ||
		!validAttemptPath(value.Source) || !validAttemptPath(value.Target) ||
		value.Source == value.Target ||
		!validHex(value.ChangeHash, sha256.Size*2) ||
		!validHex(value.EvidenceSet, sha256.Size*2) ||
		!validHex(value.SyncRecord, sha256.Size*2) ||
		!validHex(value.Transaction, sha256.Size*2) ||
		value.RevisionBefore == 0 || value.RevisionAfter != value.RevisionBefore+1 {
		return errors.New("archive binding is malformed")
	}
	return validSpecHashes(value.Accepted, false)
}

func DecodeArchivePayload(raw json.RawMessage) (ArchivePayload, error) {
	var value ArchivePayload
	if err := decodePayload(raw, &value); err != nil {
		return ArchivePayload{}, err
	}
	return value, value.Validate()
}

func NewFrictionPayload(value FrictionPayload) (FrictionPayload, error) {
	value.SchemaVersion = FrictionSchemaVersion
	return value, value.Validate()
}

func (value FrictionPayload) Validate() error {
	if value.SchemaVersion != FrictionSchemaVersion || value.Change == "" ||
		value.TaskID == "" || strings.TrimSpace(value.Operation) == "" ||
		strings.TrimSpace(value.Domain) == "" || strings.TrimSpace(value.Blocker) == "" ||
		strings.TrimSpace(value.Consequence) == "" || strings.TrimSpace(value.Actor) == "" ||
		value.ObservedRevision == 0 ||
		!validHex(value.ContractHash, sha256.Size*2) ||
		!validHex(value.StateHash, sha256.Size*2) ||
		!validHex(value.EvidenceSet, sha256.Size*2) {
		return errors.New("friction binding is malformed")
	}
	return nil
}

func DecodeFrictionPayload(raw json.RawMessage) (FrictionPayload, error) {
	var value FrictionPayload
	if err := decodePayload(raw, &value); err != nil {
		return FrictionPayload{}, err
	}
	return value, value.Validate()
}

// validSpecHashes keeps accepted-spec references unique, ordered, and safe.
// requireOutputs is false for archive, which may record a change that touched
// no capability at all.
func validSpecHashes(items []SpecHash, requireOutputs bool) error {
	if requireOutputs && len(items) == 0 {
		return errors.New("sync binding is malformed")
	}
	for index, item := range items {
		if item.Capability == "" || !validAttemptPath(item.Path) ||
			!validHex(item.After, sha256.Size*2) ||
			item.Before != "" && !validHex(item.Before, sha256.Size*2) ||
			index > 0 && items[index-1].Capability >= item.Capability {
			return errors.New("accepted spec references are malformed")
		}
	}
	return nil
}

func decodePayload(raw json.RawMessage, out any) error {
	if err := rejectDuplicateKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("payload has trailing data")
	}
	return nil
}

func NewAttemptPayload(attempt AttemptPayload) (AttemptPayload, error) {
	attempt.SchemaVersion = AttemptSchemaVersion
	attempt.ID = attemptIdentity(attempt)
	return attempt, attempt.Validate()
}

func attemptIdentity(attempt AttemptPayload) string {
	attempt.ID = ""
	raw, _ := json.Marshal(attempt)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (attempt AttemptPayload) Validate() error {
	if attempt.SchemaVersion != AttemptSchemaVersion ||
		attempt.ID == "" || attempt.Change == "" || attempt.TaskID == "" ||
		!validHex(attempt.BaselineHEAD, 40, 64) ||
		attempt.RevisionBefore == 0 || attempt.RevisionAfter != attempt.RevisionBefore+1 ||
		!validHex(attempt.ContractHash, sha256.Size*2) ||
		!validHex(attempt.ApprovalHash, sha256.Size*2) ||
		!validHex(attempt.StateGuardHash, sha256.Size*2) ||
		len(attempt.DeclaredFiles) == 0 || strings.TrimSpace(attempt.Actor) == "" ||
		attempt.Assurance != "advisory" ||
		attempt.ActivityFrom != "pending" || attempt.ActivityTo != "in_progress" ||
		attempt.ID != attemptIdentity(attempt) {
		return errors.New("attempt binding is malformed")
	}
	for index, path := range attempt.DeclaredFiles {
		if !validAttemptPath(path) || index > 0 && attempt.DeclaredFiles[index-1] >= path {
			return errors.New("attempt declared files are malformed")
		}
	}
	return nil
}

func validAttemptPath(value string) bool {
	return value != "" && value != "." && path.Clean(value) == value &&
		!strings.HasPrefix(value, "/") && value != ".." && !strings.HasPrefix(value, "../") &&
		!strings.ContainsAny(value, "\\*?[") &&
		value != ".specd" && !strings.HasPrefix(value, ".specd/") &&
		value != ".git" && !strings.HasPrefix(value, ".git/")
}

func DecodeAttemptPayload(raw json.RawMessage) (AttemptPayload, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return AttemptPayload{}, err
	}
	var attempt AttemptPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&attempt); err != nil {
		return AttemptPayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return AttemptPayload{}, errors.New("attempt payload has trailing data")
	}
	return attempt, attempt.Validate()
}

func validHex(value string, lengths ...int) bool {
	validLength := false
	for _, length := range lengths {
		validLength = validLength || len(value) == length
	}
	if !validLength || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateFamilyKind(r Record) error {
	switch {
	case r.Family == FamilyHistory && r.Kind == KindCreated:
		if r.ExpectedRevision == nil || r.ResultingRevision == nil ||
			*r.ExpectedRevision != 0 || *r.ResultingRevision != 1 {
			return errors.New("history/created requires initial revision transition 0 to 1")
		}
	case r.Family == FamilyHistory &&
		(r.Kind == KindApproved || r.Kind == KindTaskTransition ||
			r.Kind == KindAttempt || r.Kind == KindCompletion ||
			r.Kind == KindSynced || r.Kind == KindArchived):
		if r.ExpectedRevision == nil || r.ResultingRevision == nil ||
			*r.ResultingRevision != *r.ExpectedRevision+1 {
			return errors.New("history/approved requires one forward revision transition")
		}
	case r.Family == FamilyHistory && r.Kind == KindFriction:
		// Friction is an observation inside history, not a transition of it:
		// carrying no revision is what structurally keeps it from moving
		// lifecycle, authority, or approval state.
		if r.ExpectedRevision != nil || r.ResultingRevision != nil {
			return errors.New("history/friction must not contain transition revisions")
		}
	case r.Family == FamilyEvidence && r.Kind == KindObserved:
		if r.ExpectedRevision != nil || r.ResultingRevision != nil {
			return errors.New("evidence/observed must not contain transition revisions")
		}
	default:
		return fmt.Errorf("unknown family/kind %q/%q", r.Family, r.Kind)
	}
	return nil
}

func canonicalPayload(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("object is required")
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, errors.New("object is required")
	}
	return json.Marshal(value)
}

func decodeStrict(line []byte, out *Record) error {
	if err := rejectDuplicateKeys(line); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok {
					return errors.New("invalid object key")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func Replay(path string, family Family) ([]Record, []Diagnostic, error) {
	file, err := openRead(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, refusal("record-read", path, 0, err.Error())
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, refusal("record-read", path, 0, err.Error())
	}
	return replay(raw, path, family)
}

func replay(raw []byte, path string, family Family) ([]Record, []Diagnostic, error) {
	var diagnostics []Diagnostic
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		last := bytes.LastIndexByte(raw, '\n')
		tail := raw[last+1:]
		if json.Valid(tail) {
			return nil, nil, refusal("record-malformed", path, bytes.Count(raw, []byte("\n"))+1, "complete record lacks newline terminator")
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code: "record-torn-tail", File: path, Line: bytes.Count(raw, []byte("\n")) + 1,
			Reason: "ignored incomplete final record",
		})
		raw = raw[:last+1]
	}
	if len(raw) == 0 {
		return nil, diagnostics, nil
	}

	lines := bytes.Split(raw[:len(raw)-1], []byte("\n"))
	records := make([]Record, 0, len(lines))
	ids := make(map[string]struct{}, len(lines))
	chains := make(map[string]uint64)
	for index, line := range lines {
		lineNumber := index + 1
		if len(line) == 0 {
			return nil, nil, refusal("record-malformed", path, lineNumber, "empty complete line")
		}
		var record Record
		if err := decodeStrict(line, &record); err != nil {
			return nil, nil, refusal("record-malformed", path, lineNumber, err.Error())
		}
		if err := record.Validate(); err != nil {
			return nil, nil, refusal("record-invalid", path, lineNumber, err.Error())
		}
		if record.Family != family {
			return nil, nil, refusal("record-family", path, lineNumber, "record does not match file family")
		}
		if _, exists := ids[record.ID]; exists {
			return nil, nil, refusal("record-duplicate", path, lineNumber, "duplicate record id")
		}
		ids[record.ID] = struct{}{}
		if record.ExpectedRevision != nil {
			if previous, exists := chains[record.Change]; exists && *record.ExpectedRevision != previous {
				return nil, nil, refusal("record-revision-chain", path, lineNumber, "non-contiguous change revision")
			}
			chains[record.Change] = *record.ResultingRevision
		}
		records = append(records, record)
	}
	return records, diagnostics, nil
}

func refusal(code, path string, line int, reason string) error {
	if line > 0 {
		reason = fmt.Sprintf("line %d: %s", line, reason)
	}
	return failure.New(code, "", path, reason, "restore the file from a trusted source")
}
