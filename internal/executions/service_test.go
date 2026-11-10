package executions

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// listed is an execution that a Safe Transaction Service lists.
type listed struct {
	tx         *safenet.SafeTransaction
	safeTxHash ethrpc.Hash
	txHash     ethrpc.Hash
	block      uint64
}

// listingService starts a Safe Transaction Service that lists the executions of
// the Safe, and returns a client for it. It says that the transaction with the
// hash creation created the Safe, if given.
func listingService(t *testing.T, executions []listed, creation ...ethrpc.Hash) *txservice.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/safes/"+safe.String()+"/creation/" && len(creation) > 0 {
			json.NewEncoder(w).Encode(map[string]any{"transactionHash": creation[0], "creator": relayer})
			return
		}
		if r.URL.Path != "/api/v1/safes/"+safe.String()+"/multisig-transactions/" {
			http.NotFound(w, r)
			return
		}
		results := []map[string]any{}
		if r.URL.Query().Get("offset") == "0" {
			for _, e := range executions {
				tx := e.tx
				results = append(results, map[string]any{
					"safe": tx.Safe, "to": tx.To, "value": tx.Value.String(), "data": tx.Data,
					"operation": uint8(tx.Operation), "safeTxGas": tx.SafeTxGas.Uint64(), "baseGas": tx.BaseGas.Uint64(),
					"gasPrice": tx.GasPrice.String(), "gasToken": tx.GasToken, "refundReceiver": tx.RefundReceiver,
					"nonce": tx.Nonce.Uint64(), "safeTxHash": e.safeTxHash, "transactionHash": e.txHash,
					"blockNumber": e.block, "isExecuted": true,
				})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	t.Cleanup(server.Close)
	return txservice.NewClient(server.URL, ethrpc.Mainnet)
}

func TestFromService(t *testing.T) {
	const last = 200
	// The Safe's nonce is 7 at the last block. The executions at the nonces 0 and 1
	// are confirmed, 2 isn't listed, and 3 to 6 are listed wrongly.
	ok0, ok1 := transaction("first", 0), transaction("second", 1)
	wrongHash := transaction("wrong hash", 3)
	noLog := transaction("no log", 4)
	noReceipt := transaction("no receipt", 5)
	otherSafe := transaction("other Safe", 6)
	otherSafe.Safe = relayer
	later := transaction("later", 7)
	receiptLater := transaction("receipt later", 8)

	receipt := func(hash ethrpc.Hash, block uint64, logs ...ethrpc.Log) ethrpc.Receipt {
		return ethrpc.Receipt{TransactionHash: hash, BlockNumber: ethrpc.BlockNumber(block), Status: 1, Logs: logs}
	}
	c := &chain{
		nonce: big.NewInt(7),
		receipts: map[ethrpc.Hash]ethrpc.Receipt{
			// The two first executions are in one onchain transaction, a batch, with a log
			// of another contract before them. Safe 1.3.0 has the hash in the data.
			hash(1): receipt(hash(1), 120,
				func() ethrpc.Log {
					l := executionLog(executionSuccessEvent, ok0.Hash(), hash(1), true, 120, 0)
					l.Address = relayer
					return l
				}(),
				executionLog(executionFailureEvent, ok1.Hash(), hash(1), false, 120, 1),
				executionLog(executionSuccessEvent, ok0.Hash(), hash(1), true, 120, 2),
			),
			hash(3): receipt(hash(3), 130),
			hash(4): receipt(hash(4), 140, executionLog(executionSuccessEvent, hash(9), hash(4), true, 140, 0)),
			hash(8): receipt(hash(8), last+1, executionLog(executionSuccessEvent, receiptLater.Hash(), hash(8), true, last+1, 0)),
		},
	}
	f := &Finder{
		Eth: c.serve(t),
		Service: listingService(t, []listed{
			{ok0, ok0.Hash(), hash(1), 120},
			{ok1, ok1.Hash(), hash(1), 120},
			{wrongHash, hash(9), hash(3), 130},
			{noLog, noLog.Hash(), hash(4), 140},
			{noReceipt, noReceipt.Hash(), hash(5), 150},
			{otherSafe, otherSafe.Hash(), hash(6), 160},
			{later, later.Hash(), hash(7), last + 1},
			{receiptLater, receiptLater.Hash(), hash(8), last},
		}),
	}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }

	got, err := f.FromService(t.Context(), safe, last)
	if err != nil {
		t.Fatalf("FromService: %v", err)
	}
	wantExecutions := []Execution{
		{120, hash(1), 1, ok1.Hash(), false, big.NewInt(9), SourceService, ok1},
		{120, hash(1), 2, ok0.Hash(), true, big.NewInt(9), SourceService, ok0},
	}
	if !reflect.DeepEqual(got.Executions, wantExecutions) {
		g, _ := json.MarshalIndent(got.Executions, "", "  ")
		w, _ := json.MarshalIndent(wantExecutions, "", "  ")
		t.Errorf("executions:\ngot  %s\nwant %s", g, w)
	}
	wantRejected := map[ethrpc.Hash]string{
		hash(9):          "don't hash",
		noLog.Hash():     "logged no execution",
		noReceipt.Hash(): "no receipt",
		otherSafe.Hash(): "for the Safe",
	}
	if len(got.Rejected) != len(wantRejected) {
		t.Errorf("rejected: got %+v, want %d", got.Rejected, len(wantRejected))
	}
	for _, r := range got.Rejected {
		if want, ok := wantRejected[r.SafeTxHash]; !ok || !strings.Contains(r.Reason, want) {
			t.Errorf("rejected %s: got reason %q, want one containing %q", r.SafeTxHash, r.Reason, want)
		}
	}
	if got.Nonce.Int64() != 7 || !reflect.DeepEqual(got.Missing, []NonceRange{{2, 6}}) {
		t.Errorf("nonce: got %s with missing %v, want 7 with 2 to 6 missing", got.Nonce, got.Missing)
	}
	// The only warning is the missing receipt.
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "receipt") {
		t.Errorf("warnings: got %v, want one about the receipt", warnings)
	}
}

