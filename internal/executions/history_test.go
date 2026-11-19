package executions

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// listing is an executed Safe transaction as a Safe Transaction Service lists
// it. A listing without a transaction hash has neither it nor a block.
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
// the Safe, as the service lists them, or fails to if listings is nil, and
// returns a client for it. It says that the transaction with the hash creation
// created the Safe, if given. It has no transactions by hash.
func historyService(t *testing.T, listings []listing, creation ...ethrpc.Hash) *txservice.Client {
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
		if listings == nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		results := []map[string]any{}
		for _, l := range listings {
			tx := l.tx
			result := map[string]any{
				"safe": tx.Safe, "to": tx.To, "value": tx.Value.String(), "data": tx.Data,
				"operation": uint8(tx.Operation), "safeTxGas": tx.SafeTxGas.Uint64(), "baseGas": tx.BaseGas.Uint64(),
				"gasPrice": tx.GasPrice.String(), "gasToken": tx.GasToken, "refundReceiver": tx.RefundReceiver,
				"nonce": tx.Nonce.Uint64(), "safeTxHash": l.safeTxHash, "transactionHash": l.txHash, "blockNumber": l.block,
				"isExecuted": true,
			}
			if l.txHash == (ethrpc.Hash{}) {
				result["transactionHash"], result["blockNumber"] = nil, nil
			}
			results = append(results, result)
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(results), "results": results})
	}))
	t.Cleanup(server.Close)
	return txservice.NewClient(server.URL, ethrpc.Mainnet)
}

// executed returns what Find reports about an execution of tx in the onchain
// transaction hash at block, from source.
func executed(tx *safenet.SafeTransaction, hash ethrpc.Hash, block uint64, source Source) Execution {
	return Execution{block, time.Time{}, hash, 0, tx.Hash(), true, big.NewInt(9), source, tx}
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

// checkRejected reports the rejected executions in got that differ from want,
// which has the start of the reason of each by Safe transaction hash, in order.
func checkRejected(t *testing.T, got []Rejected, want []ethrpc.Hash, reasons []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("rejected: got %+v, want %d", got, len(want))
		return
	}
	for i, r := range got {
		if r.SafeTxHash != want[i] || !strings.Contains(r.Reason, reasons[i]) {
			t.Errorf("rejected %d: got %s with reason %q, want %s with one containing %q", i, r.SafeTxHash, r.Reason, want[i], reasons[i])
		}
	}
}

func TestFindAll(t *testing.T) {
	const last = 200
	// Four executions, at the nonces 0 to 3, and a Safe whose nonce is 4 at the
	// last block. The service lists three of them, with one after them, and the one
	// at nonce 2 is found by scanning the blocks between those at the nonces 1 and
	// 3.
	viaService := transaction("service", 0)
	viaEvent := transaction("event", 1)
	viaCalldata := transaction("calldata", 2)
	viaService2 := transaction("service again", 3)
	later := transaction("later", 4)
	c := &chain{
		nonce: big.NewInt(4),
		txs: map[ethrpc.Hash]ethrpc.Transaction{
			hash(3): {To: &safe, Input: execCall(viaCalldata)},
		},
		logs: []ethrpc.Log{
			executionLog(executionSuccessEvent, viaService.Hash(), hash(1), false, 100, 0),
			eventLog(viaEvent, hash(2), 110, 0),
			executionLog(executionSuccessEvent, viaEvent.Hash(), hash(2), true, 110, 1),
			executionLog(executionSuccessEvent, viaCalldata.Hash(), hash(3), true, 115, 0),
			executionLog(executionSuccessEvent, viaService2.Hash(), hash(4), true, 120, 0),
			executionLog(executionSuccessEvent, later.Hash(), hash(5), true, last+1, 0),
		},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{
		listed(viaService, hash(1), 100), listed(viaEvent, hash(2), 110), listed(viaService2, hash(4), 120), listed(later, hash(5), last+1),
	})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }

	got, err := f.FindAll(t.Context(), safe, last)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	// A transaction that the service lists is taken from it, unless the Safe's
	// SafeMultiSigTransaction event has it.
	executionWithEvent := executed(viaEvent, hash(2), 110, SourceEvent)
	executionWithEvent.LogIndex = 1
	checkExecutions(t, got.Executions, []Execution{
		executed(viaService, hash(1), 100, SourceService),
		executionWithEvent,
		executed(viaCalldata, hash(3), 115, SourceCalldata),
		executed(viaService2, hash(4), 120, SourceService),
	})
	if !got.Complete() || got.Nonce.Int64() != 4 || len(got.Missing) != 0 || len(got.Rejected) != 0 || got.Creation != nil ||
		!reflect.DeepEqual(got.Scanned, []BlockRange{{110, 120}}) {
		t.Errorf("FindAll: got %+v, complete %t; want nonce 4, blocks 110 to 120 scanned, none missing or rejected, complete", got, got.Complete())
	}
	// It reads the logs of each block that the service lists up to the last block,
	// then those of the gap.
	if want := []BlockRange{{100, 100}, {110, 110}, {120, 120}, {110, 120}}; !reflect.DeepEqual(c.ranges, want) {
		t.Errorf("FindAll: read the logs of the blocks %v, want %v", c.ranges, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: got %v, want none", warnings)
	}
}

