package plan

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
)

type DeltaOperation string

const (
	Added    DeltaOperation = "ADDED"
	Modified DeltaOperation = "MODIFIED"
	Removed  DeltaOperation = "REMOVED"
	Renamed  DeltaOperation = "RENAMED"
)

type Scenario struct {
	Name     string
	Raw      []byte
	Location Location
}

type Requirement struct {
	Name      string
	Identity  string
	Raw       []byte
	Location  Location
	Scenarios []Scenario
}

type Operation struct {
	Kind        DeltaOperation
	Requirement *Requirement
	From        string
	To          string
	Reason      string
	Migration   string
	Raw         []byte
	Location    Location
}

type CapabilityDelta struct {
	Capability  string
	Purpose     []byte
	Source      Source
	Operations  []Operation
	Diagnostics []Diagnostic
}

var (
	normativePattern = regexp.MustCompile(`\b(?:MUST|SHALL)\b`)
	stepPattern      = regexp.MustCompile(`(?i)^\s*-\s*(?:\*\*)?(GIVEN|AND|WHEN|THEN)(?:\*\*)?(?:\s*:)?\s+`)
	fieldPattern     = regexp.MustCompile(`(?m)^\s*\*\*(Reason|Migration)\*\*:\s*(.+?)\s*$`)
	fromToPattern    = regexp.MustCompile(`(?m)^\s*-?\s*(FROM|TO):\s*` + "`?" + `###\s+Requirement:\s*(.+?)` + "`?" + `\s*$`)
)

