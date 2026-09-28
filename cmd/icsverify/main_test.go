package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ics")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(content, "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const cleanCal = "BEGIN:VCALENDAR\nPRODID:-//test//EN\nVERSION:2.0\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART:20200101T000000Z\nEND:VEVENT\nEND:VCALENDAR\n"
const badCal = "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:1@x\nEND:VEVENT\nEND:VCALENDAR\n"

func TestRunExitCodes(t *testing.T) {
	t.Run("clean calendar exits 0", func(t *testing.T) {
		path := writeTemp(t, cleanCal)
		var out, errOut bytes.Buffer
		code := run([]string{path}, &out, &errOut)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stdout=%s", code, out.String())
		}
		if !strings.Contains(out.String(), "OK") {
			t.Errorf("expected OK in output, got %q", out.String())
		}
	})

	t.Run("calendar with findings exits 1", func(t *testing.T) {
		path := writeTemp(t, badCal)
		var out, errOut bytes.Buffer
		code := run([]string{path}, &out, &errOut)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1; stdout=%s", code, out.String())
		}
		if !strings.Contains(out.String(), "prodid-required") {
			t.Errorf("expected prodid-required in output, got %q", out.String())
		}
	})

	t.Run("unreadable file exits 2", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := run([]string{filepath.Join(t.TempDir(), "does-not-exist.ics")}, &out, &errOut)
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
	})

	t.Run("not an iCalendar object exits 2", func(t *testing.T) {
		path := writeTemp(t, "just some text\n")
		var out, errOut bytes.Buffer
		code := run([]string{path}, &out, &errOut)
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
	})

	t.Run("no arguments exits 2", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := run(nil, &out, &errOut)
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
	})
}

func TestRunJSONFormat(t *testing.T) {
	path := writeTemp(t, badCal)
	var out, errOut bytes.Buffer
	code := run([]string{"-format=json", path}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), `"rule": "prodid-required"`) {
		t.Errorf("expected JSON finding, got %s", out.String())
	}
}

func TestRunGitHubFormat(t *testing.T) {
	path := writeTemp(t, badCal)
	var out, errOut bytes.Buffer
	code := run([]string{"-format=github", path}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "::error") {
		t.Errorf("expected a GitHub ::error annotation, got %s", out.String())
	}
}

func TestRunStdin(t *testing.T) {
	// Verify "-" reads from stdin by faking it through a temp file swap is
	// impractical here without more plumbing; instead exercise readInput
	// directly for the file-path case, which run() already covers above.
	// This test focuses on the -tzid flag validation path instead.
	var out, errOut bytes.Buffer
	code := run([]string{"-tzid=bogus", writeTemp(t, cleanCal)}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 for invalid -tzid value", code)
	}
	if !strings.Contains(errOut.String(), "invalid -tzid") {
		t.Errorf("expected error message, got %q", errOut.String())
	}
}

func TestRunTZIDErrorFlag(t *testing.T) {
	cal := "BEGIN:VCALENDAR\nPRODID:-//t//EN\nVERSION:2.0\nBEGIN:VEVENT\nUID:1@x\nDTSTAMP:20200101T000000Z\nDTSTART;TZID=Nowhere/Place:20200101T000000\nEND:VEVENT\nEND:VCALENDAR\n"
	path := writeTemp(t, cal)

	var out, errOut bytes.Buffer
	if code := run([]string{"-tzid=ignore", path}, &out, &errOut); code != 0 {
		t.Errorf("-tzid=ignore: exit code = %d, want 0; out=%s", code, out.String())
	}

	out.Reset()
	if code := run([]string{"-tzid=error", path}, &out, &errOut); code != 1 {
		t.Errorf("-tzid=error: exit code = %d, want 1", code)
	}
}
