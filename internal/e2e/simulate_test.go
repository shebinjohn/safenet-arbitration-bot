package e2e_test

import (
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// simulateOutput is the JSON output of `arbot simulate`.
type simulateOutput struct {
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	Safe      ethrpc.Address `json:"safe"`
	Number    int            `json:"number"`
	First     bool           `json:"first"`
	Call      struct {
		Kind string `json:"kind"`
	} `json:"call"`
	Result struct {
		Status   string `json:"status"`
		Function *struct {
			Signature string `json:"signature"`
			Outputs   []any  `json:"outputs"`
		} `json:"function"`
		Source *struct {
			URL string `json:"url"`
		} `json:"source"`
	} `json:"result"`
}

// TestSimulate runs `arbot simulate` against a real Safe 1.3.0 on anvil: a call
// to the Safe that adds an owner, which the Safe allows from itself, but which
// reverts, since it sends value to a function that isn't payable.
func TestSimulate(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	// The first transaction is a delegate call and the second a call, both to the
	// Safe on Ethereum Mainnet, and the second has a refund.
	delegate := n.Propose(nil)
	r := n.Propose(nil)
	tx := r.Transaction
	if tx.ChainID.Uint64() != ethrpc.Mainnet || tx.Operation != 0 || tx.To != tx.Safe || tx.Value.Sign() == 0 || tx.GasPrice.Sign() == 0 {
		t.Fatalf("Safe transaction %+v isn't a call with value to the Safe on Ethereum Mainnet with a refund", tx)
	}
	mainnet, a := n.Mainnet, n.Artifacts
	owner := ethrpc.Address{0: 0x0e, 19: 1}
	ether := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	mainnet.SetBalance(owner, ether)
	mainnet.SetBalance(tx.Safe, ether)
	n.SetupSafe(mainnet, tx.Safe, a.SafeSingleton, owner)
	// The proxy's ABI with the singleton's function, which is enough to check the
	// output.
	n.ServeSourcify(tx.Safe, `[
		{"type": "function", "name": "addOwnerWithThreshold", "inputs": [{"name": "owner", "type": "address"}, {"name": "_threshold", "type": "uint256"}], "outputs": [], "stateMutability": "nonpayable"}
	]`, nil)
	mainnet.MineUntil(n.Header(r.Block).Time())

	arbot := n.Arbot()
	dir := t.TempDir()
	path := filepath.Join(dir, "request.json")
	if err := os.WriteFile(path, arbot.Run(t, "info", "-json", r.ID.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	pathDelegate := filepath.Join(dir, "delegate.json")
	if err := os.WriteFile(pathDelegate, arbot.Run(t, "info", "-json", delegate.ID.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got := decode[simulateOutput](t, arbot.Run(t, "simulate", "-json", "-request-file", path))
	if got.ChainID != ethrpc.Mainnet || got.Safe != tx.Safe || got.Number != 1 || !got.First || got.Call.Kind != "root" ||
		got.Result.Status != "reverted" || got.Result.Function == nil ||
		got.Result.Function.Signature != "addOwnerWithThreshold(address,uint256)" || got.Result.Function.Outputs != nil ||
		got.Result.Source == nil {
		t.Errorf("simulate: got %+v, want a reverted root call of addOwnerWithThreshold", got)
	}
	text := arbot.Run(t, "simulate", "-request-file", path)
	for _, want := range []string{`(?m)^Result +reverted$`, `(?m)^  Function  addOwnerWithThreshold\(address,uint256\)$`} {
		if !regexp.MustCompile(want).Match(text) {
			t.Errorf("simulate: output has no line matching %q:\n%s", want, text)
		}
	}

	// The refund call, a call that doesn't exist, and a delegate call can't be
	// simulated.
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"-call", "2", "-request-file", path}, "refund"},
		{[]string{"-call", "3", "-request-file", path}, "no call 3"},
		{[]string{"-request-file", pathDelegate}, "delegate call"},
	} {
		stderr := arbot.Fail(t, 1, append([]string{"simulate"}, test.args...)...)
		if !regexp.MustCompile(test.want).Match(stderr) {
			t.Errorf("simulate %v: got %q, want an error with %q", test.args, stderr, test.want)
		}
	}
}