func NormalizeRequirementIdentity(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// ParseCapabilityDelta projects one capability delta. accepted is the accepted
// capability spec this delta targets; an absent Source means a new capability.
func ParseCapabilityDelta(source Source, capability string, accepted Source) CapabilityDelta {
	result := CapabilityDelta{Capability: capability, Source: cloneSource(source)}
	if err := corepath.ValidateSegment(capability); err != nil {
		result.Diagnostics = []Diagnostic{diagnostic(
			"capability_unsafe", Location{Path: source.Path, Line: 1, Column: 1},
			"capability must be one lowercase kebab-case segment", "rename capability to lowercase kebab-case",
		)}
		return result
	}
	if !source.Present {
		result.Diagnostics = []Diagnostic{diagnostic(
			"delta_missing", Location{Path: source.Path, Line: 1, Column: 1},
			"capability delta is absent", "create "+source.Path,
		)}
		return result
	}

	lines := splitLines(source.Bytes)
	headings, unterminated := scanHeadings(lines)
	var diagnostics []Diagnostic
	hasPlaceholder := false
	if unterminated != nil {
		diagnostics = append(diagnostics, diagnostic(
			"fence_unterminated", locationAt(source.Path, source.Bytes, unterminated.start),
			"fenced code block is not closed", "close the fenced code block",
		))
	}

	sections := map[string][]heading{}
	for _, item := range headings {
		if canonicalOperation(item.title) != "" || strings.EqualFold(item.title, "Purpose") {
			if item.level != 2 {
				diagnostics = append(diagnostics, diagnostic(
					"delta_section_level", locationAt(source.Path, source.Bytes, item.line.start),
					item.title+" must use a level-two heading", "change heading to ## "+item.title,
				))
				continue
			}
			sections[strings.ToLower(item.title)] = append(sections[strings.ToLower(item.title)], item)
		}
	}

	if purpose := sections["purpose"]; len(purpose) > 0 {
		if len(purpose) > 1 {
			diagnostics = append(diagnostics, diagnostic(
				"purpose_duplicate", locationAt(source.Path, source.Bytes, purpose[1].line.start),
				"Purpose section appears more than once", "keep exactly one ## Purpose section",
			))
		}
		start, end := purpose[0].line.next, sectionEnd(headings, purpose[0], len(source.Bytes))
		result.Purpose = append([]byte(nil), source.Bytes[start:end]...)
		if len(bytes.TrimSpace(result.Purpose)) == 0 {
			diagnostics = append(diagnostics, diagnostic(
				"purpose_empty", locationAt(source.Path, source.Bytes, purpose[0].line.start),
				"Purpose section is empty", "add concrete capability purpose",
			))
		}
		if accepted.Present {
			diagnostics = append(diagnostics, diagnostic(
				"purpose_existing", locationAt(source.Path, source.Bytes, purpose[0].line.start),
				"existing capability delta must not repeat Purpose", "remove the ## Purpose section",
			))
		}
		if placeholderPattern.Match(result.Purpose) {
			hasPlaceholder = true
			diagnostics = append(diagnostics, diagnostic(
				"delta_placeholder", locationAt(source.Path, source.Bytes, start),
				"Purpose contains scaffold placeholder text", "replace placeholder with concrete purpose",
			))
		}
	} else if !accepted.Present {
		diagnostics = append(diagnostics, diagnostic(
			"purpose_missing", Location{Path: source.Path, Offset: len(source.Bytes), Line: lineNumberAt(source.Bytes, len(source.Bytes)), Column: 1},
			"new capability delta requires Purpose", "add ## Purpose with concrete content",
		))
	}

	for _, section := range headings {
		kind := canonicalOperation(section.title)
		if section.level != 2 || kind == "" {
			continue
		}
		if !exactOperationHeading(section.title, kind) {
			diagnostics = append(diagnostics, diagnostic(
				"delta_heading", locationAt(source.Path, source.Bytes, section.line.start),
				"delta operation heading is not canonical", "change heading to ## "+string(kind)+" Requirements",
			))
			continue
		}
		if !accepted.Present && kind != Added {
			diagnostics = append(diagnostics, diagnostic(
				"operation_new_capability", locationAt(source.Path, source.Bytes, section.line.start),
				string(kind)+" is not valid for a new capability", "use ADDED requirements or target an existing capability",
			))
			continue
		}
		end := sectionEnd(headings, section, len(source.Bytes))
		children := headingsWithin(headings, section.line.next, end)
		switch kind {
		case Added, Modified:
			parseRequirementOperations(&result, &diagnostics, &hasPlaceholder, source, kind, section, end, children)
		case Removed:
			parseRemovedOperations(&result, &diagnostics, source, section, end, children)
		case Renamed:
			parseRenameOperations(&result, &diagnostics, source, section, end)
		}
	}

	if len(result.Operations) == 0 && !hasPlaceholder {
		diagnostics = append(diagnostics, diagnostic(
			"delta_empty", Location{Path: source.Path, Offset: len(source.Bytes), Line: lineNumberAt(source.Bytes, len(source.Bytes)), Column: 1},
			"delta contains no valid operations", "add one complete delta operation",
		))
	}
	validateAcceptedIdentities(accepted, result.Operations, &diagnostics)
	validateOperationIdentities(result.Operations, &diagnostics)
	sortDiagnostics(diagnostics)
	result.Diagnostics = diagnostics
	return result
}

func DiscoverCapabilityDeltas(owner *corepath.Owner, change string) ([]CapabilityDelta, []Diagnostic) {
	specs, err := owner.ChangeSpecs(change)
	if err != nil {
		return nil, []Diagnostic{pathDiagnostic("delta_path", specs, err)}
	}
	info, err := os.Lstat(specs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, []Diagnostic{pathDiagnostic("delta_path", specs, err)}
	}
	entries, err := os.ReadDir(specs)
	if err != nil {
		return nil, []Diagnostic{pathDiagnostic("delta_unreadable", specs, err)}
	}
	var results []CapabilityDelta
	var diagnostics []Diagnostic
	for _, entry := range entries {
		entryPath := filepath.Join(specs, entry.Name())
		if entry.Name() == "spec.md" || corepath.ValidateSegment(entry.Name()) != nil {
			diagnostics = append(diagnostics, diagnostic(
				"capability_path", Location{Path: entryPath, Line: 1, Column: 1},
				"delta must be specs/<one-kebab-case-capability>/spec.md", "move delta to one valid capability directory",
			))
			continue
		}
		info, statErr := os.Lstat(entryPath)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			diagnostics = append(diagnostics, pathDiagnostic("capability_path", entryPath, statErr))
			continue
		}
		children, readErr := os.ReadDir(entryPath)
		if readErr != nil {
			diagnostics = append(diagnostics, pathDiagnostic("delta_unreadable", entryPath, readErr))
			continue
		}
		if len(children) != 1 || children[0].Name() != "spec.md" {
			diagnostics = append(diagnostics, diagnostic(
				"capability_path", Location{Path: entryPath, Line: 1, Column: 1},
				"capability directory must contain only spec.md", "remove nested or extra delta paths",
			))
			continue
		}
		target, targetErr := owner.ChangeSpec(change, entry.Name())
		if targetErr != nil {
			diagnostics = append(diagnostics, pathDiagnostic("capability_path", entryPath, targetErr))
			continue
		}
		source, readDiagnostics := readPlanSource(target, nil)
		if len(readDiagnostics) != 0 || !source.Present {
			diagnostics = append(diagnostics, readDiagnostics...)
			if len(readDiagnostics) == 0 {
				diagnostics = append(diagnostics, diagnostic(
					"delta_missing", Location{Path: target, Line: 1, Column: 1},
					"capability delta is absent", "create "+target,
				))
			}
			continue
		}
		accepted, acceptedErr := owner.AcceptedSpec(entry.Name())
		if acceptedErr != nil {
			diagnostics = append(diagnostics, pathDiagnostic("accepted_path", accepted, acceptedErr))
			continue
		}
		acceptedInfo, acceptedErr := os.Lstat(accepted)
		acceptedSource := Source{Path: accepted, Present: acceptedErr == nil}
		if acceptedErr != nil && !errors.Is(acceptedErr, os.ErrNotExist) {
			diagnostics = append(diagnostics, pathDiagnostic("accepted_unreadable", accepted, acceptedErr))
			continue
		}
		if acceptedSource.Present && (!acceptedInfo.Mode().IsRegular() || acceptedInfo.Mode()&os.ModeSymlink != 0) {
			diagnostics = append(diagnostics, pathDiagnostic("accepted_path", accepted, nil))
			continue
		}
		if acceptedSource.Present {
			acceptedBytes, acceptedReadErr := os.ReadFile(accepted)
			if acceptedReadErr != nil {
				diagnostics = append(diagnostics, pathDiagnostic("accepted_unreadable", accepted, acceptedReadErr))
				continue
			}
			acceptedSource.Bytes = acceptedBytes
		}
		results = append(results, ParseCapabilityDelta(source, entry.Name(), acceptedSource))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Capability < results[j].Capability })
	sortDiagnostics(diagnostics)
	return results, diagnostics
}

