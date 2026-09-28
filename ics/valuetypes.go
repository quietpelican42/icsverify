package ics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DateTimeForm classifies a DATE-TIME value's time reference (RFC 5545
// §3.3.5): a "form #1" floating local time, a "form #2" UTC time (trailing
// Z), or a "form #3" local time with a TZID parameter.
type DateTimeForm int

const (
	FormFloating DateTimeForm = iota
	FormUTC
	FormLocalTZ
)

var dateRe = regexp.MustCompile(`^\d{8}$`)
var dateTimeRe = regexp.MustCompile(`^\d{8}T\d{6}(Z)?$`)

// IsValidDate reports whether s is a syntactically valid DATE value (RFC
// 5545 §3.3.4: date-value = date-fullyear date-month date-mday) with a
// plausible calendar date (month 01-12, day within the month, leap years
// honored).
func IsValidDate(s string) bool {
	if !dateRe.MatchString(s) {
		return false
	}
	y, _ := strconv.Atoi(s[0:4])
	m, _ := strconv.Atoi(s[4:6])
	d, _ := strconv.Atoi(s[6:8])
	return validYMD(y, m, d)
}

// ParseDateTime reports whether s is a syntactically valid DATE-TIME value
// (RFC 5545 §3.3.5) and, if so, its form. tzidParam is the TZID parameter
// value on the property, if any.
func ParseDateTime(s string, tzidParam string) (form DateTimeForm, ok bool) {
	if !dateTimeRe.MatchString(s) {
		return 0, false
	}
	y, _ := strconv.Atoi(s[0:4])
	m, _ := strconv.Atoi(s[4:6])
	d, _ := strconv.Atoi(s[6:8])
	if !validYMD(y, m, d) {
		return 0, false
	}
	hh, _ := strconv.Atoi(s[9:11])
	mm, _ := strconv.Atoi(s[11:13])
	ss, _ := strconv.Atoi(s[13:15])
	if hh > 23 || mm > 59 || ss > 60 { // 60 for leap second, §3.3.12
		return 0, false
	}
	utc := strings.HasSuffix(s, "Z")
	switch {
	case utc:
		return FormUTC, true
	case tzidParam != "":
		return FormLocalTZ, true
	default:
		return FormFloating, true
	}
}

func validYMD(y, m, d int) bool {
	if y < 0 || y > 9999 || m < 1 || m > 12 || d < 1 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	max := days[m-1]
	if m == 2 && isLeap(y) {
		max = 29
	}
	return d <= max
}

func isLeap(y int) bool {
	return (y%4 == 0 && y%100 != 0) || y%400 == 0
}

// durationRe implements RFC 5545 §3.3.6:
//
//	dur-value  = (["+"] / "-") "P" (dur-date / dur-time / dur-week)
//	dur-date   = dur-day [dur-time]
//	dur-time   = "T" (dur-hour / dur-minute / dur-second)
//	dur-week   = 1*DIGIT "W"
//	dur-hour   = 1*DIGIT "H" [dur-minute]
//	dur-minute = 1*DIGIT "M" [dur-second]
//	dur-second = 1*DIGIT "S"
//	dur-day    = 1*DIGIT "D"
//
// Group 2 is the day count (if any), group 3 is the whole "T..." block (if
// any), groups 4/5/6 are the hour/minute/second counts.
var durationRe = regexp.MustCompile(`^[+-]?P(\d+W|(\d+D)?(T(\d+H)?(\d+M)?(\d+S)?)?)$`)

// IsValidDuration reports whether s is a syntactically valid DURATION value
// (RFC 5545 §3.3.6). It rejects the empty "P" and "PT" forms (at least one
// component is required) and the case where a "T" is present but nothing
// follows it.
func IsValidDuration(s string) bool {
	if s == "" {
		return false
	}
	m := durationRe.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	if strings.Contains(s, "W") {
		return true // dur-week: \d+W already required at least one digit
	}
	hasDate := m[2] != ""
	tPresent := m[3] != ""
	hasTime := m[4] != "" || m[5] != "" || m[6] != ""
	if !hasDate && !tPresent {
		return false // bare "P"
	}
	if tPresent && !hasTime {
		return false // "T" present but no hour/minute/second follows
	}
	return true
}

