package plan

import (
	"bytes"
	"regexp"
	"strings"
)

var (
	placeholderPattern = regexp.MustCompile(`(?i)(<[^>\r\n]+>|\bTODO\b|\bTBD\b)`)
	referencePattern   = regexp.MustCompile(`([a-z0-9]+(?:-[a-z0-9]+)*)/Requirement:[ \t]+([^\r\n]+)`)
	referenceLike      = regexp.MustCompile(`(?i)([^/[:space:]]+)/Requirement:[ \t]*([^\r\n]*)`)
)

var proposalSections = []string{
	"Problem", "Outcome", "Scope", "Non-goals", "Affected capabilities",
}

var designSections = []string{
	"Boundaries", "Interfaces", "Invariants", "Failure behavior",
	"Integration", "Alternatives", "Owner",
}

func ParseProposal(source Source) Proposal {
	sections, diagnostics := parseRequiredSections(source, proposalSections)
	return Proposal{Source: cloneSource(source), Sections: sections, Diagnostics: diagnostics}
}

func ParseDesign(source Source) Design {
	sections, diagnostics := parseRequiredSections(source, designSections)
	var references []RequirementReference
	for _, section := range sections {
		found, invalid := parseReferences(source, section)
		references = append(references, found...)
		diagnostics = append(diagnostics, invalid...)
	}
	sortDiagnostics(diagnostics)
	return Design{
		Source: cloneSource(source), Sections: sections, References: references,
		Diagnostics: diagnostics,
	}
}

func parseReferences(source Source, section Section) ([]RequirementReference, []Diagnostic) {
	var references []RequirementReference
	var diagnostics []Diagnostic
	var fence byte
	var fenceLength int
	for _, line := range splitLines(section.Content) {
		trimmed, indent := trimIndent(line.text)
		if fence != 0 {
			if indent <= 3 && markerLength(trimmed, fence) >= fenceLength &&
				len(bytes.TrimSpace(trimmed[markerLength(trimmed, fence):])) == 0 {
				fence, fenceLength = 0, 0
			}
			continue
		}
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			if length := markerLength(trimmed, trimmed[0]); length >= 3 {
				fence, fenceLength = trimmed[0], length
				continue
			}
		}
		for _, match := range referenceLike.FindAllSubmatchIndex(line.text, -1) {
			offset := section.ContentFrom.Offset + line.start + match[0]
			raw := line.text[match[0]:match[1]]
			canonical := referencePattern.FindSubmatch(raw)
			if len(canonical) != 3 || !bytes.Equal(canonical[0], raw) ||
				len(bytes.TrimSpace(canonical[2])) == 0 {
				diagnostics = append(diagnostics, diagnostic(
					"reference_syntax", locationAt(source.Path, source.Bytes, offset),
					"design requirement reference is not canonical",
					"use <capability>/Requirement: <authored name>",
				))
				continue
			}
			references = append(references, RequirementReference{
				Capability:  string(canonical[1]),
				Requirement: strings.TrimSpace(string(canonical[2])),
				Location:    locationAt(source.Path, source.Bytes, offset),
			})
		}
	}
	return references, diagnostics
}

type sourceLine struct {
	start, end, next int
	number           int
	text             []byte
}

type heading struct {
	line  sourceLine
	level int
	title string
}

