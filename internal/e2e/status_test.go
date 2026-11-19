package e2e_test

import (
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/status"
)

// TestStatus runs `arbot status` against real Safe 1.3.0 contracts on anvil,
// for requests whose Safe transactions were executed on Ethereum Mainnet a
// second after their proposal on Gnosis Chain, during the voting, as for
// request cbfadf6b9595, and two seconds before it, in the Safe block, as for
// request a049284bbb54, and for two requests for a nonce that the Safe hasn't
// used yet.
func TestStatus(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	mainnet, a := n.Mainnet, n.Artifacts
	// The Mainnet node is mined up to the Gnosis Chain node's time once, here, and
	// Gnosis Chain is then a few minutes ahead, so that the Mainnet blocks of the
	// test can be placed at any time around the proposals.
	arbot := n.Arbot()
	n.Mine(40)
	owner := ethrpc.Address{0: 0x0e, 19: 1}
	target := ethrpc.Address{0: 0xa1, 19: 1}
	mainnet.SetBalance(owner, new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil))

	// safeTx sets the fields of a Safe transaction of the Safe on Mainnet that its
	// owner can execute, a call to the target with data.
	safeTx := func(nonce int64, data string) func(tx *safenet.SafeTransaction) {
		return func(tx *safenet.SafeTransaction) {
			tx.ChainID, tx.To, tx.Value, tx.Data, tx.Operation = big.NewInt(ethrpc.Mainnet), target, new(big.Int), []byte(data), safenet.OperationCall
			tx.SafeTxGas, tx.BaseGas, tx.GasPrice = new(big.Int), new(big.Int), new(big.Int)
			tx.GasToken, tx.RefundReceiver, tx.Nonce = ethrpc.Address{}, ethrpc.Address{}, big.NewInt(nonce)
		}
	}
	// execAt has the owner execute tx in a Mainnet block at time t.
	execAt := func(tx *safenet.SafeTransaction, t time.Time) *e2e.Receipt {
		mainnet.RPC(nil, "evm_setNextBlockTimestamp", ethrpc.Quantity(t.Unix()))
		return mainnet.Transact(owner, tx.Safe, n.ExecCalldata(tx, owner))
	}
	dir := t.TempDir()
	requestFile := func(r *e2e.Request) string {
		path := filepath.Join(dir, r.ID.String()+".json")
		if err := os.WriteFile(path, arbot.Run(t, "info", "-json", r.ID.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	statusOf := func(arbot *e2e.Arbot, path string) status.Report {
		t.Helper()
		return decode[status.Report](t, arbot.Run(t, "status", "-json", "-request-file", path))
	}

	// The first request's transaction is executed a second after its proposal, and
	// a split vote freezes it later.
	during := n.ProposeTransaction(safeTx(0, "during"))
	proposed := n.Header(during.Block).Time()
	n.SetupSafe(mainnet, during.Transaction.Safe, a.SafeSingleton, owner)
	executed := execAt(during.Transaction, proposed.Add(time.Second))
	n.Vote(during.Approve(a.Sentinels[0]), during.Deny(a.Sentinels[1], "R-4.1"))
	if during.FrozenBlock == 0 {
		t.Fatal("the split vote didn't freeze the first request")
	}
	duringPath := requestFile(during)

	t.Run("executed during the voting", func(t *testing.T) {
		got := statusOf(arbot, duringPath)
		x, timing := got.Execution, got.Timing
		if got.Request != during.ID || got.State != safenet.StateFrozen || got.SafeTxHash != during.SafeTxHash || got.ChainID != ethrpc.Mainnet {
			t.Errorf("status: got request %s, %s, Safe transaction %s on chain %d, want the first, frozen", got.Request, got.State, got.SafeTxHash, got.ChainID)
		}
		if n := got.Nonce; n.Transaction.Int64() != 0 || n.SafeAtSafeBlock.Int64() != 0 || n.SafeAtLatest.Int64() != 1 || n.Stale || !n.ConsumedAfterSafeBlock {
			t.Errorf("status: got nonce %+v, want 0, used after the Safe block", n)
		}
		if x.Status != status.Executed || x.Source != status.SourceLogs || x.Block == nil || *x.Block != uint64(executed.BlockNumber) ||
			*x.TxHash != executed.TransactionHash || !x.BlockTime.Equal(proposed.Add(time.Second)) || !*x.Success || x.Service != nil {
			t.Errorf("status: got execution %+v, want the one in block %d, from the logs", x, executed.BlockNumber)
		}
		if timing == nil || timing.Value != status.DuringVoting || timing.Proposal.Block != during.Block || !timing.Proposal.Time.Equal(proposed) ||
			timing.Frozen == nil || timing.Frozen.Block != during.FrozenBlock || !timing.Frozen.Time.Equal(n.Header(during.FrozenBlock).Time()) ||
			!timing.CommitDeadline.Reached || timing.CommitDeadline.Block != during.CommitDeadline || timing.Resolved != nil {
			t.Errorf("status: got timing %+v, want during the voting, before the freeze in block %d", timing, during.FrozenBlock)
		}
		if got.Queue == nil || len(got.Queue.Proposals) != 0 || got.Notes[0] != status.NotEvidence {
			t.Errorf("status: got queue %+v and notes %q, want no other proposals, and the note that effects aren't evidence", got.Queue, got.Notes)
		}
	})

	t.Run("executed during the voting, from the service", func(t *testing.T) {
		service := e2e.NewTxService(t)
		service.ListExecutions(e2e.TxServiceExecution{
			Transaction: during.Transaction, SafeTxHash: during.SafeTxHash, TxHash: executed.TransactionHash, Block: uint64(executed.BlockNumber),
		})
		got := statusOf(arbot.WithTxService(t, ethrpc.Mainnet, service.URL), duringPath)
		if x := got.Execution; x.Status != status.Executed || x.Source != status.SourceService || x.Block == nil || *x.Block != uint64(executed.BlockNumber) ||
			x.Service == nil || x.Service.URL != service.URL || x.Scanned != nil {
			t.Errorf("status: got execution %+v, want the one in block %d, from the service, without a scan", x, executed.BlockNumber)
		}
	})

	t.Run("text", func(t *testing.T) {
		text := arbot.Run(t, "status", "-request-file", duringPath)
		for _, want := range []string{
			`(?m)^` + regexp.QuoteMeta(status.NotEvidence) + `$`,
			`(?m)^  Status +executed$`,
			`(?m)^  Executed +block \d+ \(` + proposed.Add(time.Second).Format(time.RFC3339) + `\), transaction ` + executed.TransactionHash.String() + `, success$`,
			`(?m)^Timing: during-voting$`,
		} {
			if !regexp.MustCompile(want).Match(text) {
				t.Errorf("status: output has no line matching %q:\n%s", want, text)
			}
		}
	})

	// The second request's transaction is executed two seconds before its proposal,
	// in the last Mainnet block before it, which is its Safe block.
	second := &safenet.SafeTransaction{Safe: during.Transaction.Safe}
	safeTx(1, "before")(second)
	latest, err := n.BlockNumber(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	next := n.Header(uint64(latest)).Time().Add(5 * time.Second)
	before := execAt(second, next.Add(-2*time.Second))
	n.RPC(nil, "evm_setNextBlockTimestamp", ethrpc.Quantity(next.Unix()))
	early := n.ProposeTransaction(safeTx(1, "before"))
	if !n.Header(early.Block).Time().Equal(next) || early.SafeTxHash != second.Hash() {
		t.Fatalf("the second proposal is at %s for %s, want %s for %s", n.Header(early.Block).Time(), early.SafeTxHash, next, second.Hash())
	}
	mainnet.Mine(1)

	t.Run("executed before the proposal", func(t *testing.T) {
		got := statusOf(arbot, requestFile(early))
		x := got.Execution
		if got.SafeBlock != uint64(before.BlockNumber) {
			t.Errorf("status: got Safe block %d, want the block of the execution, %d", got.SafeBlock, before.BlockNumber)
		}
		if n := got.Nonce; n.Transaction.Int64() != 1 || n.SafeAtSafeBlock.Int64() != 2 || !n.Stale || n.ConsumedAfterSafeBlock {
			t.Errorf("status: got nonce %+v, want 1, below the Safe's 2 at the Safe block, stale", n)
		}
		if x.Status != status.Executed || x.Source != status.SourceLogs || x.Block == nil || *x.Block != uint64(before.BlockNumber) ||
			!x.BlockTime.Equal(next.Add(-2*time.Second)) || x.Scanned == nil || x.Scanned.ToBlock != got.SafeBlock {
			t.Errorf("status: got execution %+v, want the one in block %d, from a scan up to the Safe block", x, before.BlockNumber)
		}
		if got.Timing == nil || got.Timing.Value != status.BeforeProposal || got.Timing.Frozen != nil || got.Timing.RevealDeadline.Reached {
			t.Errorf("status: got timing %+v, want before the proposal, which isn't frozen, and whose reveal deadline isn't reached", got.Timing)
		}
		if q := got.Queue; q == nil || len(q.Proposals) != 1 || q.Proposals[0].RequestID != during.ID || q.Proposals[0].SameNonce {
			t.Errorf("status: got queue %+v, want the first request, with another nonce", q)
		}
	})

	// Two requests for the same nonce, which the Safe hasn't used.
	unused := n.ProposeTransaction(safeTx(5, "unused"))
	rival := n.ProposeTransaction(safeTx(5, "rival"))
	mainnet.MineUntil(n.Header(rival.Block).Time())

	t.Run("not executed", func(t *testing.T) {
		got := statusOf(arbot, requestFile(unused))
		if x := got.Execution; x.Status != status.NotExecuted || x.Source != status.SourceNonce || x.Block != nil || got.Timing != nil {
			t.Errorf("status: got execution %+v and timing %+v, want not executed, from the nonce, without a timing", x, got.Timing)
		}
		if n := got.Nonce; n.Stale || n.ConsumedAfterSafeBlock || n.SafeAtLatest.Int64() != 2 {
			t.Errorf("status: got nonce %+v, want 5, unused, with the Safe's at 2", n)
		}
		q := got.Queue
		if q == nil {
			t.Fatal("status: got no queue")
		}
		i := slices.IndexFunc(q.Proposals, func(p status.Proposal) bool { return p.RequestID == rival.ID })
		if len(q.Proposals) != 3 || i < 0 || !q.Proposals[i].SameNonce {
			t.Errorf("status: got queue %+v, want the three others, with the rival for the same nonce", q)
		}
	})
}
