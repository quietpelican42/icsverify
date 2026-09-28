package ics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TZIDPolicy controls how a TZID parameter that does not resolve to a
// VTIMEZONE defined in the same object is reported. Many real-world
// producers reference IANA time zone database names (e.g.
// "America/New_York") without embedding a VTIMEZONE, relying on the
// consumer's own zone database; RFC 5545 §3.6.5 requires a matching
// VTIMEZONE, so this is a genuine (if common) violation.
type TZIDPolicy int

const (
	// TZIDWarn reports an unresolved TZID as a warning (default).
	TZIDWarn TZIDPolicy = iota
	// TZIDError reports it as an error.
	TZIDError
	// TZIDIgnore does not check TZID resolution at all.
	TZIDIgnore
)

// Options configures Validate.
type Options struct {
	// TZID controls how unresolved TZID parameters are reported.
	TZID TZIDPolicy
}

// Validate parses and validates an iCalendar object. It always returns a
// non-nil *Result (possibly with Findings from partial analysis); err is
// non-nil only when no usable VCALENDAR object could be found at all,
// which callers should treat as "could not analyse" (CLI exit code 2)
// rather than "found problems" (exit code 1).
func Validate(data []byte, opts Options) (*Result, error) {
	res := &Result{}

	lines, diags := tokenize(data)
	for _, d := range diags {
		res.add(d.Severity, d.Rule, d.Section, "", d.Line, "%s", d.Message)
	}

	root, ok := buildTree(lines, res)
	if !ok {
		return res, fmt.Errorf("no BEGIN:VCALENDAR object found")
	}

	validateCalendar(root, res, opts)
	return res, nil
}

func validateCalendar(cal *Component, res *Result, opts Options) {
	path := cal.Path()

	requireOnce(cal, res, "PRODID", "RFC 5545 §3.7.3", path)
	requireOnce(cal, res, "VERSION", "RFC 5545 §3.7.4", path)
	atMostOnce(cal, res, "CALSCALE", "RFC 5545 §3.7.1", path)
	atMostOnce(cal, res, "METHOD", "RFC 5545 §3.7.2", path)

	if v, ok := cal.First("VERSION"); ok {
		if !isAcceptedVersion(v.Value) {
			res.warnf("vcalendar-version-value", "RFC 5545 §3.7.4", path, v.Line,
				"VERSION value %q is not \"2.0\" or a recognized min;max range", v.Value)
		}
	}

	methodPresent := false
	if m, ok := cal.First("METHOD"); ok {
		methodPresent = true
		if !validITIPMethod[strings.ToUpper(m.Value)] {
			res.warnf("method-unknown", "RFC 5546 §1.4", path, m.Line,
				"METHOD value %q is not one of the registered iTIP methods (PUBLISH, REQUEST, REPLY, ADD, CANCEL, REFRESH, COUNTER, DECLINECOUNTER)", m.Value)
		}
	}

	if len(cal.Children) == 0 {
		res.errorf("vcalendar-empty", "RFC 5545 §3.4", path, cal.Line,
			"VCALENDAR contains no VEVENT, VTODO, VJOURNAL, VFREEBUSY or VTIMEZONE component")
	}

	// Collect VTIMEZONE TZIDs defined at the top level for reference
	// resolution, and validate nesting for every component in the tree.
	tzids := map[string]bool{}
	for _, tz := range cal.ChildrenNamed("VTIMEZONE") {
		if p, ok := tz.First("TZID"); ok {
			tzids[p.Value] = true
		}
	}

	uidGroups := map[string][]uidOccurrence{}

	var walk func(c *Component)
	walk = func(c *Component) {
		checkNesting(c, res)
		switch strings.ToUpper(c.Name) {
		case "VEVENT":
			validateEvent(c, res, opts, methodPresent, tzids)
			collectUID(c, uidGroups)
		case "VTODO":
			validateTodo(c, res, opts, tzids)
			collectUID(c, uidGroups)
		case "VJOURNAL":
			validateJournal(c, res, opts, tzids)
			collectUID(c, uidGroups)
		case "VFREEBUSY":
			validateFreeBusy(c, res, opts, tzids)
			collectUID(c, uidGroups)
		case "VTIMEZONE":
			validateTimezone(c, res)
		case "VALARM":
			validateAlarm(c, res)
		}
		for _, ch := range c.Children {
			walk(ch)
		}
	}
	for _, ch := range cal.Children {
		walk(ch)
	}

	checkTZIDReferences(cal, res, opts, tzids)
	checkUIDUniqueness(uidGroups, res)
}

