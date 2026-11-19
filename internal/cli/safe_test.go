package cli

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
)

// stateSafeBlock is the Safe block of the requests of the tests of a stateNode.
const stateSafeBlock = 200

var (
	stateSafe  = ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001")
	stateOther = ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000002")
	stateEOA   = ethrpc.MustParseAddress("0xe0a0000000000000000000000000000000000001")
	stateToken = ethrpc.MustParseAddress("0x7000000000000000000000000000000000000001")

	// stateSafeProxy is the code of the SafeProxy of Safe 1.3.0, and stateSingleton
	// the Safe 1.3.0 singleton.
	stateSafeProxy = func() ethrpc.Bytes {
		b, _ := hex.DecodeString("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea2646970667358221220d1429297349653a4918076d650332de1a1068c5f3e07c5c82360c277770b955264736f6c63430007060033")
		return b
	}()
	stateSingleton = ethrpc.MustParseAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552")
)

// stateAccount is an account of a stateNode at a block.
type stateAccount struct {
	code    ethrpc.Bytes
	storage map[ethrpc.Hash]ethrpc.Hash
	balance int64
	nonce   uint64
	// calls are what eth_call to the account returns for each selector. A call with
	// another selector reverts.
	calls map[string]ethrpc.Bytes
}

// stateNode is a fake node for Ethereum Mainnet with the accounts of each
// block, which fails every request but eth_chainId with HTTP status 500 if
// overloaded, as an overloaded node does.
type stateNode struct {
	blocks     map[uint64]map[ethrpc.Address]stateAccount
	overloaded bool
}

