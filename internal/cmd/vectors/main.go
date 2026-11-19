// Command vectors runs arbot on the Sentinel test vectors of
// https://github.com/safe-research/sentinel-test-vectors: for each vector, it
// makes a request with arbot vector, classifies it with arbot classify, and
// runs the commands of the evidence playbooks that match the vector's rule,
// then reports which vectors the deterministic checks decide, whether any is
// decided wrongly, and which commands failed.
//
// Usage:
//
//	go run ./internal/cmd/vectors [-config file] [-out dir] [-run regexp [-keep]] [dir]
//
// The vectors are the spec files under dir/specs (default
// ../sentinel-test-vectors). The playbooks are those of skills/evidence, read
// from their Markdown, so that the harness runs the commands that the agents
// run: for an insecure vector, proposal-status and the playbooks of its rule,
// and for a secure vector, every playbook, since a secure verdict holds under
// every rule. It fills in a playbook's placeholders from the outputs of the
// commands before it, such as <recipient> from the ERC-20 transfers that arbot
// calls decodes, and skips a command whose placeholder it can't fill, or that
// needs the oracle request, which a vector doesn't have, such as arbot status.
//
// It runs arbot in-process, one command at a time, since public RPCs rate
// limit, and runs a command that fails because a request to a node failed
// again, after a wait. The commands read the vectors' chains at their blocks,
// so they need an archive node, such as the RPC URLs of the -config file.
//
// It writes each vector's request file and the output of each command to
// dir/<group>/<case>/ under -out (default .msgboard/vectors), and the report to
// vectors.md and vectors.json there.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/cli"
	"github.com/safe-research/safenet-arbitration-bot/internal/functions"
	"github.com/safe-research/safenet-arbitration-bot/internal/playbooks"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/vector"
)

// transientHint is what arbot writes after the error of a command that failed
// because a request to a node failed rather than being answered.
const transientHint = "a request to the node failed rather than being answered"

// serviceFailures are the HTTP statuses with which a public service, such as
// the Safe Transaction Service or Sourcify, fails a request rather than
// answering it, after httpretry retried it. arbot fails on them without saying
// that they are transient, so the harness runs the command again, as for a node
// that failed.
var serviceFailures = []string{"HTTP 429 Too Many Requests", "HTTP 502 Bad Gateway", "HTTP 503 Service Unavailable", "HTTP 504 Gateway Timeout"}

// prunedState are the errors with which nodes answer a request for a state that
// they no longer keep, such as one that isn't an archive node answers for an
// old block. arbot reports them as answers, but they say nothing about the
// chain, so the report counts them as node errors.
var prunedState = []string{"missing trie node", "header not found", "historical state", "state not available", "state is not available", "pruned"}

// oracleCommands are the commands that read a request's oracle fields, which a
// vector's request doesn't have.
var oracleCommands = []string{"status", "info", "charter"}

// maxValues is the most values that a placeholder is filled with, so that a
// batch with many calls doesn't run a command for each of its accounts.
const maxValues = 6

func main() {
	config := flag.String("config", "", "the arbot configuration file to run arbot with, such as one with the RPC URL of an archive node")
	out := flag.String("out", ".msgboard/vectors", "the directory to write the request files, the outputs, and the report to")
	skills := flag.String("skills", "skills/evidence", "the directory of the evidence skill, whose playbooks to run")
	keep := flag.Bool("keep", false, "keep the results of the vectors that -run doesn't match from the report already in -out, such as to run again only those that a rate limit failed")
	run := flag.String("run", "", "only run the vectors whose group and case, such as address-poisoning/usdt-100k-near-match, match this regular expression")
	retries := flag.Int("retries", 2, "how many times to run a command again that failed because a request to a node failed")
	wait := flag.Duration("wait", 30*time.Second, "how long to wait before running such a command again, doubling each time")
	timeout := flag.Duration("timeout", 10*time.Minute, "how long a command may run")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: go run ./internal/cmd/vectors [flags] [dir]\n\nRuns arbot on the Sentinel test vectors in dir (default ../sentinel-test-vectors).\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	dir := "../sentinel-test-vectors"
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	} else if flag.NArg() == 1 {
		dir = flag.Arg(0)
	}
	filter, err := regexp.Compile(*run)
	if err != nil {
		fatalf("-run: %v", err)
	}
	h := &harness{config: *config, retries: *retries, wait: *wait, timeout: *timeout, keep: *keep}
	if err := h.main(dir, *out, *skills, filter); err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "vectors: "+format+"\n", args...)
	os.Exit(1)
}

// harness runs arbot on vectors.
type harness struct {
	config  string
	retries int
	wait    time.Duration
	timeout time.Duration
	// keep is whether to keep the earlier results of the vectors that aren't run.
	keep bool
}

