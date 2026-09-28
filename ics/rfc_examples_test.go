package ics

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRFCExamplesLintClean transcribes the RFC 5545 §3.6.x and §4 example
// calendar objects verbatim (CRLF line endings, original folding) and
// requires that every one of them produces zero findings. Any finding on
// an RFC example indicates the rule, not the example, is wrong.
func TestRFCExamplesLintClean(t *testing.T) {
	dir := "../testdata/rfc-examples"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read testdata dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no RFC example fixtures found")
	}
	for _, e := range entries {
		e := e
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			res, err := Validate(data, Options{})
			if err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
			for _, f := range res.Findings {
				t.Errorf("unexpected finding: %s", f)
			}
		})
	}
}