func parseRequiredSections(source Source, required []string) ([]Section, []Diagnostic) {
	source = cloneSource(source)
	if !source.Present {
		return nil, []Diagnostic{diagnostic(
			"artifact_missing", Location{Path: source.Path, Line: 1, Column: 1},
			"required planning artifact is absent", "create "+source.Path,
		)}
	}

	lines := splitLines(source.Bytes)
	headings, unterminated := scanHeadings(lines)
	requiredByName := make(map[string]string, len(required))
	for _, name := range required {
		requiredByName[strings.ToLower(name)] = name
	}

	var sections []Section
	var diagnostics []Diagnostic
	seen := map[string]bool{}
	malformed := map[string]bool{}
	for i, current := range headings {
		canonical, wanted := requiredByName[strings.ToLower(current.title)]
		if !wanted {
			continue
		}
		location := Location{Path: source.Path, Offset: current.line.start, Line: current.line.number, Column: 1}
		if current.level != 2 {
			malformed[canonical] = true
			diagnostics = append(diagnostics, diagnostic(
				"section_level", location,
				canonical+" must use a level-two heading", "change heading to ## "+canonical,
			))
			continue
		}
		if seen[canonical] {
			diagnostics = append(diagnostics, diagnostic(
				"section_duplicate", location,
				canonical+" section appears more than once", "keep exactly one ## "+canonical+" section",
			))
			continue
		}
		seen[canonical] = true
		end := len(source.Bytes)
		for j := i + 1; j < len(headings); j++ {
			if headings[j].level <= current.level {
				end = headings[j].line.start
				break
			}
		}
		contentStart := current.line.next
		content := append([]byte(nil), source.Bytes[contentStart:end]...)
		section := Section{
			Name: canonical, Heading: current.title, Content: content,
			Location: location, ContentFrom: locationAt(source.Path, source.Bytes, contentStart),
			Placeholder: placeholderPattern.Match(content),
		}
		sections = append(sections, section)
		switch {
		case len(bytes.TrimSpace(content)) == 0:
			diagnostics = append(diagnostics, diagnostic(
				"section_empty", location, canonical+" section is empty",
				"add concrete content under ## "+canonical,
			))
		case section.Placeholder:
			diagnostics = append(diagnostics, diagnostic(
				"section_placeholder", section.ContentFrom,
				canonical+" section contains scaffold placeholder text",
				"replace placeholder with concrete content",
			))
		}
	}

	for _, name := range required {
		if !seen[name] && !malformed[name] {
			diagnostics = append(diagnostics, diagnostic(
				"section_missing", Location{Path: source.Path, Offset: len(source.Bytes), Line: lineNumberAt(source.Bytes, len(source.Bytes)), Column: 1},
				name+" section is missing", "add ## "+name+" with concrete content",
			))
		}
	}
	if unterminated != nil {
		diagnostics = append(diagnostics, diagnostic(
			"fence_unterminated",
			Location{Path: source.Path, Offset: unterminated.start, Line: unterminated.number, Column: 1},
			"fenced code block is not closed", "close the fenced code block",
		))
	}
	sortDiagnostics(diagnostics)
	return sections, diagnostics
}

func scanHeadings(lines []sourceLine) ([]heading, *sourceLine) {
	var headings []heading
	var fence byte
	var fenceLength int
	var opened *sourceLine
	for _, line := range lines {
		trimmed, indent := trimIndent(line.text)
		if fence != 0 {
			if indent <= 3 && markerLength(trimmed, fence) >= fenceLength && len(bytes.TrimSpace(trimmed[markerLength(trimmed, fence):])) == 0 {
				fence, fenceLength, opened = 0, 0, nil
			}
			continue
		}
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			if length := markerLength(trimmed, trimmed[0]); length >= 3 {
				fence, fenceLength = trimmed[0], length
				copy := line
				opened = &copy
				continue
			}
		}
		level := markerLength(line.text, '#')
		if level < 1 || level > 6 || len(line.text) <= level || (line.text[level] != ' ' && line.text[level] != '\t') {
			continue
		}
		title := strings.TrimSpace(string(line.text[level:]))
		if title != "" {
			headings = append(headings, heading{line: line, level: level, title: title})
		}
	}
	return headings, opened
}

func trimIndent(line []byte) ([]byte, int) {
	trimmed := bytes.TrimLeft(line, " \t")
	return trimmed, len(line) - len(trimmed)
}

func markerLength(line []byte, marker byte) int {
	n := 0
	for n < len(line) && line[n] == marker {
		n++
	}
	return n
}

func splitLines(raw []byte) []sourceLine {
	if len(raw) == 0 {
		return nil
	}
	var lines []sourceLine
	for start, number := 0, 1; start < len(raw); number++ {
		end := bytes.IndexByte(raw[start:], '\n')
		next := len(raw)
		if end < 0 {
			end = len(raw)
		} else {
			end += start
			next = end + 1
		}
		textEnd := end
		if textEnd > start && raw[textEnd-1] == '\r' {
			textEnd--
		}
		lines = append(lines, sourceLine{
			start: start, end: textEnd, next: next, number: number,
			text: raw[start:textEnd],
		})
		start = next
	}
	return lines
}

func locationAt(path string, raw []byte, offset int) Location {
	if offset < 0 {
		offset = 0
	}
	if offset > len(raw) {
		offset = len(raw)
	}
	lastNewline := bytes.LastIndexByte(raw[:offset], '\n')
	return Location{
		Path: path, Offset: offset, Line: lineNumberAt(raw, offset),
		Column: offset - lastNewline,
	}
}

func lineNumberAt(raw []byte, offset int) int {
	if offset > len(raw) {
		offset = len(raw)
	}
	return 1 + bytes.Count(raw[:offset], []byte{'\n'})
}

func cloneSource(source Source) Source {
	source.Bytes = append([]byte(nil), source.Bytes...)
	return source
}
