package status

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/executions"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

var (
	safe             = ethrpc.Address{0: 0x5a, 19: 1}
	executionSuccess = solabi.Event("ExecutionSuccess(bytes32,uint256)")
	ownHash          = ethrpc.Hash{0: 0x0e, 31: 1}
	otherHash        = ethrpc.Hash{0: 0x07, 31: 2}
)

// The blocks of the fake chains are at these times: Gnosis Chain block g at
// genesis plus 5 g seconds, and Ethereum Mainnet block m at genesis plus 12 m
// seconds.
const genesis = 1_700_000_000

func gnosisTime(g uint64) time.Time  { return time.Unix(genesis+5*int64(g), 0).UTC() }
func mainnetTime(m uint64) time.Time { return time.Unix(genesis+12*int64(m), 0).UTC() }

// The request of the tests is proposed in Gnosis Chain block 1201, at genesis
// plus 6005 seconds, so its Safe block is Mainnet block 500, at genesis plus
// 6000. Its commit and reveal deadlines are blocks 1207 and 1213, it was frozen
// in block 1208, and the arbitration was settled in block 2000.
const (
	proposalBlock  = 1201
	safeBlock      = 500
	commitDeadline = 1207
	revealDeadline = 1213
	frozenBlock    = 1208
	resolvedBlock  = 2000
	txNonce        = 5
)

// execution is an execution of a Safe transaction on a fake chain.
type execution struct {
	block      uint64
	safeTxHash ethrpc.Hash
	txHash     ethrpc.Hash
	// input is the calldata of the onchain transaction, or nil if the node doesn't
	// have it.
	input ethrpc.Bytes
	// keepsNonce is set for an execution that leaves the Safe's nonce as it was, as
	// one whose delegate call sets it back can.
	keepsNonce bool
}

// node is a fake node for a chain whose blocks are at the times that time
// returns, up to latest, and on which the Safe made executions, using the
// nonces from startNonce on.
type node struct {
	chainID    uint64
	latest     uint64
	time       func(uint64) time.Time
	startNonce int64
	executions []execution
	// failing are the methods that the node fails with HTTP status 500, as an
	// overloaded node does, and logsError the JSON-RPC error that it answers
	// eth_getLogs with, if any, as for a range that it won't scan.
	failing   []string
	logsError map[string]any
}

// nonce returns the Safe's nonce at block.
func (n *node) nonce(block uint64) int64 {
	nonce := n.startNonce
	for _, x := range n.executions {
		if x.block <= block && !x.keepsNonce {
			nonce++
		}
	}
	return nonce
}

// log returns the ExecutionSuccess log of x, as Safe 1.3.0 logs it.
func (x execution) log() ethrpc.Log {
	data := make([]byte, 64)
	copy(data, x.safeTxHash[:])
	return ethrpc.Log{Address: safe, Topics: []ethrpc.Hash{executionSuccess}, Data: data, BlockNumber: ethrpc.BlockNumber(x.block), TransactionHash: x.txHash}
}