var validITIPMethod = map[string]bool{
	"PUBLISH": true, "REQUEST": true, "REPLY": true, "ADD": true,
	"CANCEL": true, "REFRESH": true, "COUNTER": true, "DECLINECOUNTER": true,
}

var versionRe = regexp.MustCompile(`^\d+\.\d+$`)

func isAcceptedVersion(v string) bool {
	if v == "2.0" {
		return true
	}
	parts := strings.Split(v, ";")
	if len(parts) == 2 {
		return versionRe.MatchString(parts[0]) && versionRe.MatchString(parts[1])
	}
	return versionRe.MatchString(v)
}

// allowedParents maps a component name to the set of names its parent
// must be one of. Component names not present here are not structurally
// scoped by this tool (e.g. RFC 7953 VAVAILABILITY and its AVAILABLE
// sub-component, RFC 9073 VLOCATION/PARTICIPANT): they are left alone
// rather than flagged, since validating them is out of scope.
var allowedParents = map[string][]string{
	"VEVENT":    {"VCALENDAR"},
	"VTODO":     {"VCALENDAR"},
	"VJOURNAL":  {"VCALENDAR"},
	"VFREEBUSY": {"VCALENDAR"},
	"VTIMEZONE": {"VCALENDAR"},
	"STANDARD":  {"VTIMEZONE"},
	"DAYLIGHT":  {"VTIMEZONE"},
	"VALARM":    {"VEVENT", "VTODO"},
}

func checkNesting(c *Component, res *Result) {
	allowed, scoped := allowedParents[strings.ToUpper(c.Name)]
	if !scoped {
		return
	}
	parentName := "the top level"
	if c.Parent != nil {
		parentName = c.Parent.Name
	}
	for _, a := range allowed {
		if c.Parent != nil && equalFold(c.Parent.Name, a) {
			return
		}
	}
	res.errorf("invalid-nesting", componentSection(c.Name), c.Path(), c.Line,
		"%s cannot appear inside %s; it may only appear inside %s", c.Name, parentName, strings.Join(allowed, " or "))
}

func componentSection(name string) string {
	switch strings.ToUpper(name) {
	case "VEVENT":
		return "RFC 5545 §3.6.1"
	case "VTODO":
		return "RFC 5545 §3.6.2"
	case "VJOURNAL":
		return "RFC 5545 §3.6.3"
	case "VFREEBUSY":
		return "RFC 5545 §3.6.4"
	case "VTIMEZONE", "STANDARD", "DAYLIGHT":
		return "RFC 5545 §3.6.5"
	case "VALARM":
		return "RFC 5545 §3.6.6"
	}
	return "RFC 5545 §3.6"
}

// uidOccurrence records one component's UID (and RECURRENCE-ID, if any)
// for cross-component uniqueness checking (RFC 5545 §3.8.4.7, §3.8.4.4).
type uidOccurrence struct {
	path        string
	line        int
	recurrence  string // raw RECURRENCE-ID value, "" for master
	hasRecurIDp bool
}

func collectUID(c *Component, groups map[string][]uidOccurrence) {
	uid, ok := c.First("UID")
	if !ok || uid.Value == "" {
		return
	}
	rid, hasRid := c.First("RECURRENCE-ID")
	occ := uidOccurrence{path: c.Path(), line: uid.Line, hasRecurIDp: hasRid}
	if hasRid {
		occ.recurrence = rid.Value
	}
	groups[uid.Value] = append(groups[uid.Value], occ)
}