func parseRequirementOperations(result *CapabilityDelta, diagnostics *[]Diagnostic, hasPlaceholder *bool, source Source, kind DeltaOperation, section heading, end int, children []heading) {
	found := false
	for _, child := range children {
		if strings.HasPrefix(strings.ToLower(child.title), "scenario:") && child.level != 4 {
			*diagnostics = append(*diagnostics, diagnostic(
				"scenario_level", locationAt(source.Path, source.Bytes, child.line.start),
				"scenario must use a level-four heading", "change heading to #### Scenario: <name>",
			))
		}
	}
	for i, child := range children {
		if child.level != 3 || !strings.HasPrefix(child.title, "Requirement: ") {
			if child.level == 3 && strings.HasPrefix(strings.ToLower(child.title), "requirement:") {
				*diagnostics = append(*diagnostics, diagnostic(
					"requirement_heading", locationAt(source.Path, source.Bytes, child.line.start),
					"requirement must use a named level-three heading", "change heading to ### Requirement: <name>",
				))
			}
			continue
		}
		found = true
		blockEnd := end
		wrongScenarioLevel := false
		for j := i + 1; j < len(children); j++ {
			if children[j].level <= 3 {
				wrongScenarioLevel = strings.HasPrefix(strings.ToLower(children[j].title), "scenario:")
				blockEnd = children[j].line.start
				break
			}
		}
		name := strings.TrimSpace(strings.TrimPrefix(child.title, "Requirement: "))
		raw := append([]byte(nil), source.Bytes[child.line.start:blockEnd]...)
		requirement := Requirement{Name: name, Identity: NormalizeRequirementIdentity(name), Raw: raw, Location: locationAt(source.Path, source.Bytes, child.line.start)}
		valid := name != "" && !wrongScenarioLevel
		if name == "" {
			*diagnostics = append(*diagnostics, diagnostic("requirement_name", requirement.Location, "requirement name is empty", "add a requirement name"))
		}
		bodyEnd := blockEnd
		for _, scenarioHeading := range headingsWithin(children, child.line.next, blockEnd) {
			if scenarioHeading.level != 4 {
				if strings.HasPrefix(strings.ToLower(scenarioHeading.title), "scenario:") {
					valid = false
				}
				continue
			}
			if !strings.HasPrefix(scenarioHeading.title, "Scenario: ") {
				*diagnostics = append(*diagnostics, diagnostic(
					"scenario_heading", locationAt(source.Path, source.Bytes, scenarioHeading.line.start),
					"level-four heading must be Scenario", "change heading to #### Scenario: <name>",
				))
				valid = false
				continue
			}
			if bodyEnd == blockEnd {
				bodyEnd = scenarioHeading.line.start
			}
			scenarioEnd := blockEnd
			for _, next := range children {
				if next.line.start > scenarioHeading.line.start && next.level <= 4 {
					scenarioEnd = next.line.start
					break
				}
			}
			scenarioRaw := append([]byte(nil), source.Bytes[scenarioHeading.line.start:scenarioEnd]...)
			hasWhen, hasThen := false, false
			for _, line := range visibleLines(source.Bytes[scenarioHeading.line.next:scenarioEnd]) {
				match := stepPattern.FindSubmatch(line.text)
				if len(match) == 2 {
					hasWhen = hasWhen || strings.EqualFold(string(match[1]), "WHEN")
					hasThen = hasThen || strings.EqualFold(string(match[1]), "THEN")
				}
			}
			if !hasWhen || !hasThen {
				*diagnostics = append(*diagnostics, diagnostic(
					"scenario_steps", locationAt(source.Path, source.Bytes, scenarioHeading.line.start),
					"scenario requires WHEN and THEN steps", "add observable WHEN and THEN steps",
				))
				valid = false
			}
			requirement.Scenarios = append(requirement.Scenarios, Scenario{
				Name: strings.TrimSpace(strings.TrimPrefix(scenarioHeading.title, "Scenario: ")),
				Raw:  scenarioRaw, Location: locationAt(source.Path, source.Bytes, scenarioHeading.line.start),
			})
		}
		if !normativeIn(source.Bytes[child.line.next:bodyEnd]) {
			*diagnostics = append(*diagnostics, diagnostic(
				"requirement_normative", requirement.Location,
				string(kind)+" requirement needs MUST or SHALL in its normative text", "add observable MUST or SHALL behavior",
			))
			valid = false
		}
		if len(requirement.Scenarios) == 0 {
			*diagnostics = append(*diagnostics, diagnostic(
				"requirement_scenario", requirement.Location,
				string(kind)+" requirement needs at least one scenario", "add #### Scenario with WHEN and THEN",
			))
			valid = false
		}
		if placeholderPattern.Match(raw) {
			*hasPlaceholder = true
			*diagnostics = append(*diagnostics, diagnostic(
				"delta_placeholder", requirement.Location,
				string(kind)+" requirement contains scaffold placeholder text", "replace placeholders with concrete behavior",
			))
			valid = false
		}
		if valid {
			copy := requirement
			result.Operations = append(result.Operations, Operation{Kind: kind, Requirement: &copy, Raw: raw, Location: requirement.Location})
		}
	}
	if !found {
		*diagnostics = append(*diagnostics, diagnostic(
			"operation_empty", locationAt(source.Path, source.Bytes, section.line.start),
			string(kind)+" section has no complete requirements", "add ### Requirement: <name> with normative text and scenario",
		))
	}
}

