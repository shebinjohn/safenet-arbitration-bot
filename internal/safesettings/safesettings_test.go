package safesettings

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

const testBlock = 42

var testSafe = ethrpc.Address{0: 0x5a, 19: 0xfe}

// fakeNode serves the code and storage of testSafe at testBlock, and those of
// the other accounts, and fails the test for any other account or block.
type fakeNode struct {
	code    ethrpc.Bytes
	storage map[ethrpc.Hash]ethrpc.Hash
	// accounts are the other accounts that the node serves.
	accounts map[ethrpc.Address]fakeAccount
	// fail makes the node answer eth_getStorageAt with an error.
	fail bool
	// overloaded makes the node answer every request but eth_chainId with HTTP
	// status 500, as an overloaded node does.
	overloaded bool
}

// fakeAccount is the code and storage of an account.
type fakeAccount struct {
	code    ethrpc.Bytes
	storage map[ethrpc.Hash]ethrpc.Hash
}

// account returns the account at address, and whether the node serves it.
func (n *fakeNode) account(address ethrpc.Address) (fakeAccount, bool) {
	if address == testSafe {
		return fakeAccount{code: n.code, storage: n.storage}, true
	}
	a, ok := n.accounts[address]
	return a, ok
}

func (n *fakeNode) client(t *testing.T) *ethrpc.Client {
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
		if n.overloaded && req.Method != "eth_chainId" {
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		switch req.Method {
		case "eth_chainId":
			reply["result"] = ethrpc.Quantity(ethrpc.Mainnet)
		case "eth_getCode":
			var address ethrpc.Address
			var block ethrpc.BlockNumber
			if len(req.Params) != 2 || json.Unmarshal(req.Params[0], &address) != nil || json.Unmarshal(req.Params[1], &block) != nil || block != testBlock {
				t.Errorf("eth_getCode: got params %s, want an account at block %d", req.Params, testBlock)
			}
			account, ok := n.account(address)
			if !ok {
				t.Errorf("eth_getCode: got account %s, which the node doesn't serve", address)
			}
			if account.code == nil {
				account.code = ethrpc.Bytes{}
			}
			reply["result"] = account.code
		case "eth_getStorageAt":
			var address ethrpc.Address
			var slot ethrpc.Hash
			var block ethrpc.BlockNumber
			if len(req.Params) != 3 || json.Unmarshal(req.Params[0], &address) != nil ||
				json.Unmarshal(req.Params[1], &slot) != nil || json.Unmarshal(req.Params[2], &block) != nil || block != testBlock {
				t.Errorf("eth_getStorageAt: got params %s, want an account at block %d", req.Params, testBlock)
			}
			account, ok := n.account(address)
			if !ok {
				t.Errorf("eth_getStorageAt: got account %s, which the node doesn't serve", address)
			}
			if n.fail {
				reply["error"] = map[string]any{"code": -32000, "message": "missing trie node"}
			} else {
				reply["result"] = account.storage[slot]
			}
		default:
			t.Errorf("unexpected method %s", req.Method)
		}
		json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(server.Close)
	client, err := ethrpc.NewClient(t.Context(), ethrpc.Mainnet, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// word returns an address or number as a storage word.
func word[T ethrpc.Address | uint64](v T) ethrpc.Hash {
	var h ethrpc.Hash
	switch v := any(v).(type) {
	case ethrpc.Address:
		copy(h[12:], v[:])
	case uint64:
		new(big.Int).SetUint64(v).FillBytes(h[:])
	}
	return h
}

// linkedList returns the storage of a linked list of entries in the mapping at
// slot, from sentinel through the entries and back to it.
func linkedList(slot ethrpc.Hash, entries ...ethrpc.Address) map[ethrpc.Hash]ethrpc.Hash {
	storage := map[ethrpc.Hash]ethrpc.Hash{}
	previous := sentinel
	for _, entry := range append(entries, sentinel) {
		storage[mappingSlot(previous, slot)] = word(entry)
		previous = entry
	}
	return storage
}

func TestRead(t *testing.T) {
	singleton := ethrpc.MustParseAddress("0x41675C099F32341bf84BFc5382aF534df5C7461a")
	owner1, owner2 := ethrpc.Address{0: 0x01, 19: 1}, ethrpc.Address{0: 0x01, 19: 2}
	module := ethrpc.Address{0: 0x0d, 19: 1}
	handler, guard, moduleGuard := ethrpc.Address{0: 0xfb}, ethrpc.Address{0: 0x6a}, ethrpc.Address{0: 0x36}

	// A Safe with two owners, a module, and every other setting set, at the slots
	// of the official Safe singletons.
	full := map[ethrpc.Hash]ethrpc.Hash{
		{}:      word(singleton),
		{31: 3}: word(uint64(2)),
		{31: 4}: word(uint64(1)),
		{31: 5}: word(uint64(71)),
		ethrpc.MustParseHash("0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5"): word(handler),
		ethrpc.MustParseHash("0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8"): word(guard),
		ethrpc.MustParseHash("0xb104e0b93118902c651344349b610029d694cfdec91c589c91ebafbcd0289947"): word(moduleGuard),
	}
	for _, list := range []map[ethrpc.Hash]ethrpc.Hash{linkedList(ownersSlot, owner1, owner2), linkedList(modulesSlot, module)} {
		for k, v := range list {
			full[k] = v
		}
	}
	// A Safe whose lists are empty, as setup leaves its modules.
	empty := linkedList(ownersSlot)
	for k, v := range linkedList(modulesSlot) {
		empty[k] = v
	}

	tests := []struct {
		name    string
		storage map[ethrpc.Hash]ethrpc.Hash
		want    Settings
	}{
		{
			"every setting",
			full,
			Settings{
				Singleton: singleton, Owners: []ethrpc.Address{owner1, owner2}, OwnerCount: big.NewInt(2),
				Threshold: big.NewInt(1), Nonce: big.NewInt(71), Modules: []ethrpc.Address{module},
				FallbackHandler: handler, Guard: guard, ModuleGuard: moduleGuard,
			},
		},
		{
			"empty lists",
			empty,
			Settings{Owners: []ethrpc.Address{}, OwnerCount: new(big.Int), Threshold: new(big.Int), Nonce: new(big.Int), Modules: []ethrpc.Address{}},
		},
		{
			"never set up",
			nil,
			Settings{Owners: []ethrpc.Address{}, OwnerCount: new(big.Int), Threshold: new(big.Int), Nonce: new(big.Int), Modules: []ethrpc.Address{}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := &fakeNode{storage: test.storage}
			got, err := Read(t.Context(), node.client(t), testSafe, testBlock)
			if err != nil {
				t.Fatal(err)
			}
			// Settings are compared as JSON, since equal big.Int values can differ in their
			// representation.
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(test.want)
			if string(g) != string(w) {
				t.Errorf("Read():\ngot  %s\nwant %s", g, w)
			}
		})
	}
}

func TestReadMalformedLists(t *testing.T) {
	a, b := ethrpc.Address{0: 0xa}, ethrpc.Address{0: 0xb}
	// A list that ends at the zero address, without coming back to sentinel.
	unterminated := map[ethrpc.Hash]ethrpc.Hash{mappingSlot(sentinel, ownersSlot): word(a)}
	// A list that loops between two entries, and never comes back to sentinel.
	loop := map[ethrpc.Hash]ethrpc.Hash{
		mappingSlot(sentinel, modulesSlot): word(a),
		mappingSlot(a, modulesSlot):        word(b),
		mappingSlot(b, modulesSlot):        word(a),
	}
	for _, test := range []struct {
		name    string
		storage map[ethrpc.Hash]ethrpc.Hash
		want    string
	}{
		{"unterminated", unterminated, "list of owners of " + testSafe.String() + " ends"},
		{"loop", loop, "list of modules of " + testSafe.String() + " has more than 1024 entries"},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := &fakeNode{storage: test.storage}
			if _, err := Read(t.Context(), node.client(t), testSafe, testBlock); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("Read() = _, %v; want an error containing %q", err, test.want)
			}
		})
	}
}

func TestReadError(t *testing.T) {
	node := &fakeNode{fail: true}
	if _, err := Read(t.Context(), node.client(t), testSafe, testBlock); err == nil || !strings.Contains(err.Error(), "missing trie node") {
		t.Errorf("Read() = _, %v; want the node's error", err)
	}
}

func TestMappingSlot(t *testing.T) {
	// The slot of owners[sentinel] in a Safe, which is the same in every Safe.
	want := ethrpc.MustParseHash("0xe90b7bceb6e7df5418fb78d8ee546e97c83a08bbccc01a0644d599ccd2a7c2e0")
	if got := mappingSlot(sentinel, ownersSlot); got != want {
		t.Errorf("mappingSlot(sentinel, ownersSlot) = %s, want %s", got, want)
	}
	if mappingSlot(sentinel, ownersSlot) == mappingSlot(sentinel, modulesSlot) {
		t.Error("mappingSlot: the owners and modules of sentinel have the same slot")
	}
}
