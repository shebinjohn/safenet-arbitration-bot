// Package reportcheck checks the values in a draft ruling, report.md, against
// the files of the arbitration that it was drafted from: every hash, address,
// number, and time must be in one of them, every Charter quotation in the
// Charter, and every precedent in the rulings corpus, with the outcome that the
// report gives it.
//
// The drafts are written by agents, which copy values from long outputs, so a
// value can change on the way, as a Safe transaction hash and a data length did
// in two drafts. A value that the sources don't have is either derived, which
// the report says, or wrong.
package reportcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/corpus"
)

// The files of an arbitration directory that the checks read.
const (
	ReportFile  = "report.md"
	CharterFile = "charter.md"
	SummaryFile = "evidence-summary.json"
)

// sourcePatterns are the files of an arbitration directory whose values a
// report may cite, as the arbitrate skill names them: the request, the outputs
// of arbot that the orchestrator saves, the evidence, and the cases and
// judgments, from which the report takes its facts.
var sourcePatterns = []string{
	"request.json",
	"classification.json",
	"status.json",
	"disputed-rules.md",
	"evidence.md",
	SummaryFile,
	"case-*.md",
	"judgment-*.md",
	CharterFile,
}

// Kind is the kind of value that a finding is about.
type Kind string

const (
	// KindAddress is a full address, of 40 hex digits.
	KindAddress Kind = "address"
	// KindHash is a full hash, of 64 hex digits.
	KindHash Kind = "hash"
	// KindHex is other hex data of at least 8 digits, such as a selector or the
	// data of a call.
	KindHex Kind = "hex"
	// KindAbbreviation is a hex value shortened with an ellipsis, such as
	// 0x1235…4306.
	KindAbbreviation Kind = "abbreviation"
	// KindNumber is a number of at least 4 digits.
	KindNumber Kind = "number"
	// KindTime is an RFC 3339 time, such as 2026-11-10T01:26:25Z.
	KindTime Kind = "time"
	// KindQuotation is a blockquote, which quotes the Charter.
	KindQuotation Kind = "quotation"
	// KindPrecedent is the request ID of a past ruling.
	KindPrecedent Kind = "precedent"
	// KindOutcome is the outcome that the report gives a precedent.
	KindOutcome Kind = "outcome"
	// KindSummary is the evidence summary, or what the report says of it.
	KindSummary Kind = "summary"
)

