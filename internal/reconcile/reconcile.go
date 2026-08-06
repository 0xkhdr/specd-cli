// Package reconcile turns canonical change deltas into one deterministic,
// value-only plan for accepted behavioral truth.
//
// Building a plan reads files and computes bytes and hashes in memory. It
// mutates nothing, calls no network or LLM, and reads no clock. The sync and
// archive transaction owners consume this same plan; check, status, review, and
// reports project it.
package reconcile

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"strings"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/plan"
)

// Operation is one applied delta operation in deterministic order.
type Operation struct {
	Kind        plan.DeltaOperation `json:"kind"`
	Requirement string              `json:"requirement"`
	From        string              `json:"from,omitempty"`
}

// Capability is the reconciliation of one capability: its exact sources and
// hashes, the ordered operations, and the rebuilt accepted document.
type Capability struct {
	Capability   string      `json:"capability"`
	DeltaPath    string      `json:"delta_path"`
	DeltaHash    string      `json:"delta_hash"`
	AcceptedPath string      `json:"accepted_path"`
	AcceptedHash string      `json:"accepted_hash,omitempty"`
	Created      bool        `json:"created"`
	Operations   []Operation `json:"operations"`
	Output       []byte      `json:"-"`
	OutputHash   string      `json:"output_hash,omitempty"`
	NoOp         bool        `json:"no_op"`
}

// Plan is the whole immutable projection. Outputs exist only when Applicable
// is true: one conflict withholds every output, never some of them.
type Plan struct {
	Root         string            `json:"root"`
	Change       string            `json:"change"`
	Capabilities []Capability      `json:"capabilities"`
	Diagnostics  []plan.Diagnostic `json:"diagnostics,omitempty"`
	Applicable   bool              `json:"applicable"`
	NoOp         bool              `json:"no_op"`
}

// Build reconciles every capability delta of one change against accepted truth.
func Build(owner *corepath.Owner, change string) Plan {
	result := Plan{Root: owner.Root(), Change: change}
	deltas, diagnostics := plan.DiscoverCapabilityDeltas(owner, change)
	result.Diagnostics = append(result.Diagnostics, diagnostics...)

	seen := map[string]bool{}
	for _, delta := range deltas {
		result.Diagnostics = append(result.Diagnostics, delta.Diagnostics...)
		acceptedPath, err := owner.AcceptedSpec(delta.Capability)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"accepted_path", location(delta.Source.Path, nil, 0), err.Error(),
				"resolve the capability to one safe accepted spec path"))
			continue
		}
		if seen[acceptedPath] {
			result.Diagnostics = append(result.Diagnostics, diagnostic(
				"capability_ambiguous", location(delta.Source.Path, nil, 0),
				"two deltas resolve to the accepted spec "+acceptedPath,
				"keep one delta directory per capability"))
			continue
		}
		seen[acceptedPath] = true

		current, readDiagnostics := readAccepted(acceptedPath)
		result.Diagnostics = append(result.Diagnostics, readDiagnostics...)

		capability := Capability{
			Capability: delta.Capability, DeltaPath: delta.Source.Path,
			DeltaHash: hash(delta.Source.Bytes), AcceptedPath: acceptedPath,
			Created: !current.present, Operations: operations(delta),
		}
		if current.present {
			capability.AcceptedHash = hash(current.bytes)
		}
		if len(readDiagnostics) == 0 {
			output, applyDiagnostics := rebuild(delta, current)
			result.Diagnostics = append(result.Diagnostics, applyDiagnostics...)
			if len(applyDiagnostics) == 0 {
				capability.Output = output
				capability.OutputHash = hash(output)
				capability.NoOp = current.present && capability.OutputHash == capability.AcceptedHash
			}
		}
		result.Capabilities = append(result.Capabilities, capability)
	}

	sortDiagnostics(result.Diagnostics)
	if len(result.Diagnostics) != 0 {
		// One conflict withholds every rebuilt document: a partially applicable
		// plan is exactly what invariant "no partial write" forbids.
		for index := range result.Capabilities {
			result.Capabilities[index].Output = nil
			result.Capabilities[index].OutputHash = ""
			result.Capabilities[index].NoOp = false
		}
		return result
	}
	result.Applicable = true
	result.NoOp = true
	for _, capability := range result.Capabilities {
		result.NoOp = result.NoOp && capability.NoOp
	}
	return result
}

func operations(delta plan.CapabilityDelta) []Operation {
	result := make([]Operation, 0, len(delta.Operations))
	for _, operation := range delta.Operations {
		item := Operation{Kind: operation.Kind}
		if operation.Requirement != nil {
			item.Requirement = operation.Requirement.Name
		} else {
			item.Requirement, item.From = operation.To, operation.From
		}
		result = append(result, item)
	}
	return result
}

// acceptedSpec is one accepted capability document with its requirement blocks.
type acceptedSpec struct {
	path    string
	present bool
	bytes   []byte
	blocks  []block
}

