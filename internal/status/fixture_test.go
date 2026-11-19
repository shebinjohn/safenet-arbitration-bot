package status

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/config"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/replay"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

var (
	record       = flag.Bool("record", false, "record the fixtures in testdata from live nodes and services, instead of replaying them")
	recordConfig = flag.String("record-config", "", "with -record, the arbot configuration file with the RPC URLs of Ethereum Mainnet and Gnosis Chain to record from")
)

// newFixture returns a fixture that serves the recording in the file at path,
// or records it with -record.
//
// The exchanges were recorded by running Check, with the request's Progress and
// Proposals, against live nodes and Safe's public service, through proxies that
// keep every request and its answer (replay). Run TestFixtures with -record and
// -record-config to record them again, which moves their latest blocks.
func newFixture(t *testing.T, path string) *replay.Fixture {
	t.Helper()
	opts := replay.Options{Record: *record, TrimReply: trimService}
	if *record {
		if *recordConfig == "" {
			t.Fatal("-record needs -record-config")
		}
		cfg, err := config.Load(*recordConfig)
		if err != nil {
			t.Fatal(err)
		}
		opts.RPCs = cfg.RPCs
	}
	return replay.Open(t, path, opts)
}

// serviceFields are the fields of a multisig transaction of the Safe
// Transaction Service that a recording keeps: those that the client reads,
// without the transaction's data, which Check doesn't read, and which can be
// long.
var serviceFields = []string{
	"safe", "to", "value", "operation", "safeTxGas", "baseGas", "gasPrice", "gasToken", "refundReceiver", "nonce",
	"safeTxHash", "transactionHash", "blockNumber", "isExecuted", "isSuccessful",
}

// trimService keeps the serviceFields of a reply of the service.
func trimService(_, _ string, body json.RawMessage) json.RawMessage {
	return replay.Fields(body, serviceFields...)
}

// recordedService returns a client for a Safe Transaction Service of Ethereum
// Mainnet that serves the recording of f, or records it from Safe's public
// service.
func recordedService(f *replay.Fixture) *txservice.Client {
	return txservice.NewClient(f.ServiceURL("txservice", txservice.DefaultURLs[ethrpc.Mainnet]), ethrpc.Mainnet)
}