func parseRemovedOperations(result *CapabilityDelta, diagnostics *[]Diagnostic, source Source, section heading, end int, children []heading) {
	found := false
	for i, child := range children {
		if child.level != 3 || !strings.HasPrefix(child.title, "Requirement: ") {
			if child.level == 3 && strings.HasPrefix(strings.ToLower(child.title), "requirement:") {
				*diagnostics = append(*diagnostics, diagnostic(
					"requirement_heading", locationAt(source.Path, source.Bytes, child.line.start),
					"requirement must use a named level-three heading", "change heading to ### Requirement: <name>",
				))
			}
			continue
		}
		found = true
		blockEnd := end
		for j := i + 1; j < len(children); j++ {
			if children[j].level <= 3 {
				blockEnd = children[j].line.start
				break
			}
		}
		raw := append([]byte(nil), source.Bytes[child.line.start:blockEnd]...)
		fields := map[string]string{}
		for _, line := range visibleLinesAt(raw, child.line.start) {
			if match := fieldPattern.FindSubmatch(line.text); len(match) == 3 {
				fields[string(match[1])] = strings.TrimSpace(string(match[2]))
			}
		}
		name := strings.TrimSpace(strings.TrimPrefix(child.title, "Requirement: "))
		valid := name != "" && fields["Reason"] != "" && fields["Migration"] != ""
		location := locationAt(source.Path, source.Bytes, child.line.start)
		if !valid {
			*diagnostics = append(*diagnostics, diagnostic(
				"removed_incomplete", location, "REMOVED requires identity, Reason, and Migration",
				"add ### Requirement, **Reason**:, and **Migration**:",
			))
			continue
		}
		requirement := Requirement{Name: name, Identity: NormalizeRequirementIdentity(name), Raw: raw, Location: location}
		result.Operations = append(result.Operations, Operation{
			Kind: Removed, Requirement: &requirement, Reason: fields["Reason"], Migration: fields["Migration"], Raw: raw, Location: location,
		})
	}
	if !found {
		*diagnostics = append(*diagnostics, diagnostic(
			"operation_empty", locationAt(source.Path, source.Bytes, section.line.start),
			"REMOVED section has no requirements", "add a complete removed requirement block",
		))
	}
}