// block is one accepted requirement, from its heading through the bytes that
// precede the next requirement or section.
type block struct {
	start, headEnd, end int
	name, identity      string
}

func readAccepted(path string) (acceptedSpec, []plan.Diagnostic) {
	result := acceptedSpec{path: path}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, []plan.Diagnostic{diagnostic("accepted_unreadable", location(path, nil, 0),
			err.Error(), "repair the accepted spec path and retry")}
	}
	if !info.Mode().IsRegular() {
		return result, []plan.Diagnostic{diagnostic("accepted_unsafe", location(path, nil, 0),
			"accepted spec must be a regular non-symlink file",
			"replace it with a regular file inside .specd/specs")}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return result, []plan.Diagnostic{diagnostic("accepted_unreadable", location(path, nil, 0),
			err.Error(), "repair the accepted spec path and retry")}
	}
	result.present, result.bytes = true, raw

	var diagnostics []plan.Diagnostic
	found := headings(raw)
	for index, item := range found {
		if item.level == 2 && deltaHeading(item.title) {
			diagnostics = append(diagnostics, diagnostic("accepted_delta_heading",
				location(path, raw, item.start),
				"accepted truth must not contain a "+item.title+" heading",
				"remove the delta heading from the accepted spec"))
			continue
		}
		if item.level != 3 || !strings.HasPrefix(item.title, "Requirement: ") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(item.title, "Requirement: "))
		if name == "" {
			diagnostics = append(diagnostics, diagnostic("accepted_requirement_name",
				location(path, raw, item.start), "accepted requirement has no name",
				"name the requirement in the accepted spec"))
			continue
		}
		end := len(raw)
		for _, next := range found[index+1:] {
			if next.level <= 3 {
				end = next.start
				break
			}
		}
		result.blocks = append(result.blocks, block{
			start: item.start, headEnd: item.textEnd, end: end,
			name: name, identity: plan.NormalizeRequirementIdentity(name),
		})
	}
	seen := map[string]bool{}
	for _, item := range result.blocks {
		if seen[item.identity] {
			diagnostics = append(diagnostics, diagnostic("accepted_duplicate",
				location(path, raw, item.start),
				"accepted requirement "+item.name+" is declared more than once",
				"keep one block per requirement identity in the accepted spec"))
		}
		seen[item.identity] = true
	}
	return result, diagnostics
}

// rebuild applies every operation of one delta to one accepted document.
// Identity existence, completeness, duplicates, and contradictions are already
// decided by the canonical parser; only the rules that need accepted bytes are
// decided here.
func rebuild(delta plan.CapabilityDelta, current acceptedSpec) ([]byte, []plan.Diagnostic) {
	var diagnostics []plan.Diagnostic
	existing := map[string]bool{}
	for _, item := range current.blocks {
		existing[item.identity] = true
	}
	actions := map[string]plan.Operation{}
	var added [][]byte
	for _, operation := range delta.Operations {
		switch operation.Kind {
		case plan.Added:
			added = append(added, operation.Raw)
		case plan.Renamed:
			if existing[plan.NormalizeRequirementIdentity(operation.To)] {
				diagnostics = append(diagnostics, diagnostic("rename_destination_exists",
					operation.Location,
					"RENAMED destination "+operation.To+" already exists in the accepted capability spec",
					"choose a destination name that is not already accepted"))
				continue
			}
			actions[plan.NormalizeRequirementIdentity(operation.From)] = operation
		default:
			actions[operation.Requirement.Identity] = operation
		}
	}

	remaining := len(current.blocks) + len(added)
	for _, operation := range actions {
		if operation.Kind == plan.Removed {
			remaining--
		}
	}
	if current.present && remaining == 0 {
		diagnostics = append(diagnostics, diagnostic("capability_empty",
			location(delta.Source.Path, delta.Source.Bytes, 0),
			"removing the last requirement would leave capability "+delta.Capability+" empty",
			"keep at least one requirement in the capability"))
	}
	if len(diagnostics) != 0 {
		return nil, diagnostics
	}

	if !current.present {
		return newDocument(delta, added), nil
	}

	var out bytes.Buffer
	cursor, insertAt := 0, -1
	for _, item := range current.blocks {
		out.Write(current.bytes[cursor:item.start])
		operation, applied := actions[item.identity]
		switch {
		case !applied:
			out.Write(current.bytes[item.start:item.end])
		case operation.Kind == plan.Removed:
		case operation.Kind == plan.Modified:
			out.Write(replaceBlock(operation.Raw, current.bytes[item.start:item.end]))
		case operation.Kind == plan.Renamed:
			out.WriteString("### Requirement: " + operation.To)
			out.Write(current.bytes[item.headEnd:item.end])
		}
		cursor = item.end
		insertAt = out.Len()
	}
	tail := current.bytes[cursor:]
	if insertAt < 0 {
		insertAt = out.Len() + len(tail)
	}
	document := append(out.Bytes(), tail...)
	return insertBlocks(document, insertAt, added), nil
}