func checkUIDUniqueness(groups map[string][]uidOccurrence, res *Result) {
	for uid, occs := range groups {
		masters := 0
		var masterLoc string
		seenRid := map[string]string{}
		for _, o := range occs {
			if !o.hasRecurIDp {
				masters++
				if masters > 1 {
					res.errorf("duplicate-uid", "RFC 5545 §3.8.4.7", o.path, o.line,
						"UID %q is shared by more than one component with no RECURRENCE-ID (first at %s); UID MUST be unique within the calendar", uid, masterLoc)
				} else {
					masterLoc = o.path
				}
				continue
			}
			if prev, dup := seenRid[o.recurrence]; dup {
				res.errorf("duplicate-recurrence-id", "RFC 5545 §3.8.4.4", o.path, o.line,
					"UID %q has more than one component with RECURRENCE-ID %q (first at %s)", uid, o.recurrence, prev)
			} else {
				seenRid[o.recurrence] = o.path
			}
		}
	}
}

func checkTZIDReferences(root *Component, res *Result, opts Options, tzids map[string]bool) {
	if opts.TZID == TZIDIgnore {
		return
	}
	seen := map[string]bool{}
	var walk func(c *Component)
	walk = func(c *Component) {
		for _, p := range c.Properties {
			if tz, ok := p.Param("TZID"); ok && len(tz) > 0 {
				key := tz[0] + "\x00" + c.Path()
				if seen[key] {
					continue
				}
				seen[key] = true
				if !tzids[tz[0]] {
					msg := fmt.Sprintf("TZID=%s on %s does not resolve to any VTIMEZONE defined in this object; it relies on the consumer's own time zone database", tz[0], p.Name)
					if opts.TZID == TZIDError {
						res.errorf("tzid-unresolved", "RFC 5545 §3.6.5", c.Path(), p.Line, "%s", msg)
					} else {
						res.warnf("tzid-unresolved", "RFC 5545 §3.6.5", c.Path(), p.Line, "%s", msg)
					}
				}
			}
		}
		for _, ch := range c.Children {
			walk(ch)
		}
	}
	walk(root)
}

// --- generic property-count helpers -----------------------------------

func requireOnce(c *Component, res *Result, name, section, path string) {
	props := c.All(name)
	switch len(props) {
	case 0:
		res.errorf(strings.ToLower(name)+"-required", section, path, c.Line,
			"%s is REQUIRED and MUST NOT be missing", name)
	default:
		if len(props) > 1 {
			res.errorf(strings.ToLower(name)+"-once", section, path, props[1].Line,
				"%s MUST NOT occur more than once (found %d)", name, len(props))
		}
	}
}

func atMostOnce(c *Component, res *Result, name, section, path string) {
	props := c.All(name)
	if len(props) > 1 {
		res.errorf(strings.ToLower(name)+"-once", section, path, props[1].Line,
			"%s MUST NOT occur more than once (found %d)", name, len(props))
	}
}

func shouldNotRepeat(c *Component, res *Result, name, section, path string) {
	props := c.All(name)
	if len(props) > 1 {
		res.warnf(strings.ToLower(name)+"-repeated", section, path, props[1].Line,
			"%s SHOULD NOT occur more than once (found %d)", name, len(props))
	}
}

func forbidden(c *Component, res *Result, name, section, path, reason string) {
	for _, p := range c.All(name) {
		res.errorf(strings.ToLower(name)+"-forbidden", section, path, p.Line,
			"%s is not permitted here: %s", name, reason)
	}
}

// --- date/time value helpers --------------------------------------------

type dtValue struct {
	isDate bool
	form   DateTimeForm
	valid  bool
	raw    string
}

func parseDTProp(p Property) dtValue {
	if strings.EqualFold(p.ParamValue("VALUE"), "DATE") {
		return dtValue{isDate: true, valid: IsValidDate(p.Value), raw: p.Value}
	}
	form, ok := ParseDateTime(p.Value, p.ParamValue("TZID"))
	return dtValue{form: form, valid: ok, raw: p.Value}
}

// sortKey returns a string that sorts correctly for wall-clock ordering
// comparison. This is a textual heuristic: it does not convert between
// time zones, so DTSTART/DTEND pairs that use different TZIDs are compared
// on their literal local wall-clock digits.
func (d dtValue) sortKey() string {
	s := d.raw
	s = strings.TrimSuffix(s, "Z")
	if d.isDate {
		s += "T000000"
	}
	return s
}