// Finding is a value that the sources don't confirm.
type Finding struct {
	// File is the file that the value is in, such as report.md.
	File string `json:"file"`
	// Line is the line of the value in the file, from 1, or 0 for the file as a
	// whole.
	Line    int    `json:"line"`
	Kind    Kind   `json:"kind"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s %s: %s", f.File, f.Line, f.Kind, f.Value, f.Message)
}

// Result is the findings on a report. A report passes if it has no errors.
// Warnings are values that the report marks as derived, or that the sources
// confirm only in part, which the orchestrator justifies or fixes.
type Result struct {
	Errors   []Finding `json:"errors"`
	Warnings []Finding `json:"warnings"`
	// Sources are the files of the arbitration directory that the values were
	// checked against, by name.
	Sources []string `json:"sources"`
	// Rulings are the paths of the corpus files of the rulings that the file names,
	// whose values were sources too.
	Rulings []string `json:"rulings"`
}

// Check checks the report.md of the arbitration directory dir against its
// sources, and the precedents that it cites against the rulings corpus in
// corpusDir. It fails if the report or the request can't be read.
func Check(dir, corpusDir string) (*Result, error) {
	return CheckFile(dir, ReportFile, corpusDir)
}

// CheckFile checks the file with the name in the arbitration directory dir as
// Check checks its report, such as a case before the report is written. For a
// file other than the report, the cases and judgments aren't sources, since a
// case must only state facts that the evidence has, and its own file never is.
func CheckFile(dir, name, corpusDir string) (*Result, error) {
	report, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	src, files, err := readSources(dir, name)
	if err != nil {
		return nil, err
	}
	// The request's own ruling, once the Council has made it, is neither a source
	// nor a precedent of its draft.
	var request struct {
		ID string `json:"id"`
	}
	path := filepath.Join(dir, "request.json")
	if data, err := os.ReadFile(path); err != nil {
		return nil, err
	} else if err := json.Unmarshal(data, &request); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	rulings, err := readCorpus(corpusDir, strings.ToLower(strings.TrimPrefix(request.ID, "0x")))
	if err != nil {
		return nil, err
	}
	for _, id := range rulings.ids {
		src.addHex(id)
	}
	// The facts of a precedent, such as its Safe or an amount, come from its
	// ruling, so the rulings that the file names are sources too.
	cited, err := rulings.cited(string(report), src)
	if err != nil {
		return nil, err
	}

	c := &checker{file: name, src: src, rulings: rulings, result: &Result{Errors: []Finding{}, Warnings: []Finding{}}}
	c.result.Sources, c.result.Rulings = files, cited
	if charter, err := os.ReadFile(filepath.Join(dir, CharterFile)); err == nil {
		c.charter = normalizeQuote(strings.Split(string(charter), "\n"))
		c.hasCharter = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	c.checkReport(string(report))
	if err := c.checkSummary(dir, string(report)); err != nil {
		return nil, err
	}
	return c.result, nil
}

// checker checks a report.
type checker struct {
	// file is the name of the file that is checked, such as report.md.
	file       string
	src        *sources
	rulings    *rulings
	charter    string
	hasCharter bool
	result     *Result
	seen       map[string]bool
}

func (c *checker) errorf(line int, kind Kind, value, format string, args ...any) {
	c.add(&c.result.Errors, line, kind, value, format, args...)
}

func (c *checker) warnf(line int, kind Kind, value, format string, args ...any) {
	c.add(&c.result.Warnings, line, kind, value, format, args...)
}

// add adds a finding about a value of the file, once per line, since a line can
// have a value twice, such as a block in a link's text and its URL.
func (c *checker) add(findings *[]Finding, line int, kind Kind, value, format string, args ...any) {
	key := fmt.Sprintf("%d %s %s", line, kind, strings.ToLower(value))
	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	*findings = append(*findings, Finding{File: c.file, Line: line, Kind: kind, Value: value, Message: fmt.Sprintf(format, args...)})
}

// checkReport checks each line of the report.
func (c *checker) checkReport(report string) {
	lines := strings.Split(report, "\n")
	var (
		fence      string
		headed     bool
		section    string
		quote      []string
		quoteStart int
	)
	flushQuote := func() {
		if len(quote) > 0 {
			c.checkQuotation(quoteStart, quote)
		}
		quote = nil
	}
	for i, line := range lines {
		n := i + 1
		trimmed := strings.TrimSpace(line)

		// A blockquote in a code block isn't one, and the code block of the ruling
		// context has values to check like any other line.
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
		} else if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flushQuote()
			fence = trimmed[:3]
		} else if heading, ok := strings.CutPrefix(trimmed, "## "); ok {
			flushQuote()
			headed, section = true, sectionName(heading)
		} else if text, ok := strings.CutPrefix(trimmed, ">"); ok && headed {
			// The notice before the first section, which the template starts with, is the
			// bot's own, and every other blockquote quotes the Charter.
			if len(quote) == 0 {
				quoteStart = n
			}
			quote = append(quote, text)
		} else {
			flushQuote()
		}

		precedents := section == "Precedents" || strings.HasPrefix(trimmed, "precedents:")
		c.checkValues(n, line, precedents)
	}
	flushQuote()
}

// sectionNumber matches the number that a section's heading can start with.
var sectionNumber = regexp.MustCompile(`^\d+\.\s*`)

// sectionName returns the name of a section from its heading, without its
// number or emphasis, such as "Precedents" for "5. **Precedents**".
func sectionName(heading string) string {
	heading = sectionNumber.ReplaceAllString(strings.Trim(heading, " *_"), "")
	return strings.Trim(heading, " *_")
}

// outcomes matches the outcome that a parenthesis right after a request ID
// gives its ruling, such as "(insecure)" or "(secure, R-4.3)".
var outcomes = regexp.MustCompile("^\\s*`?(?i:(insecure|secure|out-of-scope|out of scope))\\b")

// parenthesis matches a parenthesis without nested ones.
var parenthesis = regexp.MustCompile(`\([^()]*\)`)

// checkValues checks the hex values, numbers, and times of line n. In the
// Precedents section and the precedents tag, the request IDs are precedents.
// Anywhere, a request ID followed by a parenthesis that starts with an outcome,
// such as "(insecure)", is a precedent with that outcome.
func (c *checker) checkValues(n int, line string, precedents bool) {
	values := scanHex(line)

	// Assign each outcome to the request IDs between it and the parenthesis or the
	// start of the line before it, so that "A and B (insecure)" gives both.
	stated := make(map[int]string)
	from := 0
	for _, loc := range parenthesis.FindAllStringIndex(line, -1) {
		if m := outcomes.FindStringSubmatch(line[loc[0]+1 : loc[1]-1]); m != nil {
			outcome := strings.ReplaceAll(strings.ToLower(m[1]), " ", "-")
			for i, v := range values {
				if v.start >= from && v.end <= loc[0] && v.requestID() {
					stated[i] = outcome
				}
			}
		}
		from = loc[1]
	}

	for i, v := range values {
		outcome, hasOutcome := stated[i]
		if v.requestID() && (precedents || hasOutcome) {
			c.checkPrecedent(n, v, outcome)
			continue
		}
		c.checkHex(n, v)
	}

	rest := maskHex(line, values)
	for _, t := range times.FindAllString(rest, -1) {
		if !c.src.times[strings.ToUpper(t)] {
			c.errorf(n, KindTime, t, "not in any source")
		}
	}
	rest = times.ReplaceAllStringFunc(rest, blank)
	rest = dates.ReplaceAllStringFunc(rest, blank)
	derivedLine := derivedWord.MatchString(rest)
	for _, num := range scanNumbers(rest, 4) {
		if c.src.numbers[num.value] {
			continue
		}
		switch {
		case derivedLine:
			c.warnf(n, KindNumber, num.text, "not in any source; the line marks its numbers as derived")
		case approximate.MatchString(rest[:num.start]):
			c.warnf(n, KindNumber, num.text, "not in any source; marked as approximate, so derived")
		default:
			c.errorf(n, KindNumber, num.text, "not in any source; if the report computed it, mark it as derived")
		}
	}
}

// checkHex checks that a hex value is in a source.
func (c *checker) checkHex(n int, v hexValue) {
	switch {
	case v.abbreviated:
		if len(v.prefix)+len(v.suffix) < 4 {
			return
		}
		if !c.src.hasAbbreviation(v.prefix, v.suffix) {
			c.errorf(n, KindAbbreviation, v.text, "no value in the sources starts with %s and ends with %q", "0x"+v.prefix, v.suffix)
		}
	case len(v.prefix) == 40:
		if !c.src.hasHex(v.prefix) {
			c.errorf(n, KindAddress, v.text, "not in any source")
		}
	case len(v.prefix) == 64:
		if !c.src.hasHex(v.prefix) {
			c.errorf(n, KindHash, v.text, "not in any source")
		}
	case len(v.prefix) >= 8:
		if !c.src.hasHex(v.prefix) {
			c.errorf(n, KindHex, v.text, "not in any source")
		}
	}
}

// checkPrecedent checks that a request ID cited as a precedent is a ruling of
// the corpus, with the outcome that the report gives it, if any.
func (c *checker) checkPrecedent(n int, v hexValue, outcome string) {
	ids := c.rulings.match(v)
	switch {
	case len(ids) == 0 && v.abbreviated && outcome == "" && c.src.matchAbbreviation(v.prefix, v.suffix, 40) != "":
		// An address that a precedent's description mentions, shortened.
		return
	case len(ids) == 0:
		c.errorf(n, KindPrecedent, v.text, "no other ruling in the corpus has this request ID")
		return
	case len(ids) > 1:
		c.warnf(n, KindPrecedent, v.text, "%d rulings in the corpus match it; give more of the request ID", len(ids))
		return
	}
	if outcome == "" {
		return
	}
	got, err := c.rulings.outcome(ids[0])
	if err != nil {
		c.errorf(n, KindOutcome, v.text, "reading its ruling: %v", err)
		return
	}
	if got != outcome {
		c.errorf(n, KindOutcome, v.text, "the report gives it the outcome %s, but its ruling's is %s", outcome, got)
	}
}

// checkQuotation checks that the blockquote whose lines, without their ">",
// start at line n is in the Charter verbatim, ignoring Markdown blockquote,
// list, and heading markers and whitespace. An ellipsis leaves text out, so
// each part around it must be in the Charter.
func (c *checker) checkQuotation(n int, lines []string) {
	quote := normalizeQuote(lines)
	if quote == "" {
		return
	}
	value := shorten(quote)
	if !c.hasCharter {
		c.errorf(n, KindQuotation, value, "there is no %s to check it against", CharterFile)
		return
	}
	if parts := splitEllipsis(quote); len(parts) > 1 {
		for _, part := range parts {
			if !strings.Contains(c.charter, part) {
				c.errorf(n, KindQuotation, shorten(part), "not in %s", CharterFile)
				return
			}
		}
		return
	}
	if strings.Contains(c.charter, quote) {
		return
	}
	// Find the line that differs, so that it can be fixed.
	for i, line := range lines {
		part := normalizeQuote([]string{line})
		if part == "" {
			continue
		}
		for _, p := range splitEllipsis(part) {
			if !strings.Contains(c.charter, p) {
				c.errorf(n+i, KindQuotation, shorten(p), "not in %s", CharterFile)
				return
			}
		}
	}
	c.warnf(n, KindQuotation, value, "each line is in %s, but not together in this order", CharterFile)
}

// listMarker matches the Markdown markers that a line of a blockquote or of the
// Charter can start with: blockquote markers, a heading, and a list item.
var listMarker = regexp.MustCompile(`^(?:\s*>)*\s*(?:#{1,6}\s+|[-*+]\s+|\d+[.)]\s+)?`)

// normalizeQuote joins lines without their Markdown markers, with single spaces
// between words.
func normalizeQuote(lines []string) string {
	var words []string
	for _, line := range lines {
		words = append(words, strings.Fields(listMarker.ReplaceAllString(line, ""))...)
	}
	return strings.Join(words, " ")
}

// ellipsis matches an ellipsis that leaves text out of a quotation.
var ellipsis = regexp.MustCompile(`\s*(?:\[…\]|\[\.\.\.\]|…|\.\.\.)\s*`)

// splitEllipsis splits a quotation at the ellipses that leave text out.
func splitEllipsis(quote string) []string {
	var parts []string
	for _, part := range ellipsis.Split(quote, -1) {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// shorten shortens a quotation for a finding.
func shorten(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:77]) + "..."
	}
	return s
}

