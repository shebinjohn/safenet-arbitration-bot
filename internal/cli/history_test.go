package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

var (
	historySafe  = ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001")
	historyToken = ethrpc.MustParseAddress("0x7000000000000000000000000000000000000001")
	historyAlice = ethrpc.MustParseAddress("0xa11ce00000000000000000000000000000000003")
	historyBob   = ethrpc.MustParseAddress("0xb0b0000000000000000000000000000000000004")

	executionSuccess = solabi.Event("ExecutionSuccess(bytes32,uint256)")
	erc20Transfer    = solabi.Event("Transfer(address,address,uint256)")
)

// historyChain is a fake node for Ethereum Mainnet with logs and the Safe's
// nonce at block 200, the Safe block of the requests of the tests.
type historyChain struct {
	logs  []ethrpc.Log
	nonce int64
	// failingBlocks are the blocks whose logs the node fails to serve with HTTP
	// status 500, as an overloaded node does.
	failingBlocks []uint64

	mu sync.Mutex
	// ranges are the block ranges of the eth_getLogs requests, in order.
	ranges [][2]uint64
}

func (c *historyChain) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "eth_chainId":
			res["result"] = "0x1"
		case "eth_call":
			word := solabi.Uint(big.NewInt(c.nonce))
			res["result"] = ethrpc.Bytes(word[:])
		case "eth_getCode":
			res["result"] = "0x"
		case "eth_getLogs":
			var filter ethrpc.LogFilter
			json.Unmarshal(req.Params[0], &filter)
			c.mu.Lock()
			c.ranges = append(c.ranges, [2]uint64{uint64(filter.FromBlock), uint64(filter.ToBlock)})
			c.mu.Unlock()
			if slices.ContainsFunc(c.failingBlocks, func(b uint64) bool { return b >= uint64(filter.FromBlock) && b <= uint64(filter.ToBlock) }) {
				http.Error(w, "overloaded", http.StatusInternalServerError)
				return
			}
			logs := []ethrpc.Log{}
			for _, log := range c.logs {
				if log.BlockNumber < filter.FromBlock || log.BlockNumber > filter.ToBlock ||
					len(filter.Addresses) > 0 && !slices.Contains(filter.Addresses, log.Address) {
					continue
				}
				matches := true
				for i, topics := range filter.Topics {
					if topics != nil && (i >= len(log.Topics) || !slices.Contains(topics, log.Topics[i])) {
						matches = false
					}
				}
				if matches {
					logs = append(logs, log)
				}
			}
			res["result"] = logs
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// historyService is a fake Safe Transaction Service for Ethereum Mainnet that
// lists the executions and transfers of the Safe, or fails with HTTP status 500
// if failing.
type historyService struct {
	executions []map[string]any
	transfers  []map[string]any
	failing    bool
}

