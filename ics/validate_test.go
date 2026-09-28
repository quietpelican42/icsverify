package ics

import (
	"strings"
	"testing"
)

// crlf converts a Go raw string written with plain "\n" into RFC
// 5545-compliant CRLF-terminated content, so test fixtures stay readable.
func crlf(s string) []byte {
	s = strings.TrimPrefix(s, "\n")
	return []byte(strings.ReplaceAll(s, "\n", "\r\n"))
}

func hasRule(res *Result, rule string) bool {
	for _, f := range res.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func findingsOf(res *Result, rule string) []Finding {
	var out []Finding
	for _, f := range res.Findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

const minimalHeader = "BEGIN:VCALENDAR\nPRODID:-//test//EN\nVERSION:2.0\n"

func wrapEvent(body string) []byte {
	return crlf(minimalHeader + "BEGIN:VEVENT\n" + body + "END:VEVENT\nEND:VCALENDAR\n")
}

func mustValidate(t *testing.T, data []byte) *Result {
	t.Helper()
	res, err := Validate(data, Options{})
	if err != nil {
		t.Fatalf("Validate returned error: %v\nfindings so far: %v", err, res.Findings)
	}
	return res
}

// --- VCALENDAR-level ------------------------------------------------------

func TestVCalendarRequiredProperties(t *testing.T) {
	t.Run("missing PRODID and VERSION is an error", func(t *testing.T) {
		res := mustValidate(t, crlf("BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nEND:VEVENT\nEND:VCALENDAR\n"))
		if !hasRule(res, "prodid-required") {
			t.Error("expected prodid-required")
		}
		if !hasRule(res, "version-required") {
			t.Error("expected version-required")
		}
	})

	t.Run("duplicate PRODID is an error", func(t *testing.T) {
		res := mustValidate(t, crlf("BEGIN:VCALENDAR\nPRODID:-//a//EN\nPRODID:-//b//EN\nVERSION:2.0\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nEND:VEVENT\nEND:VCALENDAR\n"))
		if !hasRule(res, "prodid-once") {
			t.Error("expected prodid-once")
		}
	})

	t.Run("clean minimal calendar has no calendar-level findings", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@example.com\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\n"))
		for _, r := range []string{"prodid-required", "version-required", "prodid-once", "version-once"} {
			if hasRule(res, r) {
				t.Errorf("unexpected %s on clean calendar", r)
			}
		}
	})

	t.Run("empty VCALENDAR has no components", func(t *testing.T) {
		res := mustValidate(t, crlf("BEGIN:VCALENDAR\nPRODID:-//a//EN\nVERSION:2.0\nEND:VCALENDAR\n"))
		if !hasRule(res, "vcalendar-empty") {
			t.Error("expected vcalendar-empty")
		}
	})

	t.Run("unknown METHOD value warns", func(t *testing.T) {
		res := mustValidate(t, crlf("BEGIN:VCALENDAR\nPRODID:-//a//EN\nVERSION:2.0\nMETHOD:BOGUS\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nEND:VEVENT\nEND:VCALENDAR\n"))
		if !hasRule(res, "method-unknown") {
			t.Error("expected method-unknown")
		}
	})

	t.Run("known METHOD value does not warn, and DTSTART becomes optional", func(t *testing.T) {
		res := mustValidate(t, crlf("BEGIN:VCALENDAR\nPRODID:-//a//EN\nVERSION:2.0\nMETHOD:PUBLISH\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nEND:VEVENT\nEND:VCALENDAR\n"))
		if hasRule(res, "method-unknown") {
			t.Error("did not expect method-unknown for PUBLISH")
		}
		if hasRule(res, "dtstart-required") {
			t.Error("DTSTART should be optional when METHOD is present")
		}
	})
}

// --- VEVENT ----------------------------------------------------------------

func TestEventRequiredProperties(t *testing.T) {
	t.Run("missing UID and DTSTAMP", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("DTSTART:20200101T000000Z\n"))
		if !hasRule(res, "uid-required") {
			t.Error("expected uid-required")
		}
		if !hasRule(res, "dtstamp-required") {
			t.Error("expected dtstamp-required")
		}
	})

	t.Run("missing DTSTART without METHOD is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\n"))
		if !hasRule(res, "dtstart-required") {
			t.Error("expected dtstart-required")
		}
	})

	t.Run("clean VEVENT has none of these", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\n"))
		for _, r := range []string{"uid-required", "dtstamp-required", "dtstart-required"} {
			if hasRule(res, r) {
				t.Errorf("unexpected %s", r)
			}
		}
	})
}