func (n *node) serve(t *testing.T) *ethrpc.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if slices.Contains(n.failing, req.Method) {
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "eth_chainId":
			res["result"] = ethrpc.Quantity(n.chainID)
		case "eth_blockNumber":
			res["result"] = ethrpc.Quantity(n.latest)
		case "eth_getBlockByNumber":
			var number ethrpc.BlockNumber
			json.Unmarshal(req.Params[0], &number)
			if uint64(number) > n.latest {
				res["result"] = nil
				break
			}
			res["result"] = ethrpc.Block{Number: number, Timestamp: ethrpc.Quantity(n.time(uint64(number)).Unix())}
		case "eth_call":
			var block ethrpc.BlockNumber
			json.Unmarshal(req.Params[1], &block)
			word := solabi.Uint(big.NewInt(n.nonce(uint64(block))))
			res["result"] = ethrpc.Bytes(word[:])
		case "eth_getLogs":
			if n.logsError != nil {
				res["error"] = n.logsError
				break
			}
			var filter ethrpc.LogFilter
			json.Unmarshal(req.Params[0], &filter)
			logs := []ethrpc.Log{}
			for _, x := range n.executions {
				if x.block >= uint64(filter.FromBlock) && x.block <= uint64(filter.ToBlock) {
					logs = append(logs, x.log())
				}
			}
			res["result"] = logs
		case "eth_getTransactionByHash":
			var hash ethrpc.Hash
			json.Unmarshal(req.Params[0], &hash)
			res["result"] = nil
			for _, x := range n.executions {
				if x.txHash == hash && x.input != nil {
					res["result"] = ethrpc.Transaction{Hash: hash, To: &safe, Input: x.input}
				}
			}
		case "eth_getTransactionReceipt":
			var hash ethrpc.Hash
			json.Unmarshal(req.Params[0], &hash)
			res["result"] = nil
			for _, x := range n.executions {
				if x.txHash == hash {
					res["result"] = ethrpc.Receipt{TransactionHash: hash, BlockNumber: ethrpc.BlockNumber(x.block), Status: 1, Logs: []ethrpc.Log{x.log()}}
				}
			}
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	client, err := ethrpc.NewClient(t.Context(), n.chainID, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// service starts a fake Safe Transaction Service that has the multisig
// transactions by Safe transaction hash, and fails with status 500 if failing,
// and returns a client for it.
func service(t *testing.T, transactions map[ethrpc.Hash]map[string]any, failing bool) *txservice.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		for hash, tx := range transactions {
			if r.URL.Path == "/api/v1/multisig-transactions/"+hash.String()+"/" {
				json.NewEncoder(w).Encode(tx)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return txservice.NewClient(server.URL, ethrpc.Mainnet)
}

// listing returns the service's multisig transaction for the request's Safe
// transaction, executed in the onchain transaction txHash in block, unless
// txHash is zero.
func listing(txHash ethrpc.Hash, block uint64) map[string]any {
	tx := transaction()
	result := map[string]any{
		"safe": tx.Safe, "to": tx.To, "value": "0", "data": nil, "operation": 0, "safeTxGas": 0, "baseGas": 0, "gasPrice": "0",
		"gasToken": tx.GasToken, "refundReceiver": tx.RefundReceiver, "nonce": txNonce, "safeTxHash": ownHash, "isExecuted": false,
	}
	if txHash != (ethrpc.Hash{}) {
		result["transactionHash"], result["blockNumber"], result["isExecuted"], result["isSuccessful"] = txHash, block, true, true
	}
	return result
}

// transaction returns the request's Safe transaction.
func transaction() safenet.SafeTransaction {
	return safenet.SafeTransaction{
		ChainID: big.NewInt(ethrpc.Mainnet), Safe: safe, To: ethrpc.Address{19: 0x70}, Value: new(big.Int),
		SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: big.NewInt(txNonce),
	}
}

// request returns the request of the tests.
func request() *safenet.Request {
	return &safenet.Request{
		ID: ethrpc.Hash{0: 0x4e, 31: 1},
		Proposal: safenet.Proposal{
			Block: proposalBlock, Time: gnosisTime(proposalBlock), SafeBlock: safeBlock, SafeTxHash: ownHash, Transaction: transaction(),
		},
	}
}

// progress returns the request's progress as of Gnosis Chain block latest:
// frozen, and resolved if latest is at or after the block that settled it.
func progress(latest uint64) *safenet.Progress {
	p := &safenet.Progress{
		Block: latest, State: safenet.StatePending, ArbitrationTimeout: 100,
		Terms: safenet.Terms{CommitDeadline: commitDeadline, RevealDeadline: revealDeadline},
	}
	if latest >= frozenBlock {
		p.State = safenet.StateFrozen
		p.Arbitration = &safenet.Arbitration{FrozenBlock: frozenBlock, Deadline: frozenBlock + 100, Outcome: safenet.OutcomePending}
	}
	if latest >= resolvedBlock {
		p.State = safenet.StateResolvedApproved
		p.Arbitration.Outcome = safenet.OutcomeSecure
		p.Arbitration.Record = &safenet.Record{Block: resolvedBlock}
	}
	return p
}

// checker returns a Checker for the fake chains, with the service, if any,
// which scans 100 blocks: Mainnet blocks 401 to 500 for a stale nonce, and 501
// to 600 otherwise.
func checker(t *testing.T, gnosis, mainnet *node, svc *txservice.Client) *Checker {
	t.Helper()
	return &Checker{Gnosis: gnosis.serve(t), Safe: mainnet.serve(t), Service: svc, Blocks: 100}
}

func gnosisNode(latest uint64) *node {
	return &node{chainID: ethrpc.Gnosis, latest: latest, time: gnosisTime}
}

func mainnetNode(latest uint64, executions ...execution) *node {
	return &node{chainID: ethrpc.Mainnet, latest: latest, time: mainnetTime, startNonce: txNonce, executions: executions}
}

func TestCheck(t *testing.T) {
	// The Gnosis Chain block of the latest Mainnet block, 1000, at genesis plus
	// 12,000 seconds, after the arbitration was settled.
	const latestGnosis, latestMainnet = 2400, 1000
	own := func(block uint64) execution {
		return execution{block: block, safeTxHash: ownHash, txHash: ethrpc.Hash{0: 0xaa, 31: byte(block)}}
	}
	other := func(block uint64) execution {
		return execution{block: block, safeTxHash: otherHash, txHash: ethrpc.Hash{0: 0xbb, 31: byte(block)}}
	}
	tests := []struct {
		name       string
		executions []execution
		// startNonce is the Safe's nonce before the executions, txNonce if zero.
		startNonce int64
		service    map[ethrpc.Hash]map[string]any
		// wantStatus, wantSource, wantBlock, and wantSafeTxHash are the execution that
		// Check should report, and wantTiming its timing, or "" for none.
		wantStatus, wantSource string
		wantBlock              uint64
		wantSafeTxHash         ethrpc.Hash
		wantTiming             string
		wantStale              bool
		wantScanned            *executions.BlockRange
		wantNote               string
	}{
		{
			name: "executed before the proposal", executions: []execution{own(safeBlock)},
			wantStatus: Executed, wantSource: SourceLogs, wantBlock: safeBlock, wantSafeTxHash: ownHash, wantTiming: BeforeProposal,
			wantStale: true, wantScanned: &executions.BlockRange{FromBlock: 401, ToBlock: safeBlock}, wantNote: "Charter issue #20",
		},
		{
			name: "executed during voting", executions: []execution{own(502)},
			wantStatus: Executed, wantSource: SourceLogs, wantBlock: 502, wantSafeTxHash: ownHash, wantTiming: DuringVoting,
			wantScanned: &executions.BlockRange{FromBlock: 501, ToBlock: 600},
		},
		{
			name: "executed during voting, from the service", executions: []execution{own(502)},
			service:    map[ethrpc.Hash]map[string]any{ownHash: listing(own(502).txHash, 502)},
			wantStatus: Executed, wantSource: SourceService, wantBlock: 502, wantSafeTxHash: ownHash, wantTiming: DuringVoting,
		},
		{
			name: "executed after the freeze", executions: []execution{own(510)},
			service:    map[ethrpc.Hash]map[string]any{ownHash: listing(own(510).txHash, 510)},
			wantStatus: Executed, wantSource: SourceService, wantBlock: 510, wantSafeTxHash: ownHash, wantTiming: AfterFreeze,
		},
		{
			name: "executed after the resolution", executions: []execution{own(900)},
			service:    map[ethrpc.Hash]map[string]any{ownHash: listing(own(900).txHash, 900)},
			wantStatus: Executed, wantSource: SourceService, wantBlock: 900, wantSafeTxHash: ownHash, wantTiming: AfterResolution,
		},
		{
			// The service names a transaction whose receipt has no log of the execution, so
			// the chain is read instead.
			name: "misreported by the service", executions: []execution{own(502), other(600)}, startNonce: txNonce,
			service:    map[ethrpc.Hash]map[string]any{ownHash: listing(other(600).txHash, 600)},
			wantStatus: Executed, wantSource: SourceLogs, wantBlock: 502, wantSafeTxHash: ownHash, wantTiming: DuringVoting,
			wantNote: "which the chain doesn't confirm",
		},
		{
			name:       "not executed",
			service:    map[ethrpc.Hash]map[string]any{ownHash: listing(ethrpc.Hash{}, 0)},
			wantStatus: NotExecuted, wantSource: SourceNonce, wantNote: "doesn't list the Safe transaction as executed",
		},
		{
			name:       "not executed, without the service",
			wantStatus: NotExecuted, wantSource: SourceNonce,
		},
		{
			// No source has the other transaction, so the execution that used the nonce is
			// the only one in the blocks scanned, as the nonce rose by one.
			name: "nonce used by another transaction", executions: []execution{other(503)},
			wantStatus: OtherTransaction, wantSource: SourceLogs, wantBlock: 503, wantSafeTxHash: otherHash, wantTiming: DuringVoting,
			wantNote: "used the transaction's nonce 5 after the Safe block",
		},
		{
			// The Safe used the nonce before the Safe block, in the blocks scanned, in a
			// transaction that no source has.
			name: "stale nonce used by another transaction", executions: []execution{other(400), other(450)}, startNonce: txNonce - 1,
			wantStatus: OtherTransaction, wantSource: SourceLogs, wantBlock: 450, wantSafeTxHash: otherHash, wantTiming: BeforeProposal,
			wantStale: true, wantScanned: &executions.BlockRange{FromBlock: 401, ToBlock: safeBlock},
		},
		{
			// The Safe used the nonce before the blocks scanned.
			name: "stale nonce used before the scan", executions: []execution{other(300), other(350), other(450)}, startNonce: txNonce - 1,
			wantStatus: Unknown, wantStale: true, wantScanned: &executions.BlockRange{FromBlock: 401, ToBlock: safeBlock},
		},
		{
			// The Safe logged two executions after the Safe block, but its nonce rose by
			// one, as a delegate call that sets it can make it, so neither is taken to have
			// used the nonce, which it used all the same.
			name: "executions that the nonce doesn't account for", executions: []execution{other(503), {block: 504, safeTxHash: otherHash, txHash: ethrpc.Hash{31: 4}, keepsNonce: true}},
			wantStatus: OtherTransaction, wantSource: SourceLogs, wantNote: "used the transaction's nonce 5 after the Safe block",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mainnet := mainnetNode(latestMainnet, test.executions...)
			if test.startNonce != 0 {
				mainnet.startNonce = test.startNonce
			}
			var svc *txservice.Client
			if test.service != nil {
				svc = service(t, test.service, false)
			}
			c := checker(t, gnosisNode(latestGnosis), mainnet, svc)
			got, err := c.Check(t.Context(), request(), progress(latestGnosis))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			data, _ := json.MarshalIndent(got, "", "  ")
			x := got.Execution
			if x.Status != test.wantStatus || x.Source != test.wantSource {
				t.Errorf("Check: got execution %s from %q, want %s from %q\n%s", x.Status, x.Source, test.wantStatus, test.wantSource, data)
			}
			if test.wantBlock != 0 {
				if x.Block == nil || *x.Block != test.wantBlock || !x.BlockTime.Equal(mainnetTime(test.wantBlock)) || x.SafeTxHash == nil || *x.SafeTxHash != test.wantSafeTxHash {
					t.Errorf("Check: got execution %s, want one in block %d of %s\n%s", x.Status, test.wantBlock, test.wantSafeTxHash, data)
				}
			} else if x.Block != nil || x.TxHash != nil || x.SafeTxHash != nil {
				t.Errorf("Check: got an execution in a block, want none\n%s", data)
			}
			switch {
			case test.wantTiming == "" && got.Timing != nil:
				t.Errorf("Check: got timing %s, want none", got.Timing.Value)
			case test.wantTiming != "" && (got.Timing == nil || got.Timing.Value != test.wantTiming):
				t.Errorf("Check: got timing %+v, want %s\n%s", got.Timing, test.wantTiming, data)
			}
			if got.Nonce.Stale != test.wantStale {
				t.Errorf("Check: got stale %t, want %t", got.Nonce.Stale, test.wantStale)
			}
			if test.wantScanned != nil && (x.Scanned == nil || *x.Scanned != *test.wantScanned) {
				t.Errorf("Check: got blocks %+v scanned, want %+v", x.Scanned, test.wantScanned)
			}
			if (test.service != nil) != (x.Service != nil) {
				t.Errorf("Check: got service %+v, want it only with a service", x.Service)
			}
			if len(got.Notes) == 0 || got.Notes[0] != NotEvidence {
				t.Errorf("Check: got notes %q, want the first to say that effects after the Safe block aren't evidence", got.Notes)
			}
			if test.wantNote != "" && !slices.ContainsFunc(got.Notes, func(n string) bool { return strings.Contains(n, test.wantNote) }) {
				t.Errorf("Check: got notes %q, want one containing %q", got.Notes, test.wantNote)
			}
			if x.Reason == "" {
				t.Errorf("Check: got no reason for the execution's status")
			}
		})
	}
}

// TestCheckTiming checks the times that Check places an execution against.
func TestCheckTiming(t *testing.T) {
	const latestGnosis = 2400
	mainnet := mainnetNode(1000, execution{block: 510, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}})
	got, err := checker(t, gnosisNode(latestGnosis), mainnet, nil).Check(t.Context(), request(), progress(latestGnosis))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	commit, reveal := gnosisTime(commitDeadline), gnosisTime(revealDeadline)
	want := &Timing{
		Value:          AfterFreeze,
		Execution:      Point{Block: 510, Time: mainnetTime(510)},
		Proposal:       Point{Block: proposalBlock, Time: gnosisTime(proposalBlock)},
		CommitDeadline: Deadline{Block: commitDeadline, Reached: true, Time: &commit},
		RevealDeadline: Deadline{Block: revealDeadline, Reached: true, Time: &reveal},
		Frozen:         &Point{Block: frozenBlock, Time: gnosisTime(frozenBlock)},
		Resolved:       &Resolution{Point: Point{Block: resolvedBlock, Time: gnosisTime(resolvedBlock)}, Outcome: safenet.OutcomeSecure},
		Now:            Point{Block: latestGnosis, Time: gnosisTime(latestGnosis)},
	}
	g, _ := json.Marshal(got.Timing)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("Check: got timing\n%s\nwant\n%s", g, w)
	}
}