func checkEndAfterStart(res *Result, path string, startProp, endProp Property, startLabel, endLabel, section string) {
	sv := parseDTProp(startProp)
	ev := parseDTProp(endProp)
	if !sv.valid || !ev.valid {
		return
	}
	if sv.isDate != ev.isDate {
		res.errorf(strings.ToLower(endLabel)+"-value-type-mismatch", section, path, endProp.Line,
			"%s value type (%s) does not match %s value type (%s)", endLabel, valueTypeName(ev.isDate), startLabel, valueTypeName(sv.isDate))
		return
	}
	switch {
	case ev.sortKey() < sv.sortKey():
		res.errorf(strings.ToLower(endLabel)+"-before-start", section, path, endProp.Line,
			"%s (%s) is before %s (%s)", endLabel, endProp.Value, startLabel, startProp.Value)
	case ev.sortKey() == sv.sortKey():
		// RFC 5545 does not explicitly forbid a zero-length interval (some
		// real-world producers emit these deliberately, e.g. as instant
		// markers), so this is a warning rather than a MUST violation.
		res.warnf(strings.ToLower(endLabel)+"-equals-start", section, path, endProp.Line,
			"%s (%s) is equal to %s, giving this a zero-length duration", endLabel, endProp.Value, startLabel)
	}
}

func valueTypeName(isDate bool) string {
	if isDate {
		return "DATE"
	}
	return "DATE-TIME"
}

// --- VEVENT ---------------------------------------------------------------

func validateEvent(c *Component, res *Result, opts Options, methodPresent bool, tzids map[string]bool) {
	path := c.Path()
	requireOnce(c, res, "UID", "RFC 5545 §3.6.1", path)
	requireOnce(c, res, "DTSTAMP", "RFC 5545 §3.6.1", path)

	dtstartProps := c.All("DTSTART")
	if len(dtstartProps) == 0 && !methodPresent {
		res.errorf("dtstart-required", "RFC 5545 §3.6.1", path, c.Line,
			"DTSTART is REQUIRED when the iCalendar object has no METHOD property")
	}
	atMostOnce(c, res, "DTSTART", "RFC 5545 §3.6.1", path)

	for _, name := range []string{"CLASS", "CREATED", "DESCRIPTION", "GEO", "LAST-MODIFIED",
		"LOCATION", "ORGANIZER", "PRIORITY", "SEQUENCE", "STATUS", "SUMMARY", "TRANSP", "URL", "RECURRENCE-ID"} {
		atMostOnce(c, res, name, "RFC 5545 §3.6.1", path)
	}
	shouldNotRepeat(c, res, "RRULE", "RFC 5545 §3.6.1", path)

	dtend := c.All("DTEND")
	duration := c.All("DURATION")
	if len(dtend) > 0 && len(duration) > 0 {
		res.errorf("dtend-duration-exclusive", "RFC 5545 §3.6.1", path, dtend[0].Line,
			"DTEND and DURATION MUST NOT both appear on the same VEVENT")
	}
	atMostOnce(c, res, "DTEND", "RFC 5545 §3.6.1", path)
	atMostOnce(c, res, "DURATION", "RFC 5545 §3.8.2.5", path)

	if len(dtstartProps) > 0 && len(dtend) > 0 {
		checkEndAfterStart(res, path, dtstartProps[0], dtend[0], "DTSTART", "DTEND", "RFC 5545 §3.8.2.2")
	}
	if len(duration) > 0 && !IsValidDuration(duration[0].Value) {
		res.errorf("duration-syntax", "RFC 5545 §3.3.6", path, duration[0].Line,
			"DURATION value %q is not a valid ISO 8601-derived duration", duration[0].Value)
	}

	if st, ok := c.First("STATUS"); ok {
		checkEnum(res, path, st, []string{"TENTATIVE", "CONFIRMED", "CANCELLED"}, "RFC 5545 §3.8.1.11")
	}

	if len(dtstartProps) > 0 {
		checkRRules(c, res, path, dtstartProps[0])
	}
	checkAlarmTriggerContext(c, res, path, len(dtstartProps) > 0, len(dtend) > 0, len(duration) > 0, "DTEND")
}

