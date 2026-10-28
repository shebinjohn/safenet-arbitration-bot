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

// listing is an executed Safe transaction as a Safe Transaction Service lists
// it.
type listing struct {
	tx         *safenet.SafeTransaction
	safeTxHash ethrpc.Hash
	txHash     ethrpc.Hash
	block      uint64
}

// listed returns the listing of tx, executed in the onchain transaction hash at
// block.
func listed(tx *safenet.SafeTransaction, hash ethrpc.Hash, block uint64) listing {
	return listing{tx, tx.Hash(), hash, block}
}

// historyService starts a Safe Transaction Service that lists the executions of
// the Safe, or fails to if listings is nil, and returns a client for it. It has
// no transactions by hash.
func historyService(t *testing.T, listings []listing) *txservice.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/safes/"+safe.String()+"/multisig-transactions/" {
			http.NotFound(w, r)
			return
		}
		if listings == nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		results := []map[string]any{}
		for _, l := range listings {
			tx := l.tx
			results = append(results, map[string]any{
				"safe": tx.Safe, "to": tx.To, "value": tx.Value.String(), "data": tx.Data,
				"operation": uint8(tx.Operation), "safeTxGas": tx.SafeTxGas.Uint64(), "baseGas": tx.BaseGas.Uint64(),
				"gasPrice": tx.GasPrice.String(), "gasToken": tx.GasToken, "refundReceiver": tx.RefundReceiver,
				"nonce": tx.Nonce.Uint64(), "safeTxHash": l.safeTxHash, "transactionHash": l.txHash, "blockNumber": l.block,
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(results), "results": results})
	}))
	t.Cleanup(server.Close)
	return txservice.NewClient(server.URL, ethrpc.Mainnet)
}

// executed returns what Find reports about an execution of tx in the onchain
// transaction hash at block, from source.
func executed(tx *safenet.SafeTransaction, hash ethrpc.Hash, block uint64, source Source) Execution {
	return Execution{block, hash, 0, tx.Hash(), true, big.NewInt(9), source, tx}
}

// checkExecutions reports the executions in got that differ from want. They are
// compared as JSON, since a computed big.Int zero isn't DeepEqual to
// big.NewInt(0).
func checkExecutions(t *testing.T, got, want []Execution) {
	t.Helper()
	for i := range max(len(got), len(want)) {
		var g, w []byte
		if i < len(got) {
			g, _ = json.Marshal(got[i])
		}
		if i < len(want) {
			w, _ = json.Marshal(want[i])
		}
		if string(g) != string(w) {
			t.Errorf("execution %d:\ngot  %s\nwant %s", i, g, w)
		}
	}
}

func TestFindAll(t *testing.T) {
	const last = 200
	// Three executions, at the nonces 0 to 2, and a Safe whose nonce is 3 at the
	// last block, which the service lists with one after it.
	viaCalldata := transaction("calldata", 0)
	viaEvent := transaction("event", 1)
	viaService := transaction("service", 2)
	later := transaction("later", 3)
	c := &chain{
		nonce: big.NewInt(3),
		txs: map[ethrpc.Hash]ethrpc.Transaction{
			hash(1): {To: &safe, Input: execCall(viaCalldata)},
			hash(2): {To: &relayer, Input: []byte("relay")},
			hash(3): {To: &relayer, Input: []byte("relay")},
		},
		logs: []ethrpc.Log{
			executionLog(executionSuccessEvent, viaCalldata.Hash(), hash(1), false, 100, 0),
			eventLog(viaEvent, hash(2), 110, 0),
			executionLog(executionSuccessEvent, viaEvent.Hash(), hash(2), true, 110, 1),
			executionLog(executionSuccessEvent, viaService.Hash(), hash(3), true, 120, 0),
			executionLog(executionSuccessEvent, later.Hash(), hash(4), true, last+1, 0),
		},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{
		listed(viaCalldata, hash(1), 100), listed(viaEvent, hash(2), 110), listed(viaService, hash(3), 120), listed(later, hash(4), last+1),
	})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }

	got, err := f.FindAll(t.Context(), safe, last)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	executionWithEvent := executed(viaEvent, hash(2), 110, SourceEvent)
	executionWithEvent.LogIndex = 1
	checkExecutions(t, got.Executions, []Execution{
		executed(viaCalldata, hash(1), 100, SourceCalldata),
		executionWithEvent,
		executed(viaService, hash(3), 120, SourceService),
	})
	if !got.Complete() || got.Nonce.Int64() != 3 || len(got.Scanned) != 0 {
		t.Errorf("FindAll: got nonce %s and scanned %v, complete %t; want nonce 3, no scans, complete", got.Nonce, got.Scanned, got.Complete())
	}
	// It reads the logs of each block that the service lists up to the last block,
	// and scans no others.
	if want := []BlockRange{{100, 100}, {110, 110}, {120, 120}}; !reflect.DeepEqual(c.ranges, want) {
		t.Errorf("FindAll: read the logs of the blocks %v, want %v", c.ranges, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: got %v, want none", warnings)
	}
}

