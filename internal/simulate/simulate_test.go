package simulate

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
	"github.com/safe-research/safenet-arbitration-bot/internal/sourcify"
)

var (
	safe    = ethrpc.Address{0: 0x5a, 19: 0xfe}
	target  = ethrpc.Address{0: 0xc0, 19: 1}
	created = ethrpc.Address{0: 0x60, 19: 2}
	code    = ethrpc.Bytes{0x60, 0x80}
	create  = solabi.Selector("create(address)")
)

// node serves eth_call: a call from the Safe to target at block 100 returns
// result, or reverts if revert is set, and it records the last call.
type node struct {
	result ethrpc.Bytes
	revert bool
	last   map[string]any
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
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "eth_chainId":
			res["result"] = "0x1"
		case "eth_call":
			json.Unmarshal(req.Params[0], &n.last)
			var block ethrpc.BlockNumber
			json.Unmarshal(req.Params[1], &block)
			switch {
			case block != 100:
				res["error"] = map[string]any{"code": -32000, "message": "header not found"}
			case n.revert:
				res["error"] = map[string]any{"code": 3, "message": "execution reverted: exists", "data": "0x08c379a0"}
			default:
				res["result"] = n.result
			}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	eth, err := ethrpc.NewClient(t.Context(), ethrpc.Mainnet, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return eth
}

// repository serves contract for every address.
type repository struct{ contract *sourcify.Contract }

func (r *repository) URL(chainID uint64, address ethrpc.Address, fields ...sourcify.Field) (string, error) {
	return "https://sourcify.test/" + address.String(), nil
}

func (r *repository) Contract(ctx context.Context, chainID uint64, address ethrpc.Address, fields ...sourcify.Field) (*sourcify.Contract, error) {
	if r.contract == nil {
		return nil, sourcify.ErrNotFound
	}
	return r.contract, nil
}

// verified returns a repository with a verified contract of code whose ABI is
// abi.
func verified(t *testing.T, abi string) *repository {
	t.Helper()
	var entries []solabi.ABIEntry
	if err := json.Unmarshal([]byte(abi), &entries); err != nil {
		t.Fatal(err)
	}
	return &repository{contract: &sourcify.Contract{Match: "match", RuntimeMatch: "match", ABI: entries, RuntimeCode: code}}
}

const factoryABI = `[{"type": "function", "name": "create", "inputs": [{"name": "gauge", "type": "address"}],
	"outputs": [{"name": "vault", "type": "address"}, {"name": "", "type": "uint256"}, {"name": "ok", "type": "bool"}, {"name": "id", "type": "bytes4"}],
	"stateMutability": "nonpayable"}]`

func TestCall(t *testing.T) {
	n := &node{result: solabi.Encode(created, big.NewInt(7), true, [32]byte{0xde, 0xad, 0xbe, 0xef})}
	data := solabi.Call(create, ethrpc.Address{19: 9})
	got, err := Call(t.Context(), n.serve(t), verified(t, factoryABI), safe, target, big.NewInt(5), data, safenet.OperationCall, code, 100)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	want := &Function{Signature: "create(address)", Outputs: []Output{
		{"vault", "address", created},
		{"", "uint256", "7"},
		{"ok", "bool", true},
		{"id", "bytes4", ethrpc.Bytes{0xde, 0xad, 0xbe, 0xef}},
	}}
	if got.Status != Success || !reflect.DeepEqual(got.Function, want) || got.Source == nil {
		t.Errorf("Call: got %+v with function %+v, want success with %+v", got, got.Function, want)
	}
	// The call is from the Safe, with its value and data.
	if n.last["from"] != strings.ToLower(safe.String()) || n.last["to"] != strings.ToLower(target.String()) ||
		n.last["value"] != "0x5" || n.last["data"] != data.String() {
		t.Errorf("eth_call: got %v", n.last)
	}
}

func TestCallReverted(t *testing.T) {
	n := &node{revert: true}
	got, err := Call(t.Context(), n.serve(t), verified(t, factoryABI), safe, target, nil, solabi.Call(create, ethrpc.Address{}), safenet.OperationCall, code, 100)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.Status != Reverted || !strings.Contains(got.RevertReason, "exists") || got.ReturnData.String() != "0x08c379a0" ||
		got.Function == nil || got.Function.Outputs != nil {
		t.Errorf("Call: got %+v, want a revert with its data and no outputs", got)
	}
	if _, ok := n.last["value"]; ok {
		t.Errorf("eth_call without value: got %v", n.last)
	}
}

func TestCallUndecoded(t *testing.T) {
	n := &node{result: solabi.Encode(created)}
	eth := n.serve(t)
	// An unverified contract, and a function with a dynamic output, have no decoded
	// outputs.
	got, err := Call(t.Context(), eth, &repository{}, safe, target, nil, solabi.Call(create, ethrpc.Address{}), safenet.OperationCall, code, 100)
	if err != nil || got.Function != nil || got.Source == nil || got.ReturnData.String() != ethrpc.Bytes(solabi.Encode(created)).String() {
		t.Errorf("Call of an unverified contract: got %+v, %v", got, err)
	}
	dynamic := `[{"type": "function", "name": "create", "inputs": [{"type": "address"}], "outputs": [{"type": "string"}]}]`
	got, err = Call(t.Context(), eth, verified(t, dynamic), safe, target, nil, solabi.Call(create, ethrpc.Address{}), safenet.OperationCall, code, 100)
	if err != nil || got.Function == nil || got.Function.Outputs != nil {
		t.Errorf("Call with a dynamic output: got %+v, %v", got, err)
	}
	// A delegate call can't be simulated, and a node error fails.
	if _, err := Call(t.Context(), eth, &repository{}, safe, target, nil, nil, safenet.OperationDelegateCall, code, 100); err == nil {
		t.Errorf("Call of a delegate call: expected an error")
	}
	if _, err := Call(t.Context(), eth, &repository{}, safe, target, nil, nil, safenet.OperationCall, code, 99); err == nil {
		t.Errorf("Call at a block that the node doesn't have: expected an error")
	}
}