func checkEnum(res *Result, path string, p Property, allowed []string, section string) {
	up := strings.ToUpper(p.Value)
	for _, a := range allowed {
		if a == up {
			return
		}
	}
	res.warnf(strings.ToLower(p.Name)+"-enum", section, path, p.Line,
		"%s value %q is not one of %s", p.Name, p.Value, strings.Join(allowed, ", "))
}

// --- VTODO ------------------------------------------------------------

func validateTodo(c *Component, res *Result, opts Options, tzids map[string]bool) {
	path := c.Path()
	requireOnce(c, res, "UID", "RFC 5545 §3.6.2", path)
	requireOnce(c, res, "DTSTAMP", "RFC 5545 §3.6.2", path)

	for _, name := range []string{"CLASS", "COMPLETED", "CREATED", "DESCRIPTION", "DTSTART",
		"GEO", "LAST-MODIFIED", "LOCATION", "ORGANIZER", "PERCENT-COMPLETE", "PRIORITY",
		"RECURRENCE-ID", "SEQUENCE", "STATUS", "SUMMARY", "URL"} {
		atMostOnce(c, res, name, "RFC 5545 §3.6.2", path)
	}
	shouldNotRepeat(c, res, "RRULE", "RFC 5545 §3.6.2", path)

	dtstart := c.All("DTSTART")
	due := c.All("DUE")
	duration := c.All("DURATION")
	if len(due) > 0 && len(duration) > 0 {
		res.errorf("due-duration-exclusive", "RFC 5545 §3.6.2", path, due[0].Line,
			"DUE and DURATION MUST NOT both appear on the same VTODO")
	}
	atMostOnce(c, res, "DUE", "RFC 5545 §3.6.2", path)
	atMostOnce(c, res, "DURATION", "RFC 5545 §3.8.2.5", path)
	if len(duration) > 0 && len(dtstart) == 0 {
		res.errorf("duration-requires-dtstart", "RFC 5545 §3.6.2", path, duration[0].Line,
			"DURATION MUST NOT appear on a VTODO unless DTSTART is also present")
	}
	if len(duration) > 0 && !IsValidDuration(duration[0].Value) {
		res.errorf("duration-syntax", "RFC 5545 §3.3.6", path, duration[0].Line,
			"DURATION value %q is not a valid ISO 8601-derived duration", duration[0].Value)
	}
	if len(dtstart) > 0 && len(due) > 0 {
		checkEndAfterStart(res, path, dtstart[0], due[0], "DTSTART", "DUE", "RFC 5545 §3.8.2.3")
	}
	if st, ok := c.First("STATUS"); ok {
		checkEnum(res, path, st, []string{"NEEDS-ACTION", "COMPLETED", "IN-PROCESS", "CANCELLED"}, "RFC 5545 §3.8.1.11")
	}
	if len(dtstart) > 0 {
		checkRRules(c, res, path, dtstart[0])
	}
	checkAlarmTriggerContext(c, res, path, len(dtstart) > 0, len(due) > 0, len(duration) > 0, "DUE")
}

// --- VJOURNAL ----------------------------------------------------------

func validateJournal(c *Component, res *Result, opts Options, tzids map[string]bool) {
	path := c.Path()
	requireOnce(c, res, "UID", "RFC 5545 §3.6.3", path)
	requireOnce(c, res, "DTSTAMP", "RFC 5545 §3.6.3", path)
	for _, name := range []string{"CLASS", "CREATED", "DTSTART", "LAST-MODIFIED",
		"ORGANIZER", "RECURRENCE-ID", "SEQUENCE", "STATUS", "SUMMARY", "URL"} {
		atMostOnce(c, res, name, "RFC 5545 §3.6.3", path)
	}
	shouldNotRepeat(c, res, "RRULE", "RFC 5545 §3.6.3", path)
	if st, ok := c.First("STATUS"); ok {
		checkEnum(res, path, st, []string{"DRAFT", "FINAL", "CANCELLED"}, "RFC 5545 §3.8.1.11")
	}
	if dtstart, ok := c.First("DTSTART"); ok {
		checkRRules(c, res, path, dtstart)
	}
}

