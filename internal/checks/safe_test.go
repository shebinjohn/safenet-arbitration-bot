package checks

import (
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// The code of the SafeProxy contracts of Safe 1.0.0, 1.1.1 on Ethereum Mainnet
// and on Gnosis Chain, 1.3.0, 1.4.1, and 1.5.0.
var (
	safeProxy100        = must(hex.DecodeString("608060405273ffffffffffffffffffffffffffffffffffffffff600054163660008037600080366000845af43d6000803e6000811415603d573d6000fd5b3d6000f3fea165627a7a723058201e7d648b83cfac072cbccefc2ffc62a6999d4a050ee87a721942de1da9670db80029"))
	safeProxy111Mainnet = must(hex.DecodeString("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea265627a7a72315820d8a00dc4fe6bf675a9d7416fc2d00bb3433362aa8186b750f76c4027269667ff64736f6c634300050e0032"))
	safeProxy111Gnosis  = must(hex.DecodeString("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea265627a7a723158200a23d2285c529de46060100feb312da05c409802321897869814263b54bb746264736f6c634300050e0032"))
	safeProxy130        = must(hex.DecodeString("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea2646970667358221220d1429297349653a4918076d650332de1a1068c5f3e07c5c82360c277770b955264736f6c63430007060033"))
	safeProxy141        = must(hex.DecodeString("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea264697066735822122003d1488ee65e08fa41e58e888a9865554c535f2c77126a82cb4c0f917f31441364736f6c63430007060033"))
	safeProxy150        = must(hex.DecodeString("608060405260005463a619486e60003560e01c14156024578060601b606c5260206060f35b3660008037600080366000845af43d6000803e806040573d6000fd5b3d6000f3fea2646970667358221220e61834ebd2d8cd909d362bf67c47ef58fd665df38e6dd036ce65611101d072e964736f6c63430007060033"))
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// account is the state of an account that a fake node serves: its code, and the
// value of its storage slot 0.
type account struct {
	code  ethrpc.Bytes
	slot0 ethrpc.Hash
	nonce uint64
}

// proxy returns the account of a SafeProxy with code, whose singleton is
// singleton.
func proxy(code []byte, singleton ethrpc.Address) account {
	a := account{code: code}
	copy(a.slot0[12:], singleton[:])
	return a
}

// fakeNode is a node that serves the state of accounts at any block, on any
// chain. Accounts that it doesn't have are empty.
type fakeNode struct {
	accounts map[ethrpc.Address]account
	// reads counts the eth_getCode and eth_getStorageAt requests that the node
	// answered.
	reads atomic.Int64
	// fail is a method that the node answers with an error, with the HTTP status
	// failStatus if it is set.
	fail       string
	failStatus int
}

// dialer starts the node, and returns a Dialer that connects to it for any
// chain. It fails the test if a request is for a block other than block, or for
// a storage slot other than slot 0.
func (n *fakeNode) dialer(t *testing.T, block uint64) ethrpc.Dialer {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
			return
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if req.Method == n.fail && n.failStatus != 0 {
			http.Error(w, "overloaded", n.failStatus)
			return
		}
		if req.Method == n.fail {
			reply["error"] = map[string]any{"code": -32000, "message": "missing trie node"}
			json.NewEncoder(w).Encode(reply)
			return
		}
		// read decodes the address and block of an eth_getCode or eth_getStorageAt
		// request, which has the storage slot in between.
		read := func() account {
			n.reads.Add(1)
			var address ethrpc.Address
			var at ethrpc.BlockNumber
			last := len(req.Params) - 1
			if last < 1 || json.Unmarshal(req.Params[0], &address) != nil ||
				json.Unmarshal(req.Params[last], &at) != nil || uint64(at) != block {
				t.Errorf("%s: got params %s, want an address and block %d", req.Method, req.Params, block)
			}
			return n.accounts[address]
		}
		switch req.Method {
		case "eth_chainId":
			// The chain ID is the path of the node's URL.
			chainID, _ := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/"), 10, 64)
			reply["result"] = ethrpc.Quantity(chainID)
		case "eth_getCode":
			reply["result"] = read().code
		case "eth_getTransactionCount":
			reply["result"] = ethrpc.Quantity(read().nonce)
		case "eth_getStorageAt":
			if len(req.Params) != 3 || string(req.Params[1]) != `"`+(ethrpc.Hash{}).String()+`"` {
				t.Errorf("eth_getStorageAt: got params %s, want slot 0", req.Params)
			}
			reply["result"] = read().slot0
		default:
			t.Errorf("unexpected method %s", req.Method)
		}
		json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(server.Close)
	return func(ctx context.Context, chainID uint64) (*ethrpc.Client, error) {
		return ethrpc.NewClient(ctx, chainID, fmt.Sprintf("%s/%d", server.URL, chainID))
	}
}

func TestUnsupportedSafe(t *testing.T) {
	safe130 := ethrpc.MustParseAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552")
	safeL2130 := ethrpc.MustParseAddress("0xfb1bffC9d739B8D520DaF37dF666da4C687191EA")
	safe141 := ethrpc.MustParseAddress("0x41675C099F32341bf84BFc5382aF534df5C7461a")
	safeL2150 := ethrpc.MustParseAddress("0xEdd160fEBBD92E350D4D398fb636302fccd67C7e")
	dirty := proxy(safeProxy150, safeL2150)
	dirty.slot0[0] = 0xff

	tests := []struct {
		name    string
		account account
		// chainID is the Safe's network, Gnosis Chain if 0.
		chainID int64
		want    bool
		// reads is the number of requests that the check makes.
		reads int64
	}{
		{"1.0.0 proxy on Ethereum Mainnet", proxy(safeProxy100, safe141), ethrpc.Mainnet, false, 2},
		{"1.0.0 proxy on Gnosis Chain", proxy(safeProxy100, safe130), ethrpc.Gnosis, false, 2},
		{"1.0.0 proxy on Arbitrum One", proxy(safeProxy100, safe141), ethrpc.ArbitrumOne, true, 1},
		{"1.1.1 proxy on Ethereum Mainnet", proxy(safeProxy111Mainnet, safe130), ethrpc.Mainnet, false, 2},
		{"1.1.1 proxy on Gnosis Chain", proxy(safeProxy111Gnosis, safeL2130), ethrpc.Gnosis, false, 2},
		{"Gnosis Chain 1.1.1 proxy on Ethereum Mainnet", proxy(safeProxy111Gnosis, safe130), ethrpc.Mainnet, true, 1},
		{"Ethereum Mainnet 1.1.1 proxy on Gnosis Chain", proxy(safeProxy111Mainnet, safe130), ethrpc.Gnosis, true, 1},
		{"1.1.1 proxy on Arbitrum One", proxy(safeProxy111Mainnet, safe130), ethrpc.ArbitrumOne, true, 1},
		{"1.0.0 proxy with a 1.1.1 singleton", proxy(safeProxy100, ethrpc.MustParseAddress("0x34CfAC646f301356fAa8B21e94227e3583Fe3F5F")), ethrpc.Mainnet, true, 2},
		{"1.3.0 proxy on Arbitrum One", proxy(safeProxy130, safe130), ethrpc.ArbitrumOne, false, 2},
		{"1.3.0", proxy(safeProxy130, safe130), 0, false, 2},
		{"1.3.0 L2", proxy(safeProxy130, safeL2130), 0, false, 2},
		{"1.4.1", proxy(safeProxy141, safe141), 0, false, 2},
		{"1.5.0 L2", proxy(safeProxy150, safeL2150), 0, false, 2},
		{"migrated proxy", proxy(safeProxy130, safeL2150), 0, false, 2},
		{"dirty singleton slot", dirty, 0, false, 2},
		{"empty account", account{}, 0, true, 1},
		{"other code", proxy([]byte{0x60, 0x80}, safe141), 0, true, 1},
		{"truncated proxy", proxy(safeProxy141[:len(safeProxy141)-1], safe141), 0, true, 1},
		{"unsupported singleton", proxy(safeProxy141, ethrpc.Address{19: 1}), 0, true, 2},
		{"no singleton", proxy(safeProxy141, ethrpc.Address{}), 0, true, 2},
		{"singleton without proxy", proxy(nil, safe141), 0, true, 1},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			safeVersions.Clear()
			safe := ethrpc.Address{0: 0x5a, 19: byte(i)}
			node := &fakeNode{accounts: map[ethrpc.Address]account{safe: test.account}}
			at := &env{dial: node.dialer(t, testSafeBlock), block: testSafeBlock}
			chainID := cmp.Or(test.chainID, ethrpc.Gnosis)
			got, err := unsupportedSafe.fn(t.Context(), at, &safeID{address: safe, chainID: big.NewInt(chainID)}, call{})
			if got != test.want || err != nil {
				t.Errorf("unsupportedSafe = %t, %v; want %t", got, err, test.want)
			}
			if reads := node.reads.Load(); reads != test.reads {
				t.Errorf("got %d requests, want %d", reads, test.reads)
			}
		})
	}
}