func TestFindAllMissing(t *testing.T) {
	const last = 200
	// Five executions, at the nonces 0 to 4, of a Safe deployed at block 90, of
	// which the service only lists those at the nonces 1 and 3 rightly.
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
	never := transaction("never", 2)
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{
		listed(txs[1], hash(2), 110),
		listed(txs[3], hash(4), 130),
		// A transaction whose fields don't have its hash, so it says nothing about its
		// nonce, but whose block has the execution that it misreports.
		{&tampered, txs[4].Hash(), hash(5), 140},
		// A transaction that wasn't executed at the block that the service says.
		listed(never, hash(9), 150),
	})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }

	got, err := f.FindAll(t.Context(), safe, last)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	var want []Execution
	for n, tx := range txs {
		source := SourceCalldata
		if n == 1 || n == 3 {
			source = SourceService
		}
		want = append(want, executed(tx, hash(byte(n+1)), uint64(100+10*n), source))
	}
	checkExecutions(t, got.Executions, want)
	// The missing executions are between those with the nonces before and after
	// them, after the deployment for the first, and up to the last block for the
	// last.
	scanned := []BlockRange{{90, 110}, {110, 130}, {130, last}}
	if !got.Complete() || !reflect.DeepEqual(got.Scanned, scanned) {
		t.Errorf("FindAll: got scanned %v, missing %v, complete %t; want %v, complete", got.Scanned, got.Missing, got.Complete(), scanned)
	}
	checkRejected(t, got.Rejected, []ethrpc.Hash{txs[4].Hash(), never.Hash()},
		[]string{"fields don't hash", "the Safe logged no execution of it in block 150"})
	// The block without the Safe's executions is read again before the listing is
	// rejected.
	reads := 0
	for _, r := range c.ranges {
		if r == (BlockRange{150, 150}) {
			reads++
		}
	}
	if reads != 2 {
		t.Errorf("FindAll: read block 150 %d times, want 2", reads)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: got %v, want none", warnings)
	}
}

func TestFindAllRejected(t *testing.T) {
	const last = 200
	// The Safe's nonce is 7 at the last block. The executions at the nonces 0 and 1
	// are confirmed, 2 to 4 aren't executed, and 5 and 6 are executed but listed
	// wrongly.
	ok0, ok1 := transaction("first", 0), transaction("second", 1)
	wrongHash := transaction("wrong hash", 3)
	noLog := transaction("no log", 4)
	wrongTx := transaction("wrong transaction", 5)
	noTx := transaction("no transaction", 6)
	otherSafe := transaction("other Safe", 6)
	otherSafe.Safe = relayer
	later := transaction("later", 7)

	foreign := executionLog(executionSuccessEvent, ok0.Hash(), hash(1), true, 120, 0)
	foreign.Address = relayer
	c := &chain{
		nonce: big.NewInt(7),
		logs: []ethrpc.Log{
			// The two first executions are in one onchain transaction, a batch, with a log
			// of another contract before them. Safe 1.3.0 has the hash in the data.
			foreign,
			executionLog(executionFailureEvent, ok1.Hash(), hash(1), false, 120, 1),
			executionLog(executionSuccessEvent, ok0.Hash(), hash(1), true, 120, 2),
			executionLog(executionSuccessEvent, wrongTx.Hash(), hash(15), true, 150, 0),
			executionLog(executionSuccessEvent, noTx.Hash(), hash(16), true, 160, 0),
		},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{
		listed(ok0, hash(1), 120),
		listed(ok1, hash(1), 120),
		{wrongHash, hash(9), hash(3), 130},
		listed(noLog, hash(4), 140),
		listed(wrongTx, hash(5), 150),
		{noTx, noTx.Hash(), ethrpc.Hash{}, 0},
		listed(otherSafe, hash(6), 160),
		listed(later, hash(7), last+1),
	})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }

	got, err := f.FindAll(t.Context(), safe, last)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	failure := executed(ok1, hash(1), 120, SourceService)
	failure.LogIndex, failure.Success = 1, false
	success := executed(ok0, hash(1), 120, SourceService)
	success.LogIndex = 2
	// The executions listed with the wrong transaction or none are found all the
	// same, in the blocks that the service names.
	checkExecutions(t, got.Executions, []Execution{
		failure, success,
		executed(wrongTx, hash(15), 150, SourceService),
		executed(noTx, hash(16), 160, SourceService),
	})
	checkRejected(t, got.Rejected,
		[]ethrpc.Hash{hash(9), noLog.Hash(), wrongTx.Hash(), noTx.Hash(), otherSafe.Hash()},
		[]string{
			"fields don't hash", "no execution of it in block 140", "in the transaction " + hash(15).String(),
			"no onchain transaction or block", "for the Safe " + relayer.String(),
		})
	if got.Complete() || got.Nonce.Int64() != 7 || !reflect.DeepEqual(got.Missing, []NonceRange{{2, 4}}) {
		t.Errorf("FindAll: got nonce %s with missing %v, complete %t; want 7 with 2 to 4 missing, incomplete", got.Nonce, got.Missing, got.Complete())
	}
	// The execution listed without its transaction is in the block that the listing
	// for another Safe names, so only the gap before it is scanned.
	if want := []BlockRange{{120, 150}}; !reflect.DeepEqual(got.Scanned, want) {
		t.Errorf("FindAll: got scanned %v, want %v", got.Scanned, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: got %v, want none", warnings)
	}
}

