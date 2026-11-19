package vector

import (
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// classified returns a fake result of arbot classify.
func classified(verdict, rule string) *Classification {
	if verdict == "" {
		return &Classification{}
	}
	return &Classification{Verdict: &verdict, Rule: rule}
}

func TestCompare(t *testing.T) {
	insecure43 := &safenet.Vector{Verdict: Insecure, Rule: "R-4.3"}
	eoa43 := &safenet.Vector{Verdict: Insecure, Rule: "R-4.3", EOA: true}
	secure := &safenet.Vector{Verdict: Secure}
	for _, test := range []struct {
		name    string
		vector  *safenet.Vector
		covered bool
		c       *Classification
		want    Outcome
	}{
		{"insecure, same rule", insecure43, true, classified(Insecure, "R-4.3"), Correct},
		{"secure, secure", secure, true, classified(Secure, ""), Correct},
		{"unclassified", insecure43, true, classified("", ""), Unclassified},
		{"insecure found secure", insecure43, true, classified(Secure, ""), Wrong},
		{"secure found insecure", secure, true, classified(Insecure, "R-4.1"), Wrong},
		{"insecure under another rule", insecure43, true, classified(Insecure, "R-4.1"), OtherRule},
		{"EOA, R-4.0", eoa43, true, classified(Insecure, "R-4.0"), EOAIgnored},
		// An EOA vector found secure is still wrong: no Safe check is ignored then.
		{"EOA, secure", eoa43, true, classified(Secure, ""), Wrong},
		{"off network, secure vector", secure, false, classified(Insecure, "R-4.0"), OffNetwork},
		{"off network, insecure vector", insecure43, false, classified(Insecure, "R-4.0"), OffNetwork},
		// R-4.0 on a covered network for a Safe vector is a finding like any other.
		{"R-4.0 on a covered network", secure, true, classified(Insecure, "R-4.0"), Wrong},
		{"R-4.0 vector", &safenet.Vector{Verdict: Insecure, Rule: "R-4.0", EOA: true}, true, classified(Insecure, "R-4.0"), Correct},
		{"classify failed", secure, true, nil, Failed},
	} {
		if got := Compare(test.vector, test.covered, test.c); got != test.want {
			t.Errorf("%s: got %s, want %s", test.name, got, test.want)
		}
	}
}

func TestReport(t *testing.T) {
	results := []Result{
		{Name: "safenet/stake", ChainID: 1, Verdict: Secure, Classification: classified(Secure, ""), Outcome: Correct, Commands: []Command{
			{Line: "arbot classify -json -request-file r.json", Status: CommandOK},
			{Line: "arbot calls -json -request-file r.json", Status: CommandOK},
		}},
		{Name: "address-poisoning/eoa", ChainID: 1, Verdict: Insecure, Rule: "R-4.3", EOA: true, Classification: classified(Insecure, "R-4.0"), Outcome: EOAIgnored, Commands: []Command{
			{Line: "arbot safe -json -request-file r.json", Status: CommandFailed, Error: "arbot: no code | not a `Safe`"},
			{Line: "arbot status -json -request-file <request.json>", Status: CommandSkipped, Error: "it needs the oracle request"},
		}},
		{Name: "transfers/lookalike", ChainID: 1, Verdict: Insecure, Rule: "R-4.3", Classification: classified("", ""), Outcome: Unclassified, Commands: []Command{
			{Line: "arbot executions -all -json -request-file r.json", Status: CommandFailed, NodeError: true, Error: "arbot: eth_getLogs: HTTP 429"},
		}},
		{Name: "settings-change/other-rule", ChainID: 1, Verdict: Insecure, Rule: "R-4.2", Classification: classified(Insecure, "R-4.1"), Outcome: OtherRule},
		{Name: "address-poisoning/bnb", ChainID: 56, Verdict: Secure, Classification: classified(Insecure, "R-4.0"), Outcome: OffNetwork},
		{Name: "safenet/failed", ChainID: 1, Verdict: Secure, Outcome: Failed, Commands: []Command{
			{Line: "arbot classify -json -request-file r.json", Status: CommandFailed, NodeError: true, Error: "rate limited"},
		}},
	}
	s := Summarize(results)
	want := Summary{
		Vectors: 6, Correct: 1, Wrong: 0, Agents: 3, EOAIgnored: 1, OtherRule: 1, OffNetwork: 1, Failed: 1, NodeErrors: 2,
		CommandsOK: 2, CommandsFailed: 3, CommandsSkipped: 1,
	}
	if s != want {
		t.Errorf("Summarize: got %+v, want %+v", s, want)
	}

	md := Markdown(results)
	for _, line := range []string{
		"- Decided correctly by the deterministic checks: 1.",
		"- Decided wrongly: 0.",
		"- Left to the agents: 3 (1 with the R-4.0 finding of an EOA transaction ignored, 1 insecure under another rule than the vector's).",
		"- With a command that hit a node or service error: 2.",
		"| `safenet/stake` | 1 | secure | secure | correct | 0 of 2 | no |",
		"| `address-poisoning/eoa` | 1 | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored | 1 of 2 | no |",
		"| `transfers/lookalike` | 1 | insecure R-4.3 | unclassified | unclassified | 1 of 1 | yes |",
		"| `address-poisoning/bnb` | 56 | secure | insecure R-4.0 | off-network | 0 of 0 | no |",
		"| `safenet/failed` | 1 | secure | classify failed | failed | 1 of 1 | yes |",
		// An error is on one line, with its pipes and backticks escaped.
		"- failed: `arbot safe -json -request-file r.json`: arbot: no code \\| not a 'Safe'",
		"- failed (node or service error): `arbot executions -all -json -request-file r.json`: arbot: eth_getLogs: HTTP 429",
		"- skipped: `arbot status -json -request-file <request.json>`: it needs the oracle request",
	} {
		if !strings.Contains(md, line+"\n") {
			t.Errorf("Markdown: no line %q in\n%s", line, md)
		}
	}
	if strings.Contains(md, "### `safenet/stake`") {
		t.Errorf("Markdown: a vector whose commands all succeeded has a section of failures:\n%s", md)
	}
}