func (n *stateNode) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if req.Method == "eth_chainId" {
			res["result"] = "0x1"
			json.NewEncoder(w).Encode(res)
			return
		}
		if n.overloaded {
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		var address ethrpc.Address
		var call ethrpc.CallRequest
		if req.Method == "eth_call" {
			json.Unmarshal(req.Params[0], &call)
			address = call.To
		} else {
			json.Unmarshal(req.Params[0], &address)
		}
		var block ethrpc.BlockNumber
		json.Unmarshal(req.Params[len(req.Params)-1], &block)
		accounts, ok := n.blocks[uint64(block)]
		if !ok {
			res["error"] = map[string]any{"code": -32000, "message": fmt.Sprintf("header not found: block %d", block)}
			json.NewEncoder(w).Encode(res)
			return
		}
		a := accounts[address]
		switch req.Method {
		case "eth_getCode":
			res["result"] = append(ethrpc.Bytes{}, a.code...)
		case "eth_getStorageAt":
			var slot ethrpc.Hash
			json.Unmarshal(req.Params[1], &slot)
			res["result"] = a.storage[slot]
		case "eth_getBalance":
			res["result"] = fmt.Sprintf("0x%x", a.balance)
		case "eth_getTransactionCount":
			res["result"] = ethrpc.Quantity(a.nonce)
		case "eth_call":
			if len(a.code) == 0 {
				res["result"] = "0x"
			} else if result, ok := a.calls[hex.EncodeToString(call.Data[:min(4, len(call.Data))])]; ok {
				res["result"] = result
			} else {
				res["error"] = map[string]any{"code": 3, "message": "execution reverted"}
			}
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// storageWord returns an address or number as a storage word.
func storageWord(v any) ethrpc.Hash {
	var h ethrpc.Hash
	switch v := v.(type) {
	case ethrpc.Address:
		copy(h[12:], v[:])
	case int:
		big.NewInt(int64(v)).FillBytes(h[:])
	}
	return h
}

// listSlot returns the storage slot of the entry for key in the mapping of a
// Safe's linked list at slot index.
func listSlot(key ethrpc.Address, index byte) ethrpc.Hash {
	var preimage [64]byte
	copy(preimage[12:32], key[:])
	preimage[63] = index
	return keccak256.Hash(preimage[:])
}

// safeAccount returns a Safe 1.3.0 proxy with the owners, threshold, and nonce,
// and no modules.
func safeAccount(threshold, nonce int, owners ...ethrpc.Address) stateAccount {
	sentinel := ethrpc.Address{19: 1}
	storage := map[ethrpc.Hash]ethrpc.Hash{
		{}:      storageWord(stateSingleton),
		{31: 3}: storageWord(len(owners)),
		{31: 4}: storageWord(threshold),
		{31: 5}: storageWord(nonce),
		// The modules, an empty list.
		listSlot(sentinel, 1): storageWord(sentinel),
	}
	previous := sentinel
	for _, owner := range append(owners, sentinel) {
		storage[listSlot(previous, 2)] = storageWord(owner)
		previous = owner
	}
	return stateAccount{code: stateSafeProxy, storage: storage}
}

// stateFiles writes a configuration file that reads Mainnet from the node at
// rpc, and a request of stateSafe whose Safe block is stateSafeBlock, and
// returns their paths.
func stateFiles(t *testing.T, rpc string) (config, request string) {
	t.Helper()
	dir := t.TempDir()
	config = filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"rpcs": {"1": %q}}`, rpc), 0o644); err != nil {
		t.Fatal(err)
	}
	request = filepath.Join(dir, "request.json")
	data := fmt.Appendf(nil, `{"proposal": {"safeBlock": %d, "transaction": {"chainId": 1, "safe": %q}}}`, stateSafeBlock, stateSafe)
	if err := os.WriteFile(request, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return config, request
}

// runArbot runs arbot with args, and returns its status and output.
func runArbot(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(t.Context(), append([]string{"arbot"}, args...), &out, &errOut)
	return code, out.String(), errOut.String()
}

// safeSettingsJSON is the part of the settings of an account in the JSON output
// of `arbot safe` that the tests check.
type safeSettingsJSON struct {
	Address          ethrpc.Address `json:"address"`
	Block            uint64         `json:"block"`
	ProxyVersion     *string        `json:"proxyVersion"`
	SingletonVersion *string        `json:"singletonVersion"`
	Settings         *struct {
		Owners    []ethrpc.Address `json:"owners"`
		Threshold *big.Int         `json:"threshold"`
		Nonce     *big.Int         `json:"nonce"`
	} `json:"settings"`
	Reason *string `json:"reason"`
}

func TestSafeAddressAndCompare(t *testing.T) {
	shared, onlySafe, onlyOther := ethrpc.Address{0: 0x0e, 19: 1}, ethrpc.Address{0: 0x0e, 19: 2}, ethrpc.Address{0: 0x0e, 19: 3}
	// The Safe added an owner and raised its threshold between blocks 150 and 200.
	// The other Safe shares an owner with it, with a lower threshold.
	node := &stateNode{blocks: map[uint64]map[ethrpc.Address]stateAccount{
		150: {stateSafe: safeAccount(1, 4, shared)},
		200: {
			stateSafe:  safeAccount(2, 5, onlySafe, shared),
			stateOther: safeAccount(1, 9, shared, onlyOther),
			stateEOA:   {balance: 1, nonce: 3},
			stateToken: {code: ethrpc.Bytes{0x60, 0x00}, storage: map[ethrpc.Hash]ethrpc.Hash{{}: storageWord(stateSingleton)}},
		},
	}}
	config, request := stateFiles(t, node.serve(t))
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		code, stdout, stderr := runArbot(t, append([]string{"-config", config, "safe", "-request-file", request}, args...)...)
		if code != 0 {
			t.Fatalf("safe %v: got status %d, stderr %q", args, code, stderr)
		}
		return stdout
	}

	t.Run("plain", func(t *testing.T) {
		// The output without flags keeps its shape, with the settings at its top level.
		var got map[string]json.RawMessage
		json.Unmarshal([]byte(run(t, "-json")), &got)
		for _, key := range []string{"safe", "chainId", "safeBlock", "singleton", "owners", "ownerCount", "threshold", "nonce", "modules", "fallbackHandler", "guard", "moduleGuard"} {
			if _, ok := got[key]; !ok {
				t.Errorf("safe -json: output has no %q", key)
			}
		}
		if len(got) != 12 || string(got["threshold"]) != "2" {
			t.Errorf("safe -json: got %v, want only the settings, with threshold 2", got)
		}
	})

	t.Run("address", func(t *testing.T) {
		var got struct {
			ChainID   uint64 `json:"chainId"`
			SafeBlock uint64 `json:"safeBlock"`
			safeSettingsJSON
		}
		json.Unmarshal([]byte(run(t, "-json", "-address", stateOther.String())), &got)
		if got.ChainID != 1 || got.SafeBlock != stateSafeBlock || got.Address != stateOther || got.Block != stateSafeBlock ||
			got.ProxyVersion == nil || *got.ProxyVersion != "1.3.0" || got.SingletonVersion == nil || *got.SingletonVersion != "1.3.0" ||
			got.Settings == nil || got.Settings.Threshold.Int64() != 1 || len(got.Settings.Owners) != 2 || got.Reason != nil {
			t.Errorf("safe -address: got %+v, want the other Safe's settings", got)
		}
		text := run(t, "-address", stateOther.String())
		for _, want := range []string{
			`(?m)^Safe ` + stateOther.String() + ` on chain 1, as of block 200$`,
			`(?m)^  Singleton         ` + stateSingleton.String() + `, Safe singleton 1\.3\.0$`,
			`(?m)^  Threshold         1 of 2 owners$`,
			`(?m)^Shared owners, or an identical configuration, are facts about who controls the Safes`,
		} {
			if !regexp.MustCompile(want).MatchString(text) {
				t.Errorf("safe -address: output has no line matching %q:\n%s", want, text)
			}
		}
	})

	t.Run("at", func(t *testing.T) {
		var got safeSettingsJSON
		json.Unmarshal([]byte(run(t, "-json", "-at", "150")), &got)
		if got.Address != stateSafe || got.Block != 150 || got.Settings == nil || got.Settings.Threshold.Int64() != 1 || got.Settings.Nonce.Int64() != 4 {
			t.Errorf("safe -at 150: got %+v, want the Safe's settings at block 150", got)
		}
	})

	t.Run("not a Safe", func(t *testing.T) {
		for _, test := range []struct {
			address ethrpc.Address
			reason  string
		}{{stateEOA, "no-code"}, {stateToken, "not-a-safe-proxy"}} {
			out := run(t, "-json", "-address", test.address.String())
			var got safeSettingsJSON
			var fields map[string]json.RawMessage
			json.Unmarshal([]byte(out), &got)
			json.Unmarshal([]byte(out), &fields)
			if got.Address != test.address || got.Reason == nil || *got.Reason != test.reason || string(fields["settings"]) != "null" {
				t.Errorf("safe -address %s: got %s, want null settings, for the reason %q", test.address, out, test.reason)
			}
		}
		text := run(t, "-address", stateEOA.String())
		if want := "  Settings  none: the account has no code, so it isn't a Safe\n"; !strings.Contains(text, want) {
			t.Errorf("safe -address of an account without code: got %q, want a line %q", text, want)
		}
	})

	t.Run("compare", func(t *testing.T) {
		var got struct {
			ChainID   uint64           `json:"chainId"`
			SafeBlock uint64           `json:"safeBlock"`
			A         safeSettingsJSON `json:"a"`
			B         safeSettingsJSON `json:"b"`
			Diff      *struct {
				Owners struct {
					Result string           `json:"result"`
					Shared []ethrpc.Address `json:"shared"`
					OnlyA  []ethrpc.Address `json:"onlyA"`
					OnlyB  []ethrpc.Address `json:"onlyB"`
				} `json:"owners"`
				Singleton         string `json:"singleton"`
				Threshold         string `json:"threshold"`
				Nonce             string `json:"nonce"`
				SameConfiguration bool   `json:"sameConfiguration"`
			} `json:"diff"`
		}
		json.Unmarshal([]byte(run(t, "-json", "-compare", stateOther.String())), &got)
		if got.A.Address != stateSafe || got.A.Block != stateSafeBlock || got.B.Address != stateOther || got.B.Block != stateSafeBlock || got.Diff == nil {
			t.Fatalf("safe -compare: got %+v, want the Safe and the other Safe at the Safe block, compared", got)
		}
		d := got.Diff
		if d.Singleton != "equal" || d.Threshold != "different" || d.Nonce != "different" || d.SameConfiguration ||
			d.Owners.Result != "different" || len(d.Owners.Shared) != 1 || d.Owners.Shared[0] != shared ||
			len(d.Owners.OnlyA) != 1 || d.Owners.OnlyA[0] != onlySafe || len(d.Owners.OnlyB) != 1 || d.Owners.OnlyB[0] != onlyOther {
			t.Errorf("safe -compare: got diff %+v, want a shared owner, one owner only in each, and different thresholds", d)
		}

		text := run(t, "-compare", stateOther.String())
		for _, want := range []string{
			`(?m)^  A  ` + stateSafe.String() + ` as of block 200, Safe proxy 1\.3\.0, Safe singleton 1\.3\.0$`,
			`(?m)^threshold +different +2 +1$`,
			`(?m)^singleton +equal +` + stateSingleton.String() + ` +` + stateSingleton.String() + `$`,
			`(?m)^Owners: different as sets$`,
			`(?m)^  both +` + shared.String() + `$`,
			`(?m)^  only B +` + onlyOther.String() + `$`,
			`(?m)^Every setting but the nonce, with the lists compared as sets, is different\.$`,
			`(?m)^on 0x8b83aa12d81c160fc8f1b2dd5af6e4f8032e61a6b648ff9e2cdb80f6570c3b2d gave a recipient's shared$`,
		} {
			if !regexp.MustCompile(want).MatchString(text) {
				t.Errorf("safe -compare: output has no line matching %q:\n%s", want, text)
			}
		}
	})

	t.Run("compare at two blocks", func(t *testing.T) {
		var got struct {
			A    safeSettingsJSON `json:"a"`
			B    safeSettingsJSON `json:"b"`
			Diff *struct {
				Owners struct {
					OnlyB []ethrpc.Address `json:"onlyB"`
				} `json:"owners"`
				Threshold string `json:"threshold"`
			} `json:"diff"`
		}
		json.Unmarshal([]byte(run(t, "-json", "-compare", stateSafe.String(), "-at", "150")), &got)
		if got.A.Address != stateSafe || got.A.Block != 150 || got.B.Address != stateSafe || got.B.Block != stateSafeBlock ||
			got.Diff == nil || got.Diff.Threshold != "different" || len(got.Diff.Owners.OnlyB) != 1 || got.Diff.Owners.OnlyB[0] != onlySafe {
			t.Errorf("safe -compare -at 150: got %+v, want the Safe at blocks 150 and 200, with the owner that it added", got)
		}
	})

	t.Run("compare with an account that isn't a Safe", func(t *testing.T) {
		out := run(t, "-json", "-compare", stateEOA.String())
		var got map[string]json.RawMessage
		json.Unmarshal([]byte(out), &got)
		var b safeSettingsJSON
		json.Unmarshal(got["b"], &b)
		if string(got["diff"]) != "null" || b.Settings != nil || b.Reason == nil || *b.Reason != "no-code" {
			t.Errorf("safe -compare of an account without code: got %s, want no diff, and no settings for B", out)
		}
		if text := run(t, "-compare", stateEOA.String()); !strings.Contains(text, "Not compared") {
			t.Errorf("safe -compare of an account without code: got %q, want it not compared", text)
		}
	})

	t.Run("errors", func(t *testing.T) {
		for _, test := range []struct {
			args   []string
			code   int
			stderr string
		}{
			{[]string{"-at", "201"}, 2, "-at 201 is after the request's Safe block 200"},
			{[]string{"-compare", stateOther.String(), "-at", "201"}, 2, "-at 201 is after the request's Safe block 200"},
			{[]string{"-compare", stateOther.String(), "-at", "150", "-address", stateOther.String()}, 2, "-address doesn't apply with -compare and -at"},
			{[]string{"-address", "0x12"}, 2, `-address "0x12"`},
			{[]string{"-compare", "0x12"}, 2, `-compare "0x12"`},
		} {
			code, stdout, stderr := runArbot(t, append([]string{"-config", config, "safe", "-request-file", request}, test.args...)...)
			if code != test.code || stdout != "" || !strings.Contains(stderr, test.stderr) {
				t.Errorf("safe %v: got status %d, stdout %q, stderr %q; want %d, no output, and stderr containing %q",
					test.args, code, stdout, stderr, test.code, test.stderr)
			}
		}
	})
}