func TestFindAllEmptyBlock(t *testing.T) {
	// A block served once without its logs is read again, so the execution is
	// confirmed rather than rejected.
	tx := transaction("first", 0)
	c := &chain{
		nonce:     big.NewInt(1),
		logs:      []ethrpc.Log{executionLog(executionSuccessEvent, tx.Hash(), hash(1), true, 120, 0)},
		emptyOnce: map[uint64]bool{120: true},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{listed(tx, hash(1), 120)})}
	got, err := f.FindAll(t.Context(), safe, 200)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	checkExecutions(t, got.Executions, []Execution{executed(tx, hash(1), 120, SourceService)})
	if !got.Complete() || len(got.Rejected) != 0 || !reflect.DeepEqual(c.ranges, []BlockRange{{120, 120}, {120, 120}}) {
		t.Errorf("FindAll: got %+v, complete %t, with the logs of %v read; want the execution confirmed, after reading block 120 twice", got, got.Complete(), c.ranges)
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
	// Safe's, which is wrong, so its calldata doesn't check out, and both nonces
	// are missing.
	if got.Complete() || len(got.Executions) != 1 || got.Executions[0].Transaction != nil ||
		!reflect.DeepEqual(got.Scanned, []BlockRange{{50, 200}}) || !reflect.DeepEqual(got.Missing, []NonceRange{{0, 1}}) {
		t.Errorf("FindAll: got %+v, complete %t; want one unknown execution, from scanning blocks 50 to 200, both nonces missing", got, got.Complete())
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
		logs:  []ethrpc.Log{executionLog(executionSuccessEvent, tx.Hash(), hash(1), true, 100, 0)},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{listed(tx, hash(1), 100)})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }
	got, err := f.FindAll(t.Context(), safe, 200)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got.Complete() || len(got.Executions) != 1 || len(got.Scanned) != 0 || !reflect.DeepEqual(got.Missing, []NonceRange{{0, 0}}) {
		t.Errorf("FindAll: got %+v; want one execution, no scans, nonce 0 missing", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "finding the block that") || !strings.Contains(warnings[0].Error(), "has no code at block 100") {
		t.Errorf("warnings: got %v, want one about finding the deployment", warnings)
	}
}

