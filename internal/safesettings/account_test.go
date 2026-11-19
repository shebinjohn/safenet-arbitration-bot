package safesettings

import (
	"encoding/hex"
	"encoding/json"
	"maps"
	"math/big"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// The code of the SafeProxy contracts of Safe 1.3.0, 1.4.1, and 1.5.0, as the
// checks identify them.
var (
	safeProxy130 = mustDecode("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea2646970667358221220d1429297349653a4918076d650332de1a1068c5f3e07c5c82360c277770b955264736f6c63430007060033")
	safeProxy141 = mustDecode("608060405273ffffffffffffffffffffffffffffffffffffffff600054167fa619486e0000000000000000000000000000000000000000000000000000000060003514156050578060005260206000f35b3660008037600080366000845af43d6000803e60008114156070573d6000fd5b3d6000f3fea264697066735822122003d1488ee65e08fa41e58e888a9865554c535f2c77126a82cb4c0f917f31441364736f6c63430007060033")
	safeProxy150 = mustDecode("608060405260005463a619486e60003560e01c14156024578060601b606c5260206060f35b3660008037600080366000845af43d6000803e806040573d6000fd5b3d6000f3fea2646970667358221220e61834ebd2d8cd909d362bf67c47ef58fd665df38e6dd036ce65611101d072e964736f6c63430007060033")
)

// The official Safe singletons of 1.3.0, SafeL2 1.4.1, and 1.5.0.
var (
	singleton130   = ethrpc.MustParseAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552")
	singletonL2141 = ethrpc.MustParseAddress("0x29fcB43b46531BcA003ddC8FCB67FFE91900C762")
	singleton150   = ethrpc.MustParseAddress("0xFf51A5898e281Db6DfC7855790607438dF2ca44b")
)

func mustDecode(s string) ethrpc.Bytes {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// safeStorage returns the storage of a Safe with singleton, threshold, nonce,
// owners, and modules, and the other settings in extra.
func safeStorage(singleton ethrpc.Address, threshold, nonce uint64, owners, modules []ethrpc.Address, extra map[ethrpc.Hash]ethrpc.Hash) map[ethrpc.Hash]ethrpc.Hash {
	storage := map[ethrpc.Hash]ethrpc.Hash{
		singletonSlot:  word(singleton),
		ownerCountSlot: word(uint64(len(owners))),
		thresholdSlot:  word(threshold),
		nonceSlot:      word(nonce),
	}
	maps.Copy(storage, linkedList(ownersSlot, owners...))
	maps.Copy(storage, linkedList(modulesSlot, modules...))
	maps.Copy(storage, extra)
	return storage
}

func version(v string) *string { return &v }

func TestReadAccount(t *testing.T) {
	owner1, owner2 := ethrpc.Address{0: 0x01, 19: 1}, ethrpc.Address{0: 0x01, 19: 2}
	module := ethrpc.Address{0: 0x0d, 19: 1}
	handler, guard, moduleGuard := ethrpc.Address{0: 0xfb}, ethrpc.Address{0: 0x6a}, ethrpc.Address{0: 0x36}

	tests := []struct {
		name             string
		code             ethrpc.Bytes
		storage          map[ethrpc.Hash]ethrpc.Hash
		proxyVersion     *string
		singletonVersion *string
		want             *Settings
	}{
		{
			// Safe 1.3.0 has no module guard, so its slot is empty.
			"Safe 1.3.0",
			safeProxy130,
			safeStorage(singleton130, 1, 7, []ethrpc.Address{owner1, owner2}, nil, map[ethrpc.Hash]ethrpc.Hash{fallbackHandlerSlot: word(handler)}),
			version("1.3.0"), version("1.3.0"),
			&Settings{
				Singleton: singleton130, Owners: []ethrpc.Address{owner1, owner2}, OwnerCount: big.NewInt(2), Threshold: big.NewInt(1),
				Nonce: big.NewInt(7), Modules: []ethrpc.Address{}, FallbackHandler: handler,
			},
		},
		{
			"SafeL2 1.4.1 with a guard and a module",
			safeProxy141,
			safeStorage(singletonL2141, 2, 0, []ethrpc.Address{owner2, owner1}, []ethrpc.Address{module}, map[ethrpc.Hash]ethrpc.Hash{guardSlot: word(guard)}),
			version("1.4.1"), version("1.4.1+L2"),
			&Settings{
				Singleton: singletonL2141, Owners: []ethrpc.Address{owner2, owner1}, OwnerCount: big.NewInt(2), Threshold: big.NewInt(2),
				Nonce: new(big.Int), Modules: []ethrpc.Address{module}, Guard: guard,
			},
		},
		{
			"Safe 1.5.0 with a module guard",
			safeProxy150,
			safeStorage(singleton150, 1, 3, []ethrpc.Address{owner1}, nil, map[ethrpc.Hash]ethrpc.Hash{moduleGuardSlot: word(moduleGuard)}),
			version("1.5.0"), version("1.5.0"),
			&Settings{
				Singleton: singleton150, Owners: []ethrpc.Address{owner1}, OwnerCount: big.NewInt(1), Threshold: big.NewInt(1),
				Nonce: big.NewInt(3), Modules: []ethrpc.Address{}, ModuleGuard: moduleGuard,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := &fakeNode{code: test.code, storage: test.storage}
			got, err := ReadAccount(t.Context(), node.client(t), testSafe, testBlock)
			if err != nil {
				t.Fatal(err)
			}
			want := &Account{Block: testBlock, Settings: test.want}
			want.Address, want.CodeHash, want.CodeSize = testSafe, got.CodeHash, len(test.code)
			want.ProxyVersion, want.Singleton, want.SingletonVersion = test.proxyVersion, test.want.Singleton, test.singletonVersion
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			if string(g) != string(w) {
				t.Errorf("ReadAccount():\ngot  %s\nwant %s", g, w)
			}
		})
	}
}

func TestReadAccountNotASafe(t *testing.T) {
	owner := ethrpc.Address{0: 0x01, 19: 1}
	// A contract that isn't a SafeProxy, with storage that has the layout of a
	// Safe, which mustn't be read as settings.
	contract := mustDecode("60006000fd")
	unknownSingleton := ethrpc.Address{0: 0x51, 19: 1}
	for _, test := range []struct {
		name    string
		code    ethrpc.Bytes
		storage map[ethrpc.Hash]ethrpc.Hash
		reason  string
	}{
		{"no code", nil, nil, NoCode},
		{"another contract", contract, safeStorage(singleton130, 1, 0, []ethrpc.Address{owner}, nil, nil), NotASafeProxy},
		{"a proxy of another singleton", safeProxy130, safeStorage(unknownSingleton, 1, 0, []ethrpc.Address{owner}, nil, nil), UnknownSingleton},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := &fakeNode{code: test.code, storage: test.storage}
			got, err := ReadAccount(t.Context(), node.client(t), testSafe, testBlock)
			if err != nil {
				t.Fatal(err)
			}
			if got.Settings != nil || got.Reason != test.reason || got.Address != testSafe || got.Block != testBlock || got.CodeSize != len(test.code) {
				t.Errorf("ReadAccount() = %+v; want no settings, for the reason %q", got, test.reason)
			}
			if Reasons[got.Reason] == "" {
				t.Errorf("Reasons has no description of %q", got.Reason)
			}
			g, _ := json.Marshal(got)
			var fields map[string]json.RawMessage
			json.Unmarshal(g, &fields)
			if string(fields["settings"]) != "null" || string(fields["reason"]) != `"`+test.reason+`"` {
				t.Errorf("ReadAccount() as JSON = %s; want null settings and the reason %q", g, test.reason)
			}
		})
	}
}