func (s *historyService) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.failing {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		results := []map[string]any{}
		switch r.URL.Path {
		case "/api/v1/safes/" + historySafe.String() + "/multisig-transactions/":
			results = s.executions
		case "/api/v1/safes/" + historySafe.String() + "/transfers/":
			results = s.transfers
		default:
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("offset") != "0" {
			results = []map[string]any{}
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(results), "results": results})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// safeTransaction returns a Safe transaction of the Safe, on Mainnet, with data
// and nonce.
func safeTransaction(data string, nonce int64) *safenet.SafeTransaction {
	return &safenet.SafeTransaction{
		ChainID: big.NewInt(1), Safe: historySafe, To: historyAlice, Value: new(big.Int), Data: []byte(data),
		SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: big.NewInt(nonce),
	}
}

// listing returns tx as the service lists it, executed in the transaction hash
// at block.
func listing(tx *safenet.SafeTransaction, hash ethrpc.Hash, block uint64) map[string]any {
	return map[string]any{
		"safe": tx.Safe, "to": tx.To, "value": tx.Value.String(), "data": tx.Data, "operation": uint8(tx.Operation),
		"safeTxGas": 0, "baseGas": 0, "gasPrice": "0", "gasToken": tx.GasToken, "refundReceiver": tx.RefundReceiver,
		"nonce": tx.Nonce.Uint64(), "safeTxHash": tx.Hash(), "transactionHash": hash, "blockNumber": block, "isExecuted": true,
	}
}

// executionLog returns the ExecutionSuccess log of the Safe for tx, in the
// transaction hash at block, as Safe 1.4.1 logs it.
func executionLog(tx *safenet.SafeTransaction, hash ethrpc.Hash, block uint64) ethrpc.Log {
	payment := solabi.Uint(new(big.Int))
	return ethrpc.Log{
		Address: historySafe, Topics: []ethrpc.Hash{executionSuccess, tx.Hash()}, Data: payment[:],
		BlockNumber: ethrpc.BlockNumber(block), TransactionHash: hash,
	}
}

// historyFiles writes a configuration file that reads Mainnet from the node at
// rpc, with the service at service, and a request whose Safe block is 200 and
// whose Safe transaction is proposal, and returns their paths.
func historyFiles(t *testing.T, rpc, service string, proposal *safenet.SafeTransaction) (config, request string) {
	t.Helper()
	dir := t.TempDir()
	config = filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"rpcs": {"1": %q}, "txServices": {"1": %q}}`, rpc, service), 0o644); err != nil {
		t.Fatal(err)
	}
	request = filepath.Join(dir, "request.json")
	data, err := json.Marshal(map[string]any{"proposal": map[string]any{
		"safeBlock": 200, "safeTxHash": proposal.Hash(), "transaction": proposal,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return config, request
}

func hashOf(n byte) ethrpc.Hash { return ethrpc.Hash{0: 0xaa, 31: n} }

func TestExecutionsAll(t *testing.T) {
	// Three executions, of which the second is the request's own Safe transaction,
	// executed before its proposal, and a listing for another Safe.
	first, own, third := safeTransaction("first", 0), safeTransaction("own", 1), safeTransaction("third", 2)
	other := safeTransaction("other", 2)
	other.Safe = historyBob
	chain := &historyChain{nonce: 3, logs: []ethrpc.Log{
		executionLog(first, hashOf(1), 100), executionLog(own, hashOf(2), 110), executionLog(third, hashOf(3), 120),
	}}
	otherListing := listing(other, hashOf(4), 130)
	otherListing["safeTxHash"] = other.Hash()
	service := &historyService{executions: []map[string]any{
		listing(first, hashOf(1), 100), listing(own, hashOf(2), 110), listing(third, hashOf(3), 120), otherListing,
	}}
	serviceURL := service.serve(t)
	config, request := historyFiles(t, chain.serve(t), serviceURL, own)

	var stdout, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"arbot", "-config", config, "executions", "-all", "-json", "-request-file", request}, &stdout, &stderr); code != 0 {
		t.Fatalf("executions -all: exit status %d\n%s", code, stderr.String())
	}
	var got struct {
		FromBlock  uint64 `json:"fromBlock"`
		ToBlock    uint64 `json:"toBlock"`
		Executions []struct {
			SafeTxHash ethrpc.Hash `json:"safeTxHash"`
			Source     string      `json:"source"`
		} `json:"executions"`
		History struct {
			Service struct {
				URL        string `json:"url"`
				ObservedAt string `json:"observedAt"`
			} `json:"service"`
			Nonce    int64             `json:"nonce"`
			Complete bool              `json:"complete"`
			Missing  []json.RawMessage `json:"missingNonces"`
			Scanned  []json.RawMessage `json:"scanned"`
			Rejected []struct {
				SafeTxHash ethrpc.Hash `json:"safeTxHash"`
				Reason     string      `json:"reason"`
			} `json:"rejected"`
			Creation          json.RawMessage `json:"creation"`
			ProposalExecution *struct {
				Block      uint64      `json:"block"`
				SafeTxHash ethrpc.Hash `json:"safeTxHash"`
			} `json:"proposalExecution"`
		} `json:"history"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("executions -all: %v\n%s", err, stdout.String())
	}
	h := got.History
	if got.FromBlock != 0 || got.ToBlock != 200 || len(got.Executions) != 2 ||
		got.Executions[0].SafeTxHash != first.Hash() || got.Executions[1].SafeTxHash != third.Hash() || got.Executions[0].Source != "service" {
		t.Errorf("executions -all: got %s, want the first and third executions, from the service, in blocks 0 to 200", stdout.String())
	}
	// The request's own execution is apart, and counts toward the nonces.
	if p := h.ProposalExecution; p == nil || p.SafeTxHash != own.Hash() || p.Block != 110 {
		t.Errorf("executions -all: got the proposal's execution %+v, want the one in block 110", p)
	}
	if h.Service.URL != serviceURL || h.Service.ObservedAt == "" || h.Nonce != 3 || !h.Complete || h.Missing == nil || len(h.Missing) != 0 ||
		h.Scanned == nil || len(h.Rejected) != 1 || h.Rejected[0].SafeTxHash != other.Hash() || !strings.Contains(h.Rejected[0].Reason, "for the Safe") ||
		string(h.Creation) != "null" {
		t.Errorf("executions -all: got history %+v, want the service, nonce 3, complete, the other Safe's listing rejected, and no creation", h)
	}

	stdout.Reset()
	if code := Run(t.Context(), []string{"arbot", "-config", config, "executions", "-all", "-request-file", request}, &stdout, &stderr); code != 0 {
		t.Fatalf("executions -all: exit status %d\n%s", code, stderr.String())
	}
	for _, want := range []string{
		`(?m)^Safe transactions executed by ` + historySafe.String() + ` on chain 1, up to block 200$`,
		`(?m)^Nonce at block 200: 3 \(complete\)$`,
		`(?m)^Missing nonces: none$`,
		`(?m)^Creation: unknown$`,
		`(?m)^The request's own Safe transaction was executed in block 110, transaction ` + hashOf(2).String() + ` \(not listed below\)$`,
		`(?m)^120 +` + hashOf(3).String() + ` +success +` + third.Hash().String() + ` +service +2 +`,
		`(?m)^2 +` + other.Hash().String() + ` +` + hashOf(4).String() + ` +the transaction is for the Safe`,
	} {
		if !regexp.MustCompile(want).Match(stdout.Bytes()) {
			t.Errorf("executions -all: output has no line matching %q:\n%s", want, stdout.String())
		}
	}
}