func TestFindAllFromCreation(t *testing.T) {
	// The scan for the first executions starts at the Safe's creation, which the
	// service reports and the chain confirms, without searching for its code.
	tx := transaction("calldata", 1)
	first := transaction("calldata", 0)
	c := &chain{
		nonce: big.NewInt(2), deployed: 80,
		txs: map[ethrpc.Hash]ethrpc.Transaction{hash(2): {To: &safe, Input: execCall(first)}},
		logs: []ethrpc.Log{
			executionLog(executionSuccessEvent, first.Hash(), hash(2), true, 90, 0),
			executionLog(executionSuccessEvent, tx.Hash(), hash(1), true, 100, 0),
		},
		receipts: map[ethrpc.Hash]ethrpc.Receipt{hash(7): {TransactionHash: hash(7), BlockNumber: 80, Status: 1}},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{listed(tx, hash(1), 100)}, hash(7))}
	got, err := f.FindAll(t.Context(), safe, 200)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if !got.Complete() || len(got.Executions) != 2 || !reflect.DeepEqual(got.Scanned, []BlockRange{{80, 100}}) ||
		!reflect.DeepEqual(got.Creation, &Creation{80, time.Time{}, hash(7), nil}) {
		t.Errorf("FindAll: got %+v; want two executions, from scanning blocks 80 to 100, and the creation at block 80", got)
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

func TestFindAllNodeFailure(t *testing.T) {
	// The node fails to serve the logs of the second block that the service names,
	// as a node does that rate limits the client midway. That says nothing about
	// the history, so FindAll fails rather than reject the listing.
	ok, failing := transaction("first", 0), transaction("second", 1)
	c := &chain{
		nonce: big.NewInt(2),
		logs: []ethrpc.Log{
			executionLog(executionSuccessEvent, ok.Hash(), hash(1), true, 100, 0),
			executionLog(executionSuccessEvent, failing.Hash(), hash(2), true, 110, 0),
		},
		failingBlocks: []uint64{110},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{listed(ok, hash(1), 100), listed(failing, hash(2), 110)})}
	if got, err := f.FindAll(t.Context(), safe, 200); !ethrpc.Transient(err) {
		t.Errorf("FindAll with a failing block: got %+v, %v, want a transient error", got, err)
	}

	// The creation's receipt that the node fails to serve fails FindAll.
	c = &chain{nonce: big.NewInt(0), failing: []string{"eth_getTransactionReceipt"}}
	f = &Finder{Eth: c.serve(t), Service: historyService(t, []listing{}, hash(7))}
	if _, err := f.FindAll(t.Context(), safe, 200); !ethrpc.Transient(err) || !strings.Contains(err.Error(), "creation") {
		t.Errorf("FindAll with a failing receipt: got error %v, want a transient error about the creation", err)
	}
}

func TestFindAllGapScanFailure(t *testing.T) {
	// The service lists the first and third executions, and the node fails to serve
	// the blocks between them, as a public node times out a long scan. The second
	// nonce stays missing, with a warning, and the range isn't scanned.
	first, second, third := transaction("first", 0), transaction("second", 1), transaction("third", 2)
	c := &chain{
		nonce: big.NewInt(3),
		logs: []ethrpc.Log{
			executionLog(executionSuccessEvent, first.Hash(), hash(1), true, 100, 0),
			executionLog(executionSuccessEvent, second.Hash(), hash(2), true, 105, 0),
			executionLog(executionSuccessEvent, third.Hash(), hash(3), true, 110, 0),
		},
		failingBlocks: []uint64{105},
	}
	f := &Finder{Eth: c.serve(t), Service: historyService(t, []listing{listed(first, hash(1), 100), listed(third, hash(3), 110)})}
	var warnings []error
	f.Warn = func(err error) { warnings = append(warnings, err) }
	got, err := f.FindAll(t.Context(), safe, 200)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got.Complete() || len(got.Executions) != 2 || len(got.Scanned) != 0 || !reflect.DeepEqual(got.Missing, []NonceRange{{1, 1}}) {
		t.Errorf("FindAll: got %+v; want two executions, nothing scanned, nonce 1 missing", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "scanning blocks 100 to 110") {
		t.Errorf("warnings: got %v, want one about the scan", warnings)
	}
}

func TestMissing(t *testing.T) {
	executions := []Execution{
		{Transaction: transaction("", 1)}, {Transaction: transaction("", 2)}, {Transaction: transaction("", 5)},
		{Transaction: transaction("", 5)}, {Transaction: transaction("", 9)}, {},
	}
	if got, want := missing(executions, 8), []NonceRange{{0, 0}, {3, 4}, {6, 7}}; !reflect.DeepEqual(got, want) {
		t.Errorf("missing: got %v, want %v", got, want)
	}
	if got := missing(nil, 0); got == nil || len(got) != 0 {
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
		deployed uint64
		creation []ethrpc.Hash
		receipts map[ethrpc.Hash]ethrpc.Receipt
		want     *Creation
		warning  string
	}{
		{"confirmed", 100, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): created}, &Creation{100, time.Time{}, hash(1), &factory}, ""},
		{"without a log", 100, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): {BlockNumber: 100}}, &Creation{100, time.Time{}, hash(1), nil}, ""},
		{"code before", 90, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): created}, nil, "has code at block 99"},
		{"no code", 0, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): created}, nil, "no code at block 100"},
		{"later", 100, []ethrpc.Hash{hash(1)}, map[ethrpc.Hash]ethrpc.Receipt{hash(1): {BlockNumber: last + 1}}, nil, ""},
		{"no receipt", 100, []ethrpc.Hash{hash(1)}, nil, nil, "receipt"},
		{"unknown", 100, nil, nil, nil, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &chain{deployed: test.deployed, receipts: test.receipts}
			f := &Finder{Eth: c.serve(t), Service: historyService(t, nil, test.creation...)}
			var warnings []error
			f.Warn = func(err error) { warnings = append(warnings, err) }
			got, err := f.CreationFromService(t.Context(), safe, last)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Errorf("CreationFromService: got %+v, %v, want %+v", got, err, test.want)
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