func TestSafeNotASafe(t *testing.T) {
	// Without flags, the request's Safe must be a Safe, since the output has no
	// place to say that it isn't.
	node := &stateNode{blocks: map[uint64]map[ethrpc.Address]stateAccount{stateSafeBlock: {}}}
	config, request := stateFiles(t, node.serve(t))
	code, stdout, stderr := runArbot(t, "-config", config, "safe", "-json", "-request-file", request)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "isn't a Safe of a § 2.1 version at block 200") || !strings.Contains(stderr, "(no-code)") {
		t.Errorf("safe of an account without code: got status %d, stdout %q, stderr %q; want 1 and an error", code, stdout, stderr)
	}
}

func TestSafeAndAccountTransientError(t *testing.T) {
	// A node that fails to answer says nothing about the account, so the commands
	// fail rather than reporting an account without code or settings.
	node := &stateNode{overloaded: true}
	config, request := stateFiles(t, node.serve(t))
	for _, args := range [][]string{
		{"safe", "-request-file", request},
		{"safe", "-request-file", request, "-address", stateOther.String()},
		{"safe", "-request-file", request, "-compare", stateOther.String()},
		{"account", "-request-file", request, stateToken.String()},
	} {
		code, stdout, stderr := runArbot(t, append([]string{"-config", config}, args...)...)
		if code != 1 || stdout != "" || !strings.Contains(stderr, "run the command again later") {
			t.Errorf("%v: got status %d, stdout %q, stderr %q; want 1 and a hint to run it again", args, code, stdout, stderr)
		}
	}
}

