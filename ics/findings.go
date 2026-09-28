package ics

import "fmt"

// Severity classifies a Finding.
type Severity int

const (
	// SeverityWarning marks a SHOULD-level deviation (RFC language such as
	// "SHOULD" or "SHOULD NOT") or a configurable heuristic.
	SeverityWarning Severity = iota
	// SeverityError marks a MUST-level violation of RFC 5545 (or RFC 5546
	// when METHOD is present): the object is structurally non-conformant.
	SeverityError
)

// String returns "warning" or "error".
func (s Severity) String() string {
	if s == SeverityError {
		return "error"
	}
	return "warning"
}

// Finding is a single validation result, always tied to the RFC section
// that motivates it.
type Finding struct {
	// Rule is a short, stable, machine-matchable identifier, e.g.
	// "vevent-uid-required".
	Rule string `json:"rule"`
	// Section cites the normative source, e.g. "RFC 5545 §3.6.1".
	Section  string   `json:"section"`
	Severity Severity `json:"-"`
	// SeverityText is Severity rendered as "error"/"warning" for JSON.
	SeverityText string `json:"severity"`
	Message      string `json:"message"`
	// Line is the 1-based physical line number in the source file the
	// finding applies to, or 0 if not tied to a specific line.
	Line int `json:"line,omitempty"`
	// Path identifies the component the finding applies to, e.g.
	// "VCALENDAR/VEVENT[0]".
	Path string `json:"path,omitempty"`
}

func (f Finding) String() string {
	loc := f.Path
	if f.Line > 0 {
		loc = fmt.Sprintf("line %d", f.Line)
		if f.Path != "" {
			loc = fmt.Sprintf("line %d (%s)", f.Line, f.Path)
		}
	}
	if loc == "" {
		return fmt.Sprintf("%s: %s [%s, %s]", f.Severity, f.Message, f.Rule, f.Section)
	}
	return fmt.Sprintf("%s: %s: %s [%s, %s]", f.Severity, loc, f.Message, f.Rule, f.Section)
}

// Result is the outcome of validating one iCalendar object.
type Result struct {
	Findings []Finding `json:"findings"`
}

// HasErrors reports whether any finding has SeverityError.
func (r *Result) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r *Result) add(sev Severity, rule, section, path string, line int, format string, args ...any) {
	r.Findings = append(r.Findings, Finding{
		Rule: rule, Section: section, Severity: sev, SeverityText: sev.String(),
		Message: fmt.Sprintf(format, args...), Line: line, Path: path,
	})
}

func (r *Result) errorf(rule, section, path string, line int, format string, args ...any) {
	r.add(SeverityError, rule, section, path, line, format, args...)
}

func (r *Result) warnf(rule, section, path string, line int, format string, args ...any) {
	r.add(SeverityWarning, rule, section, path, line, format, args...)
}
