// Package ics implements a strict RFC 5545 (iCalendar) content-line parser
// and a validator that checks parsed calendars against the property tables
// and value-type grammars defined by RFC 5545, RFC 7986 and (for the METHOD
// property) RFC 5546.
package ics

import (
	"bytes"
	"fmt"
)

// Param is a single property parameter: NAME=VALUE1,VALUE2,...
// (RFC 5545 §3.2, ABNF "param" in §3.1).
type Param struct {
	Name   string
	Values []string
}

// ContentLine is one unfolded, parsed content line (RFC 5545 §3.1):
//
//	contentline = name *(";" param) ":" value CRLF
//
// Line is the 1-based physical line number on which the content line
// started in the original (folded) input, used for diagnostics.
type ContentLine struct {
	Name   string
	Params []Param
	Value  string
	Line   int
}

// Param looks up the first parameter with the given name (case-insensitive)
// and returns its values. ok is false if the parameter is absent.
func (c ContentLine) Param(name string) (values []string, ok bool) {
	for _, p := range c.Params {
		if equalFold(p.Name, name) {
			return p.Values, true
		}
	}
	return nil, false
}

// ParamValue returns the first value of the first parameter with the given
// name (case-insensitive), or "" if absent.
func (c ContentLine) ParamValue(name string) string {
	if vs, ok := c.Param(name); ok && len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// LineDiag is a diagnostic produced while unfolding/tokenizing, independent
// of any semantic validation (e.g. an over-long physical line, RFC 5545
// §3.1, or a bare-LF line terminator).
type LineDiag struct {
	Line     int
	Severity Severity
	Rule     string
	Section  string
	Message  string
}

// unfoldResult holds the unfolded logical lines plus positional metadata
// and any diagnostics raised purely by the folding/line-ending mechanics.
type unfoldResult struct {
	// logical[i] is the unfolded text of logical line i (0-based).
	logical []string
	// startLine[i] is the 1-based physical line number where logical
	// line i began (before any folding continuation).
	startLine []int
	diags     []LineDiag
}

// unfold splits raw content into physical lines on CRLF or bare LF, tracks
// >75-octet physical lines (RFC 5545 §3.1, a SHOULD-fold rule -> warning),
// tracks non-CRLF terminators, and rejoins folded continuation lines
// (a line beginning with SPACE or HTAB) onto the logical line they
// continue.
func unfold(data []byte) unfoldResult {
	var res unfoldResult

	// Split into raw physical lines, remembering the terminator used.
	type physLine struct {
		text      []byte
		line      int
		bareLF    bool
		noNewline bool
	}
	var phys []physLine

	lineNo := 1
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			end := i
			bare := true
			if end > start && data[end-1] == '\r' {
				end--
				bare = false
			}
			phys = append(phys, physLine{text: data[start:end], line: lineNo, bareLF: bare})
			lineNo++
			start = i + 1
		}
	}
	if start < len(data) {
		phys = append(phys, physLine{text: data[start:], line: lineNo, noNewline: true})
	}

	for _, pl := range phys {
		if len(pl.text) == 0 {
			continue // blank line between/around content; RFC permits ignoring
		}
		if pl.bareLF {
			res.diags = append(res.diags, LineDiag{
				Line: pl.line, Severity: SeverityWarning,
				Rule: "line-ending", Section: "RFC 5545 §3.1",
				Message: "content line terminated by bare LF, not CRLF",
			})
		}
		if pl.noNewline {
			res.diags = append(res.diags, LineDiag{
				Line: pl.line, Severity: SeverityWarning,
				Rule: "line-ending", Section: "RFC 5545 §3.1",
				Message: "file does not end with a line break",
			})
		}
		if n := len(pl.text); n > 75 {
			res.diags = append(res.diags, LineDiag{
				Line: pl.line, Severity: SeverityWarning,
				Rule: "line-too-long", Section: "RFC 5545 §3.1",
				Message: fmt.Sprintf("content line is %d octets, exceeds the 75-octet SHOULD-fold limit", n),
			})
		}

		isCont := pl.text[0] == ' ' || pl.text[0] == '\t'
		if isCont && len(res.logical) > 0 {
			res.logical[len(res.logical)-1] += string(pl.text[1:])
			continue
		}
		res.logical = append(res.logical, string(pl.text))
		res.startLine = append(res.startLine, pl.line)
	}
	return res
}