func TestEventDTEndDuration(t *testing.T) {
	t.Run("DTEND and DURATION both present is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T100000Z\nDTEND:20200101T110000Z\nDURATION:PT1H\n"))
		if !hasRule(res, "dtend-duration-exclusive") {
			t.Error("expected dtend-duration-exclusive")
		}
	})

	t.Run("DTEND alone is clean", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T100000Z\nDTEND:20200101T110000Z\n"))
		if hasRule(res, "dtend-duration-exclusive") {
			t.Error("unexpected dtend-duration-exclusive")
		}
	})

	t.Run("DTEND before DTSTART is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T110000Z\nDTEND:20200101T100000Z\n"))
		if !hasRule(res, "dtend-before-start") {
			t.Error("expected dtend-before-start")
		}
	})

	t.Run("DTEND equal to DTSTART warns (zero-length interval, not a MUST violation)", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T100000Z\nDTEND:20200101T100000Z\n"))
		if hasRule(res, "dtend-before-start") {
			t.Error("a zero-length interval is not the same as DTEND before DTSTART")
		}
		fs := findingsOf(res, "dtend-equals-start")
		if len(fs) != 1 || fs[0].Severity != SeverityWarning {
			t.Errorf("expected exactly one dtend-equals-start warning, got %v", fs)
		}
	})

	t.Run("DTEND after DTSTART is clean", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T100000Z\nDTEND:20200101T110000Z\n"))
		if hasRule(res, "dtend-before-start") {
			t.Error("unexpected dtend-before-start")
		}
	})

	t.Run("DTEND value type mismatch (DATE vs DATE-TIME) is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART;VALUE=DATE:20200101\nDTEND:20200102T000000Z\n"))
		if !hasRule(res, "dtend-value-type-mismatch") {
			t.Error("expected dtend-value-type-mismatch")
		}
	})

	t.Run("DTEND same value type as DATE DTSTART is clean", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART;VALUE=DATE:20200101\nDTEND;VALUE=DATE:20200102\n"))
		if hasRule(res, "dtend-value-type-mismatch") {
			t.Error("unexpected dtend-value-type-mismatch")
		}
	})

	t.Run("malformed DURATION is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nDURATION:1H\n"))
		if !hasRule(res, "duration-syntax") {
			t.Error("expected duration-syntax")
		}
	})
}

// --- UID uniqueness / RECURRENCE-ID overrides ------------------------------

func TestUIDUniqueness(t *testing.T) {
	t.Run("duplicate UID with no RECURRENCE-ID is an error", func(t *testing.T) {
		body := minimalHeader +
			"BEGIN:VEVENT\nUID:dup@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nEND:VEVENT\n" +
			"BEGIN:VEVENT\nUID:dup@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200102T000000Z\nEND:VEVENT\n" +
			"END:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if !hasRule(res, "duplicate-uid") {
			t.Error("expected duplicate-uid")
		}
	})

	t.Run("same UID with distinct RECURRENCE-ID overrides is clean", func(t *testing.T) {
		body := minimalHeader +
			"BEGIN:VEVENT\nUID:series@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nRRULE:FREQ=DAILY;COUNT=5\nEND:VEVENT\n" +
			"BEGIN:VEVENT\nUID:series@x\nRECURRENCE-ID:20200102T000000Z\nDTSTAMP:20200101T000000Z\nDTSTART:20200102T120000Z\nEND:VEVENT\n" +
			"END:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if hasRule(res, "duplicate-uid") {
			t.Error("unexpected duplicate-uid for a recurrence override")
		}
		if hasRule(res, "duplicate-recurrence-id") {
			t.Error("unexpected duplicate-recurrence-id")
		}
	})

	t.Run("same UID and same RECURRENCE-ID twice is an error", func(t *testing.T) {
		body := minimalHeader +
			"BEGIN:VEVENT\nUID:series@x\nRECURRENCE-ID:20200102T000000Z\nDTSTAMP:20200101T000000Z\nDTSTART:20200102T120000Z\nEND:VEVENT\n" +
			"BEGIN:VEVENT\nUID:series@x\nRECURRENCE-ID:20200102T000000Z\nDTSTAMP:20200101T000000Z\nDTSTART:20200102T130000Z\nEND:VEVENT\n" +
			"END:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if !hasRule(res, "duplicate-recurrence-id") {
			t.Error("expected duplicate-recurrence-id")
		}
	})
}