func TestReadAccountTransient(t *testing.T) {
	// A node that fails to answer says nothing about the account, so ReadAccount
	// fails, rather than reporting an account without code.
	node := &fakeNode{code: safeProxy130, overloaded: true}
	if got, err := ReadAccount(t.Context(), node.client(t), testSafe, testBlock); err == nil || !ethrpc.Transient(err) {
		t.Errorf("ReadAccount() = %+v, %v; want a transient error", got, err)
	}
}

func TestCompare(t *testing.T) {
	shared1, shared2 := ethrpc.Address{0: 0x01, 19: 1}, ethrpc.Address{0: 0x01, 19: 2}
	onlyA, onlyB := ethrpc.Address{0: 0x0a, 19: 1}, ethrpc.Address{0: 0x0b, 19: 1}
	module := ethrpc.Address{0: 0x0d, 19: 1}
	handler := ethrpc.Address{0: 0xfb}

	// Two Safes with overlapping owners, in other orders, and different thresholds.
	a := &Settings{
		Singleton: singleton130, Owners: []ethrpc.Address{shared1, onlyA, shared2}, OwnerCount: big.NewInt(3),
		Threshold: big.NewInt(2), Nonce: big.NewInt(5), Modules: []ethrpc.Address{module}, FallbackHandler: handler,
	}
	b := &Settings{
		Singleton: singleton130, Owners: []ethrpc.Address{onlyB, shared2, shared1}, OwnerCount: big.NewInt(3),
		Threshold: big.NewInt(1), Nonce: big.NewInt(5), Modules: []ethrpc.Address{}, FallbackHandler: handler,
	}
	got := Compare(a, b)
	want := &Diff{
		Singleton: Equal,
		Owners: ListDiff{
			Result: Different, Shared: []ethrpc.Address{shared1, shared2}, OnlyA: []ethrpc.Address{onlyA}, OnlyB: []ethrpc.Address{onlyB},
		},
		OwnerCount: Equal, Threshold: Different, Nonce: Equal,
		Modules:         ListDiff{Result: Different, Shared: []ethrpc.Address{}, OnlyA: []ethrpc.Address{module}, OnlyB: []ethrpc.Address{}},
		FallbackHandler: Equal, Guard: Equal, ModuleGuard: Equal,
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("Compare():\ngot  %s\nwant %s", g, w)
	}

	// The same owners in another order, and another nonce, are the same
	// configuration.
	c := *a
	c.Owners = []ethrpc.Address{shared2, onlyA, shared1}
	c.Nonce = big.NewInt(9)
	got = Compare(a, &c)
	if !got.SameConfiguration || got.Owners.Result != Equal || len(got.Owners.Shared) != 3 || got.Nonce != Different || got.Threshold != Equal {
		t.Errorf("Compare() of reordered owners and another nonce = %+v; want the same configuration with another nonce", got)
	}
	if got := Compare(a, a); !got.SameConfiguration || got.Nonce != Equal {
		t.Errorf("Compare() of a Safe with itself = %+v; want the same configuration", got)
	}
}
