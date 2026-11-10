package e2e_test

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// historyOutput is the JSON output of `arbot history`.
type historyOutput struct {
	Safe      ethrpc.Address `json:"safe"`
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	Service   struct {
		URL        string    `json:"url"`
		ObservedAt time.Time `json:"observedAt"`
	} `json:"service"`
	Nonce             *big.Int        `json:"nonce"`
	MissingNonces     []nonceRange    `json:"missingNonces"`
	ProposalExecution *executionJSON  `json:"proposalExecution"`
	Executions        []executionJSON `json:"executions"`
	Rejected          []struct {
		SafeTxHash ethrpc.Hash `json:"safeTxHash"`
		Reason     string      `json:"reason"`
	} `json:"rejected"`
}

type nonceRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}

// TestHistory runs `arbot history` against real Safe 1.3.0 contracts on anvil,
// with a Safe Transaction Service that lists the Safe's executions, some of
// them wrongly, and leaves one out.
func TestHistory(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	r := n.Propose(nil)
	mainnet, a := n.Mainnet, n.Artifacts
	safe := r.Transaction.Safe
	// The Mainnet node only keeps the states of its last 128 blocks, so its blocks
	// are mined up to just before the proposal first, and the transactions that
	// follow are in the last of them.
	mainnet.MineUntil(n.Header(r.Block).Time().Add(-3 * time.Minute))
	owner := ethrpc.Address{0: 0x0e, 19: 1}
	target := ethrpc.Address{0: 0xa1, 19: 1}
	mainnet.SetBalance(owner, new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil))
	n.SetupSafe(mainnet, safe, a.SafeSingleton, owner)

	// exec has the owner execute a Safe transaction of the Safe with data, and
	// returns what arbot should report about it, and what the service lists.
	exec := func(data string) (executionJSON, e2e.TxServiceExecution) {
		t.Helper()
		tx := safenet.SafeTransaction{
			ChainID: big.NewInt(ethrpc.Mainnet), Safe: safe, To: target, Value: new(big.Int), Data: []byte(data),
			SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: n.SafeNonce(mainnet, safe),
		}
		receipt := mainnet.Transact(owner, safe, n.ExecCalldata(&tx, owner))
		x := executionJSON{
			Block: uint64(receipt.BlockNumber), TxHash: receipt.TransactionHash, SafeTxHash: tx.Hash(),
			Success: true, Payment: new(big.Int), Source: "service", Transaction: &tx,
		}
		return x, e2e.TxServiceExecution{Transaction: &tx, SafeTxHash: tx.Hash(), TxHash: receipt.TransactionHash, Block: x.Block}
	}
	first, listedFirst := exec("first")
	exec("unlisted")
	third, listedThird := exec("third")
	_, misreported := exec("misreported")
	// The service reports a transaction that isn't the one executed, with the hash
	// of the one executed.
	misreported.Transaction = &safenet.SafeTransaction{
		ChainID: big.NewInt(ethrpc.Mainnet), Safe: safe, To: target, Value: big.NewInt(1), Data: []byte("other"),
		SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: misreported.Transaction.Nonce,
	}
	// An execution after the proposal, which the evidence must not include.
	mainnet.MineUntil(n.Header(r.Block).Time())
	_, later := exec("later")
	n.ServeTxService(listedFirst, listedThird, misreported, later)

	arbot := n.Arbot()
	dir := t.TempDir()
	path := filepath.Join(dir, "request.json")
	info := arbot.Run(t, "info", "-json", r.ID.String())
	if err := os.WriteFile(path, info, 0o644); err != nil {
		t.Fatal(err)
	}

	got := decode[historyOutput](t, arbot.Run(t, "history", "-json", "-request-file", path))
	if got.Safe != safe || got.ChainID != ethrpc.Mainnet || got.Service.URL == "" || got.Service.ObservedAt.IsZero() {
		t.Errorf("history: got Safe %s on chain %d, from %q at %s, want %s on chain 1, from the service", got.Safe, got.ChainID, got.Service.URL, got.Service.ObservedAt, safe)
	}
	if !reflect.DeepEqual(canonical(t, got.Executions), canonical(t, []executionJSON{first, third})) {
		t.Errorf("history: got executions %+v, want the first and third", got.Executions)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].SafeTxHash != misreported.SafeTxHash {
		t.Errorf("history: got rejected %+v, want the misreported transaction", got.Rejected)
	}
	// The Safe's nonce at the Safe block is 4: the unlisted and misreported
	// executions, at the nonces 1 and 3, are missing.
	if got.Nonce == nil || got.Nonce.Int64() != 4 || !reflect.DeepEqual(got.MissingNonces, []nonceRange{{1, 1}, {3, 3}}) {
		t.Errorf("history: got nonce %v with missing nonces %v, want 4 with 1 and 3 missing", got.Nonce, got.MissingNonces)
	}
	if got.ProposalExecution != nil {
		t.Errorf("history: got the execution of the proposal %+v, want none", got.ProposalExecution)
	}

	// The execution of the request's own Safe transaction is reported apart from
	// the others, here for a request whose Safe transaction is the third.
	var request map[string]any
	if err := json.Unmarshal(info, &request); err != nil {
		t.Fatal(err)
	}
	request["proposal"].(map[string]any)["safeTxHash"] = third.SafeTxHash.String()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	pathThird := filepath.Join(dir, "request-third.json")
	if err := os.WriteFile(pathThird, data, 0o644); err != nil {
		t.Fatal(err)
	}
	own := decode[historyOutput](t, arbot.Run(t, "history", "-json", "-request-file", pathThird))
	if !reflect.DeepEqual(canonical(t, own.Executions), canonical(t, []executionJSON{first})) ||
		!reflect.DeepEqual(canonical(t, own.ProposalExecution), canonical(t, third)) {
		t.Errorf("history of the third: got executions %+v and the proposal's %+v, want the first, and the third apart", own.Executions, own.ProposalExecution)
	}

	text := arbot.Run(t, "history", "-request-file", path)
	for _, want := range []string{
		`(?m)^Missing nonces: 1, 3$`,
		`(?m)^\d+ +` + first.TxHash.String() + ` +success +` + first.SafeTxHash.String() + ` +0 +` + target.String() + ` +0 +CALL +0x66697273 \(5 bytes\)$`,
		`(?m)^3 +` + misreported.SafeTxHash.String() + ` +` + misreported.TxHash.String() + ` +the transaction's fields don't hash`,
	} {
		if !regexp.MustCompile(want).Match(text) {
			t.Errorf("history: output has no line matching %q:\n%s", want, text)
		}
	}
}