// --- VFREEBUSY ---------------------------------------------------------

func validateFreeBusy(c *Component, res *Result, opts Options, tzids map[string]bool) {
	path := c.Path()
	requireOnce(c, res, "UID", "RFC 5545 §3.6.4", path)
	requireOnce(c, res, "DTSTAMP", "RFC 5545 §3.6.4", path)
	for _, name := range []string{"CONTACT", "DTSTART", "DTEND", "ORGANIZER", "URL"} {
		atMostOnce(c, res, name, "RFC 5545 §3.6.4", path)
	}
	forbidden(c, res, "RRULE", "RFC 5545 §3.6.4", path, "recurrence properties are not permitted within VFREEBUSY")
	forbidden(c, res, "RDATE", "RFC 5545 §3.6.4", path, "recurrence properties are not permitted within VFREEBUSY")
	forbidden(c, res, "EXDATE", "RFC 5545 §3.6.4", path, "recurrence properties are not permitted within VFREEBUSY")

	dtstart := c.All("DTSTART")
	dtend := c.All("DTEND")
	if len(dtstart) > 0 && len(dtend) > 0 {
		checkEndAfterStart(res, path, dtstart[0], dtend[0], "DTSTART", "DTEND", "RFC 5545 §3.6.4")
	}
	for _, fb := range c.All("FREEBUSY") {
		for _, period := range SplitList(fb.Value) {
			if !IsValidPeriod(period) {
				res.errorf("freebusy-period-syntax", "RFC 5545 §3.3.9", path, fb.Line,
					"FREEBUSY period %q is not a valid PERIOD value", period)
			}
		}
	}
}

// --- VTIMEZONE ----------------------------------------------------------

var utcOffsetRe = regexp.MustCompile(`^([+-])(\d{2})(\d{2})(\d{2})?$`)

func validateUTCOffset(res *Result, path string, p Property, section string) {
	m := utcOffsetRe.FindStringSubmatch(p.Value)
	if m == nil {
		res.errorf(strings.ToLower(p.Name)+"-syntax", section, path, p.Line,
			"%s value %q is not a valid UTC-OFFSET (§3.3.14)", p.Name, p.Value)
		return
	}
	hh, _ := strconv.Atoi(m[2])
	mm, _ := strconv.Atoi(m[3])
	ss := 0
	if m[4] != "" {
		ss, _ = strconv.Atoi(m[4])
	}
	if hh > 23 || mm > 59 || ss == 60 {
		res.errorf(strings.ToLower(p.Name)+"-range", section, path, p.Line,
			"%s value %q is out of range", p.Name, p.Value)
	}
	if m[1] == "-" && hh == 0 && mm == 0 && ss == 0 {
		res.errorf(strings.ToLower(p.Name)+"-negative-zero", "RFC 5545 §3.3.14", path, p.Line,
			"%s value %q (negative zero offset) is not allowed", p.Name, p.Value)
	}
}

func validateTimezone(c *Component, res *Result) {
	path := c.Path()
	requireOnce(c, res, "TZID", "RFC 5545 §3.6.5", path)
	atMostOnce(c, res, "LAST-MODIFIED", "RFC 5545 §3.6.5", path)
	atMostOnce(c, res, "TZURL", "RFC 5545 §3.6.5", path)

	obs := append(append([]*Component{}, c.ChildrenNamed("STANDARD")...), c.ChildrenNamed("DAYLIGHT")...)
	if len(obs) == 0 {
		res.errorf("vtimezone-no-observance", "RFC 5545 §3.6.5", path, c.Line,
			"VTIMEZONE MUST include at least one STANDARD or DAYLIGHT sub-component")
	}
	for _, o := range obs {
		validateTZObservance(o, res)
	}
}