// TestCheckDeadlineNotReached checks an execution during the voting of a
// request whose reveal deadline is yet to come.
func TestCheckDeadlineNotReached(t *testing.T) {
	// Gnosis Chain block 1210 is at genesis plus 6050 seconds, before the reveal
	// deadline, and Mainnet block 504 at genesis plus 6048.
	const latestGnosis = 1210
	mainnet := mainnetNode(504, execution{block: 502, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}})
	p := progress(latestGnosis)
	p.State, p.Arbitration = safenet.StatePending, nil
	got, err := checker(t, gnosisNode(latestGnosis), mainnet, nil).Check(t.Context(), request(), p)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	timing := got.Timing
	if timing == nil || timing.Value != DuringVoting || !timing.CommitDeadline.Reached || timing.RevealDeadline.Reached ||
		timing.RevealDeadline.Time != nil || timing.Frozen != nil || timing.Resolved != nil || timing.Now.Block != latestGnosis {
		data, _ := json.MarshalIndent(timing, "", "  ")
		t.Errorf("Check: got timing\n%s\nwant during voting, with the commit deadline reached and the reveal deadline not", data)
	}
	if got.State != safenet.StatePending {
		t.Errorf("Check: got state %s, want PENDING", got.State)
	}
}

// TestCheckNotFrozen checks that an execution after the reveal deadline of a
// request that wasn't frozen is after the voting.
func TestCheckNotFrozen(t *testing.T) {
	const latestGnosis = 2400
	mainnet := mainnetNode(1000, execution{block: 600, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}})
	p := progress(latestGnosis)
	p.State, p.Arbitration = safenet.StateResolvedApproved, nil
	got, err := checker(t, gnosisNode(latestGnosis), mainnet, nil).Check(t.Context(), request(), p)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Timing == nil || got.Timing.Value != AfterVoting {
		t.Errorf("Check: got timing %+v, want after the voting", got.Timing)
	}
}