func TestFromServiceWithoutNonce(t *testing.T) {
	f := &Finder{Eth: (&chain{}).serve(t), Service: listingService(t, nil)}
	got, err := f.FromService(t.Context(), safe, 200)
	if err != nil {
		t.Fatalf("FromService: %v", err)
	}
	if got.Nonce != nil || got.Missing != nil || len(got.Executions) != 0 {
		t.Errorf("FromService: got %+v, want no executions and an unknown nonce", got)
	}

	f.Service = nil
	if _, err := f.FromService(t.Context(), safe, 200); err == nil {
		t.Errorf("FromService without a service: expected an error")
	}
}

func TestMissing(t *testing.T) {
	executions := []Execution{{Transaction: transaction("", 1)}, {Transaction: transaction("", 2)}, {Transaction: transaction("", 5)}}
	if got, want := missing(executions, 8), []NonceRange{{0, 0}, {3, 4}, {6, 7}}; !reflect.DeepEqual(got, want) {
		t.Errorf("missing: got %v, want %v", got, want)
	}
	if got := missing(nil, 0); len(got) != 0 {
		t.Errorf("missing with nonce 0: got %v, want none", got)
	}
}

func TestCreationFromService(t *testing.T) {
	const last = 200
	factory := ethrpc.Address{0: 0xfa, 19: 1}
	// A ProxyCreation log of the factory, with the proxy as a topic like 1.4.1, and
	// one of another contract for another proxy before it.
	proxyCreation := func(emitter, proxy ethrpc.Address) ethrpc.Log {
		var topic ethrpc.Hash
		copy(topic[12:], proxy[:])
		return ethrpc.Log{Address: emitter, Topics: []ethrpc.Hash{proxyCreationEvent, topic}, Data: make([]byte, 32)}
	}
	created := ethrpc.Receipt{TransactionHash: hash(1), BlockNumber: 100, Status: 1, Logs: []ethrpc.Log{
		proxyCreation(relayer, target), proxyCreation(factory, safe),
	}}
	tests := []struct {
		name     string
		created  uint64
		creation []ethrpc.Hash
		receipts map[ethrpc.Hash]ethrpc.Receipt
		want     *Creation
		warning  string
	}{
		{"confirmed", 100, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): created}, &Creation{100, hash(1), &factory}, ""},
		{"without a log", 100, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): {BlockNumber: 100}}, &Creation{100, hash(1), nil}, ""},
		{"code before", 90, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): created}, nil, "has code at block 99"},
		{"no code", 0, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): created}, nil, "no code at block 100"},
		{"later", 100, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): {BlockNumber: last + 1}}, nil, ""},
		{"no receipt", 100, []ethrpc.Hash{hash(1)}, nil, nil, "receipt"},
		{"unknown", 100, nil, nil, nil, "not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &chain{created: test.created, receipts: test.receipts}
			f := &Finder{Eth: c.serve(t), Service: listingService(t, nil, test.creation...)}
			var warnings []error
			f.Warn = func(err error) { warnings = append(warnings, err) }
			if got := f.CreationFromService(t.Context(), safe, last); !reflect.DeepEqual(got, test.want) {
				t.Errorf("CreationFromService: got %+v, want %+v", got, test.want)
			}
			switch {
			case test.warning == "" && len(warnings) != 0:
				t.Errorf("warnings: got %v, want none", warnings)
			case test.warning != "" && (len(warnings) != 1 || !strings.Contains(warnings[0].Error(), test.warning)):
				t.Errorf("warnings: got %v, want one containing %q", warnings, test.warning)
			}
		})
	}
}
