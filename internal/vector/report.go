package vector

import (
	"fmt"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// Classification is the deterministic result for a vector, as arbot classify
// -json writes it: a verdict, or null for an unclassified request, and the rule
// and description of the classification that decided it.
type Classification struct {
	Verdict     *string `json:"verdict"`
	Rule        string  `json:"rule,omitempty"`
	Description string  `json:"description,omitempty"`
}

// String returns the classification as arbot classify prints it.
func (c *Classification) String() string {
	if c.Verdict == nil {
		return "unclassified"
	}
	return strings.Join(strings.Fields(strings.Join([]string{*c.Verdict, c.Rule}, " ")), " ")
}

// Outcome is how a vector's deterministic result compares with the verdict and
// rule that the vector expects.
type Outcome string

// The outcomes of a vector.
const (
	// Correct means that the checks decided the vector's verdict, and for an
	// insecure vector, under its rule.
	Correct Outcome = "correct"
	// Wrong means that the checks decided the other verdict: a secure vector that
	// they find insecure, or an insecure one that they find secure. No vector may
	// have it.
	Wrong Outcome = "wrong"
	// Unclassified means that the checks didn't decide the vector, so the agents
	// judge it with the evidence.
	Unclassified Outcome = "unclassified"
	// OtherRule means that the checks found an insecure vector insecure under
	// another rule than its own. The arbitrate skill uses an insecure
	// classification only if its rule is disputed, and the vector's rule is the
	// only one disputed, so the agents judge it, as for Unclassified.
	OtherRule Outcome = "other-rule"
	// EOAIgnored means that the checks found the vector of an EOA transaction
	// insecure under R-4.0, since no Safe exists at its Safe address. That is what
	// R-4.0 says of the transaction as the vector writes it, but not the question
	// that the vector asks, so the finding is ignored, and the agents judge the
	// vector's rule, as for Unclassified.
	EOAIgnored Outcome = "eoa-ignored"
	// OffNetwork means that the vector is on a network that Article I of the
	// Charter doesn't list, where R-4.0 finds every transaction insecure and no
	// other rule applies, so the checks find it insecure under R-4.0, whatever the
	// vector expects. The vectors are for sentinels on any network, so the vector
	// and the Charter disagree, which is neither right nor wrong for the checks.
	OffNetwork Outcome = "off-network"
	// Failed means that arbot classify failed, so there is no result to compare.
	Failed Outcome = "failed"
)

// Compare returns the outcome of a vector, given its deterministic result c, or
// nil if arbot classify failed, and whether the Charter's rules other than
// R-4.0 apply on its network (Article I).
func Compare(v *safenet.Vector, covered bool, c *Classification) Outcome {
	switch {
	case c == nil:
		return Failed
	case c.Verdict == nil:
		return Unclassified
	case *c.Verdict == Insecure && c.Rule == "R-4.0" && v.Rule != "R-4.0" && !covered:
		return OffNetwork
	case *c.Verdict == Insecure && c.Rule == "R-4.0" && v.Rule != "R-4.0" && v.EOA:
		return EOAIgnored
	case *c.Verdict != v.Verdict:
		return Wrong
	case *c.Verdict == Insecure && c.Rule != v.Rule:
		return OtherRule
	default:
		return Correct
	}
}

// ToAgents reports whether a vector with the outcome is left to the agents,
// which judge it with the evidence of the playbooks.
func (o Outcome) ToAgents() bool {
	return o == Unclassified || o == OtherRule || o == EOAIgnored
}

// The statuses of a command that the harness runs.
const (
	CommandOK      = "ok"
	CommandFailed  = "failed"
	CommandSkipped = "skipped"
)

// Command is an arbot command that the harness ran for a vector, or skipped.
type Command struct {
	// Playbook is the playbook that the command is from, or "" for the commands
	// that the harness runs on every vector, such as arbot classify.
	Playbook string `json:"playbook"`
	// Line is the command line, with its placeholders filled in, or as the playbook
	// has it for a skipped command.
	Line   string `json:"line"`
	Status string `json:"status"`
	// NodeError is whether the command failed because a request to a node or a
	// service failed rather than being answered, after the harness ran it again, or
	// because the node doesn't keep the old state that it read.
	NodeError bool `json:"nodeError"`
	// Error is the error of a failed command, or the reason that a command was
	// skipped.
	Error string `json:"error,omitempty"`
	// Warnings are the lines that a command that succeeded wrote to stderr, such as
	// about a transfer that the logs don't have, which are gaps.
	Warnings []string `json:"warnings,omitempty"`
	// Output is the name of the file that the harness wrote the command's output
	// to, in the vector's directory, or "" for a skipped command.
	Output string `json:"output,omitempty"`
}

// Result is the result of a vector.
type Result struct {
	// Name is the vector's group and case, such as
	// "address-poisoning/usdt-100k-near-match".
	Name    string `json:"name"`
	Path    string `json:"path"`
	ChainID uint64 `json:"chainId"`
	Block   uint64 `json:"block"`
	Verdict string `json:"verdict"`
	Rule    string `json:"rule"`
	EOA     bool   `json:"eoa"`
	// Classification is the deterministic result, or nil if arbot classify failed.
	Classification *Classification `json:"classification"`
	Outcome        Outcome         `json:"outcome"`
	Playbooks      []string        `json:"playbooks"`
	Commands       []Command       `json:"commands"`
}

// NodeError reports whether a command of the result failed because a request to
// a node or a service failed.
func (r *Result) NodeError() bool {
	for _, c := range r.Commands {
		if c.NodeError {
			return true
		}
	}
	return false
}

// Summary counts the outcomes of the vectors.
type Summary struct {
	Vectors int `json:"vectors"`
	// Correct and Wrong are the vectors that the checks decided correctly and
	// wrongly; Wrong must be 0.
	Correct int `json:"correct"`
	Wrong   int `json:"wrong"`
	// Agents are the vectors left to the agents, of which EOAIgnored had an R-4.0
	// finding of an EOA transaction ignored, and OtherRule an insecure finding
	// under a rule that the vector doesn't cite.
	Agents     int `json:"agents"`
	EOAIgnored int `json:"eoaIgnored"`
	OtherRule  int `json:"otherRule"`
	// OffNetwork are the vectors on a network that Article I doesn't list, and
	// Failed those that arbot classify failed on.
	OffNetwork int `json:"offNetwork"`
	Failed     int `json:"failed"`
	// NodeErrors are the vectors with a command that failed because a request to a
	// node or a service failed.
	NodeErrors int `json:"nodeErrors"`
	// CommandsOK, CommandsFailed, and CommandsSkipped count the commands of every
	// vector.
	CommandsOK      int `json:"commandsOk"`
	CommandsFailed  int `json:"commandsFailed"`
	CommandsSkipped int `json:"commandsSkipped"`
}

// Summarize counts the outcomes of results.
func Summarize(results []Result) Summary {
	s := Summary{Vectors: len(results)}
	for _, r := range results {
		switch r.Outcome {
		case Correct:
			s.Correct++
		case Wrong:
			s.Wrong++
		case OffNetwork:
			s.OffNetwork++
		case Failed:
			s.Failed++
		}
		if r.Outcome.ToAgents() {
			s.Agents++
		}
		if r.Outcome == EOAIgnored {
			s.EOAIgnored++
		}
		if r.Outcome == OtherRule {
			s.OtherRule++
		}
		if r.NodeError() {
			s.NodeErrors++
		}
		for _, c := range r.Commands {
			switch c.Status {
			case CommandOK:
				s.CommandsOK++
			case CommandFailed:
				s.CommandsFailed++
			case CommandSkipped:
				s.CommandsSkipped++
			}
		}
	}
	return s
}

// Lines returns the summary as sentences, one per line.
func (s Summary) Lines() []string {
	return []string{
		fmt.Sprintf("%d vectors.", s.Vectors),
		fmt.Sprintf("Decided correctly by the deterministic checks: %d.", s.Correct),
		fmt.Sprintf("Decided wrongly: %d.", s.Wrong),
		fmt.Sprintf("Left to the agents: %d (%d with the R-4.0 finding of an EOA transaction ignored, %d insecure under another rule than the vector's).", s.Agents, s.EOAIgnored, s.OtherRule),
		fmt.Sprintf("On a network that Article I doesn't list, insecure under R-4.0 whatever the vector expects: %d.", s.OffNetwork),
		fmt.Sprintf("Not classified because arbot classify failed: %d.", s.Failed),
		fmt.Sprintf("With a command that hit a node or service error: %d.", s.NodeErrors),
		fmt.Sprintf("Commands: %d succeeded, %d failed, %d skipped.", s.CommandsOK, s.CommandsFailed, s.CommandsSkipped),
	}
}

// Markdown returns a report of results as Markdown: the summary, a table of the
// vectors, and the commands that failed or were skipped, with their errors.
func Markdown(results []Result) string {
	var b strings.Builder
	b.WriteString("# Sentinel test vectors\n\n## Summary\n\n")
	for _, line := range Summarize(results).Lines() {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString("\n## Vectors\n\n")
	b.WriteString("| Vector | Chain | Expected | Deterministic result | Outcome | Commands failed | Node errors |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, r := range results {
		expected := r.Verdict
		if r.Rule != "" {
			expected += " " + r.Rule
		}
		if r.EOA {
			expected += " (EOA)"
		}
		result := "classify failed"
		if r.Classification != nil {
			result = r.Classification.String()
		}
		failed := 0
		for _, c := range r.Commands {
			if c.Status == CommandFailed {
				failed++
			}
		}
		nodeErrors := "no"
		if r.NodeError() {
			nodeErrors = "yes"
		}
		fmt.Fprintf(&b, "| `%s` | %d | %s | %s | %s | %d of %d | %s |\n", r.Name, r.ChainID, expected, cell(result), r.Outcome, failed, len(r.Commands), nodeErrors)
	}
	b.WriteString("\n## Commands that failed or were skipped\n")
	any := false
	for _, r := range results {
		var lines []string
		for _, c := range r.Commands {
			if c.Status == CommandOK {
				continue
			}
			node := ""
			if c.NodeError {
				node = " (node or service error)"
			}
			lines = append(lines, fmt.Sprintf("- %s%s: `%s`: %s", c.Status, node, c.Line, cell(c.Error)))
		}
		if len(lines) == 0 {
			continue
		}
		any = true
		fmt.Fprintf(&b, "\n### `%s`\n\n%s\n", r.Name, strings.Join(lines, "\n"))
	}
	if !any {
		b.WriteString("\nNone.\n")
	}
	return b.String()
}

// cell returns text for a Markdown table cell or list item: on one line, with
// its pipes and backticks escaped, since an error can quote data from the
// chain.
func cell(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	text = strings.ReplaceAll(text, "|", `\|`)
	return strings.ReplaceAll(text, "`", "'")
}
