package erc20

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// contract answers a call with result, or reverts if result is nil, or fails
// with a node error if failing is set.
type contract struct {
	results map[[4]byte][]byte
	failing bool
}

// callNode starts a node that answers eth_call to the contracts at block 100,
// and returns a client for it.
func callNode(t *testing.T, contracts map[ethrpc.Address]contract) *ethrpc.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "eth_chainId":
			res["result"] = "0x1"
		case "eth_call":
			var call ethrpc.CallRequest
			var block ethrpc.BlockNumber
			json.Unmarshal(req.Params[0], &call)
			json.Unmarshal(req.Params[1], &block)
			c, ok := contracts[call.To]
			var selector [4]byte
			copy(selector[:], call.Data)
			result, has := c.results[selector]
			switch {
			case c.failing:
				res["error"] = map[string]any{"code": -32000, "message": "header not found"}
			case !ok || block != 100:
				// An account without code returns nothing.
				res["result"] = "0x"
			case !has:
				res["error"] = map[string]any{"code": 3, "message": "execution reverted"}
			default:
				res["result"] = ethrpc.Bytes(result)
			}
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	eth, err := ethrpc.NewClient(t.Context(), ethrpc.Mainnet, server.URL)
	if err != nil {
		t.Fatalf("ethrpc.NewClient: %v", err)
	}
	return eth
}

func TestRead(t *testing.T) {
	balance, _ := new(big.Int).SetString("26199343952008652136834", 10)
	token := contract{results: map[[4]byte][]byte{
		decimalsSelector:  solabi.Encode(uint64(18)),
		balanceOfSelector: solabi.Encode(balance),
		nameSelector:      solabi.Encode("Token \x1b[31mA"),
		symbolSelector:    solabi.Encode("TKA"),
	}}
	// A token without a name, and whose symbol is a bytes32, as some old tokens
	// have.
	var symbol [32]byte
	copy(symbol[:], "TKB")
	tokenWithoutStrings := contract{results: map[[4]byte][]byte{
		decimalsSelector:  solabi.Encode(uint64(6)),
		balanceOfSelector: solabi.Encode(big.NewInt(0)),
		symbolSelector:    symbol[:],
	}}
	eth := callNode(t, map[ethrpc.Address]contract{tokenA: token, tokenB: tokenWithoutStrings})

	got, err := Read(t.Context(), eth, tokenA, safe, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil || got.Name != "Token \x1b[31mA" || got.Symbol != "TKA" || got.Decimals != 18 || got.Balance.Cmp(balance) != 0 {
		t.Errorf("Read: got %+v, want the token's name, symbol, 18 decimals, and balance %s", got, balance)
	}
	got, err = Read(t.Context(), eth, tokenB, safe, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil || got.Name != "" || got.Symbol != "" || got.Decimals != 6 || got.Balance.Sign() != 0 {
		t.Errorf("Read of a token without strings: got %+v, want no name or symbol, 6 decimals, and no balance", got)
	}
}

func TestReadNotAToken(t *testing.T) {
	eth := callNode(t, map[ethrpc.Address]contract{
		// A contract whose calls revert.
		tokenA: {results: map[[4]byte][]byte{}},
		// A contract with decimals, but no balanceOf.
		tokenB: {results: map[[4]byte][]byte{decimalsSelector: solabi.Encode(uint64(18))}},
		// A contract whose decimals don't fit in a uint8.
		alice: {results: map[[4]byte][]byte{decimalsSelector: solabi.Encode(uint64(256)), balanceOfSelector: solabi.Encode(uint64(1))}},
	})
	for _, account := range []ethrpc.Address{tokenA, tokenB, alice, bob} {
		if got, err := Read(t.Context(), eth, account, safe, 100); got != nil || err != nil {
			t.Errorf("Read(%s): got %+v and error %v, want nil", account, got, err)
		}
	}
}

func TestReadFailure(t *testing.T) {
	// A node error other than a revert fails, rather than saying the account isn't
	// a token.
	eth := callNode(t, map[ethrpc.Address]contract{tokenA: {failing: true}})
	if _, err := Read(t.Context(), eth, tokenA, safe, 100); err == nil || !strings.Contains(err.Error(), "header not found") {
		t.Errorf("Read with a failing node: got error %v, want the node's", err)
	}
}
