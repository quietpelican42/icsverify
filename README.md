# icsverify

A strict RFC 5545 (iCalendar) linter and validator: a Go library plus a CLI
that checks a `.ics` file's content-line syntax and per-component property
rules, and reports every finding with the exact RFC section it violates.

```go
res, err := icsverify.Validate(data, icsverify.Options{})
if err != nil {
    log.Fatal(err) // could not analyse at all, e.g. no VCALENDAR object
}
for _, f := range res.Findings {
    fmt.Println(f) // "error: line 12: UID is REQUIRED ... [uid-required, RFC 5545 §3.6.1]"
}
```

(the CLI is `icsverify file.ics`; see Usage)

## Why

`icalendar` (Python) and `golang-ical` (Go), the two libraries most people
reach for to read `.ics` files, both parse a calendar object that is missing
`PRODID`, has two `VEVENT`s sharing one `UID`, or has a `TRIGGER` in the
wrong time zone form without raising anything — they build a tree of
whatever properties are present and hand it back. Neither library validates
against RFC 5545's per-component property tables. The one purpose-built
validator found on GitHub, `codebruinc/ics-validator-action` (JavaScript, not
published to npm), does check line folding, `BEGIN`/`END` nesting and a few
required properties, but it only understands `VEVENT`; it has no rules for
`VTODO`, `VJOURNAL` or `VFREEBUSY`, does not check `RRULE UNTIL` against
`DTSTART`'s value type, and does not flag unfolded lines over 75 octets.

Re-checked directly for this package (via `gh search code` / `gh search
repos`, since web search was unavailable): `ical4j` (Java, 837 stars) and
`libical` (C, 366 stars) are real, maintained implementations with internal
validation, but neither ships a ready-to-run CLI validator — `ical4j`'s
validation is a library API you'd have to write a driver program against,
and no `icalvalidator` CLI exists in current `libical`. Every GitHub repo
actually named `ics-lint` / `icslint` found in this search is a zero-star,
zero-fork, zero-issue project a few weeks old, not an established tool.

Measured: `go test ./ics/ -run TestRFCExamplesLintClean -v -count=1`, all 20
of the RFC's own `VEVENT`/`VTODO`/`VJOURNAL`/`VFREEBUSY`/`VTIMEZONE`/`VALARM`
example objects (§3.6.x, §4), transcribed byte-for-byte, lint clean:

```
--- PASS: TestRFCExamplesLintClean (0.00s)
    --- PASS: TestRFCExamplesLintClean/section4-conference.ics (0.00s)
    --- PASS: TestRFCExamplesLintClean/section4-handcal.ics (0.00s)
    [... 18 more, all PASS ...]
PASS
```

Measured against 32 real-world `.ics` files not seen while writing the
rules (Google Calendar holiday exports for 8 countries, Outlook and Apple
calendar exports, conference and meetup feeds, and test fixtures from the
Python `icalendar` library and a fuzzing corpus): 8 lint clean, 24 produce
findings. Every finding was inspected by hand; none were false positives.
The two most common findings were bare-LF line endings (files served over
HTTP without CRLF, a real deviation from §3.1) and, more interestingly,
genuine RFC violations an incumbent parser would silently accept: the
`icalendar` library's own tutorial fixture has two `VEVENT`s sharing one
`UID` with no `RECURRENCE-ID`; a real conference-CFP feed omits `DTSTAMP` on
every one of its 114 events; a holiday-calendar feed omits `PRODID`
entirely.

## Installation

```sh
go install github.com/quietpelican42/icsverify/cmd/icsverify@latest
```

Or as a library: `go get github.com/quietpelican42/icsverify`.

## Usage

### Lint a file

```sh
icsverify calendar.ics
```

### Lint stdin

```sh
curl -s https://example.com/calendar.ics | icsverify -
```

### Machine-readable output

```sh
icsverify -format=json calendar.ics
icsverify -format=github calendar.ics   # GitHub Actions ::error/::warning annotations
```

### Treat a dangling TZID as an error

Many producers reference an IANA time zone name (`TZID=America/New_York`)
without embedding the matching `VTIMEZONE`, relying on the consumer's own
zone database. This is technically a §3.6.5 violation but extremely common,
so it is a warning by default:

```sh
icsverify -tzid=error calendar.ics   # or -tzid=ignore to skip the check
```

## Reference

### CLI flags

| Flag      | Values                  | Default | Description                                    |
| --------- | ------------------------ | ------- | ----------------------------------------------- |
| `-format` | `text`, `json`, `github` | `text`  | Output format.                                  |
| `-tzid`   | `warn`, `error`, `ignore`| `warn`  | How to report a `TZID` with no matching `VTIMEZONE`. |

### Exit codes

| Code | Meaning                                                             |
| ---- | -------------------------------------------------------------------- |
| `0`  | Clean: parsed, no findings.                                          |
| `1`  | Parsed, but has one or more findings (errors and/or warnings).       |
| `2`  | Could not analyse: unreadable file, or no `BEGIN:VCALENDAR` found.   |

### Library API

- `Validate(data []byte, opts Options) (*Result, error)` — parses and
  validates an iCalendar object. `err` is non-nil only when no usable
  `VCALENDAR` object could be found; `Result` is always non-nil.
- `Result.Findings []Finding`, `Result.HasErrors() bool`.
- `Finding{Rule, Section, Severity, Message, Line, Path}` — `Rule` is a
  stable machine-matchable id (e.g. `"vevent-uid-required"` shortened to
  `"uid-required"` in context), `Section` cites the RFC (e.g.
  `"RFC 5545 §3.6.1"`).