func parseRenameOperations(result *CapabilityDelta, diagnostics *[]Diagnostic, source Source, section heading, end int) {
	raw := source.Bytes[section.line.next:end]
	var from, to string
	var location Location
	matches := 0
	for _, line := range visibleLinesAt(raw, section.line.next) {
		match := fromToPattern.FindSubmatch(line.text)
		if len(match) != 3 {
			continue
		}
		matches++
		field := string(match[1])
		value := strings.TrimSpace(string(match[2]))
		if field == "FROM" {
			if from != "" {
				*diagnostics = append(*diagnostics, diagnostic("rename_duplicate", locationAt(source.Path, source.Bytes, line.start), "RENAMED has duplicate FROM", "split into complete FROM/TO pairs"))
			}
			from, location = value, locationAt(source.Path, source.Bytes, line.start)
		} else if from != "" && to == "" {
			to = value
			result.Operations = append(result.Operations, Operation{
				Kind: Renamed, From: from, To: to, Raw: append([]byte(nil), raw...), Location: location,
			})
			from, to = "", ""
		} else {
			*diagnostics = append(*diagnostics, diagnostic(
				"rename_incomplete", locationAt(source.Path, source.Bytes, line.start),
				"RENAMED TO has no preceding FROM", "place one FROM immediately before each TO",
			))
		}
	}
	if from != "" || matches == 0 {
		*diagnostics = append(*diagnostics, diagnostic(
			"rename_incomplete", locationAt(source.Path, source.Bytes, section.line.start),
			"RENAMED requires complete FROM and TO identities", "add matching FROM and TO requirement headers",
		))
	}
}

// acceptedRequirementIdentities reads requirement identities from an accepted
// capability spec with the same fence-aware heading scanner delta parsing uses.
func acceptedRequirementIdentities(accepted Source) map[string]bool {
	identities := map[string]bool{}
	headings, _ := scanHeadings(splitLines(accepted.Bytes))
	for _, item := range headings {
		if item.level != 3 || !strings.HasPrefix(item.title, "Requirement: ") {
			continue
		}
		if name := strings.TrimSpace(strings.TrimPrefix(item.title, "Requirement: ")); name != "" {
			identities[NormalizeRequirementIdentity(name)] = true
		}
	}
	return identities
}