func validateTZObservance(c *Component, res *Result) {
	path := c.Path()
	requireOnce(c, res, "DTSTART", "RFC 5545 §3.6.5", path)
	requireOnce(c, res, "TZOFFSETFROM", "RFC 5545 §3.8.3.3", path)
	requireOnce(c, res, "TZOFFSETTO", "RFC 5545 §3.8.3.4", path)
	shouldNotRepeat(c, res, "RRULE", "RFC 5545 §3.6.5", path)

	if p, ok := c.First("TZOFFSETFROM"); ok {
		validateUTCOffset(res, path, p, "RFC 5545 §3.8.3.3")
	}
	if p, ok := c.First("TZOFFSETTO"); ok {
		validateUTCOffset(res, path, p, "RFC 5545 §3.8.3.4")
	}

	if rr, ok := c.First("RRULE"); ok {
		r, problems := ParseRecur(rr.Value)
		for _, msg := range problems {
			res.errorf("recur-syntax", "RFC 5545 §3.3.10", path, rr.Line, "RRULE: %s", msg)
		}
		if r.Until != "" && r.UntilForm != FormUTC {
			res.errorf("tz-rrule-until-not-utc", "RFC 5545 §3.6.5", path, rr.Line,
				"RRULE UNTIL in a STANDARD/DAYLIGHT sub-component MUST always be specified in UTC time")
		}
	}
}

// --- VALARM -------------------------------------------------------------

func validateAlarm(c *Component, res *Result) {
	path := c.Path()
	requireOnce(c, res, "ACTION", "RFC 5545 §3.6.6", path)
	requireOnce(c, res, "TRIGGER", "RFC 5545 §3.6.6", path)

	duration := c.All("DURATION")
	repeat := c.All("REPEAT")
	atMostOnce(c, res, "DURATION", "RFC 5545 §3.6.6", path)
	atMostOnce(c, res, "REPEAT", "RFC 5545 §3.8.6.2", path)
	if (len(duration) > 0) != (len(repeat) > 0) {
		line := c.Line
		if len(duration) > 0 {
			line = duration[0].Line
		} else if len(repeat) > 0 {
			line = repeat[0].Line
		}
		res.errorf("alarm-repeat-duration", "RFC 5545 §3.6.6", path, line,
			"DURATION and REPEAT MUST both be present, or both absent, on a VALARM")
	}

	action, hasAction := c.First("ACTION")
	if trig, ok := c.First("TRIGGER"); ok {
		validateTrigger(c, res, path, trig)
	}
	if !hasAction {
		return
	}
	switch strings.ToUpper(action.Value) {
	case "AUDIO":
		atMostOnce(c, res, "ATTACH", "RFC 5545 §3.6.6", path)
	case "DISPLAY":
		requireOnce(c, res, "DESCRIPTION", "RFC 5545 §3.6.6", path)
	case "EMAIL":
		requireOnce(c, res, "DESCRIPTION", "RFC 5545 §3.6.6", path)
		requireOnce(c, res, "SUMMARY", "RFC 5545 §3.6.6", path)
		if len(c.All("ATTENDEE")) == 0 {
			res.errorf("valarm-email-attendee-required", "RFC 5545 §3.6.6", path, c.Line,
				"an EMAIL VALARM MUST include one or more ATTENDEE properties")
		}
	default:
		// x-name/iana-token extension action: only the common ACTION and
		// TRIGGER requirements above apply.
	}
}

func validateTrigger(c *Component, res *Result, path string, trig Property) {
	related := strings.ToUpper(trig.ParamValue("RELATED"))
	if related != "" && related != "START" && related != "END" {
		res.warnf("trigger-related", "RFC 5545 §3.8.6.3", path, trig.Line,
			"TRIGGER RELATED parameter %q is not START or END", related)
	}
	if strings.EqualFold(trig.ParamValue("VALUE"), "DATE-TIME") {
		form, ok := ParseDateTime(trig.Value, "")
		if !ok {
			res.errorf("trigger-syntax", "RFC 5545 §3.8.6.3", path, trig.Line,
				"TRIGGER value %q is not a valid DATE-TIME", trig.Value)
			return
		}
		if form != FormUTC {
			res.errorf("trigger-not-utc", "RFC 5545 §3.8.6.3", path, trig.Line,
				"an absolute TRIGGER (VALUE=DATE-TIME) MUST be specified in UTC time")
		}
		if related != "" {
			res.warnf("trigger-related-with-absolute", "RFC 5545 §3.8.6.3", path, trig.Line,
				"RELATED has no effect on an absolute (VALUE=DATE-TIME) TRIGGER")
		}
		return
	}
	if !IsValidDuration(trig.Value) {
		res.errorf("trigger-syntax", "RFC 5545 §3.8.6.3", path, trig.Line,
			"TRIGGER value %q is not a valid DURATION", trig.Value)
	}
}

