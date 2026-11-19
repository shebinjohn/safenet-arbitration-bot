package cli

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

func TestSimulateErrors(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, []byte(`{"proposal": {"transaction": {"chainId": 1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args   []string
		code   int
		stderr string
	}{
		{nil, 2, "-request-file is required"},
		{[]string{"-call", "0", "-request-file", request}, 2, "-call must be at least 1"},
		{[]string{"-all", "-call", "1", "-request-file", request}, 2, "-all and -call can't be used together"},
		{[]string{"-request-file", request, "extra"}, 2, "Usage: arbot simulate"},
		{[]string{"-request-file", request}, 1, "no Safe chain ID or block"},
		{[]string{"-request-file", filepath.Join(dir, "missing.json")}, 1, "no such file"},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		args := append([]string{"arbot", "-config", config, "simulate"}, test.args...)
		code := Run(t.Context(), args, &stdout, &stderr)
		if code != test.code || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.stderr) {
			t.Errorf("simulate %v: got status %d, stdout %q, stderr %q; want %d, no output, and stderr containing %q",
				test.args, code, stdout.String(), stderr.String(), test.code, test.stderr)
		}
	}
}

var (
	simSafe      = ethrpc.Address{0: 0x5a, 19: 0xfe}
	simSingleton = ethrpc.MustParseAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552")
	simCallOnly  = ethrpc.MustParseAddress("0x40A2aCCbd92BCA938b02010E17A5b8929b49130D")
	simTarget    = ethrpc.Address{0: 0xc0, 19: 1}
	simOther     = ethrpc.Address{0: 0xc0, 19: 2}
	simCode      = ethrpc.Bytes{0x60, 0x80, 0x60, 0x40}
	// simProxy is the code of the SafeProxy of Safe 1.3.0.
	simProxy = ethrpc.Bytes(must(hex.DecodeString("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea2646970667358221220d1429297349653a4918076d650332de1a1068c5f3e07c5c82360c277770b955264736f6c63430007060033")))
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// simNode is a node with a Safe 1.3.0 at simSafe, MultiSendCallOnly 1.3.0, and
// contracts at simTarget and simOther. The Safe's simulateAndRevert reverts
// with revert, debug_traceCall answers with trace, or that it doesn't exist if
// trace is nil, and every request but eth_chainId, or only debug_traceCall if
// onlyTrace is set, fails with HTTP status 500 if overloaded is set.
type simNode struct {
	revert     ethrpc.Bytes
	trace      json.RawMessage
	overloaded bool
	onlyTrace  bool
}

func (n *simNode) serve(t *testing.T) string {
	t.Helper()
	codes := map[ethrpc.Address]ethrpc.Bytes{
		simSafe: simProxy, simSingleton: simCode, simCallOnly: simCode, simTarget: simCode, simOther: simCode,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if n.overloaded && req.Method != "eth_chainId" && (!n.onlyTrace || req.Method == "debug_traceCall") {
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		var address ethrpc.Address
		if len(req.Params) > 0 {
			json.Unmarshal(req.Params[0], &address)
		}
		switch req.Method {
		case "eth_chainId":
			res["result"] = "0x1"
		case "eth_getCode":
			res["result"] = append(ethrpc.Bytes{}, codes[address]...)
		case "eth_getStorageAt":
			var slot0 ethrpc.Hash
			if address == simSafe {
				copy(slot0[12:], simSingleton[:])
			}
			res["result"] = slot0
		case "eth_call":
			var call ethrpc.CallRequest
			json.Unmarshal(req.Params[0], &call)
			if call.To == simSafe && bytes.HasPrefix(call.Data, []byte{0xb4, 0xfa, 0xba, 0x09}) {
				res["error"] = map[string]any{"code": 3, "message": "execution reverted", "data": n.revert}
			} else {
				res["result"] = "0x"
			}
		case "debug_traceCall":
			if n.trace == nil {
				res["error"] = map[string]any{"code": -32601, "message": "the method debug_traceCall does not exist/is not available"}
			} else {
				res["result"] = n.trace
			}
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// serveSourcify serves a Sourcify server that verified the contracts in abis,
// with the code simCode, and answers that it hasn't verified any other, or
// fails every request if failing is set.
func serveSourcify(t *testing.T, abis map[ethrpc.Address]string, failing bool) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		path, _ := strings.CutPrefix(r.URL.Path, "/v2/contract/1/")
		address, err := ethrpc.ParseAddress(path)
		abi, ok := abis[address]
		if err != nil || !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"match": null, "creationMatch": null, "runtimeMatch": null}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"abi":             json.RawMessage(abi),
			"compilation":     map[string]any{"fullyQualifiedName": "contracts/Test.sol:Test", "compilerVersion": "0.8.30+commit.73712a01"},
			"runtimeBytecode": map[string]any{"onchainBytecode": simCode},
			"creationMatch":   "exact_match", "runtimeMatch": "exact_match", "match": "exact_match",
			"verifiedAt": "2025-06-25T08:00:07Z",
		})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// simFiles writes a configuration file for the node at rpc and the Sourcify
// server at sourcify, and a request file of a delegate call from simSafe to
// MultiSendCallOnly 1.3.0 that batches calls with data to simTarget and
// simOther, and returns their paths.
func simFiles(t *testing.T, rpc, sourcify string, data ...ethrpc.Bytes) (config, request string) {
	t.Helper()
	dir := t.TempDir()
	config = filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"rpcs": {"1": %q}, "sourcify": %q}`, rpc, sourcify), 0o644); err != nil {
		t.Fatal(err)
	}
	var batch []byte
	for i, d := range data {
		to := []ethrpc.Address{simTarget, simOther}[i%2]
		batch = append(append(append(batch, 0), to[:]...), solabi.Encode(uint64(0), uint64(len(d)))...)
		batch = append(batch, d...)
	}
	multiSend := solabi.Call(solabi.Selector("multiSend(bytes)"), batch)
	request = filepath.Join(dir, "request.json")
	tx := fmt.Sprintf(`{"chainId": 1, "safe": %q, "to": %q, "value": 0, "data": %q, "operation": "DELEGATECALL",
		"safeTxGas": 0, "baseGas": 0, "gasPrice": 0, "gasToken": "0x0000000000000000000000000000000000000000",
		"refundReceiver": "0x0000000000000000000000000000000000000000", "nonce": 3}`, simSafe, simCallOnly, multiSend)
	if err := os.WriteFile(request, fmt.Appendf(nil, `{"proposal": {"safeBlock": 200, "transaction": %s}}`, tx), 0o644); err != nil {
		t.Fatal(err)
	}
	return config, request
}