func (h *harness) main(dir, out, skills string, filter *regexp.Regexp) error {
	specs := filepath.Join(dir, "specs")
	if _, err := os.Stat(specs); err != nil {
		return fmt.Errorf("no vectors in %s (%v): clone them with `git clone https://github.com/safe-research/sentinel-test-vectors %s`", dir, err, dir)
	}
	books, err := playbooks.Load(skills)
	if err != nil {
		return err
	}
	var paths []string
	err = filepath.WalkDir(specs, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".json") {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		return err
	}
	slices.Sort(paths)

	earlier := map[string]vector.Result{}
	if h.keep {
		var report struct {
			Vectors []vector.Result `json:"vectors"`
		}
		data, err := os.ReadFile(filepath.Join(out, "vectors.json"))
		if err != nil {
			return fmt.Errorf("-keep: %w", err)
		}
		if err := json.Unmarshal(data, &report); err != nil {
			return fmt.Errorf("-keep: %w", err)
		}
		for _, r := range report.Vectors {
			earlier[r.Name] = r
		}
	}

	var results []vector.Result
	for _, path := range paths {
		rel, _ := filepath.Rel(specs, path)
		name := strings.TrimSuffix(filepath.ToSlash(rel), ".json")
		if !filter.MatchString(name) {
			if r, ok := earlier[name]; ok {
				results = append(results, r)
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "vectors: %s\n", name)
		r, err := h.vector(path, name, filepath.Join(out, filepath.FromSlash(name)), books)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		results = append(results, *r)
		// Write the report after each vector, so that an interrupted run keeps the
		// results so far.
		if err := writeReport(out, results); err != nil {
			return err
		}
	}
	for _, line := range vector.Summarize(results).Lines() {
		fmt.Println(line)
	}
	fmt.Printf("The report is in %s.\n", filepath.Join(out, "vectors.md"))
	return nil
}

// writeReport writes the report of results to vectors.md and vectors.json in
// out.
func writeReport(out string, results []vector.Result) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Summary vector.Summary  `json:"summary"`
		Vectors []vector.Result `json:"vectors"`
	}{vector.Summarize(results), results}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "vectors.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "vectors.md"), []byte(vector.Markdown(results)), 0o644)
}

// vector runs arbot on the vector in the spec file at path, writing its files
// to dir.
func (h *harness) vector(path, name, dir string, books []playbooks.Playbook) (*vector.Result, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	spec, err := vector.Read(path)
	if err != nil {
		return nil, err
	}
	request := spec.SafenetRequest(path)
	tx := request.Proposal.Transaction
	r := &vector.Result{
		Name: name, Path: path, ChainID: tx.ChainID.Uint64(), Block: request.Proposal.SafeBlock,
		Verdict: request.Vector.Verdict, Rule: request.Vector.Rule, EOA: request.Vector.EOA,
		Commands: []vector.Command{},
	}

	// The request file is the one that arbot vector writes, which checks the
	// command as well as the vector.
	requestFile := filepath.Join(dir, "request.json")
	c, stdout := h.run("", []string{"vector", "-json", path})
	if c.Status != vector.CommandOK {
		return nil, fmt.Errorf("arbot vector: %s", c.Error)
	}
	if err := os.WriteFile(requestFile, stdout, 0o644); err != nil {
		return nil, err
	}

	c, stdout = h.run("", []string{"classify", "-json", "-request-file", requestFile})
	r.Commands = append(r.Commands, c)
	if c.Status == vector.CommandOK {
		var classification vector.Classification
		if err := json.Unmarshal(stdout, &classification); err != nil {
			return nil, fmt.Errorf("arbot classify -json: %w", err)
		}
		r.Classification = &classification
		if err := os.WriteFile(filepath.Join(dir, "classify.json"), stdout, 0o644); err != nil {
			return nil, err
		}
	}
	r.Outcome = vector.Compare(request.Vector, checks.Covered(r.ChainID), r.Classification)

	o := &outputs{request: request}
	ran := map[string]bool{"classify -json -request-file " + playbooks.RequestFile: true}
	for _, book := range playbooks.For(books, request.DisputedRules) {
		r.Playbooks = append(r.Playbooks, book.Name)
		for _, line := range book.Commands {
			args := strings.Fields(strings.TrimPrefix(line, "arbot "))
			key := strings.Join(args, " ")
			if ran[key] {
				continue
			}
			ran[key] = true
			if slices.Contains(oracleCommands, args[0]) {
				r.Commands = append(r.Commands, vector.Command{Playbook: book.Name, Line: line, Status: vector.CommandSkipped, Error: "it needs the oracle request, which a vector doesn't have"})
				continue
			}
			runs, reason := o.fill(args, requestFile)
			if reason != "" {
				r.Commands = append(r.Commands, vector.Command{Playbook: book.Name, Line: line, Status: vector.CommandSkipped, Error: reason})
				continue
			}
			for _, filled := range runs {
				// Playbooks share commands, such as arbot account of an address that is a
				// spender in one and an argument in another, which run once.
				key := strings.ToLower(strings.Join(filled, " "))
				if ran[key] {
					continue
				}
				ran[key] = true
				c, stdout := h.run(book.Name, filled)
				c.Output = fmt.Sprintf("%02d-%s.json", len(r.Commands)+1, filled[0])
				if err := os.WriteFile(filepath.Join(dir, c.Output), stdout, 0o644); err != nil {
					return nil, err
				}
				r.Commands = append(r.Commands, c)
				if c.Status == vector.CommandOK {
					o.add(filled[0], stdout)
				}
			}
		}
	}
	return r, nil
}

