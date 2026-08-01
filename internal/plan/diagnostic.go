package plan

import "sort"

type Diagnostic struct {
	Code     string
	Location Location
	Message  string
	Repair   string
}

func diagnostic(code string, location Location, message, repair string) Diagnostic {
	return Diagnostic{Code: code, Location: location, Message: message, Repair: repair}
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		a, b := diagnostics[i], diagnostics[j]
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.Offset != b.Location.Offset {
			return a.Location.Offset < b.Location.Offset
		}
		return a.Code < b.Code
	})
}
