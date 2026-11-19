package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/config"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/replay"
	"github.com/safe-research/safenet-arbitration-bot/internal/reportcheck"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/simulate"
	"github.com/safe-research/safenet-arbitration-bot/internal/sourcify"
	"github.com/safe-research/safenet-arbitration-bot/internal/status"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// The tests of past arbitrations run arbot's commands on the requests of
// rulings, on what Ethereum Mainnet, the Safe Transaction Service, and Sourcify
// answered when the fixtures were recorded, and pin the facts that the rulings
// rested on. Run them with -record and -record-config to record the fixtures
// again from live nodes and services.
var (
	recordCases  = flag.Bool("record", false, "record the fixtures of past arbitrations in testdata/cases from live nodes and services, instead of replaying them")
	recordConfig = flag.String("record-config", "", "with -record, the arbot configuration file with the RPC URL of an Ethereum Mainnet archive node to record from")
)

// pastCase runs arbot on the request of a past arbitration, with the nodes and
// services of a fixture.
type pastCase struct {
	t           *testing.T
	requestFile string
	configFile  string
}

// openCase returns a past case for the request with the ID that starts with id,
// whose request file and fixture are testdata/cases/<id>.request.json and
// testdata/cases/<id>.json. If keep isn't nil, the fixture records only the
// transfers of the Safe Transaction Service's listing that keep keeps, to keep
// it small.
func openCase(t *testing.T, id string, keep func(transfer json.RawMessage) bool) *pastCase {
	t.Helper()
	opts := replay.Options{Record: *recordCases, TrimReply: func(service, path string, body json.RawMessage) json.RawMessage {
		return trimServiceReply(service, path, body, keep)
	}}
	if *recordCases {
		if *recordConfig == "" {
			t.Fatal("-record needs -record-config")
		}
		cfg, err := config.Load(*recordConfig)
		if err != nil {
			t.Fatal(err)
		}
		opts.RPCs = cfg.RPCs
	}
	f := replay.Open(t, filepath.Join("testdata", "cases", id+".json"), opts)
	cfg := config.Config{
		RPCs:       map[uint64]string{ethrpc.Mainnet: f.NodeURL(ethrpc.Mainnet)},
		TxServices: map[uint64]string{ethrpc.Mainnet: f.ServiceURL("txservice", txservice.DefaultURLs[ethrpc.Mainnet])},
		Sourcify:   f.ServiceURL("sourcify", sourcify.DefaultURL),
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c := &pastCase{t: t, requestFile: filepath.Join("testdata", "cases", id+".request.json"), configFile: filepath.Join(t.TempDir(), "config.json")}
	if err := os.WriteFile(c.configFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return c
}

// run runs an arbot command with args, followed by -request-file and the case's
// request file, and decodes its JSON output into v.
func (c *pastCase) run(v any, args ...string) {
	c.t.Helper()
	full := append([]string{"arbot", "-config", c.configFile}, args[0])
	full = append(full, "-json", "-request-file", c.requestFile)
	full = append(full, args[1:]...)
	var stdout, stderr bytes.Buffer
	if code := Run(c.t.Context(), full, &stdout, &stderr); code != 0 {
		c.t.Fatalf("%s: got status %d, stderr %q", strings.Join(full[3:], " "), code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), v); err != nil {
		c.t.Fatalf("%s: %v", strings.Join(full[3:], " "), err)
	}
}

// serviceFields are the fields of the Safe Transaction Service's replies that
// the fixtures keep, in the objects of their results at any depth: those that
// txservice reads, which leaves out the confirmations and decoded data of each
// transaction, which are long.
var serviceFields = []string{
	"results", "safe", "to", "value", "data", "operation", "safeTxGas", "baseGas", "gasPrice", "gasToken", "refundReceiver", "nonce",
	"safeTxHash", "transactionHash", "blockNumber", "isExecuted", "isSuccessful", "type", "tokenAddress", "from",
}

// trimServiceReply keeps the serviceFields of a reply of the Safe Transaction
// Service, and of a page of its transfers, those that keep keeps, if it isn't
// nil. It keeps Sourcify's replies as they are.
func trimServiceReply(service, path string, body json.RawMessage, keep func(json.RawMessage) bool) json.RawMessage {
	if service != "txservice" {
		return body
	}
	var page struct {
		Results []json.RawMessage `json:"results"`
	}
	if json.Unmarshal(body, &page) != nil || page.Results == nil {
		return replay.Fields(body, serviceFields...)
	}
	transfers := strings.Contains(path, "/transfers/")
	kept := []json.RawMessage{}
	for _, r := range page.Results {
		if !transfers || keep == nil || keep(r) {
			kept = append(kept, replay.Fields(r, serviceFields...))
		}
	}
	// A page with fewer results than its limit is the last, so the page keeps its
	// count of results, as placeholders, for the client to read the next.
	for len(kept) < len(page.Results) {
		kept = append(kept, json.RawMessage(`{"type": "OMITTED"}`))
	}
	page.Results = kept
	trimmed, _ := json.Marshal(page)
	return trimmed
}

// transfersOutput is what the tests read of arbot transfers -json.
type transfersOutput struct {
	Transfers []struct {
		Type  string          `json:"type"`
		Block uint64          `json:"block"`
		Token *ethrpc.Address `json:"token"`
		From  ethrpc.Address  `json:"from"`
		To    ethrpc.Address  `json:"to"`
	} `json:"transfers"`
	Service *struct {
		Listed    int `json:"listed"`
		Unmatched int `json:"unmatched"`
	} `json:"service"`
}

// executionsOutput is what the tests read of arbot executions -json.
type executionsOutput struct {
	Executions []struct {
		Block       uint64                   `json:"block"`
		Success     bool                     `json:"success"`
		Transaction *safenet.SafeTransaction `json:"transaction"`
	} `json:"executions"`
	History struct {
		Nonce    int64 `json:"nonce"`
		Complete bool  `json:"complete"`
	} `json:"history"`
}

// TestCase998b9630ee77 pins the facts that the secure ruling under R-4.3 on
// request 998b9630ee77 rested on: the Safe had paid the recipient 8 times
// before, in USDT and USDC, so the recipient was inside the expected target set
// (§ 2.4). arbot transfers -all -to finds the 8 transfer events, which the
// service lists too, and arbot executions -all, among the Safe's 122
// executions, those that pass the recipient, in the blocks of the transfers.
func TestCase998b9630ee77(t *testing.T) {
	recipient := ethrpc.MustParseAddress("0x123593c1b9b8872fafec7e67baec842258344306")
	// The Safe Transaction Service lists 484 transfers into and out of the Safe, of
	// which the fixture keeps those to and from the recipient, the ones that
	// transfers -all -to reads the blocks of. Its executions are all kept.
	c := openCase(t, "998b9630ee77", func(transfer json.RawMessage) bool {
		var r struct{ From, To ethrpc.Address }
		return json.Unmarshal(transfer, &r) == nil && (r.From == recipient || r.To == recipient)
	})
	tokens := []ethrpc.Address{
		ethrpc.MustParseAddress("0xdac17f958d2ee523a2206206994597c13d831ec7"),
		ethrpc.MustParseAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
	}
	safe := ethrpc.MustParseAddress("0xbbca3b4e45b91031bcd498bd045203d0d113c956")

	var tr transfersOutput
	c.run(&tr, "transfers", "-all", "-to", recipient.String())
	var blocks []uint64
	for _, r := range tr.Transfers {
		if r.Type != "erc20" || r.Token == nil || !slices.Contains(tokens, *r.Token) || r.From != safe || r.To != recipient {
			t.Errorf("transfers -all -to: got %+v, want a USDT or USDC transfer from the Safe to the recipient", r)
		}
		if !slices.Contains(blocks, r.Block) {
			blocks = append(blocks, r.Block)
		}
	}
	if len(tr.Transfers) != 8 || tr.Service == nil || tr.Service.Listed != 8 || tr.Service.Unmatched != 0 {
		t.Errorf("transfers -all -to: got %d transfers, and the service's %+v; want 8, which it lists too", len(tr.Transfers), tr.Service)
	}

	// The executions that pass the recipient, in their data or as their target, are
	// the transfers'.
	var x executionsOutput
	c.run(&x, "executions", "-all")
	var paid []uint64
	for _, e := range x.Executions {
		if tx := e.Transaction; tx != nil && e.Success && (tx.To == recipient || bytes.Contains(tx.Data, recipient[:])) {
			paid = append(paid, e.Block)
		}
	}
	if len(x.Executions) != 122 || !x.History.Complete || x.History.Nonce != 122 || !slices.Equal(paid, blocks) {
		t.Errorf("executions -all: got %d executions, complete %t, nonce %d, and those that pass the recipient in blocks %v; want 122, complete, nonce 122, and the transfers' blocks %v",
			len(x.Executions), x.History.Complete, x.History.Nonce, paid, blocks)
	}
}

// TestCase6814ad32bff9 pins the facts that the insecure ruling under R-4.3 on
// request 6814ad32bff9 rested on: no transfer between the Safe and the
// recipient over the Safe's whole life, so nothing placed the recipient inside
// the expected target set (§ 2.4), and the recipient is a Safe with 3 owners
// and a threshold of 2, which says who controls it, not that it belongs there.
func TestCase6814ad32bff9(t *testing.T) {
	c := openCase(t, "6814ad32bff9", nil)
	recipient := ethrpc.MustParseAddress("0x316473f1E8bCD1549527d07A9B68B545f0D2976e")

	for _, args := range [][]string{{"transfers", "-all", "-to", recipient.String()}, {"transfers", "-all", "-in", "-from", recipient.String()}} {
		var tr transfersOutput
		c.run(&tr, args...)
		// The service knows the Safe, or the command would fail, and lists none either.
		if len(tr.Transfers) != 0 || tr.Service == nil || tr.Service.Listed != 0 {
			t.Errorf("%s: got transfers %+v and the service's %+v; want none, and none listed", strings.Join(args, " "), tr.Transfers, tr.Service)
		}
	}

	var x executionsOutput
	c.run(&x, "executions", "-all")
	for _, e := range x.Executions {
		if tx := e.Transaction; tx == nil || tx.To == recipient || bytes.Contains(tx.Data, recipient[:]) {
			t.Errorf("executions -all: got %+v, which isn't known or involves the recipient", e)
		}
	}
	if !x.History.Complete || len(x.Executions) == 0 {
		t.Errorf("executions -all: got %d executions, complete %t; want a complete history", len(x.Executions), x.History.Complete)
	}

	var s struct {
		ProxyVersion *string `json:"proxyVersion"`
		Settings     *struct {
			Owners    []ethrpc.Address `json:"owners"`
			Threshold int64            `json:"threshold"`
		} `json:"settings"`
	}
	c.run(&s, "safe", "-address", recipient.String())
	if s.Settings == nil || len(s.Settings.Owners) != 3 || s.Settings.Threshold != 2 || s.ProxyVersion == nil {
		t.Errorf("safe -address: got %+v, want a Safe with 3 owners and threshold 2", s)
	}
}

// TestCasecbfadf6b9595HumanReview shows that the evidence of the ambiguous
// R-4.6 case cbfadf6b9595, which ended with no accepted case, must go to the
// Council for human review. It builds the evidence summary that the target-risk
// playbook has the detective write from the facts that earlier fixtures pin:
// the three contracts that the transaction creates, from the trace of its
// simulation (simulate's TestParseTrace), and its execution during the voting,
// from its status (status's TestFixtures, whose recording it replays), with the
// gaps that the playbook lists for every R-4.6 case. With the decisive gap of
// the public security flags (§ 2.7) open, the summary is only valid with
// humanReview set, and the draft ruling must say that human review is required.
func TestCasecbfadf6b9595HumanReview(t *testing.T) {
	raw, err := os.ReadFile("../simulate/testdata/trace-cbfadf6b9595.json")
	if err != nil {
		t.Fatal(err)
	}
	trace, err := simulate.ParseTrace(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../status/testdata/cbfadf6b9595.request.json")
	if err != nil {
		t.Fatal(err)
	}
	var request safenet.Request
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	f := replay.Open(t, "../status/testdata/cbfadf6b9595.json", replay.Options{})
	gnosis, mainnet := f.Node(ethrpc.Gnosis), f.Node(ethrpc.Mainnet)
	sn := safenet.New(func(context.Context, uint64) (*ethrpc.Client, error) { return gnosis, nil }, safenet.DefaultOracle, safenet.DefaultConsensus)
	progress, err := sn.Progress(t.Context(), request.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := txservice.NewClient(f.ServiceURL("txservice", txservice.DefaultURLs[ethrpc.Mainnet]), ethrpc.Mainnet)
	report, err := (&status.Checker{Gnosis: gnosis, Safe: mainnet, Blocks: 100_000, Service: service}).Check(t.Context(), &request, progress)
	if err != nil {
		t.Fatal(err)
	}
	if report.Timing == nil || report.Timing.Value != status.DuringVoting || report.Execution.Block == nil {
		t.Fatalf("status: got %+v, want an execution during the voting", report)
	}

	safeBlock := request.Proposal.SafeBlock
	simulation := "arbot simulate -json -all -trace -request-file request.json"
	summary := reportcheck.Summary{
		Request:       request.ID.String(),
		DisputedRules: []string{"R-4.6"},
		Playbooks:     []string{"proposal-status", "target-risk"},
		Gaps: []reportcheck.Gap{
			{
				ID: "G1", Fact: "Public security flags on the helper and on each created contract", Rules: []string{"R-4.6", "§ 2.7"},
				Reason: "no trusted provider is integrated; the Council checks them by hand", Decisive: true,
			},
			{
				ID: "G2", Fact: "The runtime code of each created contract, and what it does", Rules: []string{"R-4.6"},
				Reason: "no arbot command decodes init code or shows the code that it deploys", Decisive: true,
			},
		},
		Conflicts:         []reportcheck.Conflict{},
		Uncertainty:       "high",
		UncertaintyReason: "G1 and G2 are decisive and open",
		SuggestedRuling: &reportcheck.SuggestedRuling{
			Suggestion: true, Rules: []string{"R-4.6"}, Charter: []string{"§ 2.7", "§ 2.8", "§ 3.7"},
			Reasoning: "The facts suggest neither verdict while the flags and the created code are unexamined.",
		},
	}
	for i, c := range trace.Creations {
		summary.Facts = append(summary.Facts, reportcheck.Fact{
			ID:   fmt.Sprintf("F%d", i+1),
			Fact: fmt.Sprintf("The simulation of the transaction creates %s with %s, from %s.", c.Address, c.Type, c.Creator),
			// The simulation runs on the state of the Safe block, which is onchain data.
			Rules: []string{"R-4.6", "§ 2.8"}, Source: reportcheck.Source{Kind: "onchain", Command: simulation},
			ChainID: ethrpc.Mainnet, Block: &safeBlock,
		})
	}
	executed := *report.Execution.Block
	summary.Facts = append(summary.Facts, reportcheck.Fact{
		ID:   "F4",
		Fact: fmt.Sprintf("The Safe executed the transaction in block %d, during the voting: the proposal's status, not evidence (§ 3.6).", executed),
		// The service named the execution, which its receipt confirms.
		Rules: []string{"§ 3.6"},
		Source: reportcheck.Source{
			Kind: "offchain", Command: "arbot status -json -request-file request.json", URL: report.Execution.Service.URL,
			ObservedAt: report.Execution.Service.ObservedAt.Format("2006-01-02T15:04:05Z"), Reliability: "confirmed onchain by its receipt",
		},
		ChainID: ethrpc.Mainnet, Block: &executed,
	})
	if len(summary.Facts) != 4 || !strings.Contains(summary.Facts[0].Fact, "0x3Da548EE3C9d7fb614C869e53ffdaa1a07b8A0a0") {
		t.Fatalf("got facts %+v, want the three created contracts, starting with 0x3Da548EE3C9d7fb614C869e53ffdaa1a07b8A0a0, and the execution", summary.Facts)
	}

	// The summary without human review is invalid.
	if problems := summary.Problems(); !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "humanReview must be true") }) {
		t.Errorf("without humanReview: got problems %q, want that humanReview must be true", problems)
	}
	summary.HumanReview = true
	summary.HumanReviewReasons = []string{"G1 is decisive and open: public security flags weren't checked (§ 2.7)."}
	if problems := summary.Problems(); len(problems) != 0 {
		t.Errorf("with humanReview: got problems %q, want none", problems)
	}

	// arbot check-report checks the summary, and that the draft ruling says that
	// human review is required.
	dir := t.TempDir()
	content, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"request.json": data, reportcheck.SummaryFile: content}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	created := trace.Creations[0].Address.String()
	for _, test := range []struct {
		report string
		want   bool
	}{
		{"## Ruling\n\nThe transaction creates `" + created + "`.\n", false},
		{"## Ruling\n\nThe transaction creates `" + created + "`.\n\nHuman review: required.\n", true},
	} {
		if err := os.WriteFile(filepath.Join(dir, reportcheck.ReportFile), []byte(test.report), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := reportcheck.Check(dir, "../../corpus")
		if err != nil {
			t.Fatal(err)
		}
		if ok := len(result.Errors) == 0; ok != test.want {
			t.Errorf("check-report on %q: got errors %v, want them %s", test.report, result.Errors, map[bool]string{true: "none", false: "to say that human review is required"}[test.want])
		}
	}
}
