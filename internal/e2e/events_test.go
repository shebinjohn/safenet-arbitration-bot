package e2e_test

import (
	"bytes"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// eventsOutput is the part of the JSON output of `arbot events` that the test
// checks.
type eventsOutput struct {
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	TxHash    ethrpc.Hash    `json:"txHash"`
	Block     uint64         `json:"block"`
	BlockTime time.Time      `json:"blockTime"`
	Status    string         `json:"status"`
	From      ethrpc.Address `json:"from"`
	To        ethrpc.Address `json:"to"`
	Events    []struct {
		LogIndex uint64         `json:"logIndex"`
		Emitter  ethrpc.Address `json:"emitter"`
		Topics   []ethrpc.Hash  `json:"topics"`
		Event    *struct {
			Name      string `json:"name"`
			Signature string `json:"signature"`
			Args      []struct {
				Name  string `json:"name"`
				Type  string `json:"type"`
				Value any    `json:"value"`
			} `json:"args"`
		} `json:"event"`
	} `json:"events"`
}

// TestEvents runs `arbot events` against the fee token and a real SafeL2 on
// anvil, with a transaction before the proposal, and one after it.
func TestEvents(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	r := n.Propose(nil)
	mainnet, a := n.Mainnet, n.Artifacts
	safeL2 := ethrpc.Address{0: 0x5a, 19: 0xf2}
	owner := ethrpc.Address{0: 0x0e, 19: 1}
	alice, bob := ethrpc.Address{0: 0xa1, 19: 1}, ethrpc.Address{0: 0xb0, 19: 2}
	mainnet.SetBalance(owner, new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil))
	n.InstallFeeToken(mainnet)
	n.SetupSafe(mainnet, safeL2, a.SafeL2Singleton, owner)

	// A transfer of the fee token, which logs a Deposit and then a Transfer in
	// separate transactions.
	amount := big.NewInt(1_000_000)
	transfer := n.TransferFeeToken(mainnet, alice, bob, amount)

	// A Safe transaction that SafeL2 logs, and then executes.
	tx := safenet.SafeTransaction{
		ChainID: big.NewInt(ethrpc.Mainnet), Safe: safeL2, To: bob, Data: []byte{0x12, 0x34},
		Value: new(big.Int), SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int),
		Nonce: n.SafeNonce(mainnet, safeL2),
	}
	exec := mainnet.Transact(owner, safeL2, n.ExecCalldata(&tx, owner))

	// After the proposal.
	mainnet.MineUntil(n.Header(r.Block).Time().Add(time.Second))
	later := n.TransferFeeToken(mainnet, alice, bob, amount)

	arbot := n.Arbot()
	path := filepath.Join(t.TempDir(), "request.json")
	info := arbot.Run(t, "info", "-json", r.ID.String())
	if err := os.WriteFile(path, info, 0o644); err != nil {
		t.Fatal(err)
	}
	safeBlock := decode[requestInfo](t, info).Proposal.SafeBlock
	events := func(hash ethrpc.Hash) eventsOutput {
		t.Helper()
		got := decode[eventsOutput](t, arbot.Run(t, "events", "-json", "-request-file", path, hash.String()))
		if got.ChainID != ethrpc.Mainnet || got.SafeBlock != safeBlock || got.TxHash != hash || got.Status != "success" {
			t.Errorf("events %s: got chain %d, Safe block %d, transaction %s, status %s", hash, got.ChainID, got.SafeBlock, got.TxHash, got.Status)
		}
		if want := n.Mainnet.Header(got.Block).Time(); !got.BlockTime.Equal(want) {
			t.Errorf("events %s: got block time %s, want %s, that of block %d", hash, got.BlockTime, want, got.Block)
		}
		return got
	}

	t.Run("token transfer", func(t *testing.T) {
		got := events(transfer.TransactionHash)
		if got.Block != uint64(transfer.BlockNumber) || got.From != alice || got.To != a.FeeToken || len(got.Events) != 1 {
			t.Fatalf("events: got block %d from %s to %s with %d events, want block %d from %s to %s with 1",
				got.Block, got.From, got.To, len(got.Events), transfer.BlockNumber, alice, a.FeeToken)
		}
		event := got.Events[0]
		if event.Emitter != a.FeeToken || event.Event == nil || event.Event.Name != "Transfer" || len(event.Event.Args) != 3 {
			t.Fatalf("events: got %+v, want a Transfer of the fee token", event)
		}
		want := []any{alice.String(), bob.String(), float64(amount.Int64())}
		for i, arg := range event.Event.Args {
			// Addresses are lowercase in JSON.
			if got, ok := arg.Value.(string); ok {
				var address ethrpc.Address
				if err := address.UnmarshalText([]byte(got)); err != nil || address.String() != want[i] {
					t.Errorf("argument %s: got %v, want %v", arg.Name, arg.Value, want[i])
				}
			} else if arg.Value != want[i] {
				t.Errorf("argument %s: got %v, want %v", arg.Name, arg.Value, want[i])
			}
		}
	})

	t.Run("SafeL2 execution", func(t *testing.T) {
		got := events(exec.TransactionHash)
		if len(got.Events) != 2 {
			t.Fatalf("events: got %d events, want 2", len(got.Events))
		}
		names := []string{"SafeMultiSigTransaction", "ExecutionSuccess"}
		for i, event := range got.Events {
			if event.Emitter != safeL2 || event.Event == nil || event.Event.Name != names[i] {
				t.Fatalf("event %d: got %+v, want %s emitted by the Safe", i, event, names[i])
			}
		}
		// SafeL2 1.3.0 doesn't index the hash of the transaction that it executed.
		success := got.Events[1].Event.Args[0]
		if success.Name != "txHash" || success.Value != tx.Hash().String() {
			t.Errorf("ExecutionSuccess: got %s %v, want the hash %s of the Safe transaction", success.Name, success.Value, tx.Hash())
		}
		if to := got.Events[0].Event.Args[0]; to.Name != "to" || !equalAddress(t, to.Value, bob) {
			t.Errorf("SafeMultiSigTransaction: got %s %v, want %s", to.Name, to.Value, bob)
		}
	})

	t.Run("after the Safe block", func(t *testing.T) {
		stderr := arbot.Fail(t, 1, "events", "-request-file", path, later.TransactionHash.String())
		if want := "after the request's Safe block"; !bytes.Contains(stderr, []byte(want)) {
			t.Errorf("events: got error %q, want one containing %q", stderr, want)
		}
	})

	t.Run("unknown transaction", func(t *testing.T) {
		stderr := arbot.Fail(t, 1, "events", "-request-file", path, ethrpc.Hash{31: 1}.String())
		if !bytes.Contains(stderr, []byte("not found")) {
			t.Errorf("events: got error %q, want one saying that the transaction was not found", stderr)
		}
	})
}

// equalAddress reports whether value, an address in JSON, is address.
func equalAddress(t *testing.T, value any, address ethrpc.Address) bool {
	t.Helper()
	s, _ := value.(string)
	var got ethrpc.Address
	return got.UnmarshalText([]byte(s)) == nil && got == address
}