// --- VTODO ------------------------------------------------------------------

func TestTodoRules(t *testing.T) {
	wrap := func(body string) []byte {
		return crlf(minimalHeader + "BEGIN:VTODO\n" + body + "END:VTODO\nEND:VCALENDAR\n")
	}

	t.Run("DUE and DURATION both present is an error", func(t *testing.T) {
		res := mustValidate(t, wrap("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nDUE:20200102T000000Z\nDURATION:P1D\n"))
		if !hasRule(res, "due-duration-exclusive") {
			t.Error("expected due-duration-exclusive")
		}
	})

	t.Run("DURATION without DTSTART is an error", func(t *testing.T) {
		res := mustValidate(t, wrap("UID:1@x\nDTSTAMP:20200101T000000Z\nDURATION:P1D\n"))
		if !hasRule(res, "duration-requires-dtstart") {
			t.Error("expected duration-requires-dtstart")
		}
	})

	t.Run("DURATION with DTSTART is clean", func(t *testing.T) {
		res := mustValidate(t, wrap("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nDURATION:P1D\n"))
		if hasRule(res, "duration-requires-dtstart") {
			t.Error("unexpected duration-requires-dtstart")
		}
	})

	t.Run("unrecognized STATUS warns", func(t *testing.T) {
		res := mustValidate(t, wrap("UID:1@x\nDTSTAMP:20200101T000000Z\nSTATUS:PENDING\n"))
		if !hasRule(res, "status-enum") {
			t.Error("expected status-enum warning")
		}
	})
}

// --- VJOURNAL -----------------------------------------------------------

func TestJournalRequiredProperties(t *testing.T) {
	body := minimalHeader + "BEGIN:VJOURNAL\nEND:VJOURNAL\nEND:VCALENDAR\n"
	res := mustValidate(t, crlf(body))
	if !hasRule(res, "uid-required") || !hasRule(res, "dtstamp-required") {
		t.Error("expected uid-required and dtstamp-required on empty VJOURNAL")
	}
}

// --- VFREEBUSY ------------------------------------------------------------

func TestFreeBusyForbidsRecurrence(t *testing.T) {
	body := minimalHeader + "BEGIN:VFREEBUSY\nUID:1@x\nDTSTAMP:20200101T000000Z\nRRULE:FREQ=DAILY\nEND:VFREEBUSY\nEND:VCALENDAR\n"
	res := mustValidate(t, crlf(body))
	if !hasRule(res, "rrule-forbidden") {
		t.Error("expected rrule-forbidden inside VFREEBUSY")
	}
}

func TestFreeBusyPeriodSyntax(t *testing.T) {
	t.Run("malformed FREEBUSY period is an error", func(t *testing.T) {
		body := minimalHeader + "BEGIN:VFREEBUSY\nUID:1@x\nDTSTAMP:20200101T000000Z\nFREEBUSY:not-a-period\nEND:VFREEBUSY\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if !hasRule(res, "freebusy-period-syntax") {
			t.Error("expected freebusy-period-syntax")
		}
	})
	t.Run("valid FREEBUSY period is clean", func(t *testing.T) {
		body := minimalHeader + "BEGIN:VFREEBUSY\nUID:1@x\nDTSTAMP:20200101T000000Z\nFREEBUSY:20200101T050000Z/PT8H30M\nEND:VFREEBUSY\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if hasRule(res, "freebusy-period-syntax") {
			t.Error("unexpected freebusy-period-syntax")
		}
	})
}