- `Options{TZID TZIDPolicy}` — `TZIDWarn` (default), `TZIDError`,
  `TZIDIgnore`.

### What is checked

Every rule cites its RFC 5545 section (or RFC 5546 for `METHOD`) in its
`Finding.Section`; the full list is in `ics/validate.go`. Summary:

- **Content lines** (§3.1): line folding/unfolding, CRLF vs bare-LF
  (warning), lines over 75 octets (warning), parameter quoting, escaped
  `TEXT` values (§3.3.11).
- **Structure**: `BEGIN`/`END` nesting and matching; each component type
  only where the RFC allows it to appear (e.g. `VALARM` only inside
  `VEVENT`/`VTODO`, `STANDARD`/`DAYLIGHT` only inside `VTIMEZONE`).
- **Property tables** for `VCALENDAR`, `VEVENT`, `VTODO`, `VJOURNAL`,
  `VFREEBUSY`, `VTIMEZONE` (+`STANDARD`/`DAYLIGHT`), `VALARM` (rules differ
  by `ACTION`: `AUDIO`/`DISPLAY`/`EMAIL`): required-once, optional-at-most-
  once, and should-not-repeat properties.
- **Value types**: `DATE` vs `DATE-TIME` (and matching between `DTSTART`
  and `DTEND`/`DUE`/`RECURRENCE-ID`), `DURATION` grammar (§3.3.6), `PERIOD`
  grammar (§3.3.9, used by `FREEBUSY`), `RECUR` grammar including `UNTIL`
  matching `DTSTART`'s value type and UTC-vs-floating form (§3.3.10),
  `UTC-OFFSET` grammar and the "`-0000`/`-000000` not allowed" rule
  (§3.3.14).
- **Cross-property logic**: `DTEND`/`DUE` vs `DURATION` mutual exclusion;
  `DTEND`/`DUE` not before `DTSTART` (equal is a warning, not an error —
  RFC 5545 does not forbid a zero-length interval); `VTODO` `DURATION`
  requires `DTSTART`; `UID` uniqueness across the object, respecting
  `RECURRENCE-ID` overrides (§3.8.4.4, §3.8.4.7); `TZID` resolution to an
  in-file `VTIMEZONE` (configurable); `VALARM` `TRIGGER` `RELATED=END`/
  `START` context (needs `DTEND`/`DUE`/`DTSTART` on the parent).
- **`METHOD`/iTIP basics** (RFC 5546 §1.4): flags a `METHOD` value that
  isn't one of the eight registered iTIP methods, and relaxes `DTSTART`
  from required to optional on `VEVENT` per §3.6.1's own exception.

## How it works

`ics/contentline.go` implements the RFC 5545 §3.1 grammar directly against
bytes: it splits on CRLF (or bare LF, with a warning), rejoins folded
continuation lines, and then parses `name *(";" param) ":" value` by hand,
including quoted parameter values and multi-valued parameters. This feeds a
small recursive-descent-free stack machine (`ics/model.go`) that turns
`BEGIN`/`END` pairs into a `Component` tree while still recovering from a
mismatched or missing `END` so the rest of the file can be checked.
`ics/validate.go` then walks that tree once, running the property-table and
value-type checks in `ics/valuetypes.go` per component.

## Limitations

- Not a full RFC 5545 implementation: it validates structure and grammar,
  not full semantics. It does not expand `RRULE`s, resolve floating times
  against a time zone database, or check `EXDATE`/`RDATE` against the
  recurrence set they're meant to exclude/add to.
- `DTEND`/`DUE` "after `DTSTART`" is a textual wall-clock comparison of the
  literal digits (ignoring `Z` and `TZID`); it does not convert between
  time zones, so a `DTSTART` and `DTEND` expressed in genuinely different
  zones are compared on local clock time, not on the instants they name.
- `VALUE=DATE`/`DATE-TIME` value-type checks do not extend to every
  property that can carry `RECUR`, `EXDATE` or `RDATE` lists item-by-item
  beyond the checks listed above.
- `METHOD`/iTIP support is basic: it checks that `METHOD` is a registered
  name and relaxes the `VEVENT` `DTSTART` requirement per §3.6.1. It does
  not implement RFC 5546's full per-method, per-role property-constraint
  tables (e.g. `REQUEST` requiring `ATTENDEE`, `CANCEL` semantics).
  RFC 5546 support beyond that is unverified.
- RFC 7986 (new calendar properties like `NAME`, `COLOR`, `IMAGE`) and
  RFC 7953 (`VAVAILABILITY`), RFC 9073 (`VLOCATION`/`PARTICIPANT`) are not
  validated; unrecognized top-level component types are parsed structurally
  but not checked against any property table, and are not flagged as
  invalid nesting.
- `CLASS`, `TRANSP` and other enumerated-but-extensible properties are not
  checked against their standard value lists (only `STATUS` is, as a
  warning), since real producers commonly use `X-` extension values there.
- No line/column-offset-within-line reporting, only physical line numbers.

## Compatibility

Developed and tested with go1.27.1 on darwin/arm64. Uses only the standard
library; no third-party dependencies. CI (`.github/workflows/test.yml`)
runs on Go 1.23 on `ubuntu-latest` but that configuration has not been run
by hand — it is untested beyond what CI itself reports once this repository
has a remote.

## Development

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
gofmt -l .   # must print nothing
```

The RFC-example fixtures under `testdata/rfc-examples/` are transcribed
verbatim from RFC 5545 §3.6.x and §4 (CRLF line endings preserved); a
finding on any of them means a rule is wrong, not the example.

## License

MIT. See [LICENSE](LICENSE).
