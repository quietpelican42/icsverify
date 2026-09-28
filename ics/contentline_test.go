package ics

import "testing"

func TestUnfoldLineFolding(t *testing.T) {
	// RFC 5545 §3.1 example: a folded DESCRIPTION line.
	data := []byte("DESCRIPTION:This is a lo\r\n ng description\r\n  that exists on a long line.\r\n")
	lines, diags := tokenize(data)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 unfolded line, got %d", len(lines))
	}
	want := "This is a long description that exists on a long line."
	if lines[0].Value != want {
		t.Errorf("unfolded value = %q, want %q", lines[0].Value, want)
	}
}

func TestParseContentLineParameters(t *testing.T) {
	cl, err := parseContentLine(`ATTENDEE;RSVP=TRUE;ROLE=REQ-PARTICIPANT;CUTYPE=GROUP:mailto:employee-A@example.com`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cl.Name != "ATTENDEE" {
		t.Errorf("Name = %q", cl.Name)
	}
	if v := cl.ParamValue("RSVP"); v != "TRUE" {
		t.Errorf("RSVP = %q", v)
	}
	if v := cl.ParamValue("CUTYPE"); v != "GROUP" {
		t.Errorf("CUTYPE = %q", v)
	}
	if cl.Value != "mailto:employee-A@example.com" {
		t.Errorf("Value = %q", cl.Value)
	}
}

func TestParseContentLineQuotedParamValue(t *testing.T) {
	// A quoted parameter value may contain ':' and ';' and ',', which are
	// otherwise delimiters (RFC 5545 §3.2, QSAFE-CHAR).
	cl, err := parseContentLine(`ATTACH;FMTTYPE="application/msword";X-NOTE="a: b; c,d":http://example.com/x`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if v := cl.ParamValue("X-NOTE"); v != "a: b; c,d" {
		t.Errorf("X-NOTE = %q", v)
	}
}

func TestParseContentLineMultiValuedParam(t *testing.T) {
	cl, err := parseContentLine(`ATTENDEE;MEMBER="mailto:a@x.com","mailto:b@x.com":mailto:c@x.com`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	vals, ok := cl.Param("MEMBER")
	if !ok || len(vals) != 2 {
		t.Fatalf("MEMBER = %v", vals)
	}
}

func TestParseContentLineMissingColon(t *testing.T) {
	if _, err := parseContentLine(`SUMMARY;X=1`); err == nil {
		t.Error("expected error for missing ':'")
	}
}

func TestUnescapeText(t *testing.T) {
	in := `1. Staff meeting: Participants include Joe\, Lisa\, and Bob.\nNext meeting on Tuesday.\n2. Second item.`
	want := "1. Staff meeting: Participants include Joe, Lisa, and Bob.\nNext meeting on Tuesday.\n2. Second item."
	if got := UnescapeText(in); got != want {
		t.Errorf("UnescapeText = %q, want %q", got, want)
	}
}

func TestSplitList(t *testing.T) {
	got := SplitList(`BUSINESS,HUMAN RESOURCES`)
	want := []string{"BUSINESS", "HUMAN RESOURCES"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("SplitList = %v, want %v", got, want)
	}
}

func TestSplitListRespectsEscapedComma(t *testing.T) {
	got := SplitList(`Joe\, Lisa\, and Bob,Second value`)
	if len(got) != 2 {
		t.Fatalf("SplitList = %v, want 2 elements", got)
	}
	if got[0] != `Joe\, Lisa\, and Bob` {
		t.Errorf("first element = %q", got[0])
	}
}

func TestBuildTreeMismatchedEnd(t *testing.T) {
	res := &Result{}
	lines, _ := tokenize([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nEND:VTODO\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	_, ok := buildTree(lines, res)
	if !ok {
		t.Fatal("expected a usable tree despite the mismatched END")
	}
	if !hasRule(res, "mismatched-end") {
		t.Error("expected mismatched-end finding")
	}
}