func TestUnsupportedSafeCache(t *testing.T) {
	safeVersions.Clear()
	safe := ethrpc.Address{0: 0x5a, 19: 0xca}
	node := &fakeNode{accounts: map[ethrpc.Address]account{safe: proxy(safeProxy141, singletonOf("1.4.1"))}}
	match := func(env *env, chainID int64) {
		t.Helper()
		got, err := unsupportedSafe.fn(t.Context(), env, &safeID{address: safe, chainID: big.NewInt(chainID)}, call{})
		if got || err != nil {
			t.Fatalf("unsupportedSafe = %t, %v; want false", got, err)
		}
	}

	// The Safe is read once for each chain and block, with two requests.
	at := &env{dial: node.dialer(t, testSafeBlock), block: testSafeBlock}
	match(at, ethrpc.Mainnet)
	match(at, ethrpc.Mainnet)
	if got := node.reads.Load(); got != 2 {
		t.Errorf("got %d requests for one chain and block, want 2", got)
	}
	match(at, ethrpc.Gnosis)
	match(&env{dial: node.dialer(t, testSafeBlock+1), block: testSafeBlock + 1}, ethrpc.Mainnet)
	if got := node.reads.Load(); got != 6 {
		t.Errorf("got %d requests for three chains and blocks, want 6", got)
	}
}