// checkSummary checks the evidence summary, if the directory has one: that it
// has the fields that the evidence skill defines, and that a summary that asks
// for human review has a report that says so.
func (c *checker) checkSummary(dir, report string) error {
	data, err := os.ReadFile(filepath.Join(dir, SummaryFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	problem := func(format string, args ...any) {
		c.result.Errors = append(c.result.Errors, Finding{File: SummaryFile, Kind: KindSummary, Value: SummaryFile, Message: fmt.Sprintf(format, args...)})
	}
	// Reject unknown fields, so that a misspelled field, such as human_review,
	// fails rather than being left out.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var summary Summary
	if err := decoder.Decode(&summary); err != nil {
		problem("not an evidence summary: %v", err)
		return nil
	}
	for _, p := range summary.Problems() {
		problem("%s", p)
	}
	if c.file == ReportFile && summary.HumanReview && !humanReview.MatchString(report) {
		c.result.Errors = append(c.result.Errors, Finding{
			File: ReportFile, Kind: KindSummary, Value: "humanReview",
			Message: "the evidence summary asks for human review, but the report doesn't say that it is required",
		})
	}
	return nil
}

// humanReview matches the line of a report that says that it needs human
// review, as the template has it.
var humanReview = regexp.MustCompile(`(?i)human review\**:?\**\s*required`)

// rulings are the rulings of the corpus, by request ID.
type rulings struct {
	dir string
	// ids are the request IDs of the rulings, as lowercase hex digits without 0x,
	// in order.
	ids      []string
	outcomes map[string]string
}

// corpusFile matches the name of a corpus file.
var corpusFile = regexp.MustCompile(`^0x([0-9a-fA-F]{64})\.md$`)

// readCorpus lists the rulings of the corpus in dir, other than the one on the
// request with the ID own. A missing corpus has no rulings.
func readCorpus(dir, own string) (*rulings, error) {
	r := &rulings{dir: dir, outcomes: make(map[string]string)}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	} else if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if m := corpusFile.FindStringSubmatch(entry.Name()); m != nil && strings.ToLower(m[1]) != own {
			r.ids = append(r.ids, strings.ToLower(m[1]))
		}
	}
	slices.Sort(r.ids)
	return r, nil
}

