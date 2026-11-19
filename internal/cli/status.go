package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/status"
)

// statusCommand reports the status of the Safe transaction of a request:
// whether its nonce was used by the proposal, and whether and when it was
// executed, against the request's voting and arbitration.
func statusCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("status")
	asJSON := flags.Bool("json", false, "write the status as JSON")
	requestFile := flags.String("request-file", "", "read the request from the JSON file at `path`, as written by arbot info -json")
	blocks := flags.Uint64("blocks", defaultScanBlocks, "scan at most this number of blocks of the Safe's execution logs, when the Safe Transaction Service and the nonce don't settle the execution")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s status [flags] -request-file <path>\n\n", e.progname)
		fmt.Fprintf(out, "Reports the status of the Safe transaction of a request, up to the latest blocks: facts about the\n")
		fmt.Fprintf(out, "proposal, not evidence of its effects, since effects after the Safe block aren't evidence (Charter\n")
		fmt.Fprintf(out, "§ 3.6). They are reported, never classified.\n\n")
		fmt.Fprintf(out, "Nonce: the transaction's nonce against the Safe's nonce at the Safe block and at the latest block. A\n")
		fmt.Fprintf(out, "nonce below the Safe's at the Safe block is stale: the Safe had used it by the proposal, so the\n")
		fmt.Fprintf(out, "transaction could never be executed after it (Charter issue #20, which is open).\n\n")
		fmt.Fprintf(out, "Execution: whether the Safe executed the transaction, confirmed onchain, from the first of these that\n")
		fmt.Fprintf(out, "settles it:\n\n")
		fmt.Fprintf(out, "  service  the Safe Transaction Service names the onchain transaction that executed it, and its\n")
		fmt.Fprintf(out, "           receipt has the Safe's ExecutionSuccess or ExecutionFailure log of its hash\n")
		fmt.Fprintf(out, "  nonce    the Safe's nonce at the latest block is at most the transaction's, so it isn't executed\n")
		fmt.Fprintf(out, "  logs     a scan of the Safe's execution logs, of at most -blocks blocks, where it used the nonce:\n")
		fmt.Fprintf(out, "           up to the Safe block for a stale nonce, and after it otherwise, which finds this\n")
		fmt.Fprintf(out, "           transaction's execution, or the other transaction that used the nonce\n\n")
		fmt.Fprintf(out, "If none settles it, such as a scan that the node rejects, the status is unknown, with the reason.\n\n")
		fmt.Fprintf(out, "Timing: the execution's block time against the proposal, the commit and reveal deadlines, the freeze,\n")
		fmt.Fprintf(out, "and the arbitration's resolution, read again as of the latest Gnosis Chain block, and now:\n")
		fmt.Fprintf(out, "before-proposal, during-voting, after-voting (for a request that wasn't frozen), after-freeze, or\n")
		fmt.Fprintf(out, "after-resolution.\n\n")
		fmt.Fprintf(out, "Queue: the other proposals for the Safe on its chain in ARBITRATION_TIMEOUT Gnosis Chain blocks\n")
		fmt.Fprintf(out, "around the proposal, half before and half after, and whether they have the same nonce.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 0); err != nil {
		return err
	}
	if *blocks == 0 {
		return usageError("-blocks must be at least 1")
	}
	ref, err := openSafe(ctx, e, *requestFile)
	if err != nil {
		return err
	}
	request, err := readRequest(*requestFile)
	if err != nil {
		return err
	}

	dial := ethrpc.NewDialer(e.cfg.RPCs)
	gnosis := ref.eth
	if ref.ChainID != ethrpc.Gnosis {
		if gnosis, err = dial(ctx, ethrpc.Gnosis); err != nil {
			return fmt.Errorf("connecting to Gnosis Chain: %w", err)
		}
	}
	sn := openSafenet(e.cfg, dial)
	progress, err := sn.Progress(ctx, request.ID)
	if err != nil {
		return err
	}
	// The terms are set when the request is posted, so other terms mean that the
	// file is for another oracle, or was changed.
	if progress.Terms.CommitDeadline != request.Terms.CommitDeadline || progress.Terms.RevealDeadline != request.Terms.RevealDeadline {
		return fmt.Errorf("the oracle has the deadlines %d and %d for request %s, not %d and %d as in %s",
			progress.Terms.CommitDeadline, progress.Terms.RevealDeadline, request.ID, request.Terms.CommitDeadline, request.Terms.RevealDeadline, *requestFile)
	}

	checker := &status.Checker{
		Gnosis:  gnosis,
		Safe:    ref.eth,
		Service: txService(e, ref.ChainID),
		Blocks:  *blocks,
		Proposals: func(ctx context.Context, from, to uint64) ([]safenet.SafeProposal, error) {
			return sn.Proposals(ctx, ref.ChainID, ref.Safe, from, to)
		},
		Warn: func(err error) { fmt.Fprintf(e.stderr, "%s: warning: %v\n", e.progname, err) },
	}
	report, err := checker.Check(ctx, request, progress)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	return writeStatus(e, report, progress)
}