// --- VTIMEZONE --------------------------------------------------------------

func TestTimezoneRules(t *testing.T) {
	t.Run("no STANDARD or DAYLIGHT is an error", func(t *testing.T) {
		body := minimalHeader + "BEGIN:VTIMEZONE\nTZID:X\nEND:VTIMEZONE\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if !hasRule(res, "vtimezone-no-observance") {
			t.Error("expected vtimezone-no-observance")
		}
	})

	t.Run("negative-zero UTC offset is an error", func(t *testing.T) {
		body := minimalHeader + "BEGIN:VTIMEZONE\nTZID:X\nBEGIN:STANDARD\nDTSTART:20200101T000000\nTZOFFSETFROM:-0000\nTZOFFSETTO:+0100\nEND:STANDARD\nEND:VTIMEZONE\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if !hasRule(res, "tzoffsetfrom-negative-zero") {
			t.Error("expected tzoffsetfrom-negative-zero")
		}
	})

	t.Run("RRULE UNTIL not UTC in STANDARD is an error", func(t *testing.T) {
		body := minimalHeader + "BEGIN:VTIMEZONE\nTZID:X\nBEGIN:STANDARD\nDTSTART:20200101T000000\nTZOFFSETFROM:-0400\nTZOFFSETTO:-0500\nRRULE:FREQ=YEARLY;UNTIL=20300101T000000\nEND:STANDARD\nEND:VTIMEZONE\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if !hasRule(res, "tz-rrule-until-not-utc") {
			t.Error("expected tz-rrule-until-not-utc")
		}
	})

	t.Run("valid VTIMEZONE is clean", func(t *testing.T) {
		body := minimalHeader + "BEGIN:VTIMEZONE\nTZID:X\nBEGIN:STANDARD\nDTSTART:20200101T000000\nTZOFFSETFROM:-0400\nTZOFFSETTO:-0500\nEND:STANDARD\nEND:VTIMEZONE\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		for _, r := range []string{"vtimezone-no-observance", "tzoffsetfrom-negative-zero", "tz-rrule-until-not-utc"} {
			if hasRule(res, r) {
				t.Errorf("unexpected %s", r)
			}
		}
	})
}

// --- VALARM ------------------------------------------------------------

func TestAlarmRules(t *testing.T) {
	wrap := func(alarmBody string) []byte {
		return crlf(minimalHeader + "BEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nDTEND:20200101T010000Z\n" +
			"BEGIN:VALARM\n" + alarmBody + "END:VALARM\nEND:VEVENT\nEND:VCALENDAR\n")
	}

	t.Run("DISPLAY without DESCRIPTION is an error", func(t *testing.T) {
		res := mustValidate(t, wrap("ACTION:DISPLAY\nTRIGGER:-PT15M\n"))
		if !hasRule(res, "description-required") {
			t.Error("expected description-required")
		}
	})

	t.Run("EMAIL without ATTENDEE is an error", func(t *testing.T) {
		res := mustValidate(t, wrap("ACTION:EMAIL\nTRIGGER:-PT15M\nDESCRIPTION:body\nSUMMARY:subj\n"))
		if !hasRule(res, "valarm-email-attendee-required") {
			t.Error("expected valarm-email-attendee-required")
		}
	})

	t.Run("only one of DURATION/REPEAT is an error", func(t *testing.T) {
		res := mustValidate(t, wrap("ACTION:DISPLAY\nTRIGGER:-PT15M\nDESCRIPTION:x\nDURATION:PT5M\n"))
		if !hasRule(res, "alarm-repeat-duration") {
			t.Error("expected alarm-repeat-duration")
		}
	})

	t.Run("absolute TRIGGER not in UTC is an error", func(t *testing.T) {
		res := mustValidate(t, wrap("ACTION:DISPLAY\nTRIGGER;VALUE=DATE-TIME:20200101T000000\nDESCRIPTION:x\n"))
		if !hasRule(res, "trigger-not-utc") {
			t.Error("expected trigger-not-utc")
		}
	})

	t.Run("valid DISPLAY alarm is clean", func(t *testing.T) {
		res := mustValidate(t, wrap("ACTION:DISPLAY\nTRIGGER:-PT15M\nDESCRIPTION:x\n"))
		if len(res.Findings) != 0 {
			t.Errorf("expected no findings, got %v", res.Findings)
		}
	})

	t.Run("VALARM inside VJOURNAL is invalid nesting", func(t *testing.T) {
		body := minimalHeader + "BEGIN:VJOURNAL\nUID:1@x\nDTSTAMP:20200101T000000Z\nBEGIN:VALARM\nACTION:DISPLAY\nTRIGGER:-PT15M\nDESCRIPTION:x\nEND:VALARM\nEND:VJOURNAL\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if !hasRule(res, "invalid-nesting") {
			t.Error("expected invalid-nesting")
		}
	})
}