func TestUnsupportedSafeErrors(t *testing.T) {
	safe := &safeID{address: ethrpc.Address{0: 0x5a}, chainID: big.NewInt(ethrpc.ArbitrumOne)}
	for _, method := range []string{"eth_getCode", "eth_getStorageAt"} {
		safeVersions.Clear()

		// An error isn't cached, so the Safe is read again.
		node := &fakeNode{accounts: map[ethrpc.Address]account{safe.address: proxy(safeProxy150, singletonOf("1.5.0"))}, fail: method}
		at := &env{dial: node.dialer(t, testSafeBlock), block: testSafeBlock}
		for range 2 {
			if _, err := unsupportedSafe.fn(t.Context(), at, safe, call{}); err == nil || !strings.Contains(err.Error(), "missing trie node") {
				t.Errorf("unsupportedSafe with %s failing: got error %v, want the node's error", method, err)
			}
		}
		if _, ok := safeVersions.Load(safeAt{address: safe.address, chainID: ethrpc.ArbitrumOne, block: testSafeBlock}); ok {
			t.Errorf("unsupportedSafe with %s failing: cached a result", method)
		}
	}

	safeVersions.Clear()
	dialErr := func(context.Context, uint64) (*ethrpc.Client, error) { return nil, fmt.Errorf("no RPC") }
	if _, err := unsupportedSafe.fn(t.Context(), &env{dial: dialErr, block: testSafeBlock}, safe, call{}); err == nil {
		t.Error("unsupportedSafe: expected an error when the chain can't be dialed")
	}
}

func TestClassifyUnsupportedSafe(t *testing.T) {
	safeVersions.Clear()
	safe := ethrpc.Address{0: 0x5a, 19: 0xcb}
	node := &fakeNode{accounts: map[ethrpc.Address]account{safe: proxy(safeProxy130, ethrpc.Address{19: 1})}}
	tx := emptyMultiSend(ethrpc.MustParseAddress("0x218543288004CD07832472D464648173c77D7eB7"))
	got, err := Classify(t.Context(), node.dialer(t, testSafeBlock), request(safenet.SafeTransaction{
		Safe:      safe,
		To:        tx.to,
		Data:      tx.data,
		Operation: tx.operation,
	}))
	if want := unsupportedSafe.classification(); got != want || err != nil {
		t.Errorf("Classify() = %+v, %v; want %+v", got, err, want)
	}
}

// singletonOf returns a singleton of version.
func singletonOf(version string) ethrpc.Address {
	for address, v := range singletons {
		if v == version {
			return address
		}
	}
	panic("no singleton of version " + version)
}
