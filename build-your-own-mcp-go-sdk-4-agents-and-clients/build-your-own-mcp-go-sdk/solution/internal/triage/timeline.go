package triage

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/server/internal/logs"
)

// bucketLimit is the most buckets one timeline may have.
const bucketLimit = 500

const secondsPerDay = 24 * 60 * 60

// decimalValue is what a captured value must look like to count as a number.
var decimalValue = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)

func timelineTool() *mcp.Tool {
	in := inputSchema[TimelineInput]()
	in.Properties["scan_id"].Pattern = `^s_[0-9a-f]{16}$`
	for _, name := range []string{"pattern", "value_pattern"} {
		p := in.Properties[name]
		p.MinLength = jsonschema.Ptr(1)
		p.MaxLength = jsonschema.Ptr(200)
		p.Default = json.RawMessage(`null`)
	}
	intParam(in, "bucket_minutes", 1, 1440, 60)
	return &mcp.Tool{
		Name:  "timeline",
		Title: "Timeline of matching lines",
		Description: "Count the lines of an earlier summarize_logs scan that match a pattern, in time buckets " +
			"inside the scan's window, empty buckets included. With value_pattern (one capture group), each " +
			"bucket also gets the min, median and max of the number it captures, so a trend shows over hours or days.",
		InputSchema: in,
		Annotations: readOnly("Timeline of matching lines"),
	}
}

// hit is one counted line: its time, and its value when it has one.
type hit struct {
	unix  int64
	value *float64
}

func (t *tools) timeline(ctx context.Context, _ *mcp.CallToolRequest, in TimelineInput) (*mcp.CallToolResult, TimelineOutput, error) {
	kept, found := t.scans.Get(callerOf(ctx).Name, in.ScanID)
	if !found {
		return nil, TimelineOutput{}, errUnknownScan(in.ScanID)
	}
	match, err := compileOptional(in.Pattern, "pattern")
	if err != nil {
		return nil, TimelineOutput{}, err
	}
	value, err := compileOptional(in.ValuePattern, "value_pattern")
	if err != nil {
		return nil, TimelineOutput{}, err
	}
	if value != nil && value.NumSubexp() != 1 {
		return nil, TimelineOutput{}, errOneCaptureGroup()
	}
	step := int64(in.BucketMinutes) * 60
	win := kept.window

	var start, end *int64
	if win.since != nil && win.until != nil {
		if start, end, err = gridSpan(gridStart(*win.since, step), *win.until, step); err != nil {
			return nil, TimelineOutput{}, err
		}
	}

	var hits []hit
	for _, e := range kept.files {
		_, lines, skip, err := readEntry(e)
		if err != nil {
			return nil, TimelineOutput{}, err
		}
		if skip != "" {
			continue
		}
		for _, l := range lines {
			if !l.Timed || !win.contains(l) || (match != nil && !match.MatchString(l.Text)) {
				continue
			}
			h := hit{unix: l.Unix}
			if value != nil {
				h.value = valueOf(value, l.Text)
			}
			hits = append(hits, h)
		}
	}

	switch {
	case start != nil:
	case len(hits) > 0:
		first, last := hits[0].unix, hits[0].unix
		for _, h := range hits {
			first, last = min(first, h.unix), max(last, h.unix)
		}
		if win.since != nil {
			first = *win.since
		}
		limit := last + 1
		if win.until != nil {
			limit = *win.until
		}
		if start, end, err = gridSpan(gridStart(first, step), limit, step); err != nil {
			return nil, TimelineOutput{}, err
		}
	case win.since != nil:
		s := gridStart(*win.since, step)
		start = &s
	}

	out := TimelineOutput{
		ScanID: in.ScanID, Pattern: in.Pattern, ValuePattern: in.ValuePattern, BucketMinutes: in.BucketMinutes,
		Start: unixText(start), End: unixText(end), Buckets: []TimelineBucket{},
	}
	if start == nil || end == nil {
		return nil, out, nil
	}
	n := (*end - *start) / step
	values := make([][]float64, n)
	counts := make([]int, n)
	for _, h := range hits {
		i := (h.unix - *start) / step
		counts[i]++
		if h.value != nil {
			values[i] = append(values[i], *h.value)
		}
	}
	for i := range n {
		b := TimelineBucket{Start: logs.FormatTime(*start + i*step), Count: counts[i], ValueCount: len(values[i])}
		if len(values[i]) > 0 {
			b.Min, b.Median, b.Max = describeValues(values[i])
		}
		out.Buckets = append(out.Buckets, b)
		out.Total += counts[i]
	}
	return nil, out, nil
}

func compileOptional(pattern *string, field string) (*regexp.Regexp, error) {
	if pattern == nil {
		return nil, nil
	}
	re, err := regexp.Compile(*pattern)
	if err != nil {
		return nil, errBadPattern(field)
	}
	return re, nil
}

// gridStart is the start of the bucket that holds unix, on a grid of step
// seconds anchored at 00:00 UTC of unix's day.
func gridStart(unix, step int64) int64 {
	midnight := floorDiv(unix, secondsPerDay) * secondsPerDay
	return midnight + floorDiv(unix-midnight, step)*step
}

// gridSpan returns the grid from start through the bucket before limit, or
// E12 when that takes more than bucketLimit buckets.
func gridSpan(start, limit, step int64) (*int64, *int64, error) {
	n := (limit - start + step - 1) / step
	if n > bucketLimit {
		return nil, nil, errTooManyBuckets(n)
	}
	end := start + n*step
	return &start, &end, nil
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// valueOf returns the number the first match's group captured, or nil when
// there is no match or the group isn't a plain decimal number.
func valueOf(re *regexp.Regexp, text string) *float64 {
	m := re.FindStringSubmatchIndex(text)
	if m == nil || m[2] < 0 {
		return nil
	}
	group := text[m[2]:m[3]]
	if !decimalValue.MatchString(group) {
		return nil
	}
	v, err := strconv.ParseFloat(group, 64)
	if err != nil {
		return nil
	}
	return &v
}

// describeValues returns the min, the median rounded to 3 decimals, and the
// max of a non-empty list.
func describeValues(values []float64) (lo, median, hi *float64) {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	mid := len(sorted) / 2
	m := sorted[mid]
	if len(sorted)%2 == 0 {
		m = (sorted[mid-1] + sorted[mid]) / 2
	}
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(m, 'f', 3, 64), 64)
	return &sorted[0], &rounded, &sorted[len(sorted)-1]
}