func TestFindAllMissing(t *testing.T) {
	const last = 200
	// Five executions, at the nonces 0 to 4, of a Safe deployed at block 90, of
	// which the service only lists those at the nonces 1 and 3, and misreports
	// others.
	var txs []*safenet.SafeTransaction
	c := &chain{nonce: big.NewInt(5), deployed: 90, txs: make(map[ethrpc.Hash]ethrpc.Transaction)}
	for n := range 5 {
		tx := transaction("calldata", int64(n))
		txs = append(txs, tx)
		c.txs[hash(byte(n+1))] = ethrpc.Transaction{To: &safe, Input: execCall(tx)}
		c.logs = append(c.logs, executionLog(executionSuccessEvent, tx.Hash(), hash(byte(n+1)), true, uint64(100+10*n), 0))
	}
	tampered := *txs[4]
	tampered.Data = []byte("not what was executed")
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{
		listed(txs[1], hash(2), 110),
		listed(txs[3], hash(4), 130),
		// A transaction whose fields don't have its hash, so it says nothing about its
		// nonce.
		{&tampered, txs[4].Hash(), hash(5), 140},
		// A transaction that wasn't executed at the block that the service says.
		listed(transaction("never", 2), hash(9), 150),
	})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }

	got, err := f.FindAll(t.Context(), safe, last)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	var want []Execution
	for n, tx := range txs {
		want = append(want, executed(tx, hash(byte(n+1)), uint64(100+10*n), SourceCalldata))
	}
	checkExecutions(t, got.Executions, want)
	// The missing executions are between those with the nonces before and after
	// them, after the deployment for the first, and up to the last block for the
	// last.
	scanned := []BlockRange{{90, 110}, {110, 130}, {130, last}}
	if !got.Complete() || !reflect.DeepEqual(got.Scanned, scanned) {
		t.Errorf("FindAll: got scanned %v, complete %t; want %v, complete", got.Scanned, got.Complete(), scanned)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: got %v, want none", warnings)
	}
}

func TestFindAllIncomplete(t *testing.T) {
	// A Safe whose nonce counts an execution that its logs don't have, such as one
	// in a block that the node misses, and a service that lists none.
	tx := transaction("calldata", 0)
	c := &chain{
		nonce: big.NewInt(2), deployed: 50,
		txs:  map[ethrpc.Hash]ethrpc.Transaction{hash(1): {To: &safe, Input: execCall(tx)}},
		logs: []ethrpc.Log{executionLog(executionSuccessEvent, tx.Hash(), hash(1), true, 100, 0)},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{})}
	got, err := f.FindAll(t.Context(), safe, 200)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	// The whole history is scanned. The execution's nonce is worked out from the
	// Safe's, which is wrong, so its calldata doesn't check out.
	if got.Complete() || len(got.Executions) != 1 || got.Executions[0].Transaction != nil ||
		!reflect.DeepEqual(got.Scanned, []BlockRange{{50, 200}}) {
		t.Errorf("FindAll: got %+v, complete %t; want one unknown execution, from scanning blocks 50 to 200, incomplete", got, got.Complete())
	}

	// A Safe that executed nothing has a complete history without any scans.
	c = &chain{nonce: big.NewInt(0)}
	f = &Finder{Eth: c.serve(t), Service: historyService(t, []listing{})}
	if got, err := f.FindAll(t.Context(), safe, 200); err != nil || !got.Complete() || len(got.Executions) != 0 || len(got.Scanned) != 0 || len(c.ranges) != 0 {
		t.Errorf("FindAll of no executions: got %+v, error %v, and log reads %v; want an empty, complete history without reads", got, err, c.ranges)
	}
}

func TestFindAllWithoutDeployment(t *testing.T) {
	// A node that has no code for the Safe, such as one that doesn't serve the code
	// of old blocks, leaves the first executions unfound, with a warning.
	tx := transaction("calldata", 1)
	c := &chain{
		nonce: big.NewInt(2),
		txs:   map[ethrpc.Hash]ethrpc.Transaction{hash(1): {To: &safe, Input: execCall(tx)}},
		logs:  []ethrpc.Log{executionLog(executionSuccessEvent, tx.Hash(), hash(1), true, 100, 0)},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{listed(tx, hash(1), 100)})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }
	got, err := f.FindAll(t.Context(), safe, 200)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got.Complete() || len(got.Executions) != 1 || len(got.Scanned) != 0 {
		t.Errorf("FindAll: got %+v; want one execution, no scans, incomplete", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "finding the block that") || !strings.Contains(warnings[0].Error(), "has no code at block 100") {
		t.Errorf("warnings: got %v, want one about finding the deployment", warnings)
	}
}

func TestFindAllErrors(t *testing.T) {
	c := &chain{nonce: big.NewInt(1)}
	eth := c.serve(t)
	if _, err := (&Finder{Eth: eth}).FindAll(t.Context(), safe, 200); err == nil || !strings.Contains(err.Error(), "needs the Safe Transaction Service") {
		t.Errorf("FindAll without a service: got error %v, want one about the service", err)
	}
	f := &Finder{Eth: eth, Service: historyService(t, nil)}
	if _, err := f.FindAll(t.Context(), safe, 200); err == nil || !strings.Contains(err.Error(), "listing the executions") || !strings.Contains(err.Error(), "500") {
		t.Errorf("FindAll with a failing service: got error %v, want one about listing the executions", err)
	}
	f = &Finder{Eth: (&chain{}).serve(t), Service: historyService(t, []listing{})}
	if _, err := f.FindAll(t.Context(), safe, 200); err == nil || !strings.Contains(err.Error(), "reading the nonce") {
		t.Errorf("FindAll without the nonce: got error %v, want one about reading it", err)
	}
}