func TestExecutionsAllErrors(t *testing.T) {
	tx := safeTransaction("first", 0)
	chain := &historyChain{nonce: 1, logs: []ethrpc.Log{executionLog(tx, hashOf(1), 100)}}
	rpc := chain.serve(t)
	service := &historyService{executions: []map[string]any{listing(tx, hashOf(1), 100)}}
	failing := &historyService{failing: true}
	// A node that fails to serve the logs of the block that the service names.
	overloaded := &historyChain{nonce: 1, failingBlocks: []uint64{100}}

	for _, test := range []struct {
		name, rpc, service string
		args               []string
		code               int
		stderr             []string
	}{
		{"-blocks", rpc, service.serve(t), []string{"-blocks", "5"}, 2, []string{"-blocks doesn't apply with -all"}},
		{"no service", rpc, "", nil, 1, []string{"-all needs the Safe Transaction Service of chain 1, which has none"}},
		{"failing service", rpc, failing.serve(t), nil, 1, []string{"listing the executions", "from the Safe Transaction Service", "500"}},
		{"failing node", overloaded.serve(t), service.serve(t), nil, 1, []string{"500", "run the command again later"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, request := historyFiles(t, test.rpc, test.service, tx)
			var stdout, stderr bytes.Buffer
			args := append([]string{"arbot", "-config", config, "executions", "-all", "-request-file", request}, test.args...)
			code := Run(t.Context(), args, &stdout, &stderr)
			if code != test.code || stdout.Len() != 0 {
				t.Errorf("executions -all %v: got status %d, stdout %q; want %d and no output", test.args, code, stdout.String(), test.code)
			}
			for _, want := range test.stderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("executions -all %v: got stderr %q, want it to contain %q", test.args, stderr.String(), want)
				}
			}
		})
	}
}