// checkAlarmTriggerContext verifies that, for any VALARM child triggering
// relative to END, the properties that anchor "the end" actually exist on
// the owning VEVENT/VTODO (RFC 5545 §3.6.6): DTEND (or DTSTART+DURATION)
// for a VEVENT, DUE (or DTSTART+DURATION) for a VTODO. Triggering on
// START requires DTSTART.
func checkAlarmTriggerContext(c *Component, res *Result, path string, hasStart, hasEndProp, hasDuration bool, endPropName string) {
	for _, a := range c.ChildrenNamed("VALARM") {
		trig, ok := a.First("TRIGGER")
		if !ok {
			continue
		}
		related := strings.ToUpper(trig.ParamValue("RELATED"))
		if related == "" {
			related = "START"
		}
		if strings.EqualFold(trig.ParamValue("VALUE"), "DATE-TIME") {
			continue // absolute trigger, not relative to START/END
		}
		if related == "START" && !hasStart {
			res.errorf("alarm-trigger-start-context", "RFC 5545 §3.6.6", a.Path(), trig.Line,
				"TRIGGER is RELATED=START but the containing %s has no DTSTART", c.Name)
		}
		if related == "END" && !hasEndProp && !(hasStart && hasDuration) {
			res.errorf("alarm-trigger-end-context", "RFC 5545 §3.6.6", a.Path(), trig.Line,
				"TRIGGER is RELATED=END but the containing %s has no %s and no DTSTART+DURATION pair", c.Name, endPropName)
		}
	}
}

// --- RRULE / DTSTART cross-checks ---------------------------------------

func checkRRules(c *Component, res *Result, path string, dtstart Property) {
	sv := parseDTProp(dtstart)
	for _, rr := range c.All("RRULE") {
		r, problems := ParseRecur(rr.Value)
		for _, msg := range problems {
			res.errorf("recur-syntax", "RFC 5545 §3.3.10", path, rr.Line, "RRULE: %s", msg)
		}
		if sv.valid {
			for _, name := range []string{"BYSECOND", "BYMINUTE", "BYHOUR"} {
				if _, present := r.Parts[name]; present && sv.isDate {
					res.errorf("rrule-byxxx-with-date", "RFC 5545 §3.3.10", path, rr.Line,
						"%s MUST NOT be specified when DTSTART has a DATE value type", name)
				}
			}
		}
		if r.Until == "" || !sv.valid {
			continue
		}
		untilIsDate := r.UntilIsDate
		if untilIsDate != sv.isDate {
			res.errorf("rrule-until-type-mismatch", "RFC 5545 §3.3.10", path, rr.Line,
				"RRULE UNTIL value type (%s) MUST match DTSTART's value type (%s)", valueTypeName(untilIsDate), valueTypeName(sv.isDate))
			continue
		}
		if !untilIsDate {
			switch sv.form {
			case FormFloating:
				if r.UntilForm != FormFloating {
					res.errorf("rrule-until-form-mismatch", "RFC 5545 §3.3.10", path, rr.Line,
						"DTSTART is a floating local time, so RRULE UNTIL MUST also be a floating local time, not UTC")
				}
			case FormUTC, FormLocalTZ:
				if r.UntilForm != FormUTC {
					res.errorf("rrule-until-not-utc", "RFC 5545 §3.3.10", path, rr.Line,
						"DTSTART has a UTC or time-zone reference, so RRULE UNTIL MUST be specified in UTC time")
				}
			}
		}
	}
}
