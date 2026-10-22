package triage

// ListLogsInput is the arguments of list_logs.
type ListLogsInput struct {
	Path   string `json:"path,omitempty" jsonschema:"file or folder under the log root; . or empty for the root"`
	Limit  int    `json:"limit,omitempty" jsonschema:"how many files to show"`
	Offset int    `json:"offset,omitempty" jsonschema:"how many files to skip first"`
}

// LogFile is one file in a list_logs answer.
type LogFile struct {
	Path       string `json:"path" jsonschema:"relative to the log root"`
	SizeBytes  int64  `json:"size_bytes" jsonschema:"size on disk"`
	Compressed bool   `json:"compressed" jsonschema:"true for a .gz file"`
	Format     string `json:"format" jsonschema:"the detected log format, or unknown"`
}

// ListLogsOutput is the answer of list_logs.
type ListLogsOutput struct {
	Path  string    `json:"path" jsonschema:"the requested path, normalized; . for the root"`
	Files []LogFile `json:"files" jsonschema:"the files on this page, in byte order of the path"`
	Total int       `json:"total" jsonschema:"how many files there are under the path"`
	Shown int       `json:"shown" jsonschema:"how many files this answer lists"`
	Note  string    `json:"note" jsonschema:"how to get the next page; empty when there is none"`
}

// SummarizeLogsInput is the arguments of summarize_logs.
type SummarizeLogsInput struct {
	Path     string  `json:"path,omitempty" jsonschema:"file or folder under the log root; . or empty for the root"`
	Since    *string `json:"since,omitempty" jsonschema:"start of the window, inclusive: 2026-01-31 or 2026-01-31T14:00, UTC unless a zone is given"`
	Until    *string `json:"until,omitempty" jsonschema:"end of the window, exclusive, in the same form as since"`
	MinLevel string  `json:"min_level,omitempty" jsonschema:"the lowest level a group needs to be listed; any also lists lines of unknown level"`
	Limit    int     `json:"limit,omitempty" jsonschema:"how many groups to show"`
}

// SkippedFile is a file a scan didn't read.
type SkippedFile struct {
	Path   string `json:"path" jsonschema:"relative to the log root"`
	Reason string `json:"reason" jsonschema:"why the file was skipped"`
}

