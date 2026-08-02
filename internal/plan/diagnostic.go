package plan

import (
	"cmp"
	"slices"
)

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
	slices.SortStableFunc(diagnostics, func(a, b Diagnostic) int {
		return cmp.Or(
			cmp.Compare(a.Location.Path, b.Location.Path),
			cmp.Compare(a.Location.Offset, b.Location.Offset),
			cmp.Compare(a.Code, b.Code),
		)
	})
}
