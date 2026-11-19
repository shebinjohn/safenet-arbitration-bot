package e2e_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// safeOutput is the JSON output of `arbot safe`.
type safeOutput struct {
	Safe            ethrpc.Address   `json:"safe"`
	ChainID         uint64           `json:"chainId"`
	SafeBlock       uint64           `json:"safeBlock"`
	Singleton       ethrpc.Address   `json:"singleton"`
	Owners          []ethrpc.Address `json:"owners"`
	OwnerCount      *big.Int         `json:"ownerCount"`
	Threshold       *big.Int         `json:"threshold"`
	Nonce           *big.Int         `json:"nonce"`
	Modules         []ethrpc.Address `json:"modules"`
	FallbackHandler ethrpc.Address   `json:"fallbackHandler"`
	Guard           ethrpc.Address   `json:"guard"`
	ModuleGuard     ethrpc.Address   `json:"moduleGuard"`
}

// TestSafe runs `arbot safe` on a Safe 1.3.0 that the singleton's setup set up
// on the Mainnet node, with a guard set before the proposal, and a module guard
// set after it, which the evidence must not include, and compares it with a
// second Safe, and the second Safe with itself before a settings change.
func TestSafe(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	r := n.Propose(nil)
	if r.Transaction.ChainID.Uint64() != ethrpc.Mainnet {
		t.Fatalf("Safe transaction is for chain %s, want Ethereum Mainnet", r.Transaction.ChainID)
	}
	mainnet, safe := n.Mainnet, r.Transaction.Safe
	owner, other := ethrpc.Address{0: 0x0e, 19: 1}, ethrpc.Address{0: 0x0e, 19: 2}
	guard, moduleGuard := ethrpc.Address{0: 0x6a, 19: 1}, ethrpc.Address{0: 0x36, 19: 1}
	// set sets the setting at the hash of name to address, as the singleton would.
	set := func(name string, address ethrpc.Address) {
		var value ethrpc.Hash
		copy(value[12:], address[:])
		mainnet.RPC(nil, "anvil_setStorageAt", safe, ethrpc.Hash(keccak256.Hash([]byte(name))), value)
	}

	// The Mainnet node's blocks are earlier than the proposal until it mines up to
	// it.
	mainnet.SetBalance(owner, new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil))
	n.SetupSafe(mainnet, safe, singleton130, owner, other)
	set("guard_manager.guard.address", guard)

	// A second Safe, which shares an owner with the Safe, and raises its threshold
	// to two in a Safe transaction before the proposal. The Mainnet node only keeps
	// the states of its last 128 blocks, which -at reads, so its blocks are mined
	// up to just before the proposal first.
	mainnet.MineUntil(n.Header(r.Block).Time().Add(-3 * time.Minute))
	second, third := ethrpc.Address{0: 0x5a, 19: 0xf3}, ethrpc.Address{0: 0x0e, 19: 3}
	n.SetupSafe(mainnet, second, singleton130, owner, third)
	changeThreshold := safenet.SafeTransaction{
		To: second, Value: new(big.Int), Data: solabi.Call(solabi.Selector("changeThreshold(uint256)"), uint64(2)),
		SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int),
	}
	changed := mainnet.Transact(owner, second, n.ExecCalldata(&changeThreshold, owner))
	beforeChange := uint64(changed.BlockNumber) - 1
	mainnet.MineUntil(n.Header(r.Block).Time())
	set("module_manager.module_guard.address", moduleGuard)
	mainnet.MineUntil(n.Header(r.Block).Time().Add(time.Minute))

	arbot := n.Arbot()
	info := arbot.Run(t, "info", "-json", r.ID.String())
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, info, 0o644); err != nil {
		t.Fatal(err)
	}
	safeBlock := decode[requestInfo](t, info).Proposal.SafeBlock

	got := decode[safeOutput](t, arbot.Run(t, "safe", "-json", "-request-file", path))
	want := safeOutput{
		Safe: safe, ChainID: ethrpc.Mainnet, SafeBlock: safeBlock, Singleton: singleton130,
		Owners: []ethrpc.Address{owner, other}, OwnerCount: big.NewInt(2), Threshold: big.NewInt(1), Nonce: new(big.Int),
		Modules: []ethrpc.Address{}, Guard: guard,
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("safe:\ngot  %s\nwant %s", g, w)
	}

	t.Run("text", func(t *testing.T) {
		text := arbot.Run(t, "safe", "-request-file", path)
		for _, line := range []string{
			`Safe ` + safe.String() + ` on chain 1, as of block \d+`,
			`  Singleton         ` + singleton130.String() + `, Safe singleton 1\.3\.0`,
			`  Threshold         1 of 2 owners`,
			`  Guard             ` + guard.String(),
			`  ` + other.String(),
			`  none`,
		} {
			if !regexp.MustCompile(`(?m)^` + line + `$`).Match(text) {
				t.Errorf("safe: output has no line matching %q:\n%s", line, text)
			}
		}
	})

	t.Run("missing request file", func(t *testing.T) {
		arbot.Fail(t, 2, "safe")
	})

	t.Run("address", func(t *testing.T) {
		got := decode[safeAccountOutput](t, arbot.Run(t, "safe", "-json", "-request-file", path, "-address", second.String()))
		if got.Address != second || got.Block != safeBlock || got.ProxyVersion == nil || *got.ProxyVersion != "1.3.0" ||
			got.Settings == nil || got.Settings.Threshold.Int64() != 2 || got.Settings.Nonce.Int64() != 1 || got.Reason != nil {
			t.Errorf("safe -address: got %+v, want the second Safe with threshold 2 and nonce 1", got)
		}
		got = decode[safeAccountOutput](t, arbot.Run(t, "safe", "-json", "-request-file", path, "-address", second.String(), "-at", fmt.Sprint(beforeChange)))
		if got.Block != beforeChange || got.Settings == nil || got.Settings.Threshold.Int64() != 1 || got.Settings.Nonce.Int64() != 0 {
			t.Errorf("safe -address -at: got %+v, want the second Safe with threshold 1 and nonce 0", got)
		}
		// An owner has no code, so it has no settings.
		got = decode[safeAccountOutput](t, arbot.Run(t, "safe", "-json", "-request-file", path, "-address", owner.String()))
		if got.Settings != nil || got.Reason == nil || *got.Reason != "no-code" {
			t.Errorf("safe -address of an owner: got %+v, want no settings, since it has no code", got)
		}
		if stderr := arbot.Fail(t, 2, "safe", "-request-file", path, "-at", fmt.Sprint(safeBlock+1)); !strings.Contains(string(stderr), "is after the request's Safe block") {
			t.Errorf("safe -at after the Safe block: got %q, want a usage error", stderr)
		}
	})

	t.Run("compare", func(t *testing.T) {
		// The two Safes as of the Safe block.
		got := decode[safeCompareOutput](t, arbot.Run(t, "safe", "-json", "-request-file", path, "-compare", second.String()))
		if got.ChainID != ethrpc.Mainnet || got.SafeBlock != safeBlock || got.A.Address != safe || got.A.Block != safeBlock ||
			got.B.Address != second || got.B.Block != safeBlock || got.Diff == nil {
			t.Fatalf("safe -compare: got %+v, want the Safe and the second Safe as of the Safe block, compared", got)
		}
		want := `{"singleton":"equal","owners":{"result":"different","shared":["` + lower(owner) + `"],"onlyA":["` + lower(other) + `"],"onlyB":["` + lower(third) + `"]},` +
			`"ownerCount":"equal","threshold":"different","nonce":"different","modules":{"result":"equal","shared":[],"onlyA":[],"onlyB":[]},` +
			`"fallbackHandler":"equal","guard":"different","moduleGuard":"equal","sameConfiguration":false}`
		if d, _ := json.Marshal(got.Diff); string(d) != want {
			t.Errorf("safe -compare: got diff\n%s\nwant\n%s", d, want)
		}

		// The second Safe before and after it raised its threshold.
		got = decode[safeCompareOutput](t, arbot.Run(t, "safe", "-json", "-request-file", path, "-compare", second.String(), "-at", fmt.Sprint(beforeChange)))
		want = `{"singleton":"equal","owners":{"result":"equal","shared":["` + lower(owner) + `","` + lower(third) + `"],"onlyA":[],"onlyB":[]},` +
			`"ownerCount":"equal","threshold":"different","nonce":"different","modules":{"result":"equal","shared":[],"onlyA":[],"onlyB":[]},` +
			`"fallbackHandler":"equal","guard":"equal","moduleGuard":"equal","sameConfiguration":false}`
		if d, _ := json.Marshal(got.Diff); got.A.Address != second || got.A.Block != beforeChange || got.B.Address != second || got.B.Block != safeBlock || string(d) != want {
			t.Errorf("safe -compare -at: got %+v with diff\n%s\nwant the second Safe at blocks %d and %d, with diff\n%s", got, d, beforeChange, safeBlock, want)
		}

		text := arbot.Run(t, "safe", "-request-file", path, "-compare", second.String())
		for _, line := range []string{
			`threshold +different +1 +2`,
			`  both +` + owner.String(),
			`Shared owners, or an identical configuration, are facts about who controls the Safes, not by`,
		} {
			if !regexp.MustCompile(`(?m)^` + line + `$`).Match(text) {
				t.Errorf("safe -compare: output has no line matching %q:\n%s", line, text)
			}
		}
	})
}

// safeAccountOutput is the JSON output of `arbot safe -address`, and an account
// in that of `arbot safe -compare`.
type safeAccountOutput struct {
	Address      ethrpc.Address `json:"address"`
	Block        uint64         `json:"block"`
	ProxyVersion *string        `json:"proxyVersion"`
	Settings     *safeOutput    `json:"settings"`
	Reason       *string        `json:"reason"`
}

// safeCompareOutput is the JSON output of `arbot safe -compare`.
type safeCompareOutput struct {
	ChainID   uint64            `json:"chainId"`
	SafeBlock uint64            `json:"safeBlock"`
	A         safeAccountOutput `json:"a"`
	B         safeAccountOutput `json:"b"`
	Diff      json.RawMessage   `json:"diff"`
}

// lower returns an address as JSON has it.
func lower(a ethrpc.Address) string {
	return strings.ToLower(a.String())
}
