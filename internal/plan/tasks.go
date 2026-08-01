package plan

import (
	"bytes"
	"path"
	"regexp"
	"strings"
)

var taskReferencePattern = regexp.MustCompile(`^([a-z0-9]+(?:-[a-z0-9]+)*)/Requirement:[ \t]+([^[:space:]].*)$`)
var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var taskColumns = []string{"id", "role", "files", "depends-on", "refs", "verify", "acceptance"}

type Task struct {
	ID         string
	Role       string
	Files      []string
	DependsOn  []string
	References []RequirementReference
	Verify     string
	Acceptance string
	Source     []byte
	Location   Location
	Valid      bool
}

type TaskWave struct {
	Number int
	Tasks  []string
}

type Tasks struct {
	Source      Source
	Tasks       []Task
	Waves       []TaskWave
	Diagnostics []Diagnostic
}

func ParseTasks(source Source) Tasks {
	result := Tasks{Source: cloneSource(source)}
	if !source.Present {
		result.Diagnostics = []Diagnostic{diagnostic("artifact_missing",
			Location{Path: source.Path, Line: 1, Column: 1},
			"required planning artifact is absent", "create "+source.Path)}
		return result
	}

	lines := splitLines(source.Bytes)
	var tables []int
	var fence byte
	var fenceLength int
	for i, line := range lines {
		trimmed := bytes.TrimLeft(line.text, " \t")
		if fence != 0 {
			if markerLength(trimmed, fence) >= fenceLength &&
				len(bytes.TrimSpace(trimmed[markerLength(trimmed, fence):])) == 0 {
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
		if i+1 < len(lines) && isTaskHeader(line.text) && isTableSeparator(lines[i+1].text) {
			tables = append(tables, i)
		}
	}
	if len(tables) == 0 {
		result.Diagnostics = append(result.Diagnostics, diagnostic("task_table_missing",
			Location{Path: source.Path, Offset: len(source.Bytes), Line: lineNumberAt(source.Bytes, len(source.Bytes)), Column: 1},
			"tasks.md has no exact seven-column task table",
			"add one table with columns id | role | files | depends-on | refs | verify | acceptance"))
		return result
	}
	if len(tables) > 1 {
		for _, index := range tables[1:] {
			result.Diagnostics = append(result.Diagnostics, diagnostic("task_table_duplicate",
				locationAt(source.Path, source.Bytes, lines[index].start),
				"tasks.md has more than one task table", "keep exactly one task table"))
		}
	}

	start := tables[0] + 2
	for i := start; i < len(lines); i++ {
		cells, ok := parseTableRow(lines[i].text)
		if !ok {
			if len(bytes.TrimSpace(lines[i].text)) == 0 {
				break
			}
			if bytes.HasPrefix(bytes.TrimSpace(lines[i].text), []byte("|")) {
				result.Diagnostics = append(result.Diagnostics, diagnostic("task_row_malformed",
					locationAt(source.Path, source.Bytes, lines[i].start),
					"task row is malformed", "use a pipe-delimited row with exactly seven columns"))
				continue
			}
			break
		}
		location := locationAt(source.Path, source.Bytes, lines[i].start)
		if len(cells) != len(taskColumns) {
			result.Diagnostics = append(result.Diagnostics, diagnostic("task_columns", location,
				"task row must contain exactly seven columns", "make the row match the task table header"))
			continue
		}
		task, diagnostics := parseTaskRow(source, lines[i], cells)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		result.Tasks = append(result.Tasks, task)
	}
	if len(result.Tasks) == 0 {
		result.Diagnostics = append(result.Diagnostics, diagnostic("task_table_empty",
			locationAt(source.Path, source.Bytes, lines[tables[0]].start),
			"task table contains no tasks", "add one atomic builder task"))
	}
	graphDiagnostics, waves := projectTaskWaves(source.Path, result.Tasks)
	result.Diagnostics = append(result.Diagnostics, graphDiagnostics...)
	result.Waves = waves
	sortDiagnostics(result.Diagnostics)
	return result
}

func parseTaskRow(source Source, line sourceLine, cells []string) (Task, []Diagnostic) {
	location := locationAt(source.Path, source.Bytes, line.start)
	task := Task{
		ID: strings.TrimSpace(cells[0]), Role: strings.TrimSpace(cells[1]),
		Verify: unwrapCodeSpan(cells[5]), Acceptance: strings.TrimSpace(cells[6]),
		Source: append([]byte(nil), source.Bytes[line.start:line.next]...), Location: location,
	}
	var diagnostics []Diagnostic
	required := []struct {
		value, code, name string
	}{
		{task.ID, "task_id_empty", "id"}, {task.Role, "task_role_empty", "role"},
		{strings.TrimSpace(cells[2]), "task_files_empty", "files"},
		{strings.TrimSpace(cells[4]), "task_refs_empty", "refs"},
		{task.Verify, "task_verify_empty", "verify"},
		{task.Acceptance, "task_acceptance_empty", "acceptance"},
	}
	for _, field := range required {
		if field.value == "" {
			diagnostics = append(diagnostics, diagnostic(field.code, location,
				"task "+field.name+" is required", "add a non-empty "+field.name+" value"))
		}
	}
	if task.Role != "" && task.Role != "builder" {
		diagnostics = append(diagnostics, diagnostic("task_role", location,
			"task role must be builder", "change role to builder"))
	}
	if task.ID != "" && !taskIDPattern.MatchString(task.ID) {
		diagnostics = append(diagnostics, diagnostic("task_id_syntax", location,
			"task id must be one stable token", "use letters, digits, dot, underscore, or hyphen"))
	}
	// Reachability is resolved with canonical requirements in ParseChange.
	// At row level only empty and explicit scaffold acceptance are knowable.
	if task.Acceptance != "" && placeholderPattern.MatchString(task.Acceptance) {
		diagnostics = append(diagnostics, diagnostic("task_acceptance_placeholder", location,
			"task acceptance contains placeholder text", "replace it with an observable result"))
	}
	// A scaffold placeholder is named as such here so the verification gate does
	// not report it as broken command syntax instead.
	if task.Verify != "" && placeholderPattern.MatchString(task.Verify) {
		diagnostics = append(diagnostics, diagnostic("task_verify_placeholder", location,
			"task verification command contains placeholder text",
			"replace it with one exact verification command"))
	}

	var listDiagnostics []Diagnostic
	task.Files, listDiagnostics = parseTaskList(cells[2], location, "files")
	diagnostics = append(diagnostics, listDiagnostics...)
	for _, file := range task.Files {
		if !safeTaskFile(file) {
			diagnostics = append(diagnostics, diagnostic("task_file_unsafe", location,
				"task file path "+file+" is unsafe or unbounded",
				"use one project-relative file path without traversal, glob, or directory syntax"))
		}
	}
	task.DependsOn, listDiagnostics = parseTaskList(cells[3], location, "depends-on")
	diagnostics = append(diagnostics, listDiagnostics...)
	for _, dependency := range task.DependsOn {
		if !taskIDPattern.MatchString(dependency) {
			diagnostics = append(diagnostics, diagnostic("task_dependency_syntax", location,
				"dependency "+dependency+" is not a stable task id", "use one valid task id token"))
		}
	}
	refs, listDiagnostics := parseTaskList(cells[4], location, "refs")
	diagnostics = append(diagnostics, listDiagnostics...)
	for _, ref := range refs {
		match := taskReferencePattern.FindStringSubmatch(ref)
		if match == nil {
			diagnostics = append(diagnostics, diagnostic("task_ref_syntax", location,
				"task reference "+ref+" has invalid syntax",
				"use <capability>/Requirement: <authored name>"))
			continue
		}
		task.References = append(task.References, RequirementReference{
			Capability: match[1], Requirement: strings.TrimSpace(match[2]), Location: location,
		})
	}
	task.Valid = len(diagnostics) == 0
	if !task.Valid {
		task.Files = nil
		task.DependsOn = nil
	}
	return task, diagnostics
}

func parseTaskList(raw string, location Location, field string) ([]string, []Diagnostic) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var values []string
	var current strings.Builder
	escaped := false
	for _, r := range raw {
		switch {
		case escaped && (r == ';' || r == '\\'):
			current.WriteRune(r)
			escaped = false
		case escaped:
			return values, []Diagnostic{diagnostic("task_list_escape", location,
				field+" contains unsupported escape", "escape only literal semicolons or backslashes")}
		case r == '\\':
			escaped = true
		case r == ';':
			value := strings.TrimSpace(current.String())
			if value == "" {
				return values, []Diagnostic{diagnostic("task_list_empty", location,
					field+" contains an empty list item", "remove the empty list item")}
			}
			values = append(values, unwrapCodeSpan(value))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		return values, []Diagnostic{diagnostic("task_list_escape", location,
			field+" ends with an incomplete escape", "escape a literal semicolon as \\;")}
	}
	value := strings.TrimSpace(current.String())
	if value == "" {
		return values, []Diagnostic{diagnostic("task_list_empty", location,
			field+" contains an empty list item", "remove the empty list item")}
	}
	return append(values, unwrapCodeSpan(value)), nil
}

func safeTaskFile(value string) bool {
	if value == "" || value == "." || strings.HasSuffix(value, "/") ||
		strings.ContainsAny(value, "*?[") || strings.Contains(value, "\\") ||
		strings.HasPrefix(value, "/") || (len(value) >= 2 && value[1] == ':') {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != ".." && !strings.HasPrefix(clean, "../")
}

func projectTaskWaves(sourcePath string, tasks []Task) ([]Diagnostic, []TaskWave) {
	byID := make(map[string]int, len(tasks))
	declared := make(map[string][]int, len(tasks))
	var diagnostics []Diagnostic
	for i, task := range tasks {
		if task.ID == "" {
			continue
		}
		declared[task.ID] = append(declared[task.ID], i)
	}
	for id, indices := range declared {
		var valid []int
		for _, index := range indices {
			if tasks[index].Valid {
				valid = append(valid, index)
			}
		}
		if len(valid) > 1 {
			for _, index := range valid[1:] {
				diagnostics = append(diagnostics, diagnostic("task_id_duplicate", tasks[index].Location,
					"task id "+id+" duplicates line "+lineString(tasks[valid[0]].Location.Line),
					"give every task a unique stable id"))
			}
			for _, index := range valid {
				tasks[index].Valid = false
				tasks[index].Files = nil
			}
			continue
		}
		if len(valid) == 1 {
			byID[id] = valid[0]
		}
	}
	for i := range tasks {
		task := &tasks[i]
		if !task.Valid {
			continue
		}
		for _, dependency := range task.DependsOn {
			_, exists := byID[dependency]
			switch {
			case dependency == task.ID:
				diagnostics = append(diagnostics, diagnostic("task_dependency_self", task.Location,
					"task "+task.ID+" depends on itself", "remove the self-dependency"))
				task.Valid, task.Files = false, nil
			case !exists && len(declared[dependency]) == 0:
				diagnostics = append(diagnostics, diagnostic("task_dependency_unknown", task.Location,
					"task "+task.ID+" depends on unknown task "+dependency,
					"add the task or remove the dependency"))
				task.Valid, task.Files = false, nil
			}
		}
	}
	invalidateDependentTasks(tasks, declared)
	byID = make(map[string]int, len(tasks))
	for i := range tasks {
		if tasks[i].Valid {
			byID[tasks[i].ID] = i
		}
	}

	state := make([]byte, len(tasks))
	stack := make([]int, 0, len(tasks))
	var cycle []int
	var visit func(int) bool
	visit = func(index int) bool {
		state[index] = 1
		stack = append(stack, index)
		for _, dependency := range tasks[index].DependsOn {
			next, exists := byID[dependency]
			if !exists || dependency == tasks[index].ID {
				continue
			}
			if state[next] == 1 {
				at := 0
				for stack[at] != next {
					at++
				}
				cycle = append(append([]int(nil), stack[at:]...), next)
				return true
			}
			if state[next] == 0 && visit(next) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[index] = 2
		return false
	}
	for i := range tasks {
		if tasks[i].Valid && state[i] == 0 && visit(i) {
			names := make([]string, len(cycle))
			for j, index := range cycle {
				names[j] = tasks[index].ID
				tasks[index].Valid = false
				tasks[index].Files = nil
			}
			invalidateDependentTasks(tasks, declared)
			diagnostics = append(diagnostics, diagnostic("task_dependency_cycle", tasks[cycle[0]].Location,
				"task dependency cycle: "+strings.Join(names, " -> "),
				"remove one dependency from the reported cycle"))
			return diagnostics, nil
		}
	}
	if len(diagnostics) != 0 {
		return diagnostics, nil
	}

	waveByID := make(map[string]int, len(tasks))
	remaining := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if task.Valid {
			remaining = append(remaining, task)
		}
	}
	var waves []TaskWave
	for len(remaining) > 0 {
		var ids []string
		var readyTasks []Task
		next := remaining[:0]
		for _, task := range remaining {
			ready := true
			for _, dependency := range task.DependsOn {
				_, done := waveByID[dependency]
				if !done {
					ready = false
					break
				}
			}
			if ready {
				ids = append(ids, task.ID)
				readyTasks = append(readyTasks, task)
			} else {
				next = append(next, task)
			}
		}
		if len(ids) == 0 {
			break
		}
		for _, task := range readyTasks {
			waveByID[task.ID] = len(waves)
		}
		waves = append(waves, TaskWave{Number: len(waves), Tasks: ids})
		remaining = next
	}
	_ = sourcePath
	return diagnostics, waves
}

func invalidateDependentTasks(tasks []Task, declared map[string][]int) {
	for changed := true; changed; {
		changed = false
		for i := range tasks {
			if !tasks[i].Valid {
				continue
			}
			for _, dependency := range tasks[i].DependsOn {
				indices := declared[dependency]
				if len(indices) != 1 || !tasks[indices[0]].Valid {
					tasks[i].Valid, tasks[i].Files = false, nil
					changed = true
					break
				}
			}
		}
	}
}

func isTaskHeader(line []byte) bool {
	cells, ok := parseTableRow(line)
	if !ok || len(cells) != len(taskColumns) {
		return false
	}
	for i := range cells {
		if strings.TrimSpace(cells[i]) != taskColumns[i] {
			return false
		}
	}
	return true
}

func isTableSeparator(line []byte) bool {
	cells, ok := parseTableRow(line)
	if !ok || len(cells) != len(taskColumns) {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		cell = strings.TrimPrefix(strings.TrimSuffix(cell, ":"), ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func parseTableRow(line []byte) ([]string, bool) {
	line = bytes.TrimSpace(line)
	if len(line) < 2 || line[0] != '|' || line[len(line)-1] != '|' {
		return nil, false
	}
	line = line[1 : len(line)-1]
	var cells []string
	var current bytes.Buffer
	var ticks int
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case escaped:
			current.WriteByte(c)
			escaped = false
		case c == '\\':
			current.WriteByte(c)
			escaped = true
		case c == '`':
			run := 1
			for i+run < len(line) && line[i+run] == '`' {
				run++
			}
			if ticks == 0 {
				ticks = run
			} else if ticks == run {
				ticks = 0
			}
			current.Write(line[i : i+run])
			i += run - 1
		case c == '|' && ticks == 0:
			cells = append(cells, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	cells = append(cells, strings.TrimSpace(current.String()))
	return cells, true
}

func unwrapCodeSpan(value string) string {
	value = strings.TrimSpace(value)
	n := 0
	for n < len(value) && value[n] == '`' {
		n++
	}
	if n > 0 && len(value) >= 2*n && strings.HasSuffix(value, strings.Repeat("`", n)) {
		return strings.TrimSpace(value[n : len(value)-n])
	}
	return value
}

func lineString(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var out [20]byte
	at := len(out)
	for value > 0 {
		at--
		out[at] = digits[value%10]
		value /= 10
	}
	return string(out[at:])
}