// TestFixtures checks Check against requests whose Safe transactions were
// executed on Ethereum Mainnet: a049284bbb54 two seconds before its proposal on
// Gnosis Chain, with a nonce that the Safe had used by then (Charter issues #27
// and #20), found by a scan of the Safe's logs, and cbfadf6b9595 one block
// after its Safe block, during the voting and before the freeze, which the Safe
// Transaction Service names and its receipt confirms.
func TestFixtures(t *testing.T) {
	// The proposals for cbfadf6b9595's Safe have the long initcode of the contracts
	// that they create, so its queue isn't recorded.
	tests := []struct {
		name                   string
		withService, withQueue bool
		want                   func(t *testing.T, r *Report)
	}{
		{"a049284bbb54", false, true, func(t *testing.T, r *Report) {
			n, x := r.Nonce, r.Execution
			if n.Transaction.Int64() != 17 || n.SafeAtSafeBlock.Int64() != 18 || !n.Stale || n.ConsumedAfterSafeBlock {
				t.Errorf("nonce: got %+v, want 17, below the Safe's 18 at the Safe block, stale", n)
			}
			if x.Status != Executed || x.Source != SourceLogs || x.Block == nil || *x.Block != 26381211 || *x.Block != r.SafeBlock ||
				x.TxHash == nil || *x.TxHash != ethrpc.MustParseHash("0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5") ||
				!x.BlockTime.Equal(time.Date(2026, 11, 10, 1, 26, 23, 0, time.UTC)) || !*x.Success {
				t.Errorf("execution: got %+v, want the logs' execution in block 26381211, the Safe block, at 01:26:23", x)
			}
			if tm := r.Timing; tm == nil || tm.Value != BeforeProposal || !tm.Proposal.Time.Equal(time.Date(2026, 11, 10, 1, 26, 25, 0, time.UTC)) ||
				tm.Frozen == nil || tm.Frozen.Block != 49199453 || tm.Resolved == nil || tm.Resolved.Outcome != safenet.OutcomeInsecure {
				t.Errorf("timing: got %+v, want before the proposal at 01:26:25, frozen in block 49199453, resolved insecure", tm)
			}
			// The Safe has 20 other proposals around it, none with the same nonce.
			if q := r.Queue; q == nil || len(q.Proposals) != 20 || slices.ContainsFunc(q.Proposals, func(p Proposal) bool { return p.SameNonce || p.RequestID == r.Request }) {
				t.Errorf("queue: got %+v, want 20 other proposals, none with the same nonce", q)
			}
		}},
		{"cbfadf6b9595", true, false, func(t *testing.T, r *Report) {
			n, x := r.Nonce, r.Execution
			if n.Transaction.Int64() != 25 || n.SafeAtSafeBlock.Int64() != 25 || n.Stale || !n.ConsumedAfterSafeBlock {
				t.Errorf("nonce: got %+v, want 25, the Safe's at the Safe block, used after it", n)
			}
			if x.Status != Executed || x.Source != SourceService || x.Block == nil || *x.Block != 26432179 || *x.Block != r.SafeBlock+1 ||
				x.TxHash == nil || *x.TxHash != ethrpc.MustParseHash("0xac1efe99e29b3bc4b3fd5a05fddfa2dd3d27f942d40620847f1d52e9d3803e83") ||
				!x.BlockTime.Equal(time.Date(2026, 11, 17, 3, 48, 59, 0, time.UTC)) {
				t.Errorf("execution: got %+v, want the service's execution in block 26432179, after the Safe block, at 03:48:59", x)
			}
			// Executed after the proposal at 03:48:55 and before the freeze at 03:49:35, so
			// during the voting, not while it was disputed.
			tm := r.Timing
			if tm == nil || tm.Value != DuringVoting || !tm.Proposal.Time.Equal(time.Date(2026, 11, 17, 3, 48, 55, 0, time.UTC)) ||
				tm.CommitDeadline.Block != 49320046 || !tm.CommitDeadline.Reached || tm.Frozen == nil || tm.Frozen.Block != 49320047 ||
				!tm.Frozen.Time.Equal(time.Date(2026, 11, 17, 3, 49, 35, 0, time.UTC)) {
				t.Errorf("timing: got %+v, want during the voting, before the freeze in block 49320047 at 03:49:35", tm)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, filepath.Join("testdata", test.name+".json"))
			data, err := os.ReadFile(filepath.Join("testdata", test.name+".request.json"))
			if err != nil {
				t.Fatal(err)
			}
			var r safenet.Request
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			gnosis, mainnet := f.Node(ethrpc.Gnosis), f.Node(ethrpc.Mainnet)
			sn := safenet.New(func(context.Context, uint64) (*ethrpc.Client, error) { return gnosis, nil }, safenet.DefaultOracle, safenet.DefaultConsensus)
			p, err := sn.Progress(t.Context(), r.ID)
			if err != nil {
				t.Fatalf("Progress: %v", err)
			}
			c := &Checker{Gnosis: gnosis, Safe: mainnet, Blocks: 100_000}
			if test.withQueue {
				c.Proposals = func(ctx context.Context, from, to uint64) ([]safenet.SafeProposal, error) {
					return sn.Proposals(ctx, ethrpc.Mainnet, r.Proposal.Transaction.Safe, from, to)
				}
			}
			if test.withService {
				c.Service = recordedService(f)
			}
			report, err := c.Check(t.Context(), &r, p)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if testing.Verbose() {
				data, _ := json.MarshalIndent(report, "", "  ")
				t.Logf("Check:\n%s", data)
			}
			test.want(t, report)
		})
	}
}