// TestCheckTie checks that an execution in a block with the same timestamp as
// the proposal isn't placed before it, and that the tie is noted.
func TestCheckTie(t *testing.T) {
	r := request()
	// Gnosis Chain block 1200 is at genesis plus 6000 seconds, as is Mainnet block
	// 500, which is then after the Safe block.
	r.Proposal.Block, r.Proposal.SafeBlock = 1200, 499
	mainnet := mainnetNode(1000, execution{block: 500, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}})
	got, err := checker(t, gnosisNode(2400), mainnet, nil).Check(t.Context(), r, progress(2400))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Timing == nil || got.Timing.Value != DuringVoting ||
		!slices.ContainsFunc(got.Notes, func(n string) bool { return strings.Contains(n, "same timestamp as the proposal") }) {
		t.Errorf("Check: got timing %+v and notes %q, want during voting, with a note on the tie", got.Timing, got.Notes)
	}
}

// TestCheckDecoded checks that another transaction that used the nonce is
// identified by its decoded nonce when a source has it, even if other
// executions make the count of executions differ from the nonce's rise.
func TestCheckDecoded(t *testing.T) {
	tx := transaction()
	tx.To = ethrpc.Address{19: 0x71}
	signature := make([]byte, 65)
	input := solabi.Call(solabi.Selector("execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)"),
		tx.To, tx.Value, []byte(tx.Data), uint64(tx.Operation), tx.SafeTxGas, tx.BaseGas, tx.GasPrice, tx.GasToken, tx.RefundReceiver, signature)
	decoded := execution{block: 503, safeTxHash: tx.Hash(), txHash: ethrpc.Hash{0: 0xdd, 31: 1}, input: input}
	mainnet := mainnetNode(1000, decoded, execution{block: 520, safeTxHash: otherHash, txHash: ethrpc.Hash{0: 0xdd, 31: 2}})
	got, err := checker(t, gnosisNode(2400), mainnet, nil).Check(t.Context(), request(), progress(2400))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	x := got.Execution
	if x.Status != OtherTransaction || x.SafeTxHash == nil || *x.SafeTxHash != tx.Hash() || !strings.Contains(x.Reason, "decoded from its calldata") {
		data, _ := json.MarshalIndent(x, "", "  ")
		t.Errorf("Check: got execution\n%s\nwant the other transaction, decoded from its calldata", data)
	}
}

