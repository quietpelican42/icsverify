// Command icsverify validates an RFC 5545 iCalendar (.ics) file against the
// RFC's content-line grammar and per-component property tables.
//
// Usage:
//
//	icsverify [flags] <file.ics|->
//
// Exit codes:
//
//	0  the object is clean (no findings)
//	1  the object parsed but has one or more findings (errors and/or warnings)
//	2  the input could not be analysed at all (unreadable file, no
//	   BEGIN:VCALENDAR object found)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/quietpelican42/icsverify/ics"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("icsverify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text, json, github")
	tzidFlag := fs.String("tzid", "warn", "how to report a TZID that doesn't resolve to a VTIMEZONE in the file: warn, error, ignore")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: icsverify [flags] <file.ics|->\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	var opts ics.Options
	switch *tzidFlag {
	case "warn":
		opts.TZID = ics.TZIDWarn
	case "error":
		opts.TZID = ics.TZIDError
	case "ignore":
		opts.TZID = ics.TZIDIgnore
	default:
		fmt.Fprintf(stderr, "icsverify: invalid -tzid value %q (want warn, error, or ignore)\n", *tzidFlag)
		return 2
	}

	path := fs.Arg(0)
	data, err := readInput(path)
	if err != nil {
		fmt.Fprintf(stderr, "icsverify: %v\n", err)
		return 2
	}

	result, verr := ics.Validate(data, opts)
	sort.SliceStable(result.Findings, func(i, j int) bool {
		return result.Findings[i].Line < result.Findings[j].Line
	})

	switch *format {
	case "json":
		writeJSON(stdout, result, verr)
	case "github":
		writeGitHub(stdout, result)
	default:
		writeText(stdout, path, result, verr)
	}

	if verr != nil {
		return 2
	}
	if len(result.Findings) > 0 {
		return 1
	}
	return 0
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func writeText(w io.Writer, path string, result *ics.Result, verr error) {
	if verr != nil {
		fmt.Fprintf(w, "%s: could not analyse: %v\n", path, verr)
	}
	for _, f := range result.Findings {
		if f.Line > 0 {
			fmt.Fprintf(w, "%s:%d: %s: %s [%s, %s]\n", path, f.Line, f.SeverityText, f.Message, f.Rule, f.Section)
		} else {
			fmt.Fprintf(w, "%s: %s: %s [%s, %s]\n", path, f.SeverityText, f.Message, f.Rule, f.Section)
		}
	}
	if verr == nil && len(result.Findings) == 0 {
		fmt.Fprintf(w, "%s: OK\n", path)
	}
}

type jsonOutput struct {
	Error    string        `json:"error,omitempty"`
	Findings []ics.Finding `json:"findings"`
}

func writeJSON(w io.Writer, result *ics.Result, verr error) {
	out := jsonOutput{Findings: result.Findings}
	if result.Findings == nil {
		out.Findings = []ics.Finding{}
	}
	if verr != nil {
		out.Error = verr.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// writeGitHub emits GitHub Actions "workflow command" annotations
// (::error file=...,line=...::message / ::warning ...), one per finding.
func writeGitHub(w io.Writer, result *ics.Result) {
	for _, f := range result.Findings {
		kind := "warning"
		if f.SeverityText == "error" {
			kind = "error"
		}
		if f.Line > 0 {
			fmt.Fprintf(w, "::%s line=%d::%s [%s, %s]\n", kind, f.Line, f.Message, f.Rule, f.Section)
		} else {
			fmt.Fprintf(w, "::%s::%s [%s, %s]\n", kind, f.Message, f.Rule, f.Section)
		}
	}
}