// transferLog returns the Transfer log of the token of value from from to to,
// in the transaction hash at block.
func transferLog(from, to ethrpc.Address, value int64, hash ethrpc.Hash, block, index uint64) ethrpc.Log {
	amount := solabi.Uint(big.NewInt(value))
	return ethrpc.Log{
		Address: historyToken, Topics: []ethrpc.Hash{erc20Transfer, solabi.Address(from), solabi.Address(to)}, Data: amount[:],
		BlockNumber: ethrpc.BlockNumber(block), TransactionHash: hash, LogIndex: ethrpc.Quantity(index),
	}
}

func TestTransfersAll(t *testing.T) {
	// The service lists a transfer out of the Safe that the logs have, one that
	// they don't, one after the Safe block, and one into the Safe.
	chain := &historyChain{logs: []ethrpc.Log{
		transferLog(historySafe, historyAlice, 5, hashOf(1), 100, 0),
		transferLog(historyAlice, historySafe, 6, hashOf(3), 120, 0),
		transferLog(historySafe, historyAlice, 7, hashOf(4), 300, 0),
	}}
	listed := func(from, to ethrpc.Address, value string, hash ethrpc.Hash, block uint64) map[string]any {
		return map[string]any{
			"type": "ERC20_TRANSFER", "blockNumber": block, "transactionHash": hash, "tokenAddress": historyToken,
			"from": from, "to": to, "value": value,
		}
	}
	service := &historyService{transfers: []map[string]any{
		listed(historySafe, historyAlice, "5", hashOf(1), 100),
		listed(historySafe, historyBob, "8", hashOf(2), 110),
		listed(historySafe, historyAlice, "7", hashOf(4), 300),
		listed(historyAlice, historySafe, "6", hashOf(3), 120),
	}}
	serviceURL := service.serve(t)
	config, request := historyFiles(t, chain.serve(t), serviceURL, safeTransaction("own", 0))

	var stdout, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"arbot", "-config", config, "transfers", "-all", "-json", "-request-file", request}, &stdout, &stderr); code != 0 {
		t.Fatalf("transfers -all: exit status %d\n%s", code, stderr.String())
	}
	var got struct {
		FromBlock uint64 `json:"fromBlock"`
		ToBlock   uint64 `json:"toBlock"`
		Transfers []struct {
			Block uint64         `json:"block"`
			To    ethrpc.Address `json:"to"`
			Value *big.Int       `json:"value"`
		} `json:"transfers"`
		Service *struct {
			URL        string `json:"url"`
			ObservedAt string `json:"observedAt"`
			Listed     int    `json:"listed"`
			Blocks     int    `json:"blocks"`
			Unmatched  int    `json:"unmatched"`
		} `json:"service"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("transfers -all: %v\n%s", err, stdout.String())
	}
	if got.FromBlock != 0 || got.ToBlock != 200 || len(got.Transfers) != 1 || got.Transfers[0].Block != 100 || got.Transfers[0].Value.Int64() != 5 {
		t.Errorf("transfers -all: got %s, want the transfer to alice in block 100", stdout.String())
	}
	if s := got.Service; s == nil || s.URL != serviceURL || s.ObservedAt == "" || s.Listed != 2 || s.Blocks != 2 || s.Unmatched != 1 {
		t.Errorf("transfers -all: got service %+v, want 2 listed in 2 blocks, 1 unmatched", s)
	}
	if want := "lists a transfer of 8 from " + historySafe.String() + " to " + historyBob.String(); !strings.Contains(stderr.String(), want) {
		t.Errorf("transfers -all: got stderr %q, want a warning containing %q", stderr.String(), want)
	}
	// The block without the transfer is read again before it is unmatched, and no
	// other blocks are read.
	if want := [][2]uint64{{100, 100}, {110, 110}, {110, 110}}; !slices.Equal(chain.ranges, want) {
		t.Errorf("transfers -all: read the logs of %v, want %v", chain.ranges, want)
	}

	// Without a service, -all fails and says so.
	config, request = historyFiles(t, chain.serve(t), "", safeTransaction("own", 0))
	stderr.Reset()
	if code := Run(t.Context(), []string{"arbot", "-config", config, "transfers", "-all", "-request-file", request}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "-all needs the Safe Transaction Service of chain 1") {
		t.Errorf("transfers -all without a service: got status %d, stderr %q; want 1 and an error about the service", code, stderr.String())
	}
}
