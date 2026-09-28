package ics

import "testing"

func TestIsValidDate(t *testing.T) {
	cases := map[string]bool{
		"20200101": true,
		"20200229": true,  // 2020 is a leap year
		"20210229": false, // 2021 is not
		"20201332": false, // invalid month
		"2020011":  false, // too short
		"":         false,
	}
	for in, want := range cases {
		if got := IsValidDate(in); got != want {
			t.Errorf("IsValidDate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseDateTimeForms(t *testing.T) {
	if form, ok := ParseDateTime("20200101T120000Z", ""); !ok || form != FormUTC {
		t.Errorf("UTC form: ok=%v form=%v", ok, form)
	}
	if form, ok := ParseDateTime("20200101T120000", ""); !ok || form != FormFloating {
		t.Errorf("floating form: ok=%v form=%v", ok, form)
	}
	if form, ok := ParseDateTime("20200101T120000", "America/New_York"); !ok || form != FormLocalTZ {
		t.Errorf("local-tz form: ok=%v form=%v", ok, form)
	}
	if _, ok := ParseDateTime("20200101T250000", ""); ok {
		t.Error("hour 25 should be invalid")
	}
	if _, ok := ParseDateTime("20200101", ""); ok {
		t.Error("a bare DATE should not parse as DATE-TIME")
	}
}

func TestIsValidDuration(t *testing.T) {
	valid := []string{"P15DT5H0M20S", "PT15M", "P7W", "-P2D", "+P1D", "PT1H", "PT1H0M0S", "P1D"}
	for _, v := range valid {
		if !IsValidDuration(v) {
			t.Errorf("IsValidDuration(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "P", "PT", "1H", "P1", "PW", "P-1D", "P1DT"}
	for _, v := range invalid {
		if IsValidDuration(v) {
			t.Errorf("IsValidDuration(%q) = true, want false", v)
		}
	}
}

func TestIsValidPeriod(t *testing.T) {
	if !IsValidPeriod("19970101T180000Z/19970102T070000Z") {
		t.Error("explicit period should be valid")
	}
	if !IsValidPeriod("19970101T180000Z/PT5H30M") {
		t.Error("start+duration period should be valid")
	}
	if IsValidPeriod("19970101T180000Z") {
		t.Error("missing '/' should be invalid")
	}
	if IsValidPeriod("19970101T180000Z/bogus") {
		t.Error("bogus end should be invalid")
	}
}

func TestParseRecurBasics(t *testing.T) {
	r, problems := ParseRecur("FREQ=YEARLY;BYMONTH=4;BYDAY=-1SU;UNTIL=19730429T070000Z")
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if r.Freq != "YEARLY" {
		t.Errorf("Freq = %q", r.Freq)
	}
	if r.UntilForm != FormUTC {
		t.Errorf("UntilForm = %v", r.UntilForm)
	}
}

func TestParseRecurMissingFreq(t *testing.T) {
	_, problems := ParseRecur("COUNT=5")
	if len(problems) == 0 {
		t.Error("expected a problem for missing FREQ")
	}
}

func TestParseRecurUntilAndCountExclusive(t *testing.T) {
	_, problems := ParseRecur("FREQ=DAILY;UNTIL=20200101T000000Z;COUNT=3")
	found := false
	for _, p := range problems {
		if p == "UNTIL and COUNT MUST NOT both be present" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected UNTIL/COUNT exclusivity problem, got %v", problems)
	}
}

func TestParseRecurDuplicatePart(t *testing.T) {
	_, problems := ParseRecur("FREQ=DAILY;FREQ=WEEKLY")
	if len(problems) == 0 {
		t.Error("expected a problem for FREQ specified twice")
	}
}