// run runs arbot with args, with the harness's configuration, and runs it again
// after a wait if a request to a node failed. It returns the command and its
// output.
func (h *harness) run(book string, args []string) (vector.Command, []byte) {
	full := []string{"arbot"}
	if h.config != "" {
		full = append(full, "-config", h.config)
	}
	full = append(full, args...)
	// The command line is the one that the playbook runs, without the
	// configuration, which is the same for every command.
	c := vector.Command{Playbook: book, Line: strings.Join(append([]string{"arbot"}, args...), " ")}
	wait := h.wait
	for try := 0; ; try++ {
		var stdout, stderr bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
		code := cli.Run(ctx, full, &stdout, &stderr)
		cancel()
		errText := strings.TrimSpace(stderr.String())
		if code == 0 {
			c.Status, c.NodeError, c.Error = vector.CommandOK, false, ""
			for _, line := range strings.Split(errText, "\n") {
				if line != "" {
					c.Warnings = append(c.Warnings, line)
				}
			}
			return c, stdout.Bytes()
		}
		c.Status, c.Error = vector.CommandFailed, errText
		contains := func(p string) bool { return strings.Contains(errText, p) }
		transient := contains(transientHint) || slices.ContainsFunc(serviceFailures, contains)
		c.NodeError = transient || slices.ContainsFunc(prunedState, contains)
		if !transient || try == h.retries {
			return c, stdout.Bytes()
		}
		fmt.Fprintf(os.Stderr, "vectors: %s: a request to a node or service failed; running it again in %s\n", c.Line, wait)
		time.Sleep(wait)
		wait *= 2
	}
}

// outputs are the outputs of the commands that ran for a vector, from which the
// harness fills in the playbooks' placeholders.
type outputs struct {
	request    *safenet.Request
	calls      *checks.TransactionCalls
	executions *executionsOutput
}

// executionsOutput is what the harness reads of the output of arbot executions
// -json.
type executionsOutput struct {
	Executions []struct {
		Block  uint64 `json:"block"`
		TxHash string `json:"txHash"`
	} `json:"executions"`
	History *struct {
		Creation *struct {
			Block uint64 `json:"block"`
		} `json:"creation"`
	} `json:"history"`
}

// add keeps the output of a command that succeeded, if a placeholder is filled
// from it.
func (o *outputs) add(command string, stdout []byte) {
	switch command {
	case "calls":
		var calls checks.TransactionCalls
		if json.Unmarshal(stdout, &calls) == nil {
			o.calls = &calls
		}
	case "executions":
		var x executionsOutput
		if json.Unmarshal(stdout, &x) == nil {
			o.executions = &x
		}
	}
}

var placeholder = regexp.MustCompile(`^<[a-z-]+(\.json)?>$`)

// fill returns the command lines to run for a playbook's command line args,
// with its placeholders filled in, one for each value of a placeholder that has
// several, or why it can't fill them in.
func (o *outputs) fill(args []string, requestFile string) ([][]string, string) {
	runs := [][]string{{}}
	for _, arg := range args {
		if !placeholder.MatchString(arg) {
			for i := range runs {
				runs[i] = append(runs[i], arg)
			}
			continue
		}
		values, reason := o.values(arg, requestFile)
		if len(values) == 0 {
			return nil, reason
		}
		var next [][]string
		for _, run := range runs {
			for _, v := range values {
				next = append(next, append(slices.Clone(run), v))
			}
		}
		if len(next) > maxValues {
			next = next[:maxValues]
		}
		runs = next
	}
	return runs, ""
}

