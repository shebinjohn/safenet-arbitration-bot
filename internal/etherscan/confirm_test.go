package etherscan

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// node is a fake node of Ethereum Mainnet with the code of one account at each
// block, and the receipts of transactions.
type node struct {
	// code is the account's code by block. A block without an entry is one whose
	// state the node doesn't keep.
	code     map[uint64]string
	receipts map[ethrpc.Hash]any
	// overloaded fails every request but eth_chainId with HTTP status 500.
	overloaded bool
}

func (n *node) client(t *testing.T) *ethrpc.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch {
		case req.Method == "eth_chainId":
			res["result"] = "0x1"
		case n.overloaded:
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		case req.Method == "eth_getCode":
			var block ethrpc.BlockNumber
			json.Unmarshal(req.Params[1], &block)
			if code, ok := n.code[uint64(block)]; ok {
				res["result"] = code
			} else {
				res["error"] = map[string]any{"code": -32000, "message": "missing trie node"}
			}
		case req.Method == "eth_getTransactionReceipt":
			var hash ethrpc.Hash
			json.Unmarshal(req.Params[0], &hash)
			res["result"] = n.receipts[hash]
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	client, err := ethrpc.NewClient(t.Context(), 1, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestConfirmCreation(t *testing.T) {
	creator := ethrpc.MustParseAddress("0x46565eedb0bb948ae787dd105cce300f736da28e")
	creation := &Creation{Creator: creator, TxHash: ethrpc.Hash{31: 1}, Block: 100}
	receipt := func(block int, status string, from ethrpc.Address) any {
		return map[string]any{
			"transactionHash": creation.TxHash, "blockNumber": ethrpc.Quantity(block), "status": status, "from": from, "to": nil, "logs": []any{},
		}
	}
	created := map[uint64]string{99: "0x", 100: "0x6080"}
	other := ethrpc.Address{19: 9}

	for _, test := range []struct {
		name string
		node node
		// want is whether the creation is confirmed, and reason a part of the reason if
		// it isn't.
		want   bool
		reason string
	}{
		{name: "confirmed", node: node{code: created, receipts: map[ethrpc.Hash]any{creation.TxHash: receipt(100, "0x1", creator)}}, want: true},
		{name: "code before", node: node{code: map[uint64]string{99: "0x6080", 100: "0x6080"}, receipts: map[ethrpc.Hash]any{creation.TxHash: receipt(100, "0x1", creator)}}, reason: "has code at block 99"},
		{name: "no code at", node: node{code: map[uint64]string{99: "0x", 100: "0x"}, receipts: map[ethrpc.Hash]any{creation.TxHash: receipt(100, "0x1", creator)}}, reason: "no code at block 100"},
		{name: "no receipt", node: node{code: created}, reason: "has no receipt"},
		{name: "other block", node: node{code: created, receipts: map[ethrpc.Hash]any{creation.TxHash: receipt(101, "0x1", creator)}}, reason: "in block 101"},
		{name: "reverted", node: node{code: created, receipts: map[ethrpc.Hash]any{creation.TxHash: receipt(100, "0x0", creator)}}, reason: "reverted"},
		{name: "other sender", node: node{code: created, receipts: map[ethrpc.Hash]any{creation.TxHash: receipt(100, "0x1", other)}}, reason: "not the creator"},
		// A node that doesn't keep old states can't confirm the creation, which isn't
		// an error.
		{name: "pruned", node: node{code: map[uint64]string{}, receipts: map[ethrpc.Hash]any{creation.TxHash: receipt(100, "0x1", creator)}}, reason: "missing trie node"},
	} {
		check, err := ConfirmCreation(t.Context(), test.node.client(t), helper, creation)
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if check.Confirmed != test.want || !strings.Contains(check.Reason, test.reason) || (test.want && check.Reason != "") {
			t.Errorf("%s: got confirmed %t, reason %q; want %t, with %q", test.name, check.Confirmed, check.Reason, test.want, test.reason)
		}
		if test.name == "pruned" && (check.CodeBefore != nil || check.CodeAt != nil || check.Receipt == nil || !*check.Receipt) {
			t.Errorf("pruned: got %+v, want unread code checks and a receipt", check)
		}
	}

	// A node that fails rather than answering says nothing about the chain.
	overloaded := node{overloaded: true}
	if _, err := ConfirmCreation(t.Context(), overloaded.client(t), helper, creation); !ethrpc.Transient(err) {
		t.Errorf("overloaded: got %v, want a transient error", err)
	}
}