// --- RRULE / UNTIL value-type matching (§3.3.10) ---------------------------

func TestRRuleUntilMatchesDTStart(t *testing.T) {
	t.Run("UNTIL is DATE but DTSTART is DATE-TIME: type mismatch", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nRRULE:FREQ=DAILY;UNTIL=20200201\n"))
		if !hasRule(res, "rrule-until-type-mismatch") {
			t.Error("expected rrule-until-type-mismatch")
		}
	})

	t.Run("DTSTART is UTC, UNTIL is floating: form mismatch", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nRRULE:FREQ=DAILY;UNTIL=20200201T000000\n"))
		if !hasRule(res, "rrule-until-not-utc") {
			t.Error("expected rrule-until-not-utc")
		}
	})

	t.Run("DTSTART floating, UNTIL UTC: form mismatch", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000\nRRULE:FREQ=DAILY;UNTIL=20200201T000000Z\n"))
		if !hasRule(res, "rrule-until-form-mismatch") {
			t.Error("expected rrule-until-form-mismatch")
		}
	})

	t.Run("DTSTART and UNTIL both UTC DATE-TIME is clean", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nRRULE:FREQ=DAILY;UNTIL=20200201T000000Z\n"))
		for _, r := range []string{"rrule-until-type-mismatch", "rrule-until-not-utc", "rrule-until-form-mismatch"} {
			if hasRule(res, r) {
				t.Errorf("unexpected %s", r)
			}
		}
	})

	t.Run("DTSTART and UNTIL both DATE is clean", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART;VALUE=DATE:20200101\nRRULE:FREQ=DAILY;UNTIL=20200201\n"))
		for _, r := range []string{"rrule-until-type-mismatch", "rrule-until-not-utc", "rrule-until-form-mismatch"} {
			if hasRule(res, r) {
				t.Errorf("unexpected %s", r)
			}
		}
	})

	t.Run("UNTIL and COUNT both present is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nRRULE:FREQ=DAILY;UNTIL=20200201T000000Z;COUNT=5\n"))
		if !hasRule(res, "recur-syntax") {
			t.Error("expected recur-syntax for UNTIL+COUNT")
		}
	})

	t.Run("missing FREQ is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nRRULE:COUNT=5\n"))
		if !hasRule(res, "recur-syntax") {
			t.Error("expected recur-syntax for missing FREQ")
		}
	})

	t.Run("BYHOUR with DATE DTSTART is an error", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART;VALUE=DATE:20200101\nRRULE:FREQ=DAILY;BYHOUR=10\n"))
		if !hasRule(res, "rrule-byxxx-with-date") {
			t.Error("expected rrule-byxxx-with-date")
		}
	})
}

// --- TZID resolution --------------------------------------------------------