func TestCheckScanFails(t *testing.T) {
	mainnet := mainnetNode(1000, execution{block: 502, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}})
	mainnet.logsError = map[string]any{"code": -32000, "message": "query timeout exceeded"}
	got, err := checker(t, gnosisNode(2400), mainnet, nil).Check(t.Context(), request(), progress(2400))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if x := got.Execution; x.Status != Unknown || x.Source != "" || !strings.Contains(x.Reason, "query timeout exceeded") || got.Timing != nil {
		t.Errorf("Check: got execution %+v and timing %+v, want unknown, with the node's error", x, got.Timing)
	}
}

// TestCheckOutsideScan checks that a nonce that the Safe used after the blocks
// scanned leaves the execution unknown.
func TestCheckOutsideScan(t *testing.T) {
	mainnet := mainnetNode(5000, execution{block: 3000, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}})
	got, err := checker(t, gnosisNode(20_000), mainnet, nil).Check(t.Context(), request(), progress(20_000))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if x := got.Execution; x.Status != Unknown || x.Scanned == nil || *x.Scanned != (executions.BlockRange{FromBlock: 501, ToBlock: 600}) ||
		!strings.Contains(x.Reason, "outside the blocks scanned") {
		t.Errorf("Check: got execution %+v, want unknown, after scanning blocks 501 to 600", x)
	}
}

