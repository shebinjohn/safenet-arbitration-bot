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
	"github.com/safe-research/safenet-arbitration-bot/internal/executions"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// historyOutput is the JSON output of `arbot executions -all`.
type historyOutput struct {
	executionsOutput
	History struct {
		Service struct {
			URL        string    `json:"url"`
			ObservedAt time.Time `json:"observedAt"`
		} `json:"service"`
		Nonce             *big.Int                `json:"nonce"`
		Complete          bool                    `json:"complete"`
		MissingNonces     []executions.NonceRange `json:"missingNonces"`
		Scanned           []executions.BlockRange `json:"scanned"`
		Rejected          []executions.Rejected   `json:"rejected"`
		Creation          *executions.Creation    `json:"creation"`
		ProposalExecution *executionJSON          `json:"proposalExecution"`
	} `json:"history"`
}

// TestExecutionsAll runs `arbot executions -all` against real Safe 1.3.0
// contracts on anvil, with a Safe Transaction Service that lists the Safe's
// executions, one of them wrongly, and leaves one out.
func TestExecutionsAll(t *testing.T) {
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
	// returns what arbot should report about it, from source, and what the service
	// lists.
	exec := func(data, source string) (executionJSON, e2e.TxServiceExecution) {
		t.Helper()
		tx := safenet.SafeTransaction{
			ChainID: big.NewInt(ethrpc.Mainnet), Safe: safe, To: target, Value: new(big.Int), Data: []byte(data),
			SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: n.SafeNonce(mainnet, safe),
		}
		receipt := mainnet.Transact(owner, safe, n.ExecCalldata(&tx, owner))
		x := executionJSON{
			Block: uint64(receipt.BlockNumber), TxHash: receipt.TransactionHash, SafeTxHash: tx.Hash(),
			Success: true, Payment: new(big.Int), Source: source, Transaction: &tx,
		}
		return x, e2e.TxServiceExecution{Transaction: &tx, SafeTxHash: tx.Hash(), TxHash: receipt.TransactionHash, Block: x.Block}
	}
	first, listedFirst := exec("first", "service")
	unlisted, _ := exec("unlisted", "calldata")
	third, listedThird := exec("third", "service")
	misreported, listedMisreported := exec("misreported", "calldata")
	// The service reports a transaction that isn't the one executed, with the hash
	// of the one executed, so the transaction is decoded from the chain.
	listedMisreported.Transaction = &safenet.SafeTransaction{
		ChainID: big.NewInt(ethrpc.Mainnet), Safe: safe, To: target, Value: big.NewInt(1), Data: []byte("other"),
		SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: misreported.Transaction.Nonce,
	}
	// An execution after the proposal, which the evidence must not include.
	mainnet.MineUntil(n.Header(r.Block).Time())
	_, listedLater := exec("later", "service")
	service := e2e.NewTxService(t)
	service.ListExecutions(listedFirst, listedThird, listedMisreported, listedLater)

	arbot := n.Arbot().WithTxService(t, ethrpc.Mainnet, service.URL)
	dir := t.TempDir()
	path := filepath.Join(dir, "request.json")
	info := arbot.Run(t, "info", "-json", r.ID.String())
	if err := os.WriteFile(path, info, 0o644); err != nil {
		t.Fatal(err)
	}

	got := decode[historyOutput](t, arbot.Run(t, "executions", "-all", "-json", "-request-file", path))
	h := got.History
	if got.Safe != safe || got.ChainID != ethrpc.Mainnet || h.Service.URL != service.URL || h.Service.ObservedAt.IsZero() {
		t.Errorf("executions -all: got Safe %s on chain %d, from %q at %s, want %s on chain 1, from the service", got.Safe, got.ChainID, h.Service.URL, h.Service.ObservedAt, safe)
	}
	// The unlisted execution is found by scanning the blocks between the first and
	// the third, and the misreported one in the block that the service names, but
	// as its nonce is unknown until it is decoded, the blocks after the third are
	// scanned too.
	if want := []executionJSON{first, unlisted, third, misreported}; !reflect.DeepEqual(canonical(t, got.Executions), canonical(t, want)) {
		g, _ := json.MarshalIndent(got.Executions, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("executions -all:\ngot  %s\nwant %s", g, w)
	}
	if len(h.Rejected) != 1 || h.Rejected[0].SafeTxHash != misreported.SafeTxHash {
		t.Errorf("executions -all: got rejected %+v, want the misreported transaction", h.Rejected)
	}
	scanned := []executions.BlockRange{{FromBlock: first.Block, ToBlock: third.Block}, {FromBlock: third.Block, ToBlock: got.ToBlock}}
	if h.Nonce == nil || h.Nonce.Int64() != 4 || !h.Complete || len(h.MissingNonces) != 0 || !reflect.DeepEqual(h.Scanned, scanned) {
		t.Errorf("executions -all: got nonce %v with missing nonces %v and blocks %v scanned, want 4, complete, with %v scanned", h.Nonce, h.MissingNonces, h.Scanned, scanned)
	}
	// The Safe's account was set up without a transaction, so it has no creation.
	if h.ProposalExecution != nil || h.Creation != nil {
		t.Errorf("executions -all: got the execution of the proposal %+v and the creation %+v, want neither", h.ProposalExecution, h.Creation)
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
	own := decode[historyOutput](t, arbot.Run(t, "executions", "-all", "-json", "-request-file", pathThird))
	if !reflect.DeepEqual(canonical(t, own.Executions), canonical(t, []executionJSON{first, unlisted, misreported})) ||
		!reflect.DeepEqual(canonical(t, own.History.ProposalExecution), canonical(t, third)) || !own.History.Complete {
		t.Errorf("executions -all of the third: got executions %+v and the proposal's %+v, want the others, and the third apart", own.Executions, own.History.ProposalExecution)
	}

	text := arbot.Run(t, "executions", "-all", "-request-file", path)
	for _, want := range []string{
		`(?m)^Missing nonces: none$`,
		`(?m)^\d+ +\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ +` + first.TxHash.String() + ` +success +` + first.SafeTxHash.String() + ` +service +0 +` + target.String() + ` +0 +CALL +0x66697273 \(5 bytes\)$`,
		`(?m)^3 +` + misreported.SafeTxHash.String() + ` +` + misreported.TxHash.String() + ` +the transaction's fields don't hash`,
	} {
		if !regexp.MustCompile(want).Match(text) {
			t.Errorf("executions -all: output has no line matching %q:\n%s", want, text)
		}
	}
}