func TestTZIDResolution(t *testing.T) {
	unresolved := crlf(minimalHeader + "BEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART;TZID=America/Nowhere:20200101T000000\nEND:VEVENT\nEND:VCALENDAR\n")

	t.Run("default policy warns", func(t *testing.T) {
		res, err := Validate(unresolved, Options{})
		if err != nil {
			t.Fatal(err)
		}
		fs := findingsOf(res, "tzid-unresolved")
		if len(fs) != 1 || fs[0].Severity != SeverityWarning {
			t.Errorf("expected exactly one warning finding, got %v", fs)
		}
	})

	t.Run("error policy escalates", func(t *testing.T) {
		res, err := Validate(unresolved, Options{TZID: TZIDError})
		if err != nil {
			t.Fatal(err)
		}
		fs := findingsOf(res, "tzid-unresolved")
		if len(fs) != 1 || fs[0].Severity != SeverityError {
			t.Errorf("expected exactly one error finding, got %v", fs)
		}
	})

	t.Run("ignore policy suppresses it", func(t *testing.T) {
		res, err := Validate(unresolved, Options{TZID: TZIDIgnore})
		if err != nil {
			t.Fatal(err)
		}
		if hasRule(res, "tzid-unresolved") {
			t.Error("did not expect tzid-unresolved with TZIDIgnore")
		}
	})

	t.Run("resolved TZID is clean", func(t *testing.T) {
		body := minimalHeader +
			"BEGIN:VTIMEZONE\nTZID:America/New_York\nBEGIN:STANDARD\nDTSTART:20200101T000000\nTZOFFSETFROM:-0400\nTZOFFSETTO:-0500\nEND:STANDARD\nEND:VTIMEZONE\n" +
			"BEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART;TZID=America/New_York:20200101T000000\nEND:VEVENT\nEND:VCALENDAR\n"
		res := mustValidate(t, crlf(body))
		if hasRule(res, "tzid-unresolved") {
			t.Error("unexpected tzid-unresolved")
		}
	})
}

// --- content-line mechanics via full Validate ------------------------------

func TestLineLengthAndEndings(t *testing.T) {
	t.Run("over-long line warns", func(t *testing.T) {
		long := "SUMMARY:" + strings.Repeat("x", 100)
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\n"+long+"\n"))
		if !hasRule(res, "line-too-long") {
			t.Error("expected line-too-long")
		}
	})

	t.Run("bare LF warns", func(t *testing.T) {
		data := []byte("BEGIN:VCALENDAR\nPRODID:-//a//EN\nVERSION:2.0\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nEND:VEVENT\nEND:VCALENDAR\n")
		res := mustValidate(t, data)
		if !hasRule(res, "line-ending") {
			t.Error("expected line-ending warning for bare LF")
		}
	})

	t.Run("folded 75-octet line does not warn", func(t *testing.T) {
		res := mustValidate(t, wrapEvent("UID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\n"))
		if hasRule(res, "line-too-long") {
			t.Error("unexpected line-too-long on short lines")
		}
	})
}

// --- structural parsing ------------------------------------------------------

func TestStructuralErrors(t *testing.T) {
	t.Run("mismatched END", func(t *testing.T) {
		res := mustValidate(t, crlf("BEGIN:VCALENDAR\nPRODID:-//a//EN\nVERSION:2.0\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nEND:VTODO\nEND:VCALENDAR\n"))
		if !hasRule(res, "mismatched-end") {
			t.Error("expected mismatched-end")
		}
	})

	t.Run("no BEGIN:VCALENDAR at all is a hard parse failure", func(t *testing.T) {
		_, err := Validate([]byte("not an ics file\n"), Options{})
		if err == nil {
			t.Error("expected an error for input with no VCALENDAR object")
		}
	})

	t.Run("unclosed component", func(t *testing.T) {
		res := mustValidate(t, crlf("BEGIN:VCALENDAR\nPRODID:-//a//EN\nVERSION:2.0\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nEND:VCALENDAR\n"))
		if !hasRule(res, "unclosed-component") {
			t.Error("expected unclosed-component")
		}
	})
}