// tokenize parses unfolded logical lines into ContentLines. Malformed lines
// produce a diagnostic and are skipped (best-effort recovery so the rest of
// the file can still be checked).
func tokenize(data []byte) ([]ContentLine, []LineDiag) {
	u := unfold(data)
	diags := u.diags
	var lines []ContentLine

	for i, text := range u.logical {
		cl, err := parseContentLine(text)
		if err != nil {
			diags = append(diags, LineDiag{
				Line: u.startLine[i], Severity: SeverityError,
				Rule: "malformed-content-line", Section: "RFC 5545 §3.1",
				Message: err.Error(),
			})
			continue
		}
		cl.Line = u.startLine[i]
		lines = append(lines, cl)
	}
	return lines, diags
}

// parseContentLine parses a single unfolded logical line into name,
// parameters and raw value, per the ABNF in RFC 5545 §3.1:
//
//	contentline = name *(";" param) ":" value CRLF
//	param       = param-name "=" param-value *("," param-value)
//	param-value = paramtext / quoted-string
//	quoted-string = DQUOTE *QSAFE-CHAR DQUOTE
func parseContentLine(s string) (ContentLine, error) {
	b := []byte(s)
	i := 0
	n := len(b)

	nameStart := i
	for i < n && isNameChar(b[i]) {
		i++
	}
	if i == nameStart {
		return ContentLine{}, fmt.Errorf("missing property name")
	}
	name := s[nameStart:i]

	var params []Param
	for i < n && b[i] == ';' {
		i++ // consume ';'
		pnStart := i
		for i < n && isNameChar(b[i]) {
			i++
		}
		if i == pnStart {
			return ContentLine{}, fmt.Errorf("malformed parameter name near byte %d", i)
		}
		pname := s[pnStart:i]
		if i >= n || b[i] != '=' {
			return ContentLine{}, fmt.Errorf("expected '=' after parameter name %q", pname)
		}
		i++ // consume '='

		var values []string
		for {
			var val string
			var err error
			val, i, err = parseParamValue(s, i)
			if err != nil {
				return ContentLine{}, err
			}
			values = append(values, val)
			if i < n && b[i] == ',' {
				i++
				continue
			}
			break
		}
		params = append(params, Param{Name: pname, Values: values})
	}

	if i >= n || b[i] != ':' {
		return ContentLine{}, fmt.Errorf("expected ':' before value in property %q", name)
	}
	i++ // consume ':'
	value := s[i:]

	return ContentLine{Name: name, Params: params, Value: value}, nil
}

func parseParamValue(s string, i int) (val string, next int, err error) {
	n := len(s)
	if i < n && s[i] == '"' {
		i++
		start := i
		for i < n && s[i] != '"' {
			if s[i] < 0x20 && s[i] != '\t' {
				return "", 0, fmt.Errorf("control character in quoted parameter value")
			}
			i++
		}
		if i >= n {
			return "", 0, fmt.Errorf("unterminated quoted parameter value")
		}
		val = s[start:i]
		i++ // consume closing quote
		return val, i, nil
	}
	start := i
	for i < n && isSafeChar(s[i]) {
		i++
	}
	return s[start:i], i, nil
}

func isNameChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
}

// isSafeChar reports whether c is a SAFE-CHAR (RFC 5545 §3.1): any
// character except CONTROL, DQUOTE, ';', ':', ','.
func isSafeChar(c byte) bool {
	if c < 0x20 && c != '\t' {
		return false
	}
	switch c {
	case '"', ';', ':', ',', 0x7f:
		return false
	}
	return true
}

func equalFold(a, b string) bool {
	return bytes.EqualFold([]byte(a), []byte(b))
}

// UnescapeText decodes the backslash escapes defined for the TEXT value
// type (RFC 5545 §3.3.11): \\ -> \, \; -> ;, \, -> ,, \N or \n -> newline.
func UnescapeText(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\':
				out = append(out, '\\')
				i++
				continue
			case ';':
				out = append(out, ';')
				i++
				continue
			case ',':
				out = append(out, ',')
				i++
				continue
			case 'n', 'N':
				out = append(out, '\n')
				i++
				continue
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

// SplitList splits a COMMA-separated list value (RFC 5545 §3.1.1),
// respecting backslash-escaped commas.
func SplitList(s string) []string {
	var out []string
	var cur []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			cur = append(cur, s[i], s[i+1])
			i++
			continue
		}
		if s[i] == ',' {
			out = append(out, string(cur))
			cur = nil
			continue
		}
		cur = append(cur, s[i])
	}
	out = append(out, string(cur))
	return out
}
