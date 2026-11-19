package e2e_test

import (
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
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
			Inputs    []struct {
				Type string `json:"type"`
			} `json:"inputs"`
			Outputs []any `json:"outputs"`
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
		len(got.Result.Function.Inputs) != 2 || got.Result.Function.Inputs[0].Type != "address" ||
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

// multiSendCallOnly130 is MultiSendCallOnly 1.3.0, at its address and with its
// code on Ethereum Mainnet. It reverts with no data if a call that it makes
// reverts.
var (
	multiSendCallOnly130     = ethrpc.MustParseAddress("0x40A2aCCbd92BCA938b02010E17A5b8929b49130D")
	multiSendCallOnly130Code = "0x60806040526004361061001e5760003560e01c80638d80ff0a14610023575b600080fd5b6100dc6004803603602081101561003957600080fd5b810190808035906020019064010000000081111561005657600080fd5b82018360208201111561006857600080fd5b8035906020019184600183028401116401000000008311171561008a57600080fd5b91908080601f016020809104026020016040519081016040528093929190818152602001838380828437600081840152601f19601f8201169050808301925050505050505091929192905050506100de565b005b805160205b8181101561015f578083015160f81c6001820184015160601c60158301850151603584018601516055850187016000856000811461012857600181146101385761013d565b6000808585888a5af1915061013d565b600080fd5b50600081141561014c57600080fd5b82605501870196505050505050506100e3565b50505056fea264697066735822122035246402746c96964495cae5b36461fd44dfb89f8e6cf6f6b8d60c0aa89f414864736f6c63430007060033"
)

// simulateAllOutput is the part of the JSON output of `arbot simulate -all
// -trace` that the test checks.
type simulateAllOutput struct {
	All    bool `json:"all"`
	First  bool `json:"first"`
	Result struct {
		Method     string       `json:"method"`
		Status     string       `json:"status"`
		ReturnData ethrpc.Bytes `json:"returnData"`
		Function   *struct {
			Signature string `json:"signature"`
		} `json:"function"`
	} `json:"result"`
	Trace struct {
		Available bool `json:"available"`
		Calls     []struct {
			Depth        int             `json:"depth"`
			Type         string          `json:"type"`
			To           *ethrpc.Address `json:"to"`
			Error        string          `json:"error"`
			RevertReason string          `json:"revertReason"`
		} `json:"calls"`
	} `json:"trace"`
}

// TestSimulateAll runs `arbot simulate -all` against a real Safe 1.3.0 on
// anvil, on two MultiSendCallOnly batches of calls that the Safe makes to
// itself: one that adds two owners, which succeeds, and one that adds the same
// owner twice, whose second call reverts only because the first one ran.
func TestSimulateAll(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	mainnet, a := n.Mainnet, n.Artifacts
	var code ethrpc.Bytes
	if err := code.UnmarshalText([]byte(multiSendCallOnly130Code)); err != nil {
		t.Fatal(err)
	}
	mainnet.RPC(nil, "anvil_setCode", multiSendCallOnly130, code)

	addOwner := func(owner ethrpc.Address) []byte {
		return solabi.Call(solabi.Selector("addOwnerWithThreshold(address,uint256)"), owner, uint64(1))
	}
	batch := func(safe ethrpc.Address, owners ...ethrpc.Address) ethrpc.Bytes {
		var transactions []byte
		for _, owner := range owners {
			data := addOwner(owner)
			transactions = append(append(transactions, 0), safe[:]...)
			transactions = append(append(transactions, solabi.Encode(uint64(0), uint64(len(data)))...), data...)
		}
		return solabi.Call(solabi.Selector("multiSend(bytes)"), transactions)
	}
	first, second := ethrpc.Address{0: 0xbe, 19: 0xa1}, ethrpc.Address{0: 0xbe, 19: 0xa2}
	propose := func(owners ...ethrpc.Address) *e2e.Request {
		return n.ProposeTransaction(func(tx *safenet.SafeTransaction) {
			tx.ChainID, tx.To, tx.Value, tx.Operation = big.NewInt(ethrpc.Mainnet), multiSendCallOnly130, new(big.Int), safenet.OperationDelegateCall
			tx.Data, tx.GasPrice = batch(tx.Safe, owners...), new(big.Int)
		})
	}
	succeeds, reverts := propose(first, second), propose(first, first)

	owner := ethrpc.Address{0: 0x0e, 19: 1}
	ether := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	mainnet.SetBalance(owner, ether)
	n.SetupSafe(mainnet, succeeds.Transaction.Safe, a.SafeSingleton, owner)
	n.ServeSourcify(multiSendCallOnly130, `[
		{"type": "function", "name": "multiSend", "inputs": [{"name": "transactions", "type": "bytes"}], "outputs": [], "stateMutability": "payable"}
	]`, nil)
	mainnet.MineUntil(n.Header(reverts.Block).Time())

	arbot := n.Arbot()
	dir := t.TempDir()
	run := func(r *e2e.Request) simulateAllOutput {
		t.Helper()
		path := filepath.Join(dir, r.ID.String()+".json")
		if err := os.WriteFile(path, arbot.Run(t, "info", "-json", r.ID.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		// Each batched call succeeds when it is simulated alone, since the owner isn't
		// one yet at the Safe block.
		for _, call := range []string{"1", "2"} {
			alone := decode[simulateOutput](t, arbot.Run(t, "simulate", "-json", "-call", call, "-request-file", path))
			if alone.Result.Status != "success" {
				t.Errorf("simulate -call %s: got %+v, want success", call, alone.Result)
			}
		}
		return decode[simulateAllOutput](t, arbot.Run(t, "simulate", "-all", "-trace", "-json", "-request-file", path))
	}

	got := run(succeeds)
	if !got.All || !got.First || got.Result.Method != "simulateAndRevert" || got.Result.Status != "success" ||
		got.Result.Function == nil || got.Result.Function.Signature != "multiSend(bytes)" {
		t.Errorf("simulate -all of the batch that succeeds: got %+v", got)
	}
	// The trace's top frame is the call of simulateAndRevert, which reverts by
	// design, then the Safe's delegate calls to its singleton and the MultiSend,
	// which make the two calls to the Safe, which delegate calls to its singleton
	// in turn.
	if tr := got.Trace; !tr.Available || len(tr.Calls) < 5 || tr.Calls[0].Error == "" || tr.Calls[2].Type != "DELEGATECALL" ||
		tr.Calls[2].To == nil || *tr.Calls[2].To != multiSendCallOnly130 {
		t.Errorf("simulate -all -trace of the batch that succeeds: got trace %+v", tr)
	}

	got = run(reverts)
	if got.Result.Status != "reverted" || len(got.Result.ReturnData) != 0 {
		t.Errorf("simulate -all of the batch that reverts: got %+v, want a revert without data", got.Result)
	}
	// The batch's calls to the Safe are the frames at depth 3. The second one
	// reverts, since the owner is already one.
	var batched []string
	for _, c := range got.Trace.Calls {
		if c.Depth == 3 {
			batched = append(batched, c.Error)
		}
	}
	if len(batched) != 2 || batched[0] != "" || batched[1] == "" {
		t.Errorf("simulate -all -trace of the batch that reverts: got the errors %q of the batched calls, want only the second to fail", batched)
	}
}