// validateAcceptedIdentities checks every operation against accepted truth:
// MODIFIED, REMOVED, and RENAMED FROM must name an existing accepted
// requirement, ADDED must not collide with one. Whether a MODIFIED block is the
// *complete* replacement is not decidable from bytes (a replacement may
// legitimately drop a scenario), so it is not guessed here.
func validateAcceptedIdentities(accepted Source, operations []Operation, diagnostics *[]Diagnostic) {
	if !accepted.Present {
		return
	}
	identities := acceptedRequirementIdentities(accepted)
	for _, operation := range operations {
		name := operation.From
		if operation.Requirement != nil {
			name = operation.Requirement.Name
		}
		identity := NormalizeRequirementIdentity(name)
		if identity == "" {
			continue
		}
		switch {
		case operation.Kind == Added && identities[identity]:
			*diagnostics = append(*diagnostics, diagnostic(
				"requirement_exists", operation.Location,
				"ADDED requirement "+name+" already exists in the accepted capability spec",
				"move the requirement under ## MODIFIED Requirements",
			))
		case operation.Kind != Added && !identities[identity]:
			*diagnostics = append(*diagnostics, diagnostic(
				"requirement_unknown", operation.Location,
				string(operation.Kind)+" requirement "+name+" is not in the accepted capability spec",
				"name an accepted requirement or move it under ## ADDED Requirements",
			))
		}
	}
}

func validateOperationIdentities(operations []Operation, diagnostics *[]Diagnostic) {
	seen := map[string]Operation{}
	for _, operation := range operations {
		keys := []string{}
		if operation.Requirement != nil {
			keys = append(keys, operation.Requirement.Identity)
		} else {
			keys = append(keys, NormalizeRequirementIdentity(operation.From), NormalizeRequirementIdentity(operation.To))
		}
		for _, key := range keys {
			if previous, exists := seen[key]; exists {
				code := "requirement_conflict"
				message := "requirement identity participates in both " + string(previous.Kind) + " and " + string(operation.Kind)
				if previous.Kind == operation.Kind {
					code = "requirement_duplicate"
					message = "requirement identity is duplicated in " + string(operation.Kind)
				}
				*diagnostics = append(*diagnostics, diagnostic(
					code, operation.Location,
					message+"; first declared at "+previous.Location.Path+":"+lineString(previous.Location.Line),
					"keep one unambiguous operation for the requirement",
				))
			} else {
				seen[key] = operation
			}
		}
	}
}

func canonicalOperation(title string) DeltaOperation {
	lower := strings.ToLower(strings.TrimSpace(title))
	for _, kind := range []DeltaOperation{Added, Modified, Removed, Renamed} {
		if lower == strings.ToLower(string(kind)+" Requirements") {
			return kind
		}
	}
	return ""
}

func exactOperationHeading(title string, kind DeltaOperation) bool {
	return title == string(kind)+" Requirements"
}

func sectionEnd(headings []heading, current heading, fallback int) int {
	for _, next := range headings {
		if next.line.start > current.line.start && next.level <= current.level {
			return next.line.start
		}
	}
	return fallback
}

func headingsWithin(headings []heading, start, end int) []heading {
	var result []heading
	for _, item := range headings {
		if item.line.start >= start && item.line.start < end {
			result = append(result, item)
		}
	}
	return result
}

func visibleLines(raw []byte) []sourceLine {
	return visibleLinesAt(raw, 0)
}

func visibleLinesAt(raw []byte, base int) []sourceLine {
	var visible []sourceLine
	var fence byte
	var fenceLength int
	for _, line := range splitLines(raw) {
		line.start += base
		line.end += base
		line.next += base
		trimmed := bytes.TrimLeft(line.text, " \t")
		if fence != 0 {
			if length := markerLength(trimmed, fence); length >= fenceLength && len(bytes.TrimSpace(trimmed[length:])) == 0 {
				fence, fenceLength = 0, 0
			}
			continue
		}
		if len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			if length := markerLength(trimmed, trimmed[0]); length >= 3 {
				fence, fenceLength = trimmed[0], length
				continue
			}
		}
		visible = append(visible, line)
	}
	return visible
}

func normativeIn(raw []byte) bool {
	for _, line := range visibleLines(raw) {
		if normativePattern.Match(line.text) {
			return true
		}
	}
	return false
}

func pathDiagnostic(code, path string, err error) Diagnostic {
	message := "managed delta path is unsafe or unreadable"
	if err != nil {
		message = err.Error()
	}
	return diagnostic(code, Location{Path: path, Line: 1, Column: 1}, message, "repair the managed path and retry")
}
