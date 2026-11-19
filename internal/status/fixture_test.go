package status

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/config"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/httpretry"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

var (
	record       = flag.Bool("record", false, "record the fixtures in testdata from live nodes and services, instead of replaying them")
	recordConfig = flag.String("record-config", "", "with -record, the arbot configuration file with the RPC URLs of Ethereum Mainnet and Gnosis Chain to record from")
)

// recording is what a fixture recorded: the JSON-RPC exchanges with each
// chain's node, by chain ID, and the replies of the Safe Transaction Service,
// by path.
//
// The exchanges were recorded by running Check, with the request's Progress and
// Proposals, against live nodes and Safe's public service, through proxies that
// keep every request and its answer. Run TestFixtures with -record and
// -record-config to record them again, which moves their latest blocks.
type recording struct {
	RPC     map[string][]exchange      `json:"rpc"`
	Service map[string]json.RawMessage `json:"service"`
}

// exchange is a JSON-RPC request and its answer: a result or an error, or, for
// an answer with another HTTP status than 200, the status and the body, such as
// for a block range that the node won't scan.
type exchange struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
	Status int             `json:"status,omitempty"`
	Body   string          `json:"body,omitempty"`
}

// fixture serves a recording, or records one, for a test.
type fixture struct {
	t   *testing.T
	cfg config.Config

	mu  sync.Mutex
	rec recording
	// served counts the exchanges served by chain and key, so that a request that
	// was made more than once, such as for the latest block, gets its answers in
	// the order recorded.
	served map[string]int
}

func newFixture(t *testing.T, path string) *fixture {
	t.Helper()
	f := &fixture{t: t, rec: recording{RPC: map[string][]exchange{}, Service: map[string]json.RawMessage{}}, served: map[string]int{}}
	if *record {
		if *recordConfig == "" {
			t.Fatal("-record needs -record-config")
		}
		cfg, err := config.Load(*recordConfig)
		if err != nil {
			t.Fatal(err)
		}
		f.cfg = cfg
		t.Cleanup(func() { f.save(path) })
		return f
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f.rec); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return f
}

// save writes the recording to path, formatted as jq formats it.
func (f *fixture) save(path string) {
	if f.t.Failed() {
		return
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(f.rec); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// key identifies a request by its method and parameters.
func key(chain, method string, params json.RawMessage) string {
	var compact bytes.Buffer
	json.Compact(&compact, params)
	return chain + " " + method + " " + compact.String()
}

// node returns a client for a node of the chain with the ID chainID that serves
// the recording, or records it from the live node.
func (f *fixture) node(chainID uint64) *ethrpc.Client {
	f.t.Helper()
	chain := strconv.FormatUint(chainID, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if req.Method == "eth_chainId" {
			res["result"] = ethrpc.Quantity(chainID)
			json.NewEncoder(w).Encode(res)
			return
		}
		x, err := f.exchange(chain, req.Method, req.Params, body)
		if err != nil {
			f.t.Errorf("chain %s: %s %s: %v", chain, req.Method, req.Params, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch {
		case x.Status != 0:
			w.WriteHeader(x.Status)
			io.WriteString(w, x.Body)
			return
		case x.Error != nil:
			res["error"] = x.Error
		default:
			res["result"] = x.Result
		}
		json.NewEncoder(w).Encode(res)
	}))
	f.t.Cleanup(server.Close)
	client, err := ethrpc.NewClient(f.t.Context(), chainID, server.URL)
	if err != nil {
		f.t.Fatal(err)
	}
	return client
}

// exchange returns the answer to a request: the next one recorded, or, when
// recording, the live node's.
func (f *fixture) exchange(chain, method string, params json.RawMessage, body []byte) (exchange, error) {
	k := key(chain, method, params)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !*record {
		var matches []exchange
		for _, x := range f.rec.RPC[chain] {
			if key(chain, x.Method, x.Params) == k {
				matches = append(matches, x)
			}
		}
		if len(matches) == 0 {
			return exchange{}, fmt.Errorf("not recorded")
		}
		i := min(f.served[k], len(matches)-1)
		f.served[k]++
		return matches[i], nil
	}

	id, _ := strconv.ParseUint(chain, 10, 64)
	resp, err := httpretry.Client.Post(f.cfg.RPCs[id], "application/json", bytes.NewReader(body))
	if err != nil {
		return exchange{}, err
	}
	defer resp.Body.Close()
	answerBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return exchange{}, err
	}
	x := exchange{Method: method, Params: params}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// A failure that says nothing about the chain isn't recorded: record again.
		return exchange{}, fmt.Errorf("HTTP %s", resp.Status)
	case resp.StatusCode != http.StatusOK:
		x.Status, x.Body = resp.StatusCode, string(answerBody)
	default:
		var answer struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(answerBody, &answer); err != nil {
			return exchange{}, err
		}
		x.Result, x.Error = trim(method, answer.Result), answer.Error
	}
	f.rec.RPC[chain] = append(f.rec.RPC[chain], x)
	return x, nil
}

// trim returns the result of a method with only the fields that arbot reads,
// which keeps the recordings small: the number, hash, and timestamp of a block,
// and the hash, sender, recipient, and input of a transaction.
func trim(method string, result json.RawMessage) json.RawMessage {
	var fields []string
	switch method {
	case "eth_getBlockByNumber":
		fields = []string{"number", "hash", "timestamp"}
	case "eth_getTransactionByHash":
		fields = []string{"hash", "from", "to", "input"}
	default:
		return result
	}
	var full map[string]json.RawMessage
	if json.Unmarshal(result, &full) != nil || full == nil {
		return result
	}
	kept := make(map[string]json.RawMessage)
	for _, field := range fields {
		if v, ok := full[field]; ok {
			kept[field] = v
		}
	}
	trimmed, _ := json.Marshal(kept)
	return trimmed
}

// serviceFields are the fields of a multisig transaction of the Safe
// Transaction Service that a recording keeps: those that the client reads,
// without the transaction's data, which Check doesn't read, and which can be
// long.
var serviceFields = []string{
	"safe", "to", "value", "operation", "safeTxGas", "baseGas", "gasPrice", "gasToken", "refundReceiver", "nonce",
	"safeTxHash", "transactionHash", "blockNumber", "isExecuted", "isSuccessful",
}

// service returns a client for a Safe Transaction Service of Ethereum Mainnet
// that serves the recording, or records it from Safe's public service.
func (f *fixture) service() *txservice.Client {
	f.t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !*record {
			reply, ok := f.rec.Service[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(reply)
			return
		}
		resp, err := httpretry.Client.Get(txservice.DefaultURLs[ethrpc.Mainnet] + r.URL.Path)
		if err != nil {
			f.t.Errorf("service: %v", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK {
			var full map[string]json.RawMessage
			if err := json.Unmarshal(body, &full); err != nil {
				f.t.Errorf("service: %v", err)
			}
			kept := make(map[string]json.RawMessage)
			for _, field := range serviceFields {
				if v, ok := full[field]; ok {
					kept[field] = v
				}
			}
			body, _ = json.Marshal(kept)
			f.rec.Service[r.URL.Path] = body
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}))
	f.t.Cleanup(server.Close)
	return txservice.NewClient(server.URL, ethrpc.Mainnet)
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
			gnosis, mainnet := f.node(ethrpc.Gnosis), f.node(ethrpc.Mainnet)
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
				c.Service = f.service()
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