func TestCheckTransient(t *testing.T) {
	executed := execution{block: 502, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}}
	for _, test := range []struct {
		name            string
		gnosis, mainnet []string
		service         bool
	}{
		{name: "nonce", mainnet: []string{"eth_call"}},
		{name: "receipt", mainnet: []string{"eth_getTransactionReceipt"}, service: true},
		{name: "logs", mainnet: []string{"eth_getLogs"}},
		{name: "execution time", mainnet: []string{"eth_getBlockByNumber"}},
		{name: "Gnosis Chain time", gnosis: []string{"eth_getBlockByNumber"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			gnosis, mainnet := gnosisNode(2400), mainnetNode(1000, executed)
			gnosis.failing, mainnet.failing = test.gnosis, test.mainnet
			var svc *txservice.Client
			if test.service {
				svc = service(t, map[ethrpc.Hash]map[string]any{ownHash: listing(executed.txHash, 502)}, false)
			}
			_, err := checker(t, gnosis, mainnet, svc).Check(t.Context(), request(), progress(2400))
			if !ethrpc.Transient(err) {
				t.Errorf("Check: got error %v, want a transient one", err)
			}
		})
	}
}

// TestCheckServiceFails checks that a service that fails is a note, and that
// the chain stands in for it.
func TestCheckServiceFails(t *testing.T) {
	mainnet := mainnetNode(1000, execution{block: 502, safeTxHash: ownHash, txHash: ethrpc.Hash{31: 1}})
	c := checker(t, gnosisNode(2400), mainnet, service(t, nil, true))
	var warnings []error
	c.Warn = func(err error) { warnings = append(warnings, err) }
	got, err := c.Check(t.Context(), request(), progress(2400))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Execution.Status != Executed || got.Execution.Source != SourceLogs || len(warnings) == 0 ||
		!slices.ContainsFunc(got.Notes, func(n string) bool { return strings.Contains(n, "Safe Transaction Service failed") }) {
		t.Errorf("Check: got execution %+v, notes %q, and warnings %v; want it from the logs, with a note and a warning on the service", got.Execution, got.Notes, warnings)
	}
}