// match returns the request IDs of the rulings that a hex value is or
// abbreviates.
func (r *rulings) match(v hexValue) []string {
	var ids []string
	for _, id := range r.ids {
		if v.abbreviated && strings.HasPrefix(id, v.prefix) && strings.HasSuffix(id, v.suffix) ||
			!v.abbreviated && id == v.prefix {
			ids = append(ids, id)
		}
	}
	return ids
}

// cited adds the values of the rulings that text names, by their request IDs or
// an abbreviation of one that names no other ruling, to src, and returns the
// paths of their files.
func (r *rulings) cited(text string, src *sources) ([]string, error) {
	paths := []string{}
	added := make(map[string]bool)
	for _, v := range scanHex(text) {
		if !v.requestID() {
			continue
		}
		ids := r.match(v)
		if len(ids) != 1 || added[ids[0]] {
			continue
		}
		added[ids[0]] = true
		path := filepath.Join(r.dir, "0x"+ids[0]+".md")
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		src.add(string(content))
		paths = append(paths, path)
	}
	return paths, nil
}

// outcome returns the outcome in the header of the ruling on request id.
func (r *rulings) outcome(id string) (string, error) {
	if outcome, ok := r.outcomes[id]; ok {
		return outcome, nil
	}
	content, err := os.ReadFile(filepath.Join(r.dir, "0x"+id+".md"))
	if err != nil {
		return "", err
	}
	header, err := corpus.Header(content)
	if err != nil {
		return "", err
	}
	for _, tag := range header {
		if tag.Key == "outcome" {
			r.outcomes[id] = tag.Value
			return tag.Value, nil
		}
	}
	return "", errors.New("its header has no outcome")
}