// newDocument builds the accepted file for a capability that has none. The
// canonical parser already refused a new capability without Purpose.
func newDocument(delta plan.CapabilityDelta, added [][]byte) []byte {
	var out bytes.Buffer
	out.WriteString("# " + title(delta.Capability) + "\n\n## Purpose\n\n")
	out.Write(bytes.TrimRight(bytes.TrimLeft(delta.Purpose, "\r\n"), " \t\r\n"))
	out.WriteString("\n")
	return insertBlocks(out.Bytes(), out.Len(), added)
}

// insertBlocks inserts rebuilt requirement blocks at one deterministic point,
// separated by exactly one blank line, without rewriting neighbouring bytes.
func insertBlocks(document []byte, at int, blocks [][]byte) []byte {
	if len(blocks) == 0 {
		return document
	}
	var out bytes.Buffer
	out.Write(document[:at])
	tail := document[at:]
	for _, raw := range blocks {
		blankLine(&out)
		out.Write(bytes.TrimRight(raw, " \t\r\n"))
		out.WriteString("\n")
	}
	if len(bytes.TrimSpace(tail)) != 0 {
		blankLine(&out)
		out.Write(tail)
	}
	return out.Bytes()
}

func blankLine(out *bytes.Buffer) {
	raw := out.Bytes()
	if len(raw) == 0 {
		return
	}
	for suffix := len(raw) - len(bytes.TrimRight(raw, "\n")); suffix < 2; suffix++ {
		out.WriteString("\n")
	}
}

// replaceBlock swaps a complete MODIFIED block in, keeping the trailing
// whitespace the accepted document used so unrelated layout does not move.
func replaceBlock(raw, original []byte) []byte {
	body := bytes.TrimRight(original, " \t\r\n")
	return append(bytes.TrimRight(raw, " \t\r\n"), original[len(body):]...)
}

func title(capability string) string {
	words := strings.ReplaceAll(capability, "-", " ")
	return strings.ToUpper(words[:1]) + words[1:]
}

func hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func deltaHeading(title string) bool {
	lower := strings.ToLower(strings.TrimSpace(title))
	for _, kind := range []plan.DeltaOperation{plan.Added, plan.Modified, plan.Removed, plan.Renamed} {
		if lower == strings.ToLower(string(kind))+" requirements" {
			return true
		}
	}
	return false
}

// ponytail: the canonical fence-aware heading scanner lives unexported in
// internal/plan. Exporting it would widen this task's declared file scope, so
// accepted specs are scanned here with the same rules. Upgrade path: export
// plan.ScanHeadings in a stage-2-scoped task and delete headings/textEnd here.
type headingLine struct {
	start, textEnd int
	level          int
	title          string
}

func headings(raw []byte) []headingLine {
	var result []headingLine
	var fence byte
	var fenceLength int
	for start := 0; start < len(raw); {
		end := bytes.IndexByte(raw[start:], '\n')
		next := len(raw)
		if end < 0 {
			end = len(raw)
		} else {
			end += start
			next = end + 1
		}
		if end > start && raw[end-1] == '\r' {
			end--
		}
		line := raw[start:end]
		trimmed := bytes.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)
		switch {
		case fence != 0:
			if indent <= 3 && marker(trimmed, fence) >= fenceLength &&
				len(bytes.TrimSpace(trimmed[marker(trimmed, fence):])) == 0 {
				fence, fenceLength = 0, 0
			}
		case indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') &&
			marker(trimmed, trimmed[0]) >= 3:
			fence, fenceLength = trimmed[0], marker(trimmed, trimmed[0])
		default:
			level := marker(line, '#')
			if level >= 1 && level <= 6 && len(line) > level &&
				(line[level] == ' ' || line[level] == '\t') {
				if text := strings.TrimSpace(string(line[level:])); text != "" {
					result = append(result, headingLine{start: start, textEnd: end, level: level, title: text})
				}
			}
		}
		start = next
	}
	return result
}

func marker(line []byte, value byte) int {
	count := 0
	for count < len(line) && line[count] == value {
		count++
	}
	return count
}

func diagnostic(code string, at plan.Location, message, repair string) plan.Diagnostic {
	return plan.Diagnostic{Code: code, Location: at, Message: message, Repair: repair}
}

func location(path string, raw []byte, offset int) plan.Location {
	if offset > len(raw) {
		offset = len(raw)
	}
	lastNewline := bytes.LastIndexByte(raw[:offset], '\n')
	return plan.Location{
		Path: path, Offset: offset,
		Line:   1 + bytes.Count(raw[:offset], []byte{'\n'}),
		Column: offset - lastNewline,
	}
}

func sortDiagnostics(diagnostics []plan.Diagnostic) {
	slices.SortStableFunc(diagnostics, func(a, b plan.Diagnostic) int {
		return cmp.Or(
			cmp.Compare(a.Location.Path, b.Location.Path),
			cmp.Compare(a.Location.Offset, b.Location.Offset),
			cmp.Compare(a.Code, b.Code),
		)
	})
}
