package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// explorerKey is the Etherscan API key of the tests, which no output may
// contain.
const explorerKey = "TESTKEY"

var (
	explorerHelper    = ethrpc.MustParseAddress("0xe530c247341e857042FdFf953E73Dd6c580f8D42")
	explorerRecipient = ethrpc.MustParseAddress("0x316473f1E8bCD1549527d07A9B68B545f0D2976e")
	explorerUSDC      = ethrpc.MustParseAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
)

// explorerCreationBlock is the block of the creation of explorerHelper in the
// recorded answer of Etherscan.
const explorerCreationBlock = 26432157

// explorerNode is a fake node of a chain whose blocks are those of fakeHeader,
// up to latest, on which explorerHelper has code from explorerCreationBlock, as
// the creation transaction of Etherscan's recorded answer made it.
type explorerNode struct {
	chainID uint64
	latest  ethrpc.BlockNumber
	// failing fails every request but eth_chainId with HTTP status 500.
	failing bool
}

func (n *explorerNode) serve(t *testing.T) string {
	t.Helper()
	creationTx := ethrpc.MustParseHash("0x14fabd2a90182726c6e4127b227fa5126672b44084bfc2228faab933ea69adb1")
	creator := ethrpc.MustParseAddress("0x46565eedb0bb948ae787dd105cce300f736da28e")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		block := func(i int) ethrpc.BlockNumber {
			var b ethrpc.BlockNumber
			json.Unmarshal(req.Params[i], &b)
			return b
		}
		switch {
		case req.Method == "eth_chainId":
			res["result"] = ethrpc.Quantity(n.chainID)
		case n.failing:
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		case req.Method == "eth_blockNumber":
			res["result"] = n.latest
		case req.Method == "eth_getBlockByNumber":
			res["result"] = fakeHeader(block(0))
		case req.Method == "eth_getCode":
			var address ethrpc.Address
			json.Unmarshal(req.Params[0], &address)
			res["result"] = "0x"
			if address == explorerHelper && block(1) >= explorerCreationBlock {
				res["result"] = "0x6080"
			}
		case req.Method == "eth_getTransactionReceipt":
			var hash ethrpc.Hash
			json.Unmarshal(req.Params[0], &hash)
			res["result"] = nil
			if hash == creationTx {
				res["result"] = map[string]any{
					"transactionHash": hash, "blockNumber": ethrpc.Quantity(explorerCreationBlock), "status": "0x1", "from": creator, "to": nil, "logs": []any{},
				}
			}
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// serveEtherscan serves the recorded answers of Etherscan in
// internal/etherscan/testdata for the chain with the ID chainID, by action, or
// fails every request with HTTP status 500 if failing.
func serveEtherscan(t *testing.T, chainID uint64, failing bool) string {
	t.Helper()
	answers := map[string]string{"getcontractcreation": "creation", "txlist": "txlist", "tokentx": "tokentx", "getsourcecode": "source-verified"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("apikey") != explorerKey || q.Get("chainid") != fmt.Sprint(chainID) {
			t.Errorf("got query %v, want the test key and chain %d", q, chainID)
		}
		if failing {
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "etherscan", "testdata", answers[q.Get("action")]+".json"))
		if err != nil {
			t.Errorf("action %q: %v", q.Get("action"), err)
		}
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v2/api"
}

// explorerFiles writes a configuration file that reads Mainnet and Arbitrum One
// from the nodes at rpcs, and Etherscan at the URL etherscan with the test key,
// or no key if etherscan is empty, and a request whose Safe block is safeBlock
// and whose proposal is at the time of block 500 of a fake node, and returns
// their paths.
func explorerFiles(t *testing.T, rpcs map[uint64]string, etherscan string, safeBlock uint64) (config, request string) {
	t.Helper()
	dir := t.TempDir()
	cfg := map[string]any{"rpcs": rpcs}
	if etherscan != "" {
		cfg["etherscan"] = map[string]string{"apiKey": explorerKey, "url": etherscan}
	}
	data, _ := json.Marshal(cfg)
	config = filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, data, 0o644); err != nil {
		t.Fatal(err)
	}
	request = filepath.Join(dir, "request.json")
	data = fmt.Appendf(nil, `{"proposal": {"time": %q, "safeBlock": %d, "transaction": {"chainId": 1, "safe": %q}}}`,
		fakeBlockTime(500).Add(1e9).Format("2006-01-02T15:04:05Z"), safeBlock, stateSafe)
	if err := os.WriteFile(request, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return config, request
}

// runExplorer runs arbot explorer with args, and fails the test if any output
// has the API key.
func runExplorer(t *testing.T, config string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	code, stdout, stderr = runArbot(t, append([]string{"-config", config, "explorer"}, args...)...)
	if strings.Contains(stdout+stderr, explorerKey) {
		t.Errorf("explorer %v: the output has the API key: %s%s", args, stdout, stderr)
	}
	return code, stdout, stderr
}

func TestExplorerNotConfigured(t *testing.T) {
	config, request := explorerFiles(t, nil, "", 100)
	for _, command := range []string{"creation", "txs", "tokentx", "source"} {
		code, stdout, stderr := runExplorer(t, config, command, "-request-file", request, explorerHelper.String())
		if code != 1 || stdout != "" || !strings.Contains(stderr, "configure etherscan in the config file") || !strings.Contains(stderr, `"Etherscan not configured"`) {
			t.Errorf("explorer %s without a key: got status %d, stdout %q, stderr %q; want 1 and a message to configure etherscan", command, code, stdout, stderr)
		}
	}
}

func TestExplorerErrors(t *testing.T) {
	config, request := explorerFiles(t, nil, "http://etherscan.invalid/v2/api", 100)
	for _, test := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{nil, 2, "Usage: arbot explorer"},
		{[]string{"-h"}, 0, "Usage: arbot explorer"},
		{[]string{"labels", explorerHelper.String()}, 2, `unknown explorer command "labels"`},
		{[]string{"txs", "-request-file", request}, 2, "Usage: arbot explorer"},
		{[]string{"txs", "-request-file", request, "0x12"}, 2, `address "0x12"`},
		{[]string{"txs", explorerHelper.String()}, 2, "-request-file is required"},
		{[]string{"txs", "-chain", "10", "-request-file", request, explorerHelper.String()}, 2, "-chain 10 isn't a network that the Charter covers"},
	} {
		code, stdout, stderr := runExplorer(t, config, test.args...)
		if code != test.code || stdout != "" || !strings.Contains(stderr, test.stderr) {
			t.Errorf("explorer %v: got status %d, stdout %q, stderr %q; want %d, no output, and stderr containing %q", test.args, code, stdout, stderr, test.code, test.stderr)
		}
	}
}

// explorerJSON is the part of the JSON output of an explorer command that the
// tests check.
type explorerJSON struct {
	ChainID          uint64          `json:"chainId"`
	SafeBlock        uint64          `json:"safeBlock"`
	Block            uint64          `json:"block"`
	Address          ethrpc.Address  `json:"address"`
	Source           string          `json:"source"`
	URL              string          `json:"url"`
	ObservedAt       string          `json:"observedAt"`
	ConfirmedOnchain json.RawMessage `json:"confirmedOnchain"`
	Account          *struct {
		CodeSize int `json:"codeSize"`
	} `json:"account"`
	Creation *struct {
		Creator     ethrpc.Address  `json:"creator"`
		Factory     *ethrpc.Address `json:"factory"`
		Block       uint64          `json:"block"`
		AfterCutoff bool            `json:"afterCutoff"`
	} `json:"creation"`
	Confirmation *struct {
		Receipt    *bool `json:"receipt"`
		CodeBefore *bool `json:"codeBefore"`
		CodeAt     *bool `json:"codeAt"`
	} `json:"confirmation"`
	Count          int             `json:"count"`
	Capped         bool            `json:"capped"`
	Transactions   []any           `json:"transactions"`
	Transfers      []any           `json:"transfers"`
	Verified       bool            `json:"verified"`
	Proxy          bool            `json:"proxy"`
	Implementation *ethrpc.Address `json:"implementation"`
	Notes          []string        `json:"notes"`
}

// decodeExplorer decodes the JSON output of an explorer command, and checks the
// labels of its source.
func decodeExplorer(t *testing.T, stdout, action string) explorerJSON {
	t.Helper()
	var out explorerJSON
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if out.Source != "etherscan" || !strings.Contains(out.URL, "action="+action) || !strings.Contains(out.URL, "apikey=REDACTED") || out.ObservedAt == "" {
		t.Errorf("got source %q, URL %q, observed at %q; want etherscan, the redacted URL, and a time", out.Source, out.URL, out.ObservedAt)
	}
	return out
}

func TestExplorerCreation(t *testing.T) {
	rpc := (&explorerNode{chainID: 1, latest: 30_000_000}).serve(t)
	config, request := explorerFiles(t, map[uint64]string{1: rpc}, serveEtherscan(t, 1, false), 26441835)
	code, stdout, stderr := runExplorer(t, config, "creation", "-json", "-request-file", request, explorerHelper.String())
	if code != 0 {
		t.Fatalf("explorer creation: got status %d, stderr %q", code, stderr)
	}
	out := decodeExplorer(t, stdout, "getcontractcreation")
	if string(out.ConfirmedOnchain) != "true" || out.Creation == nil || out.Creation.Block != explorerCreationBlock || out.Creation.AfterCutoff ||
		out.Creation.Factory == nil || out.Confirmation == nil || !*out.Confirmation.Receipt || *out.Confirmation.CodeBefore || !*out.Confirmation.CodeAt ||
		out.Account == nil || out.Account.CodeSize != 2 || out.Block != 26441835 {
		t.Errorf("explorer creation: got %s", stdout)
	}

	// A creation after the Safe block isn't evidence, so it isn't confirmed.
	config, request = explorerFiles(t, map[uint64]string{1: rpc}, serveEtherscan(t, 1, false), explorerCreationBlock-1)
	code, stdout, stderr = runExplorer(t, config, "creation", "-json", "-request-file", request, explorerHelper.String())
	if code != 0 {
		t.Fatalf("explorer creation after the Safe block: got status %d, stderr %q", code, stderr)
	}
	out = decodeExplorer(t, stdout, "getcontractcreation")
	if string(out.ConfirmedOnchain) != `"n/a"` || out.Creation == nil || !out.Creation.AfterCutoff || out.Confirmation != nil || out.Account.CodeSize != 0 ||
		!strings.Contains(strings.Join(out.Notes, " "), "isn't evidence") {
		t.Errorf("explorer creation after the Safe block: got %s", stdout)
	}

	// The text output.
	code, stdout, _ = runExplorer(t, config, "creation", "-request-file", request, explorerHelper.String())
	if code != 0 || !strings.Contains(stdout, "Creator      0x46565eeDB0bb948aE787dD105cCE300f736dA28e") || !strings.Contains(stdout, "apikey=REDACTED") {
		t.Errorf("explorer creation: got status %d, output %q", code, stdout)
	}
}

func TestExplorerListings(t *testing.T) {
	rpc := (&explorerNode{chainID: 1, latest: 30_000_000}).serve(t)
	config, request := explorerFiles(t, map[uint64]string{1: rpc}, serveEtherscan(t, 1, false), 26441835)
	for _, test := range []struct {
		command, action string
		count           int
	}{
		{"txs", "txlist", 3},
		{"tokentx", "tokentx", 2},
	} {
		code, stdout, stderr := runExplorer(t, config, test.command, "-json", "-request-file", request, explorerRecipient.String())
		if code != 0 {
			t.Fatalf("explorer %s: got status %d, stderr %q", test.command, code, stderr)
		}
		out := decodeExplorer(t, stdout, test.action)
		results := len(out.Transactions) + len(out.Transfers)
		if out.Count != test.count || results != test.count || out.Capped || string(out.ConfirmedOnchain) != "false" ||
			!strings.Contains(out.URL, "endblock=26441835") || out.ChainID != 1 || out.Block != 26441835 {
			t.Errorf("explorer %s: got %s", test.command, stdout)
		}
		code, stdout, _ = runExplorer(t, config, test.command, "-request-file", request, explorerRecipient.String())
		if code != 0 || !strings.Contains(stdout, fmt.Sprintf("Count      %d", test.count)) {
			t.Errorf("explorer %s: got status %d, output %q", test.command, code, stdout)
		}
	}
}

func TestExplorerSource(t *testing.T) {
	rpc := (&explorerNode{chainID: 1, latest: 30_000_000}).serve(t)
	config, request := explorerFiles(t, map[uint64]string{1: rpc}, serveEtherscan(t, 1, false), 26441835)
	code, stdout, stderr := runExplorer(t, config, "source", "-json", "-request-file", request, explorerUSDC.String())
	if code != 0 {
		t.Fatalf("explorer source: got status %d, stderr %q", code, stderr)
	}
	out := decodeExplorer(t, stdout, "getsourcecode")
	implementation := ethrpc.MustParseAddress("0x43506849d7c04f9138d1a2050bbf3a0c054402dd")
	if !out.Verified || !out.Proxy || out.Implementation == nil || *out.Implementation != implementation || string(out.ConfirmedOnchain) != `"n/a"` {
		t.Errorf("explorer source: got %s", stdout)
	}
	// The sources themselves aren't printed.
	if strings.Contains(stdout, "pragma solidity") {
		t.Errorf("explorer source: got the source code in %s", stdout)
	}
}

func TestExplorerOtherChain(t *testing.T) {
	// The proposal is just after block 500 of Arbitrum One, which is the last block
	// before it.
	mainnet := (&explorerNode{chainID: 1, latest: 30_000_000}).serve(t)
	arbitrum := (&explorerNode{chainID: ethrpc.ArbitrumOne, latest: 1000}).serve(t)
	config, request := explorerFiles(t, map[uint64]string{1: mainnet, ethrpc.ArbitrumOne: arbitrum}, serveEtherscan(t, ethrpc.ArbitrumOne, false), 26441835)
	code, stdout, stderr := runExplorer(t, config, "creation", "-json", "-chain", "42161", "-request-file", request, explorerHelper.String())
	if code != 0 {
		t.Fatalf("explorer creation -chain: got status %d, stderr %q", code, stderr)
	}
	out := decodeExplorer(t, stdout, "getcontractcreation")
	if out.ChainID != ethrpc.ArbitrumOne || out.SafeBlock != 26441835 || out.Block != 500 || out.Creation == nil || !out.Creation.AfterCutoff {
		t.Errorf("explorer creation -chain: got %s", stdout)
	}

	// A listing with a result after the block, as the recorded one has on this
	// chain, is an error rather than evidence as of the block.
	code, _, stderr = runExplorer(t, config, "txs", "-chain", "42161", "-request-file", request, explorerRecipient.String())
	if code != 1 || !strings.Contains(stderr, "after the end block 500") {
		t.Errorf("explorer txs -chain: got status %d, stderr %q; want 1 and a result after the block", code, stderr)
	}
}

func TestExplorerTransientError(t *testing.T) {
	rpc := (&explorerNode{chainID: 1, latest: 30_000_000}).serve(t)
	config, request := explorerFiles(t, map[uint64]string{1: rpc}, serveEtherscan(t, 1, true), 26441835)
	code, stdout, stderr := runExplorer(t, config, "txs", "-request-file", request, explorerRecipient.String())
	if code != 1 || stdout != "" || !strings.Contains(stderr, "a request to Etherscan failed rather than being answered") || !strings.Contains(stderr, "apikey=REDACTED") {
		t.Errorf("explorer txs with a failing API: got status %d, stdout %q, stderr %q; want 1 and a hint to run it again", code, stdout, stderr)
	}

	// A node that fails is a failure too, not an account without code.
	failing := (&explorerNode{chainID: 1, failing: true}).serve(t)
	config, request = explorerFiles(t, map[uint64]string{1: failing}, serveEtherscan(t, 1, false), 26441835)
	code, _, stderr = runExplorer(t, config, "creation", "-request-file", request, explorerHelper.String())
	if code != 1 || !strings.Contains(stderr, "a request to the node failed") {
		t.Errorf("explorer creation with a failing node: got status %d, stderr %q; want 1 and the node's hint", code, stderr)
	}
}
