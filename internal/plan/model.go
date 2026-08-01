package plan

// Location identifies authored bytes in one artifact.
type Location struct {
	Path   string
	Offset int
	Line   int
	Column int
}

// Source preserves an artifact exactly as read.
type Source struct {
	Path    string
	Bytes   []byte
	Present bool
}

type Section struct {
	Name        string
	Heading     string
	Content     []byte
	Location    Location
	ContentFrom Location
	Placeholder bool
}

type RequirementReference struct {
	Capability  string
	Requirement string
	Location    Location
}

type Proposal struct {
	Source      Source
	Sections    []Section
	Diagnostics []Diagnostic
}

type Design struct {
	Source      Source
	Sections    []Section
	References  []RequirementReference
	Diagnostics []Diagnostic
}