// writeStatus writes the text output of `arbot status`.
func writeStatus(e *env, r *status.Report, p *safenet.Progress) error {
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Status of request %s, %s as of Gnosis Chain block %d\n", r.Request, r.State, p.Block)
	fmt.Fprintf(w, "%s\n", status.NotEvidence)

	fmt.Fprintf(w, "\nSafe transaction\n")
	fmt.Fprintf(w, "  Safe\t%s on chain %d\n", r.Safe, r.ChainID)
	fmt.Fprintf(w, "  Safe tx hash\t%s\n", r.SafeTxHash)
	fmt.Fprintf(w, "  Safe block\t%d\n", r.SafeBlock)

	n := r.Nonce
	fmt.Fprintf(w, "\nNonce\n")
	fmt.Fprintf(w, "  Transaction\t%s\n", n.Transaction)
	fmt.Fprintf(w, "  Safe at the Safe block\t%s\n", n.SafeAtSafeBlock)
	fmt.Fprintf(w, "  Safe at the latest block\t%s (block %d)\n", n.SafeAtLatest, n.LatestBlock)
	fmt.Fprintf(w, "  Stale\t%s\n", yesNo(n.Stale))
	fmt.Fprintf(w, "  Used after the Safe block\t%s\n", yesNo(n.ConsumedAfterSafeBlock))

	x := r.Execution
	fmt.Fprintf(w, "\nExecution\n")
	fmt.Fprintf(w, "  Status\t%s\n", x.Status)
	if x.Source != "" {
		fmt.Fprintf(w, "  Source\t%s\n", x.Source)
	}
	if x.Block != nil {
		result := "failure"
		if *x.Success {
			result = "success"
		}
		fmt.Fprintf(w, "  Executed\tblock %d (%s), transaction %s, %s\n", *x.Block, x.BlockTime.Format(time.RFC3339), x.TxHash, result)
	}
	if x.SafeTxHash != nil && *x.SafeTxHash != r.SafeTxHash {
		fmt.Fprintf(w, "  Other Safe tx hash\t%s\n", x.SafeTxHash)
	}
	fmt.Fprintf(w, "  Reason\t%s\n", x.Reason)
	if s := x.Service; s != nil {
		fmt.Fprintf(w, "  Service\t%q, observed %s\n", s.URL, s.ObservedAt.Format(time.RFC3339))
	} else {
		fmt.Fprintf(w, "  Service\tnone for chain %d\n", r.ChainID)
	}
	if s := x.Scanned; s != nil {
		fmt.Fprintf(w, "  Scanned\tblocks %d to %d\n", s.FromBlock, s.ToBlock)
	}

	if t := r.Timing; t != nil {
		fmt.Fprintf(w, "\nTiming: %s\n", t.Value)
		fmt.Fprintf(w, "  Execution\tblock %d of chain %d\t%s\n", t.Execution.Block, r.ChainID, t.Execution.Time.Format(time.RFC3339))
		fmt.Fprintf(w, "  Proposal\tGnosis Chain block %d\t%s\n", t.Proposal.Block, t.Proposal.Time.Format(time.RFC3339))
		for _, d := range []struct {
			name string
			d    status.Deadline
		}{{"Commit deadline", t.CommitDeadline}, {"Reveal deadline", t.RevealDeadline}} {
			when := "not reached"
			if d.d.Reached {
				when = d.d.Time.Format(time.RFC3339)
			}
			fmt.Fprintf(w, "  %s\tGnosis Chain block %d\t%s\n", d.name, d.d.Block, when)
		}
		if f := t.Frozen; f != nil {
			fmt.Fprintf(w, "  Frozen\tGnosis Chain block %d\t%s\n", f.Block, f.Time.Format(time.RFC3339))
		} else {
			fmt.Fprintf(w, "  Frozen\tnot frozen\n")
		}
		if s := t.Resolved; s != nil {
			fmt.Fprintf(w, "  Resolved\tGnosis Chain block %d\t%s (%s)\n", s.Block, s.Time.Format(time.RFC3339), s.Outcome)
		} else {
			fmt.Fprintf(w, "  Resolved\tnot resolved\n")
		}
		fmt.Fprintf(w, "  Now\tGnosis Chain block %d\t%s\n", t.Now.Block, t.Now.Time.Format(time.RFC3339))
	}

	if q := r.Queue; q != nil {
		fmt.Fprintf(w, "\nOther proposals for the Safe in Gnosis Chain blocks %d to %d\n", q.FromBlock, q.ToBlock)
		if len(q.Proposals) == 0 {
			fmt.Fprintf(w, "  none\n")
		}
		for _, p := range q.Proposals {
			same := ""
			if p.SameNonce {
				same = "\tsame nonce"
			}
			fmt.Fprintf(w, "  %s\tblock %d\tnonce %s\tSafe tx hash %s%s\n", p.RequestID, p.Block, p.Nonce, p.SafeTxHash, same)
		}
	}

	fmt.Fprintf(w, "\nNotes\n")
	for _, note := range r.Notes[1:] {
		fmt.Fprintf(w, "  - %s\n", note)
	}
	if len(r.Notes) == 1 {
		fmt.Fprintf(w, "  none\n")
	}
	return w.Flush()
}

// yesNo formats a boolean as "yes" or "no".
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
