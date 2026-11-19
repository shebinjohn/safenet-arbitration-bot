package reportcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// saved is a saved arbitration of request 0xa049284bbb54…, as its agents and
// arbot wrote it: the report, the request, the evidence, the cases and
// judgments, and the Charter.
const saved = "testdata/a049284bbb54"

// repoCorpus is the repository's rulings corpus, whose files never change once
// arbot index writes them, so the precedents that the saved report cites stay
// in it.
const repoCorpus = "../../corpus"

// brief is a finding without its message, to compare.
type brief struct {
	line  int
	kind  Kind
	value string
}

func briefs(findings []Finding) []brief {
	var b []brief
	for _, f := range findings {
		b = append(b, brief{f.Line, f.Kind, f.Value})
	}
	return b
}

func TestCheckSavedReport(t *testing.T) {
	result, err := Check(saved, repoCorpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Errorf("got errors %v, want none", result.Errors)
	}
	// The report quotes the first rule of § 3.8 and then its next heading and list,
	// leaving out the line between them without an ellipsis, so each line is in the
	// Charter, but the quotation isn't.
	want := []brief{{94, KindQuotation, "If admissible evidence is genuinely evenly balanced on a material security qu..."}}
	if got := briefs(result.Warnings); !slices.Equal(got, want) {
		t.Errorf("got warnings %v, want %v", result.Warnings, want)
	}
	wantSources := []string{"request.json", "evidence.md", "case-2.md", "case-3.md", "case-6.md", "case-7.md",
		"judgment-3.md", "judgment-6.md", "judgment-7.md", "charter.md"}
	if !slices.Equal(result.Sources, wantSources) {
		t.Errorf("got sources %v, want %v", result.Sources, wantSources)
	}
	// A case is checked against the evidence, without the other cases.
	result, err = CheckFile(saved, "case-7.md", repoCorpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 || len(result.Warnings) != 0 || !slices.Equal(result.Sources, []string{"request.json", "evidence.md", "charter.md"}) {
		t.Errorf("case-7.md: got %+v, want no findings, against the request, the evidence, and the Charter", result)
	}
	result, err = Check(saved, repoCorpus)
	if err != nil {
		t.Fatal(err)
	}

	// The rulings that the report names, in the order that it first names them,
	// without the request's own, which the corpus has since the Council ruled.
	var rulings []string
	for _, id := range []string{
		"0xce3b54488bb44619c7b5d6deee37837b0c0a7d3c3e5a6ce47c1bd7f6bf8fc41e",
		"0x586a2ed7fc5cbbe9043a34fb9d9d81a7f46b66740a84767c8515b782b97cf6eb",
		"0x6652ef59f0d6407810cb3e7b9baead7722e2cfa4daa6f27a2dfc22b5eb58912b",
		"0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31e",
		"0x844fe527e82fbf88b70e05db4c487de8143ca5d3b97e2eca24f6348c4d894434",
		"0x077791fe52b006b7440aaccb4240ee043a64862aa4dc299632905559541e08be",
		"0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3",
		"0xd0040a42d9ff1cb18e53ccb27acf7e79022eb36f052ee0d731c05db040c675a9",
		"0x5d05c97126068fef0018afdb623304f6501e7e954dea42c0b8ec8aba8faa957d",
	} {
		rulings = append(rulings, filepath.Join(repoCorpus, id+".md"))
	}
	if !slices.Equal(result.Rulings, rulings) {
		t.Errorf("got rulings %v, want %v", result.Rulings, rulings)
	}
}

// copyDir copies the files of the arbitration directory src to a temporary
// directory, with the edits applied to its report, each of which must apply.
func copyDir(t *testing.T, src string, edits ...string) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name() == ReportFile {
			report := string(content)
			for i := 0; i < len(edits); i += 2 {
				if !strings.Contains(report, edits[i]) {
					t.Fatalf("the report has no %q", edits[i])
				}
				report = strings.Replace(report, edits[i], edits[i+1], 1)
			}
			content = []byte(report)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckPlantedErrors(t *testing.T) {
	dir := copyDir(t, saved,
		// A Safe transaction hash with its last digit wrong, as in 3d837c9edcb8.
		"`0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00f`, nonce 17",
		"`0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00e`, nonce 17",
		// A number that no source has, as the data length 4,346 in a1d41dbda94d.
		"| Value | 0 wei |", "| Value | 4,346 wei |",
		// A Charter line with a word added.
		"> - A transaction is insecure if it sends value to a recipient",
		"> - A transaction is insecure if it sends value to any recipient",
		// A precedent that isn't in the corpus.
		"`0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31e` (insecure)",
		"`0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31f` (insecure)",
		// A precedent with the wrong outcome.
		"`0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3` (secure)",
		"`0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3` (insecure)",
		// Numbers that the report computed, marked as derived.
		"## Recusals",
		"- The scans start about 87,778 blocks before the Safe's creation.\n"+
			"- Its first execution is 87,854 blocks after the start of the scans (derived).\n\n## Recusals",
	)
	result, err := Check(dir, repoCorpus)
	if err != nil {
		t.Fatal(err)
	}
	wantErrors := []brief{
		{22, KindHash, "0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00e"},
		{37, KindNumber, "4,346"},
		{50, KindQuotation, "A transaction is insecure if it sends value to any recipient address outside ..."},
		{162, KindPrecedent, "0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31f"},
		{164, KindOutcome, "0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3"},
	}
	if got := briefs(result.Errors); !slices.Equal(got, wantErrors) {
		t.Errorf("got errors %v, want %v", result.Errors, wantErrors)
	}
	wantWarnings := []brief{
		{94, KindQuotation, "If admissible evidence is genuinely evenly balanced on a material security qu..."},
		{195, KindNumber, "87,778"},
		{196, KindNumber, "87,854"},
	}
	if got := briefs(result.Warnings); !slices.Equal(got, wantWarnings) {
		t.Errorf("got warnings %v, want %v", result.Warnings, wantWarnings)
	}
	for _, f := range result.Errors {
		if f.Kind == KindOutcome && !strings.Contains(f.Message, "outcome insecure, but its ruling's is secure") {
			t.Errorf("outcome finding %q doesn't name both outcomes", f.Message)
		}
	}
}

// writeFiles writes the files to a temporary directory, and returns it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const (
	precedentA = "0xaaaa000000000000000000000000000000000000000000000000000000001111"
	precedentB = "0xbbbb000000000000000000000000000000000000000000000000000000002222"
	ownRequest = "0xdddd000000000000000000000000000000000000000000000000000000004444"
)

// corpusContent returns a corpus file with the header of a ruling.
func corpusContent(id, outcome string) string {
	return fmt.Sprintf("---\nrequest: %s\noutcome: %s\nrules: R-4.3\nchain: 1\nsafe: 0x0000000000000000000000000000000000000001\n"+
		"charter: -\nproposed: 2026-11-10T00:00:00Z\nrecord: -\n---\n\n<!-- DOCUMENT START -->\n---\n\nRuling.\n", id, outcome)
}

func TestCheckValues(t *testing.T) {
	corpusDir := writeFiles(t, map[string]string{
		precedentA + ".md": corpusContent(precedentA, "insecure"),
		precedentB + ".md": corpusContent(precedentB, "secure"),
		ownRequest + ".md": corpusContent(ownRequest, "insecure"),
		"index.json":       `{"block": 1}`,
	})
	dir := writeFiles(t, map[string]string{
		"request.json": `{"id": "` + ownRequest + `", "safe": "0x1235000000000000000000000000000000004306", "safeBlock": 26381211, "time": "2026-11-10T01:26:25Z",` +
			` "data": "0xa9059cbb000000000000000000000000c01ea83bacda63097627f45d987684569fdceae5", "fee": 400000}`,
		"evidence.md": "Topic `0x1714865b…17ce`, 10,000,000 units, Safe version 1.4.1, chain ID 42161.\n",
		"charter.md":  "### R-4.3\n\n#### Rule\n\n- A transaction is insecure if it sends value.\n- Second line.\n",
		"report.md": strings.Join([]string{
			/* 1 */ "> **Draft.** A notice that isn't a quotation: 0x1235000000000000000000000000000000004306.",
			/* 2 */ "## Request",
			/* 3 */ "Safe `0x1235…4306` in block 26,381,211 at 2026-11-10T01:26:25Z, proposed November 10, 2026, on 2026-11-10.",
			/* 4 */ "The recipient 0xc01ea83bacda63097627f45d987684569fdceae5, called with selector `0xa9059cbb`, data `0xa9059cbb0000…`.",
			/* 5 */ "R-4.3, ERC-20, EIP-712, Safe 1.4.1 and 1.5.0, chain 42161, 10000000 units, fee 400,000, topic `0x1714865b…17ce`.",
			/* 6 */ "Wrong: `0x1235…4307`, block 26381212, at 2026-11-10T01:26:26Z, and `0xdeadbeef`.",
			/* 7 */ "About 2.19 million blocks, about 12,345 blocks, ~ 23,456 blocks, nearly 34,567 blocks, and 45,678 blocks.",
			/* 8 */ "> - A transaction is insecure if it sends value.",
			/* 9 */ "> - Second line.",
			/* 10 */ "",
			/* 11 */ "> A transaction is insecure … Second line.",
			/* 12 */ "",
			/* 13 */ "> Second line.",
			/* 14 */ "> - A transaction is insecure if it sends value.",
			/* 15 */ "",
			/* 16 */ "> A transaction is secure.",
			/* 17 */ "```text",
			/* 18 */ "> not a quotation, but 0x9999999999999999999999999999999999999999 is checked",
			/* 19 */ "precedents: " + precedentA + ", 0xcccc…3333",
			/* 20 */ "```",
			/* 21 */ "Following `0xaaaa0000…1111` and `0xbbbb…2222` (insecure), and `0xbbbb…2222` (secure, R-4.3); the transaction is insecure. Distinguished: `0xaaaa…1111` (owner recipient).",
			/* 22 */ "## Precedents",
			/* 23 */ "- `" + precedentB + "` (out of scope): distinguished, since its recipient `0x1235…4306` was an owner.",
			/* 24 */ "- `0xabab…` (secure): applied.",
			/* 25 */ "- `0xdddd…4444` (insecure): the request's own ruling isn't a precedent.",
		}, "\n") + "\n",
	})
	result, err := Check(dir, corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	wantErrors := []brief{
		{6, KindAbbreviation, "0x1235…4307"},
		{6, KindHex, "0xdeadbeef"},
		{6, KindTime, "2026-11-10T01:26:26Z"},
		{6, KindNumber, "26381212"},
		{7, KindNumber, "45,678"},
		{16, KindQuotation, "A transaction is secure."},
		{18, KindAddress, "0x9999999999999999999999999999999999999999"},
		{19, KindPrecedent, "0xcccc…3333"},
		// Both request IDs before "(insecure)" have it, and 0xbbbb…2222 is secure.
		{21, KindOutcome, "0xbbbb…2222"},
		{23, KindOutcome, precedentB},
		{24, KindPrecedent, "0xabab…"},
		{25, KindPrecedent, "0xdddd…4444"},
	}
	if got := briefs(result.Errors); !slices.Equal(got, wantErrors) {
		t.Errorf("got errors\n%v\nwant\n%v", got, wantErrors)
	}
	wantWarnings := []brief{
		{7, KindNumber, "12,345"},
		{7, KindNumber, "23,456"},
		{7, KindNumber, "34,567"},
		{13, KindQuotation, "Second line. A transaction is insecure if it sends value."},
	}
	if got := briefs(result.Warnings); !slices.Equal(got, wantWarnings) {
		t.Errorf("got warnings\n%v\nwant\n%v", got, wantWarnings)
	}
}

func TestCheckMissingFiles(t *testing.T) {
	if _, err := Check(t.TempDir(), repoCorpus); err == nil {
		t.Error("a directory without a report: got no error")
	}
	if _, err := Check(writeFiles(t, map[string]string{"report.md": "# Report\n"}), repoCorpus); err == nil {
		t.Error("a directory without a request: got no error")
	}

	// Without a Charter, a quotation can't be checked.
	dir := writeFiles(t, map[string]string{"report.md": "## Provisions\n\n> A rule.\n", "request.json": "{}"})
	result, err := Check(dir, filepath.Join(dir, "no-corpus"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []brief{{3, KindQuotation, "A rule."}}; !slices.Equal(briefs(result.Errors), want) {
		t.Errorf("got errors %v, want %v", result.Errors, want)
	}
}

// summary is a valid evidence summary, in which %s is replaced.
const summary = `{
  "request": "0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27",
  "disputedRules": ["R-4.3"],
  "playbooks": ["proposal-status", "value-recipient"],
  "facts": [
    {"id": "F1", "fact": "The recipient has no code.", "rules": ["R-4.3", "§ 2.4"],
     "source": {"kind": "onchain", "command": "arbot account -json -request-file request.json 0xc01e…"},
     "chainId": 1, "block": 26381211, "blockTime": "2026-11-10T01:26:23Z"},
    {"id": "F2", "fact": "The Safe executed nonces 0 to 16.", "rules": ["§ 2.4"],
     "source": {"kind": "offchain", "command": "arbot executions -all -json -request-file request.json",
       "url": "https://api.safe.global/tx-service/eth", "observedAt": "2026-11-10T10:08:01Z",
       "reliability": "each execution confirmed onchain"},
     "chainId": 1, "block": 26381211, "blockTime": "2026-11-10T01:26:23Z"},
    {"id": "F3", "fact": "The scans start 87,777 blocks before the creation.", "rules": ["§ 2.4"],
     "source": {"kind": "derived", "command": "", "from": ["F1", "F2"]}, "chainId": 1, "block": null, "blockTime": ""}
  ],
  "gaps": [{"id": "G1", "fact": "Transfers before block 25381212", "rules": ["§ 2.4"], "reason": "not scanned",
            "decisive": %s, "command": "arbot transfers -all -in -json -request-file request.json"}],
  "conflicts": [],
  "uncertainty": "%s",
  "uncertaintyReason": "absences only",
  "suggestedRuling": {"suggestion": true, "verdict": "insecure", "rules": ["R-4.3"], "charter": ["§ 2.4", "§ 3.8"],
                      "reasoning": "No link places the recipient inside the set."},
  "humanReview": %s,
  "humanReviewReasons": %s
}`

func TestCheckSummary(t *testing.T) {
	tests := []struct {
		name                                   string
		decisive, uncertainty, review, reasons string
		report                                 string
		want                                   []string
	}{
		{"valid", "false", "medium", "false", "[]", "# Report\n", nil},
		{"review stated", "true", "high", "true", `["a decisive gap"]`, "- **Human review**: required, because a gap is decisive.\n", nil},
		{"review not stated", "true", "high", "true", `["a decisive gap"]`, "# Report\n",
			[]string{"report.md: the evidence summary asks for human review, but the report doesn't say that it is required"}},
		{"review missing", "true", "low", "false", "[]", "# Report\n",
			[]string{"evidence-summary.json: humanReview must be true, since the uncertainty is high or a decisive gap is open"}},
		{"no reasons", "false", "high", "true", "[]", "Human review: required.\n",
			[]string{"evidence-summary.json: humanReview is true, but it gives no reasons"}},
		{"bad uncertainty", "false", "unknown", "false", "[]", "# Report\n",
			[]string{`evidence-summary.json: its uncertainty is "unknown", not low, medium, or high`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{
				"report.md":    test.report,
				"request.json": "{}",
				SummaryFile:    fmt.Sprintf(summary, test.decisive, test.uncertainty, test.review, test.reasons),
			})
			result, err := Check(dir, repoCorpus)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range result.Errors {
				got = append(got, f.File+": "+f.Message)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("got errors %q, want %q", got, test.want)
			}
		})
	}

	// A misspelled field fails, rather than being left out.
	dir := writeFiles(t, map[string]string{
		"report.md":    "# Report\n",
		"request.json": "{}",
		SummaryFile:    strings.Replace(fmt.Sprintf(summary, "false", "low", "false", "[]"), `"humanReview"`, `"human_review"`, 1),
	})
	result, err := Check(dir, repoCorpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0].Message, `unknown field "human_review"`) {
		t.Errorf("a misspelled field: got errors %v, want one for the unknown field", result.Errors)
	}
}

func TestSummaryProblems(t *testing.T) {
	var s Summary
	got := s.Problems()
	for _, want := range []string{"it has no request", "it has no facts", "it has no gaps array; write [] if there are none", "it has no suggested ruling"} {
		if !slices.Contains(got, want) {
			t.Errorf("an empty summary: got problems %q, want %q among them", got, want)
		}
	}

	block := uint64(1)
	s = Summary{
		Request:     "0x01",
		Facts:       []Fact{{ID: "F1", Fact: "x", Rules: []string{"R-4.3"}, Source: Source{Kind: "offchain", Command: "arbot executions -all"}, Block: &block}, {ID: "F1", Fact: "y", Rules: []string{"R-4.3"}, Source: Source{Kind: "derived", From: []string{"F9"}}}},
		Gaps:        []Gap{},
		Conflicts:   []Conflict{{Facts: []string{"F7"}, Description: "differ"}},
		Uncertainty: "low",
		SuggestedRuling: &SuggestedRuling{
			Verdict: new("unknown"),
			Charter: []string{"§ 3.8"},
		},
	}
	want := []string{
		"fact F1: an offchain source needs its command, URL, and observation time (§ 3.4)",
		"fact F1: its ID is used twice",
		`fact 2: it is derived from "F9", which no fact has as its ID`,
		`conflict 1: no fact has the ID "F7"`,
		"its suggested ruling isn't marked as a suggestion, which it always is",
		`its suggested verdict is "unknown", not secure, insecure, or null`,
	}
	if got := s.Problems(); !slices.Equal(got, want) {
		t.Errorf("got problems\n%q\nwant\n%q", got, want)
	}
}
