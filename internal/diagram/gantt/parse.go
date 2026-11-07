// Package gantt parses and lays out Mermaid gantt charts.
package gantt

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lea/mermaid/internal/guard"
	"github.com/lea/mermaid/internal/source"
)

// Task is one bar of the chart.
type Task struct {
	Name      string
	ID        string
	Section   int
	Done      bool
	Active    bool
	Crit      bool
	Milestone bool
	Start     time.Time
	End       time.Time

	line                int
	startSpec, endSpec  string
	resolved, resolving bool
}

// Diagram is a parsed gantt chart.
type Diagram struct {
	Title           string
	Sections        []string
	Tasks           []*Task
	AxisFormat      string
	TickInterval    time.Duration // 0 = automatic
	TickMonths      int           // month-based tick interval
	dateLayout      string
	unix            int // 0, 1 (seconds) or 1000 (milliseconds)
	excludeWeekends bool
	excludeDays     map[time.Weekday]bool
	excludeDates    map[int64]bool // days since the Unix epoch
	ids             map[string]int
}

const (
	// maxDurationDays bounds task length to keep the axis computable.
	maxDurationDays = 365 * 200
	// maxAfterDepth bounds chains of "after" references.
	maxAfterDepth = 1000
)

var (
	durationRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(ms|s|m|h|d|w|M|y)$`)
	tickRe     = regexp.MustCompile(`^(\d+)(millisecond|second|minute|hour|day|week|month)$`)
)

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// Parse parses a gantt chart.
func Parse(g *guard.Guard, lines []source.Line) (*Diagram, error) {
	d := &Diagram{
		dateLayout: "2006-01-02", AxisFormat: "%Y-%m-%d",
		excludeDays: map[time.Weekday]bool{}, excludeDates: map[int64]bool{}, ids: map[string]int{},
	}
	section := -1
	type exclude struct {
		text string
		line int
	}
	var excludes []exclude
	includesLine := 0 // first line with includes
	for _, l := range lines[1:] {
		if err := g.Tick(); err != nil {
			return nil, err
		}
		t := strings.TrimSpace(l.Text)
		kw, rest, _ := strings.Cut(t, " ")
		rest = strings.TrimSpace(rest)
		switch kw {
		case "title":
			d.Title = source.Label(rest)
			continue
		case "dateFormat":
			layout, unix, err := convertDateFormat(rest)
			if errors.Is(err, errOrdinal) {
				return nil, source.Unsupported(l.No, "%v", err)
			}
			if err != nil {
				return nil, source.Errorf(l.No, "%v", err)
			}
			d.dateLayout, d.unix = layout, unix
			continue
		case "axisFormat":
			d.AxisFormat = rest
			continue
		case "tickInterval":
			m := tickRe.FindStringSubmatch(rest)
			if m == nil {
				return nil, source.Errorf(l.No, "invalid tickInterval %q", rest)
			}
			n, _ := strconv.Atoi(m[1])
			if n <= 0 {
				return nil, source.Errorf(l.No, "invalid tickInterval %q", rest)
			}
			unit := map[string]time.Duration{"millisecond": time.Millisecond, "second": time.Second,
				"minute": time.Minute, "hour": time.Hour, "day": 24 * time.Hour, "week": 7 * 24 * time.Hour}
			if m[2] == "month" {
				d.TickMonths = n
			} else {
				d.TickInterval = time.Duration(n) * unit[m[2]]
			}
			continue
		case "excludes":
			for _, e := range strings.Split(rest, ",") {
				excludes = append(excludes, exclude{e, l.No})
			}
			continue
		case "section":
			d.Sections = append(d.Sections, source.Label(rest))
			section = len(d.Sections) - 1
			continue
		case "weekend":
			// NOTE(mermaid-compat): which days a weekend has (Mermaid 11)
			// changes the excluded days; not followed.
			return nil, source.Unsupported(l.No, "weekend")
		case "includes":
			// NOTE(mermaid-compat): included dates override excludes; not
			// followed. Without excludes they change nothing.
			if strings.TrimSpace(rest) != "" && includesLine == 0 {
				includesLine = l.No
			}
			continue
		case "todayMarker", "weekday", "inclusiveEndDates", "topAxis", "click", "accTitle:", "accDescr:", "displayMode":
			// NOTE(mermaid-compat): these options do not change this renderer's
			// output; the today marker is never drawn so that output stays
			// deterministic.
			continue
		}
		name, meta, ok := strings.Cut(t, ":")
		if !ok {
			return nil, source.Errorf(l.No, "expected \"task name : details\"")
		}
		if section < 0 {
			d.Sections = append(d.Sections, "")
			section = 0
		}
		task, err := d.parseTask(strings.TrimSpace(name), meta, l.No)
		if err != nil {
			return nil, err
		}
		task.Section = section
		d.Tasks = append(d.Tasks, task)
		if task.ID != "" {
			d.ids[task.ID] = len(d.Tasks) - 1
		}
		if err := g.Count("tasks", len(d.Tasks)); err != nil {
			return nil, err
		}
	}
	if includesLine > 0 && len(excludes) > 0 {
		return nil, source.Unsupported(includesLine, "includes together with excludes")
	}
	for _, ex := range excludes {
		e := strings.TrimSpace(ex.text)
		switch lower := strings.ToLower(e); {
		case lower == "weekends":
			d.excludeWeekends = true
		case weekdays[lower] != 0 || lower == "sunday":
			d.excludeDays[weekdays[lower]] = true
		case e == "":
		default:
			tm, err := d.parseDate(e)
			if err != nil {
				return nil, source.Errorf(ex.line, "invalid excludes date %q", e)
			}
			d.excludeDates[dayNumber(tm)] = true
		}
	}
	if len(d.Tasks) == 0 {
		return nil, source.Errorf(lines[len(lines)-1].No, "gantt chart has no tasks")
	}
	for i := range d.Tasks {
		if err := d.resolve(g, i, 0); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *Diagram) parseTask(name, meta string, line int) (*Task, error) {
	t := &Task{Name: source.Label(name), line: line}
	var items []string
	for _, it := range strings.Split(meta, ",") {
		items = append(items, strings.TrimSpace(it))
	}
	for len(items) > 0 {
		switch items[0] {
		case "done":
			t.Done = true
		case "active":
			t.Active = true
		case "crit":
			t.Crit = true
		case "milestone":
			t.Milestone = true
		case "vert":
			// NOTE(mermaid-compat): vertical markers (Mermaid 11) are not
			// drawn; valid Mermaid, so not a syntax error.
			return nil, source.Unsupported(line, "vert markers")
		default:
			goto rest
		}
		items = items[1:]
	}
rest:
	switch len(items) {
	case 1:
		t.endSpec = items[0]
	case 2:
		var pe parseErr
		if _, err := d.parseDate(items[0]); err == nil || strings.HasPrefix(items[0], "after ") {
			t.startSpec, t.endSpec = items[0], items[1]
		} else if errors.As(err, &pe) {
			return nil, source.Errorf(line, "invalid start date %q: %v", items[0], err)
		} else if dateShaped(items[0]) {
			// Report "2000-13-45" as a bad date, not as a task id.
			return nil, source.Errorf(line, "invalid start date %q (check dateFormat)", items[0])
		} else {
			t.ID, t.endSpec = items[0], items[1]
		}
	case 3:
		t.ID, t.startSpec, t.endSpec = items[0], items[1], items[2]
	default:
		return nil, source.Errorf(line, "task needs [id,] [start,] end")
	}
	if t.endSpec == "" {
		return nil, source.Errorf(line, "task has no end or duration")
	}
	return t, nil
}

func (d *Diagram) resolve(g *guard.Guard, i, depth int) error {
	t := d.Tasks[i]
	if t.resolved {
		return nil
	}
	if t.resolving || depth > maxAfterDepth {
		return source.Errorf(t.line, "circular task dependency")
	}
	if err := g.Tick(); err != nil {
		return err
	}
	t.resolving = true
	defer func() { t.resolving = false }()

	switch {
	case t.startSpec == "":
		if i == 0 {
			// NOTE(mermaid-compat): Mermaid starts it today; that would
			// make the output depend on the date.
			return source.Unsupported(t.line, "a first task without a start date (it would start today)")
		}
		if err := d.resolve(g, i-1, depth+1); err != nil {
			return err
		}
		t.Start = d.Tasks[i-1].End
	case strings.HasPrefix(t.startSpec, "after "):
		first := true
		for _, id := range strings.Fields(t.startSpec[len("after "):]) {
			j, ok := d.ids[id]
			if !ok {
				return source.Errorf(t.line, "unknown task id %q", id)
			}
			if err := d.resolve(g, j, depth+1); err != nil {
				return err
			}
			if first || d.Tasks[j].End.After(t.Start) {
				t.Start = d.Tasks[j].End
			}
			first = false
		}
	default:
		tm, err := d.parseDate(t.startSpec)
		if err != nil {
			return source.Errorf(t.line, "invalid start %q", t.startSpec)
		}
		t.Start = tm
	}

	switch {
	case strings.HasPrefix(t.endSpec, "until "):
		id := strings.TrimSpace(t.endSpec[len("until "):])
		j, ok := d.ids[id]
		if !ok {
			return source.Errorf(t.line, "unknown task id %q", id)
		}
		if err := d.resolve(g, j, depth+1); err != nil {
			return err
		}
		t.End = d.Tasks[j].Start
	case durationRe.MatchString(t.endSpec):
		end, err := d.addDuration(g, t.Start, t.endSpec)
		if err != nil {
			if errors.Is(err, errTooLong) {
				return source.TooLarge(t.line, "%v", err)
			}
			var pe parseErr
			if errors.As(err, &pe) {
				return source.Errorf(t.line, "%v", err)
			}
			return err
		}
		t.End = end
	default:
		tm, err := d.parseDate(t.endSpec)
		if err != nil {
			return source.Errorf(t.line, "invalid end %q", t.endSpec)
		}
		t.End = tm
	}
	if t.End.Before(t.Start) {
		t.End = t.Start
	}
	if t.End.Sub(t.Start) > maxDurationDays*24*time.Hour {
		return source.TooLarge(t.line, "task is too long")
	}
	t.resolved = true
	return nil
}

// dayNumber returns the calendar day of tm as days since the Unix epoch.
func dayNumber(tm time.Time) int64 {
	y, m, dd := tm.Date()
	return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

func (d *Diagram) excluded(day time.Time) bool {
	return d.excludedWeekday(day) || len(d.excludeDates) > 0 && d.excludeDates[dayNumber(day)]
}

// excludedWeekday reports whether day's weekday is excluded (weekends or
// an excluded day name).
func (d *Diagram) excludedWeekday(day time.Time) bool {
	wd := day.Weekday()
	return d.excludeWeekends && (wd == time.Saturday || wd == time.Sunday) || d.excludeDays[wd]
}

// addDuration adds spec to start. Day and week durations skip excluded days,
// which needs a loop over days, so it ticks the guard.
func (d *Diagram) addDuration(g *guard.Guard, start time.Time, spec string) (time.Time, error) {
	m := durationRe.FindStringSubmatch(spec)
	v, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "d", "w":
		days := v
		if m[2] == "w" {
			days *= 7
		}
		if days > maxDurationDays {
			return start, errTooLong
		}
		whole := int(days)
		end := start
		if !d.excludeWeekends && len(d.excludeDays) == 0 && len(d.excludeDates) == 0 {
			end = start.AddDate(0, 0, whole)
		} else {
			// Only the weekday rules can exclude every day; excluded
			// dates are finite. With one day left per week, the end is
			// within 7 days per counted day plus the excluded dates.
			allWeek := true
			for k := 0; k < 7 && allWeek; k++ {
				allWeek = d.excludedWeekday(start.AddDate(0, 0, k))
			}
			if allWeek && whole > 0 {
				return start, parseErr("every day is excluded")
			}
			limit := 7*whole + len(d.excludeDates) + 7
			for n, steps := 0, 0; n < whole; steps++ {
				if steps > limit {
					return start, errTooLong
				}
				if err := g.Tick(); err != nil {
					return start, err
				}
				if !d.excluded(end) {
					n++
				}
				end = end.AddDate(0, 0, 1)
			}
		}
		frac := days - float64(whole)
		return end.Add(time.Duration(float64(frac * 24 * float64(time.Hour)))), nil
	case "M":
		if v > maxDurationDays/30 {
			return start, errTooLong
		}
		return start.AddDate(0, int(v), 0), nil
	case "y":
		if v > maxDurationDays/365 {
			return start, errTooLong
		}
		return start.AddDate(int(v), 0, 0), nil
	}
	unit := map[string]time.Duration{"ms": time.Millisecond, "s": time.Second, "m": time.Minute, "h": time.Hour}[m[2]]
	if float64(v*float64(unit)) > float64(maxDurationDays)*24*float64(time.Hour) {
		return start, errTooLong
	}
	return start.Add(time.Duration(math.Round(float64(v * float64(unit))))), nil
}

type parseErr string

func (e parseErr) Error() string { return string(e) }

const errTooLong = parseErr("duration is too long")

// errOrdinal is the dateFormat token Do (day of month with an ordinal
// suffix), which this library does not read.
const errOrdinal = parseErr("dateFormat token Do (1st, 2nd, ...) is not supported")

func (d *Diagram) parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if d.unix > 0 {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		// A JavaScript Date (which Mermaid uses) spans ±8.64e15 ms;
		// beyond it Mermaid has an invalid date. Here the end of a task
		// near MaxInt64 seconds wrapped around and reversed the axis.
		const maxJSDateMs = 8_640_000_000_000_000
		if lim := maxJSDateMs / (1000 / int64(d.unix)); v > lim || v < -lim {
			return time.Time{}, parseErr("date outside the range of a JavaScript Date")
		}
		if d.unix == 1000 {
			return time.UnixMilli(v).UTC(), nil
		}
		return time.Unix(v, 0).UTC(), nil
	}
	return time.ParseInLocation(d.dateLayout, s, time.UTC)
}

// convertDateFormat converts a Day.js style format to a Go layout.
func convertDateFormat(f string) (string, int, error) {
	switch f {
	case "X":
		return "", 1, nil
	case "x":
		return "", 1000, nil
	}
	tokens := []struct{ from, to string }{
		{"YYYY", "2006"}, {"YY", "06"}, {"MMMM", "January"}, {"MMM", "Jan"}, {"MM", "01"}, {"M", "1"},
		{"DD", "02"}, {"Do", "2"}, {"D", "2"}, {"HH", "15"}, {"H", "15"}, {"hh", "03"}, {"h", "3"},
		{"mm", "04"}, {"m", "4"}, {"ss", "05"}, {"s", "5"}, {"SSS", ".000"}, {"A", "PM"}, {"a", "pm"},
		{"ZZ", "-0700"}, {"Z", "-07:00"},
	}
	var b strings.Builder
	for i := 0; i < len(f); {
		matched := false
		for _, tk := range tokens {
			if strings.HasPrefix(f[i:], tk.from) {
				if tk.from == "Do" {
					// Go's time package cannot read "1st".
					return "", 0, errOrdinal
				}
				to := tk.to
				if tk.from == "SSS" {
					// Go wants the fraction right after its separator;
					// "ss.SSS" gave "05..000".
					if s := b.String(); strings.HasSuffix(s, ".") || strings.HasSuffix(s, ",") {
						to = "000"
					}
				}
				b.WriteString(to)
				i += len(tk.from)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(f[i])
			i++
		}
	}
	if b.Len() == 0 {
		return "", 0, parseErr("empty dateFormat")
	}
	return b.String(), 0, nil
}

// dateShaped reports whether s looks like a date or time (digits and
// separators only) rather than a task id.
func dateShaped(s string) bool {
	digit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case strings.ContainsRune("-/.: T", r):
		default:
			return false
		}
	}
	return digit && strings.ContainsAny(s, "-/.:")
}

// SetDefaultTitle sets the title from front matter unless the diagram has
// its own.
func (d *Diagram) SetDefaultTitle(t string) {
	if d.Title == "" {
		d.Title = t
	}
}