func TestAccount(t *testing.T) {
	spender := ethrpc.Address{0: 0x5e, 19: 1}
	word := func(v any) ethrpc.Bytes { w := storageWord(v); return w[:] }
	// A token, as of blocks 150 and 200, whose name is a string, and whose symbol
	// call reverts.
	token := func(balance int) stateAccount {
		name := append(append(word(32), word(5)...), []byte("Token")...)
		name = append(name, make([]byte, 27)...)
		return stateAccount{code: ethrpc.Bytes{0x60, 0x00}, calls: map[string]ethrpc.Bytes{
			"313ce567": word(6),           // decimals()
			"70a08231": word(balance),     // balanceOf(address)
			"06fdde03": name,              // name()
			"18160ddd": word(1_000_000),   // totalSupply()
			"dd62ed3e": word(balance / 2), // allowance(address,address)
		}}
	}
	node := &stateNode{blocks: map[uint64]map[ethrpc.Address]stateAccount{
		150: {stateToken: token(10)},
		200: {stateToken: token(40), stateEOA: {balance: 7, nonce: 3}, stateSafe: safeAccount(1, 0, stateEOA)},
	}}
	config, request := stateFiles(t, node.serve(t))
	type output struct {
		Block    uint64          `json:"block"`
		Address  ethrpc.Address  `json:"address"`
		CodeSize int             `json:"codeSize"`
		Self     bool            `json:"self"`
		Balance  *big.Int        `json:"balance"`
		Nonce    uint64          `json:"nonce"`
		ERC20    json.RawMessage `json:"erc20"`
	}
	run := func(args ...string) output {
		t.Helper()
		code, stdout, stderr := runArbot(t, append([]string{"-config", config, "account", "-json", "-request-file", request}, args...)...)
		if code != 0 {
			t.Fatalf("account %v: got status %d, stderr %q", args, code, stderr)
		}
		var got output
		json.Unmarshal([]byte(stdout), &got)
		return got
	}

	got := run(stateToken.String())
	if want := `{"name":"Token","symbol":"","decimals":6,"totalSupply":1000000,"safeBalance":40}`; got.Block != 200 || compactJSON(got.ERC20) != want {
		t.Errorf("account of the token: got block %d and token %s, want block 200 and %s", got.Block, got.ERC20, want)
	}
	got = run("-block", "150", "-spender", spender.String(), stateToken.String())
	if want := `{"name":"Token","symbol":"","decimals":6,"totalSupply":1000000,"safeBalance":10,"spender":"` + strings.ToLower(spender.String()) + `","safeAllowance":5}`; got.Block != 150 || compactJSON(got.ERC20) != want {
		t.Errorf("account -block 150 -spender of the token: got block %d and token %s, want block 150 and %s", got.Block, got.ERC20, want)
	}
	if got := run(stateEOA.String()); got.CodeSize != 0 || got.Balance.Int64() != 7 || got.Nonce != 3 || string(got.ERC20) != "null" {
		t.Errorf("account of an account without code: got %+v, want balance 7, nonce 3, and no token", got)
	}
	if got := run(stateSafe.String()); !got.Self || got.CodeSize == 0 || string(got.ERC20) != "null" {
		t.Errorf("account of the Safe: got %+v, want the Safe itself, which isn't a token", got)
	}
	code, _, stderr := runArbot(t, "-config", config, "account", "-block", "201", "-request-file", request, stateToken.String())
	if code != 2 || !strings.Contains(stderr, "-block 201 is after the request's Safe block 200") {
		t.Errorf("account -block 201: got status %d, stderr %q; want a usage error", code, stderr)
	}
}

// compactJSON returns data without insignificant spaces.
func compactJSON(data []byte) string {
	var b bytes.Buffer
	json.Compact(&b, data)
	return b.String()
}