// IsValidPeriod reports whether s is a syntactically valid PERIOD value
// (RFC 5545 §3.3.9): either two UTC/local DATE-TIMEs separated by "/"
// (period-explicit), or a DATE-TIME followed by a DURATION
// (period-start). Per §3.3.9, both explicit endpoints of period-explicit
// use the same form as period start/end DATE-TIME rules generally require,
// but this function only checks syntax, not that the two ends share a form.
func IsValidPeriod(s string) bool {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return false
	}
	if _, ok := ParseDateTime(parts[0], ""); !ok {
		return false
	}
	if _, ok := ParseDateTime(parts[1], ""); ok {
		return true
	}
	return IsValidDuration(parts[1])
}

// Recur is a parsed RECUR value (RFC 5545 §3.3.10).
type Recur struct {
	Freq        string
	Until       string // raw value, if present
	UntilForm   DateTimeForm
	UntilIsDate bool
	HasCount    bool
	Parts       map[string]string // all rule parts as raw strings, upper-cased keys
	Unknown     []string          // unrecognized rule part names
}

var recurPartNames = map[string]bool{
	"FREQ": true, "UNTIL": true, "COUNT": true, "INTERVAL": true,
	"BYSECOND": true, "BYMINUTE": true, "BYHOUR": true, "BYDAY": true,
	"BYMONTHDAY": true, "BYYEARDAY": true, "BYWEEKNO": true, "BYMONTH": true,
	"BYSETPOS": true, "WKST": true,
}

var validFreq = map[string]bool{
	"SECONDLY": true, "MINUTELY": true, "HOURLY": true, "DAILY": true,
	"WEEKLY": true, "MONTHLY": true, "YEARLY": true,
}

// ParseRecur parses a RECUR value's grammar (rule-part names, at-most-once
// constraints, FREQ/UNTIL/COUNT presence) per RFC 5545 §3.3.10. It does not
// validate that BYxxx numeric ranges are honored beyond basic sanity; see
// ValidateRecur in validate.go for grammar+semantic checks that also need
// DTSTART's value type.
func ParseRecur(s string) (Recur, []string) {
	var problems []string
	r := Recur{Parts: map[string]string{}}
	seen := map[string]int{}

	for _, part := range strings.Split(s, ";") {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			problems = append(problems, fmt.Sprintf("malformed rule part %q (expected NAME=VALUE)", part))
			continue
		}
		name := strings.ToUpper(kv[0])
		val := kv[1]
		seen[name]++
		if !recurPartNames[name] {
			r.Unknown = append(r.Unknown, kv[0])
			continue
		}
		r.Parts[name] = val
		switch name {
		case "FREQ":
			r.Freq = strings.ToUpper(val)
		case "UNTIL":
			r.Until = val
		case "COUNT":
			r.HasCount = true
		}
	}

	if r.Freq == "" {
		problems = append(problems, "missing required FREQ rule part")
	} else if !validFreq[r.Freq] {
		problems = append(problems, fmt.Sprintf("FREQ=%s is not one of SECONDLY/MINUTELY/HOURLY/DAILY/WEEKLY/MONTHLY/YEARLY", r.Freq))
	}
	for name, n := range seen {
		if n > 1 {
			problems = append(problems, fmt.Sprintf("rule part %s specified more than once", name))
		}
	}
	if r.Until != "" && r.HasCount {
		problems = append(problems, "UNTIL and COUNT MUST NOT both be present")
	}
	if r.Until != "" {
		if IsValidDate(r.Until) {
			r.UntilIsDate = true
		} else if form, ok := ParseDateTime(r.Until, ""); ok {
			r.UntilForm = form
		} else {
			problems = append(problems, fmt.Sprintf("UNTIL value %q is neither a valid DATE nor DATE-TIME", r.Until))
		}
	}
	return r, problems
}