// LevelCounts counts lines per level.
type LevelCounts struct {
	Critical int `json:"critical"`
	Error    int `json:"error"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
	Debug    int `json:"debug"`
	Unknown  int `json:"unknown"`
}

// Example is the first line of a group.
type Example struct {
	File string `json:"file" jsonschema:"relative to the log root"`
	Line int    `json:"line" jsonschema:"1-based line number in the file"`
	Text string `json:"text" jsonschema:"the line, cut to 500 characters"`
}

// Group is every line in the window with the same format, level and pattern.
type Group struct {
	GroupID   string   `json:"group_id" jsonschema:"12 hex characters that name the group"`
	Format    string   `json:"format" jsonschema:"the log format of the lines"`
	Level     string   `json:"level" jsonschema:"the level of the lines"`
	Pattern   string   `json:"pattern" jsonschema:"the message with numbers, addresses, ids and hex replaced"`
	Count     int      `json:"count" jsonschema:"how many lines the group has"`
	FirstSeen *string  `json:"first_seen" jsonschema:"the earliest time of its lines, or null"`
	LastSeen  *string  `json:"last_seen" jsonschema:"the latest time of its lines, or null"`
	FileCount int      `json:"file_count" jsonschema:"how many files hold its lines"`
	Files     []string `json:"files" jsonschema:"the first 5 of those files, in byte order"`
	Example   Example  `json:"example" jsonschema:"the group's first line in scan order"`
}

// SummarizeLogsOutput is the answer of summarize_logs.
type SummarizeLogsOutput struct {
	ScanID        string        `json:"scan_id" jsonschema:"the handle of this scan; pass it to group_lines"`
	ExpiresAt     string        `json:"expires_at" jsonschema:"when scan_id stops working, UTC"`
	Path          string        `json:"path" jsonschema:"the requested path, normalized; . for the root"`
	Since         *string       `json:"since" jsonschema:"the window's start as read, or null"`
	Until         *string       `json:"until" jsonschema:"the window's end as read, or null"`
	MinLevel      string        `json:"min_level" jsonschema:"the lowest level listed"`
	FilesScanned  int           `json:"files_scanned" jsonschema:"files read, skipped ones excluded"`
	Skipped       []SkippedFile `json:"skipped" jsonschema:"files not read, at most 20, by path"`
	LinesScanned  int           `json:"lines_scanned" jsonschema:"every line read"`
	LinesInWindow int           `json:"lines_in_window" jsonschema:"lines whose time is in the window"`
	FirstSeen     *string       `json:"first_seen" jsonschema:"the earliest time in the window, or null"`
	LastSeen      *string       `json:"last_seen" jsonschema:"the latest time in the window, or null"`
	ByLevel       LevelCounts   `json:"by_level" jsonschema:"lines in the window per level"`
	Groups        []Group       `json:"groups" jsonschema:"groups at or above min_level, most lines first"`
	TotalGroups   int           `json:"total_groups" jsonschema:"groups at or above min_level"`
	Shown         int           `json:"shown" jsonschema:"groups in this answer"`
	Note          string        `json:"note" jsonschema:"how to see more groups; empty when all are shown"`
}

// GroupLinesInput is the arguments of group_lines.
type GroupLinesInput struct {
	ScanID  string `json:"scan_id" jsonschema:"the scan_id a summarize_logs call returned"`
	GroupID string `json:"group_id" jsonschema:"a group_id from that scan's groups"`
	Limit   int    `json:"limit,omitempty" jsonschema:"how many lines to show"`
	Offset  int    `json:"offset,omitempty" jsonschema:"how many lines to skip first"`
}

// GroupLine is one line of a group.
type GroupLine struct {
	File string  `json:"file" jsonschema:"relative to the log root"`
	Line int     `json:"line" jsonschema:"1-based line number in the file"`
	Time *string `json:"time" jsonschema:"the line's time, or null when its file has none"`
	Text string  `json:"text" jsonschema:"the line, cut to 500 characters"`
}

// GroupLinesOutput is the answer of group_lines.
type GroupLinesOutput struct {
	ScanID  string      `json:"scan_id" jsonschema:"the scan the lines come from"`
	GroupID string      `json:"group_id" jsonschema:"the group the lines belong to"`
	Pattern string      `json:"pattern" jsonschema:"the group's pattern"`
	Lines   []GroupLine `json:"lines" jsonschema:"the lines on this page, in scan order"`
	Total   int         `json:"total" jsonschema:"how many lines the group has"`
	Shown   int         `json:"shown" jsonschema:"lines in this answer"`
	Note    string      `json:"note" jsonschema:"how to get the next page, and whether the scan kept every line"`
}

// ReadLinesInput is the arguments of read_lines.
type ReadLinesInput struct {
	Path   string `json:"path" jsonschema:"one file under the log root"`
	Line   int    `json:"line" jsonschema:"the 1-based line to read around"`
	Before int    `json:"before,omitempty" jsonschema:"how many lines to show before it"`
	After  int    `json:"after,omitempty" jsonschema:"how many lines to show after it"`
}

// TextLine is one numbered line of a file.
type TextLine struct {
	Line int    `json:"line" jsonschema:"1-based line number"`
	Text string `json:"text" jsonschema:"the line, cut to 500 characters"`
}

// ReadLinesOutput is the answer of read_lines.
type ReadLinesOutput struct {
	File       string     `json:"file" jsonschema:"the file, relative to the log root"`
	Start      int        `json:"start" jsonschema:"the first line shown"`
	End        int        `json:"end" jsonschema:"the last line shown"`
	TotalLines int        `json:"total_lines" jsonschema:"how many lines the file has"`
	Lines      []TextLine `json:"lines" jsonschema:"the lines from start to end"`
}

// SearchLogsInput is the arguments of search_logs.
type SearchLogsInput struct {
	Path     string  `json:"path,omitempty" jsonschema:"file or folder under the log root; . or empty for the root"`
	Pattern  string  `json:"pattern" jsonschema:"regular expression searched anywhere in the raw line; case-sensitive unless it starts with (?i)"`
	Since    *string `json:"since,omitempty" jsonschema:"start of the window, inclusive: 2026-01-31 or 2026-01-31T14:00, UTC unless a zone is given"`
	Until    *string `json:"until,omitempty" jsonschema:"end of the window, exclusive, in the same form as since"`
	MinLevel string  `json:"min_level,omitempty" jsonschema:"the lowest level a line needs; any also matches lines of unknown level"`
	Limit    int     `json:"limit,omitempty" jsonschema:"how many matches to show"`
	Offset   int     `json:"offset,omitempty" jsonschema:"how many matches to skip first"`
}

// Match is one matching line.
type Match struct {
	File  string  `json:"file" jsonschema:"relative to the log root"`
	Line  int     `json:"line" jsonschema:"1-based line number in the file"`
	Time  *string `json:"time" jsonschema:"the line's time, or null when its file has none"`
	Level string  `json:"level" jsonschema:"the line's level"`
	Text  string  `json:"text" jsonschema:"the line, cut to 500 characters"`
}

// SearchLogsOutput is the answer of search_logs.
type SearchLogsOutput struct {
	Path    string  `json:"path" jsonschema:"the requested path, normalized; . for the root"`
	Pattern string  `json:"pattern" jsonschema:"the pattern searched"`
	Matches []Match `json:"matches" jsonschema:"the matches on this page, by file and line"`
	Total   int     `json:"total" jsonschema:"every match in the window"`
	Shown   int     `json:"shown" jsonschema:"matches in this answer"`
	Note    string  `json:"note" jsonschema:"how to get the next page; empty when there is none"`
}

// Evidence is one line a finding relies on.
type Evidence struct {
	File string `json:"file" jsonschema:"the file, relative to the log root"`
	Line int    `json:"line" jsonschema:"1-based line number in that file"`
	Note string `json:"note" jsonschema:"what the line shows"`
}

// SaveFindingInput is the arguments of save_finding.
type SaveFindingInput struct {
	Title    string     `json:"title" jsonschema:"a short title; it also names the file"`
	Severity string     `json:"severity" jsonschema:"how bad the finding is"`
	Summary  string     `json:"summary" jsonschema:"what happened and why, in plain words"`
	Evidence []Evidence `json:"evidence" jsonschema:"the lines the finding relies on"`
}

// SaveFindingOutput is the answer of save_finding.
type SaveFindingOutput struct {
	Name    string `json:"name" jsonschema:"the file name without .md"`
	Path    string `json:"path" jsonschema:"the file, relative to the reports folder"`
	Created string `json:"created" jsonschema:"when it was saved, UTC"`
}