func TestCheckQueue(t *testing.T) {
	r := request()
	proposal := func(id byte, chainID, nonce int64) safenet.SafeProposal {
		return safenet.SafeProposal{RequestID: ethrpc.Hash{0: 0x4e, 31: id}, Block: proposalBlock, ChainID: big.NewInt(chainID), Nonce: big.NewInt(nonce)}
	}
	own, same, next, elsewhere := proposal(1, 1, txNonce), proposal(2, 1, txNonce), proposal(3, 1, txNonce+1), proposal(4, 100, txNonce)
	for _, test := range []struct {
		name      string
		proposals []safenet.SafeProposal
		want      []Proposal
	}{
		{"others", []safenet.SafeProposal{same, own, next, elsewhere}, []Proposal{{same, true}, {next, false}}},
		{"without its own", []safenet.SafeProposal{same}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := checker(t, gnosisNode(2400), mainnetNode(1000), nil)
			var from, to uint64
			c.Proposals = func(_ context.Context, f, t uint64) ([]safenet.SafeProposal, error) {
				from, to = f, t
				return test.proposals, nil
			}
			got, err := c.Check(t.Context(), r, progress(2400))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if from != proposalBlock-50 || to != proposalBlock+50 {
				t.Errorf("Check: listed the proposals in blocks %d to %d, want %d to %d", from, to, proposalBlock-50, proposalBlock+50)
			}
			if test.want == nil {
				if got.Queue != nil || !slices.ContainsFunc(got.Notes, func(n string) bool { return strings.Contains(n, "own proposal isn't among") }) {
					t.Errorf("Check: got queue %+v and notes %q, want none, and a note", got.Queue, got.Notes)
				}
				return
			}
			if got.Queue == nil || !reflectEqualJSON(t, got.Queue.Proposals, test.want) {
				t.Errorf("Check: got queue %+v, want %+v", got.Queue, test.want)
			}
			if !slices.ContainsFunc(got.Notes, func(n string) bool { return strings.Contains(n, "with the same nonce") }) {
				t.Errorf("Check: got notes %q, want one on the proposal with the same nonce", got.Notes)
			}
		})
	}
}

// reflectEqualJSON reports whether a and b have the same JSON encoding.
func reflectEqualJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}