// values returns the values that a placeholder is filled with, or why there are
// none.
func (o *outputs) values(name, requestFile string) ([]string, string) {
	if name == playbooks.RequestFile {
		return []string{requestFile}, ""
	}
	if name == "<safe>" {
		return []string{strings.ToLower(o.request.Proposal.Transaction.Safe.String())}, ""
	}
	switch name {
	case "<block>":
		if x := o.executions; x != nil && x.History != nil && x.History.Creation != nil {
			return []string{strconv.FormatUint(x.History.Creation.Block, 10)}, ""
		}
		return nil, "it needs an earlier block of the Safe's history, the creation that arbot executions -all reports, which it doesn't report or didn't run for the playbooks of this vector"
	case "<transaction-hash>":
		if x := o.executions; x != nil && len(x.Executions) > 0 {
			return []string{x.Executions[len(x.Executions)-1].TxHash}, ""
		}
		return nil, "it needs an earlier transaction of the Safe, from arbot executions, which lists none or didn't run for the playbooks of this vector"
	}
	if o.calls == nil {
		return nil, fmt.Sprintf("it needs %s, from the output of arbot calls, which failed", name)
	}
	var values []string
	add := func(a fmt.Stringer) { values = appendNew(values, a.String()) }
	for i, c := range o.calls.Calls {
		if c.Kind == "refund" || c.Placeholder != "" {
			continue
		}
		recipient, spender := tokenArgs(&c)
		switch name {
		case "<n>":
			if c.Operation == safenet.OperationCall {
				values = append(values, strconv.Itoa(i+1))
			}
		case "<token>":
			if recipient != "" || spender != "" {
				add(c.To.Address)
			}
		case "<recipient>":
			if recipient != "" {
				values = appendNew(values, recipient)
			} else if c.Value != nil && c.Value.Cmp(new(big.Int)) > 0 && !c.To.Self {
				add(c.To.Address)
			}
		case "<spender>":
			if spender != "" {
				values = appendNew(values, spender)
			}
		case "<target>":
			if c.Operation == safenet.OperationDelegateCall && !multiSend(c.To.Contract) {
				add(c.To.Address)
			}
		case "<address>":
			if !c.To.Self {
				add(c.To.Address)
			}
			for _, a := range addressArgs(&c) {
				values = appendNew(values, a)
			}
			if c.Creates != nil {
				add(c.Creates.Address)
			}
		default:
			return nil, fmt.Sprintf("the harness doesn't know the placeholder %s", name)
		}
	}
	switch {
	case len(values) == 0 && name == "<target>":
		return nil, "the calls have no delegate call to a contract other than a § 2.1 MultiSend"
	case len(values) == 0:
		return nil, fmt.Sprintf("the calls have no %s", strings.Trim(name, "<>"))
	}
	if name == "<n>" {
		values = values[:1]
	}
	return values, ""
}

// appendNew appends v to values, unless it is there already. Addresses are in
// lowercase, since arbot prints an account's address with its checksum and an
// argument's without, so that an address is only added once.
func appendNew(values []string, v string) []string {
	v = strings.ToLower(v)
	if slices.Contains(values, v) {
		return values
	}
	return append(values, v)
}

// multiSend reports whether a contract that arbot calls identifies is a § 2.1
// MultiSend or MultiSendCallOnly.
func multiSend(c *checks.Contract) bool {
	return c != nil && strings.HasPrefix(c.Name, "MultiSend")
}

var (
	recipientName = regexp.MustCompile(`^_?(to|recipient|receiver|dst)$`)
	spenderName   = regexp.MustCompile(`^_?(spender|guy)$`)
)

// inputs returns the arguments of a call's function, decoded as a well-known
// function, or else with the ABI that Sourcify verified.
func inputs(c *checks.Call) []functions.Arg {
	switch {
	case c.Function != nil:
		return c.Function.Args
	case c.ABI != nil && c.ABI.Function != nil:
		return c.ABI.Function.Inputs
	}
	return nil
}

// tokenArgs returns the recipient and the spender that a call passes as an
// address argument, by its name, such as the "to" of an ERC-20 transfer or the
// "spender" of an approval, or "".
func tokenArgs(c *checks.Call) (recipient, spender string) {
	for _, a := range inputs(c) {
		value, ok := a.Value.(string)
		if !ok || a.Type != "address" {
			continue
		}
		switch {
		case recipient == "" && recipientName.MatchString(a.Name):
			recipient = value
		case spender == "" && spenderName.MatchString(a.Name):
			spender = value
		}
	}
	return recipient, spender
}

// addressArgs returns the address arguments that a call passes.
func addressArgs(c *checks.Call) []string {
	var addresses []string
	for _, a := range inputs(c) {
		if value, ok := a.Value.(string); ok && a.Type == "address" {
			addresses = appendNew(addresses, value)
		}
	}
	return addresses
}