// packedRevert is the revert data of simulateAndRevert for a delegate call that
// succeeded or not, with data.
func packedRevert(success bool, data []byte) ethrpc.Bytes {
	flag := uint64(0)
	if success {
		flag = 1
	}
	return append(solabi.Encode(flag, uint64(len(data))), data...)
}

const simMultiSendABI = `[{"type": "function", "name": "multiSend", "inputs": [{"name": "transactions", "type": "bytes"}], "outputs": [], "stateMutability": "payable"},
	{"type": "error", "name": "DeploymentFailed", "inputs": []}]`

func TestSimulateAll(t *testing.T) {
	trace := json.RawMessage(`{"type": "CALL", "from": "0x0000000000000000000000000000000000000000", "to": "0x5a000000000000000000000000000000000000fe",
		"input": "0xb4faba09", "error": "execution reverted", "calls": [
			{"type": "CREATE2", "from": "0x5a000000000000000000000000000000000000fe", "to": "0xc000000000000000000000000000000000000009", "value": "0x0", "input": "0x6000"}]}`)
	node := &simNode{revert: packedRevert(false, []byte{0x30, 0x11, 0x64, 0x25}), trace: trace}
	sourcify := serveSourcify(t, map[ethrpc.Address]string{simCallOnly: simMultiSendABI}, false)
	config, request := simFiles(t, node.serve(t), sourcify, ethrpc.Bytes{1, 2, 3, 4}, ethrpc.Bytes{5, 6, 7, 8})

	code, stdout, stderr := runArbot(t, "-config", config, "simulate", "-all", "-trace", "-json", "-request-file", request)
	if code != 0 {
		t.Fatalf("simulate -all: got status %d, stderr %q", code, stderr)
	}
	var got struct {
		All         bool `json:"all"`
		First       bool `json:"first"`
		Number      *int `json:"number"`
		Transaction *struct {
			To        ethrpc.Address `json:"to"`
			Operation string         `json:"operation"`
		} `json:"transaction"`
		Result struct {
			Method string `json:"method"`
			Block  uint64 `json:"block"`
			Status string `json:"status"`
			Error  *struct {
				Signature string `json:"signature"`
				Source    string `json:"source"`
			} `json:"error"`
		} `json:"result"`
		Trace *struct {
			Available bool `json:"available"`
			Creations []struct {
				Address ethrpc.Address `json:"address"`
			} `json:"creations"`
		} `json:"trace"`
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	r := got.Result
	if !got.All || !got.First || got.Number != nil || got.Transaction == nil || got.Transaction.To != simCallOnly ||
		got.Transaction.Operation != "DELEGATECALL" || r.Method != "simulateAndRevert" || r.Block != 200 || r.Status != "reverted" ||
		r.Error == nil || r.Error.Signature != "DeploymentFailed()" || r.Error.Source != "sourcify" {
		t.Errorf("simulate -all: got %s", stdout)
	}
	if got.Trace == nil || !got.Trace.Available || len(got.Trace.Creations) != 1 || got.Trace.Creations[0].Address != (ethrpc.Address{0: 0xc0, 19: 9}) {
		t.Errorf("simulate -all -trace: got trace %+v", got.Trace)
	}
	notes := strings.Join(got.Notes, "\n")
	for _, want := range []string{"block 200", "refund isn't simulated", "reverts by design"} {
		if !strings.Contains(notes, want) {
			t.Errorf("simulate -all -trace: got notes %q, want one about %q", got.Notes, want)
		}
	}

	// The text output, from a node that doesn't serve traces.
	node.trace = nil
	code, stdout, stderr = runArbot(t, "-config", config, "simulate", "-all", "-trace", "-request-file", request)
	for _, want := range []string{
		"Method    simulateAndRevert", "Result      reverted", "Error     0x30116425 DeploymentFailed() (sourcify)",
		"Trace unavailable: debug_traceCall: JSON-RPC error -32601", "the gas refund isn't simulated",
	} {
		if code != 0 || !strings.Contains(stdout, want) {
			t.Errorf("simulate -all -trace without traces: got status %d, stderr %q, output without %q:\n%s", code, stderr, want, stdout)
		}
	}
	if strings.Contains(stdout, "reverts by design") {
		t.Errorf("simulate -all -trace without traces: got a note about the trace's top frame:\n%s", stdout)
	}

	// The batch's first call can be simulated alone too.
	code, _, stderr = runArbot(t, "-config", config, "simulate", "-request-file", request)
	if code != 0 {
		t.Errorf("simulate of the first batched call: got status %d, stderr %q", code, stderr)
	}
}

func TestSimulateTransientError(t *testing.T) {
	sourcify := serveSourcify(t, nil, false)
	for name, node := range map[string]*simNode{
		"node":  {overloaded: true},
		"trace": {revert: packedRevert(true, nil), overloaded: true, onlyTrace: true},
	} {
		config, request := simFiles(t, node.serve(t), sourcify, ethrpc.Bytes{1, 2, 3, 4})
		for _, args := range [][]string{{"-all"}, {"-all", "-trace"}, {"-call", "1", "-trace"}} {
			if name == "trace" && !strings.Contains(strings.Join(args, " "), "-trace") {
				continue
			}
			code, stdout, stderr := runArbot(t, append([]string{"-config", config, "simulate", "-request-file", request}, args...)...)
			if code != 1 || stdout != "" || !strings.Contains(stderr, "run the command again later") {
				t.Errorf("%s failing, simulate %v: got status %d, stdout %q, stderr %q; want 1 and a hint to run it again", name, args, code, stdout, stderr)
			}
		}
	}
}

func TestCallsABI(t *testing.T) {
	const targetABI = `[{"type": "function", "name": "deploy", "inputs": [{"name": "creationCode", "type": "bytes"}, {"name": "handoffToSafe", "type": "bool"}],
		"outputs": [{"name": "deployed", "type": "address"}], "stateMutability": "nonpayable"}]`
	deploy := solabi.Call(solabi.Selector("deploy(bytes,bool)"), []byte{0x60, 0x00}, true)
	sourcify := serveSourcify(t, map[ethrpc.Address]string{simTarget: targetABI}, false)
	node := &simNode{}
	// Calls to the verified target, to the unverified other contract, and to the
	// target again, which is looked up once.
	config, request := simFiles(t, node.serve(t), sourcify, deploy, deploy, deploy)

	code, stdout, stderr := runArbot(t, "-config", config, "calls", "-abi", "-json", "-request-file", request)
	if code != 0 {
		t.Fatalf("calls -abi: got status %d, stderr %q", code, stderr)
	}
	var got struct {
		Calls []struct {
			ABI *struct {
				Status     string  `json:"status"`
				Source     string  `json:"source"`
				URL        string  `json:"url"`
				ObservedAt *string `json:"observedAt"`
				Function   *struct {
					Signature string `json:"signature"`
					Inputs    []struct {
						Name  string `json:"name"`
						Type  string `json:"type"`
						Value any    `json:"value"`
					} `json:"inputs"`
				} `json:"function"`
			} `json:"abi"`
		} `json:"calls"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Calls) != 3 {
		t.Fatalf("calls -abi: got %s", stdout)
	}
	for _, i := range []int{0, 2} {
		a := got.Calls[i].ABI
		if a == nil || a.Status != "verified" || a.Source != "sourcify" || !strings.HasPrefix(a.URL, sourcify) || a.ObservedAt == nil ||
			a.Function == nil || a.Function.Signature != "deploy(bytes,bool)" || len(a.Function.Inputs) != 2 ||
			a.Function.Inputs[0].Value != "0x6000" || a.Function.Inputs[1].Value != true {
			t.Errorf("calls -abi: call %d: got %+v", i+1, a)
		}
	}
	if a := got.Calls[1].ABI; a == nil || a.Status != "unverified" || a.Function != nil {
		t.Errorf("calls -abi: call 2: got %+v, want an unverified account", a)
	}

	code, stdout, stderr = runArbot(t, "-config", config, "calls", "-abi", "-request-file", request)
	for _, want := range []string{"deploy(bytes,bool) (sourcify)", "- (unverified)"} {
		if code != 0 || !strings.Contains(stdout, want) {
			t.Errorf("calls -abi: got status %d, stderr %q, output without %q:\n%s", code, stderr, want, stdout)
		}
	}
	// Without -abi, no account is looked up, and the calls aren't decoded.
	code, stdout, _ = runArbot(t, "-config", config, "calls", "-json", "-request-file", request)
	if code != 0 || strings.Contains(stdout, `"abi"`) {
		t.Errorf("calls: got status %d and output with an ABI:\n%s", code, stdout)
	}

	// A Sourcify server that fails fails the command, rather than leaving the calls
	// undecoded.
	config, request = simFiles(t, node.serve(t), serveSourcify(t, nil, true), deploy)
	if code, _, stderr := runArbot(t, "-config", config, "calls", "-abi", "-request-file", request); code != 1 || !strings.Contains(stderr, "Sourcify") {
		t.Errorf("calls -abi with Sourcify failing: got status %d, stderr %q; want 1 and an error", code, stderr)
	}
	if code, _, stderr := runArbot(t, "-config", config, "executions", "-abi", "-request-file", request); code != 2 || !strings.Contains(stderr, "-abi only applies with -calls") {
		t.Errorf("executions -abi without -calls: got status %d, stderr %q; want a usage error", code, stderr)
	}
}
