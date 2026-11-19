package cli

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"text/tabwriter"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/executions"
)

// executionsReport is the JSON output of `arbot executions`.
type executionsReport struct {
	Safe    ethrpc.Address `json:"safe"`
	ChainID uint64         `json:"chainId"`
	// FromBlock and ToBlock are the blocks searched, both included. ToBlock is the
	// request's Safe block. FromBlock is 0 with -all.
	FromBlock  uint64      `json:"fromBlock"`
	ToBlock    uint64      `json:"toBlock"`
	Executions []execution `json:"executions"`
	// History is how the executions were found with -all, and is left out without
	// it.
	History *historyReport `json:"history,omitempty"`
}

// historyReport is how `arbot executions -all` found the executions.
type historyReport struct {
	// Service is the Safe Transaction Service that listed the executions, an
	// offchain source (§ 3.4), and when it was observed.
	Service serviceSource `json:"service"`
	// Nonce is the Safe's nonce at the Safe block. Every execution uses the next
	// nonce, so it is the number of Safe transactions that the Safe executed up to
	// it, unless a delegate call changed it.
	Nonce *big.Int `json:"nonce"`
	// Complete is whether the executions account for every nonce below Nonce, and
	// Missing are the ranges of those that they don't, or null if the nonce is too
	// large to list them.
	Complete bool                    `json:"complete"`
	Missing  []executions.NonceRange `json:"missingNonces"`
	// Scanned are the ranges of blocks whose logs were scanned for the executions
	// that the service doesn't list.
	Scanned []executions.BlockRange `json:"scanned"`
	// Rejected are the executions that the service lists but the chain doesn't
	// confirm.
	Rejected []executions.Rejected `json:"rejected"`
	// Creation is the creation of the Safe's account, if the service has it and the
	// chain confirms it, or null.
	Creation *executions.Creation `json:"creation"`
	// ProposalExecution is the execution of the request's own Safe transaction, if
	// it was executed by the Safe block, or null. It isn't in the executions, since
	// the transaction's own effects aren't evidence (§ 3.6).
	ProposalExecution *execution `json:"proposalExecution"`
}

// serviceSource is a Safe Transaction Service that a command read, and when.
type serviceSource struct {
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
}

// execution is an execution in the output of `arbot executions`.
type execution struct {
	executions.Execution
	// Target and Calls are the account that the transaction's own call is to, and
	// the calls that the transaction makes, as arbot calls reports them, with
	// -calls. They are left out without -calls, and for an unknown transaction.
	Target *checks.Account `json:"target,omitempty"`
	Calls  []checks.Call   `json:"calls,omitempty"`
}

// executionsCommand lists the Safe transactions that the Safe of a request
// executed before its proposal.
func executionsCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("executions")
	asJSON := flags.Bool("json", false, "write the executions as JSON")
	requestFile := flags.String("request-file", "", "read the Safe, its chain, and the block from the request in the JSON file at `path`, as written by arbot info -json")
	blocks := flags.Uint64("blocks", defaultScanBlocks, "search this number of blocks up to the request's Safe block")
	withCalls := flags.Bool("calls", false, "also list the calls that each transaction makes, as arbot calls lists them")
	all := flags.Bool("all", false, "list every execution up to the request's Safe block, found with the Safe Transaction Service, instead of searching the last -blocks blocks")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s executions [flags] -request-file <path>\n\n", e.progname)
		fmt.Fprintf(out, "Lists the Safe transactions that the Safe of a request executed, from the Safe's chain, as of the\n")
		fmt.Fprintf(out, "request's Safe block: the ExecutionSuccess and ExecutionFailure events of the Safe in the last -blocks\n")
		fmt.Fprintf(out, "blocks up to it, without the transaction's own effects or anything later. It searches that range\n")
		fmt.Fprintf(out, "only, so an execution before it isn't listed. Executions by modules aren't listed.\n\n")
		fmt.Fprintf(out, "The events only have the hash of each Safe transaction, so the command decodes the transaction from\n")
		fmt.Fprintf(out, "the first source that has one with that hash, and reports the source:\n\n")
		fmt.Fprintf(out, "  event     the SafeMultiSigTransaction event that SafeL2 logs\n")
		fmt.Fprintf(out, "  calldata  the calldata of the onchain transaction, if it calls execTransaction on the Safe\n")
		fmt.Fprintf(out, "  trace     a trace of the onchain transaction, which needs a node with debug_traceTransaction\n")
		fmt.Fprintf(out, "  service   the Safe Transaction Service (see the txServices setting, where an empty URL leaves it\n")
		fmt.Fprintf(out, "            out), an offchain source\n\n")
		fmt.Fprintf(out, "A transaction is only reported if its EIP-712 hash is the one that the Safe logged, whichever the\n")
		fmt.Fprintf(out, "source. The calldata and traces have no nonce, so it is worked out from the Safe's nonce at the last\n")
		fmt.Fprintf(out, "block. If no source has the transaction, it is an unknown transaction, or null with -json.\n")
		fmt.Fprintf(out, "The text output shows the selector and length of the data, and -json has all of it.\n\n")
		fmt.Fprintf(out, "With -calls, it also lists the calls that each known transaction makes, as arbot calls lists those\n")
		fmt.Fprintf(out, "of a request's transaction, with the accounts that they are to as of the request's Safe block: delegate\n")
		fmt.Fprintf(out, "calls to MultiSend contracts decoded into their calls, and each call's data decoded as a well-known\n")
		fmt.Fprintf(out, "function, if it is one, such as an ERC-20 transfer, or a Safe's execTransaction and the call it makes.\n")
		fmt.Fprintf(out, "The refund call is the largest refund that a transaction allows, and the payment is what it paid. The\n")
		fmt.Fprintf(out, "calls of a failed execution had no effect.\n\n")
		fmt.Fprintf(out, "With -all, it lists every execution up to the request's Safe block instead, which a scan of the\n")
		fmt.Fprintf(out, "Safe's logs can't reach on public nodes. It needs the Safe Transaction Service, an offchain source\n")
		fmt.Fprintf(out, "that it reports with its URL and when it was observed, and lists the Safe's executed transactions\n")
		fmt.Fprintf(out, "from it. It reads the Safe's logs in each block that the service names, and keeps the executions that\n")
		fmt.Fprintf(out, "the Safe logged there, whether the service lists them rightly or not, which it decodes as above, taking\n")
		fmt.Fprintf(out, "a transaction that the service lists with its hash from the listing. A block whose logs have no\n")
		fmt.Fprintf(out, "execution of the Safe is read again once. An execution that the service lists is confirmed only if\n")
		fmt.Fprintf(out, "its fields hash to its Safe transaction hash, and the Safe logged the execution of that hash in the\n")
		fmt.Fprintf(out, "block and transaction that the service reports. Any other is rejected, with the reason.\n\n")
		fmt.Fprintf(out, "Every execution uses the next nonce, so the Safe's nonce at the Safe block is the number of executions\n")
		fmt.Fprintf(out, "up to it, unless a delegate call changed it. For the nonces that no execution found used, it scans the\n")
		fmt.Fprintf(out, "blocks between the executions before and after them, from the Safe's creation, or up to the Safe\n")
		fmt.Fprintf(out, "block, and reports the ranges, then the nonces still missing. A scan that fails, such as one that a\n")
		fmt.Fprintf(out, "public node times out, is a warning, and leaves its nonces missing. The history is complete if none\n")
		fmt.Fprintf(out, "are. Executions by modules don't use nonces, and the service doesn't list them, so they aren't listed.\n\n")
		fmt.Fprintf(out, "It also reports the block and transaction that created the Safe's account, if the service has them\n")
		fmt.Fprintf(out, "and the chain confirms them, which needs a node that serves the code at old blocks, and the execution\n")
		fmt.Fprintf(out, "of the request's own Safe transaction, if it was executed by the Safe block, apart from the others,\n")
		fmt.Fprintf(out, "since its effects aren't evidence (§ 3.6).\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 0); err != nil {
		return err
	}
	blocksSet := false
	flags.Visit(func(f *flag.Flag) { blocksSet = blocksSet || f.Name == "blocks" })
	if *all && blocksSet {
		return usageError("-blocks doesn't apply with -all, which searches every block")
	}
	var ref *safeRef
	var fromBlock uint64
	if *all {
		safe, err := openSafe(ctx, e, *requestFile)
		if err != nil {
			return err
		}
		ref = safe
	} else {
		scan, err := openScan(ctx, e, *requestFile, *blocks)
		if err != nil {
			return err
		}
		ref, fromBlock = scan.safeRef, scan.FromBlock
	}

	finder := &executions.Finder{
		Eth:     ref.eth,
		Service: txService(e, ref.ChainID),
		Warn:    func(err error) { fmt.Fprintf(e.stderr, "%s: warning: %v\n", e.progname, err) },
	}
	report := executionsReport{Safe: ref.Safe, ChainID: ref.ChainID, FromBlock: fromBlock, ToBlock: ref.SafeBlock}
	var found []executions.Execution
	var proposal *executions.Execution
	if *all {
		if finder.Service == nil {
			return errNoTxService("-all", ref.ChainID)
		}
		observed := time.Now().UTC().Truncate(time.Second)
		history, err := finder.FindAll(ctx, ref.Safe, ethrpc.BlockNumber(ref.SafeBlock))
		if err != nil {
			return err
		}
		report.History = &historyReport{
			Service: serviceSource{URL: finder.Service.URL(), ObservedAt: observed},
			Nonce:   history.Nonce, Complete: history.Complete(), Missing: history.Missing,
			Scanned: history.Scanned, Rejected: history.Rejected, Creation: history.Creation,
		}
		for _, x := range history.Executions {
			if x.SafeTxHash == ref.SafeTxHash {
				proposal = &x
				continue
			}
			found = append(found, x)
		}
		if !history.Complete() {
			finder.Warn(fmt.Errorf("the executions found don't account for every nonce below the Safe's nonce %s at block %d", history.Nonce, ref.SafeBlock))
		}
	} else {
		var err error
		if found, err = finder.Find(ctx, ref.Safe, ethrpc.BlockNumber(fromBlock), ethrpc.BlockNumber(ref.SafeBlock)); err != nil {
			return err
		}
	}
	report.Executions = make([]execution, len(found))
	var reader *checks.CallReader
	if *withCalls {
		reader = checks.NewCallReader(ref.eth, ref.Safe, ethrpc.BlockNumber(ref.SafeBlock))
	}
	withCallsOf := func(x executions.Execution) (execution, error) {
		item := execution{Execution: x}
		if reader == nil || x.Transaction == nil {
			return item, nil
		}
		calls, err := reader.Calls(ctx, x.Transaction)
		if err != nil {
			return item, fmt.Errorf("decoding the calls of Safe transaction %s: %w", x.SafeTxHash, err)
		}
		item.Target, item.Calls = &calls.Target, calls.Calls
		return item, nil
	}
	for i, x := range found {
		var err error
		if report.Executions[i], err = withCallsOf(x); err != nil {
			return err
		}
	}
	if proposal != nil {
		item, err := withCallsOf(*proposal)
		if err != nil {
			return err
		}
		report.History.ProposalExecution = &item
	}

	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	if report.History != nil {
		writeHistory(e, &report)
	} else {
		fmt.Fprintf(e.stdout, "Safe transactions executed by %s on chain %d, in blocks %d to %d\n\n", report.Safe, report.ChainID, report.FromBlock, report.ToBlock)
	}
	if len(found) == 0 {
		fmt.Fprintln(e.stdout, "none")
	} else if err := writeExecutions(e, found); err != nil {
		return err
	}
	if h := report.History; h != nil && len(h.Rejected) > 0 {
		fmt.Fprintf(e.stdout, "\nRejected\n\n")
		w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NONCE\tSAFE TX HASH\tTRANSACTION\tREASON")
		for _, r := range h.Rejected {
			nonce := "-"
			if r.Nonce != nil {
				nonce = r.Nonce.String()
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", nonce, r.SafeTxHash, r.TxHash, r.Reason)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	if reader == nil || len(found) == 0 {
		return nil
	}

	fmt.Fprintf(e.stdout, "\nCalls\n\n")
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NONCE\t#\tKIND\tTO\tACCOUNT\tOPERATION\tVALUE\tFUNCTION")
	for _, x := range report.Executions {
		for i, c := range x.Calls {
			fmt.Fprintf(w, "%s\t%d\t%s\n", x.Transaction.Nonce, i+1, callText(c))
		}
	}
	return w.Flush()
}

// writeHistory writes the header of the text output of `arbot executions -all`:
// the service, the nonce, the missing nonces, the scans, the creation, and the
// execution of the request's own Safe transaction.
func writeHistory(e *env, report *executionsReport) {
	h := report.History
	fmt.Fprintf(e.stdout, "Safe transactions executed by %s on chain %d, up to block %d\n", report.Safe, report.ChainID, report.ToBlock)
	fmt.Fprintf(e.stdout, "Listed by the Safe Transaction Service %q, observed %s, and confirmed by the Safe's logs\n\n", h.Service.URL, h.Service.ObservedAt.Format(time.RFC3339))
	completeness := "complete"
	if !h.Complete {
		completeness = "incomplete"
	}
	fmt.Fprintf(e.stdout, "Nonce at block %d: %s (%s)\n", report.ToBlock, h.Nonce, completeness)
	if h.Missing != nil {
		fmt.Fprintf(e.stdout, "Missing nonces: %s\n", nonceRanges(h.Missing))
	}
	for _, r := range h.Scanned {
		fmt.Fprintf(e.stdout, "Scanned blocks %d to %d for executions that the service doesn't list\n", r.FromBlock, r.ToBlock)
	}
	if c := h.Creation; c != nil {
		factory := "no ProxyCreation event"
		if c.Factory != nil {
			factory = "ProxyCreation event of " + c.Factory.String()
		}
		fmt.Fprintf(e.stdout, "Created in block %d, transaction %s (%s)\n", c.Block, c.TxHash, factory)
	} else {
		fmt.Fprintf(e.stdout, "Creation: unknown\n")
	}
	if x := h.ProposalExecution; x != nil {
		fmt.Fprintf(e.stdout, "The request's own Safe transaction was executed in block %d, transaction %s (not listed below)\n", x.Block, x.TxHash)
	}
	fmt.Fprintln(e.stdout)
}

// writeExecutions writes a table of executions.
func writeExecutions(e *env, found []executions.Execution) error {
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BLOCK\tTRANSACTION\tRESULT\tSAFE TX HASH\tSOURCE\tNONCE\tTO\tVALUE\tOPERATION\tDATA")
	for _, x := range found {
		result := "failure"
		if x.Success {
			result = "success"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t", x.Block, x.TxHash, result, x.SafeTxHash)
		if tx := x.Transaction; tx != nil {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", x.Source, tx.Nonce, tx.To, tx.Value, tx.Operation, summarizeData(tx.Data))
		} else {
			fmt.Fprintf(w, "-\tunknown transaction\n")
		}
	}
	return w.Flush()
}

// nonceRanges formats ranges of nonces, such as "0-3, 7", or "none".
func nonceRanges(ranges []executions.NonceRange) string {
	if len(ranges) == 0 {
		return "none"
	}
	s := ""
	for i, r := range ranges {
		if i > 0 {
			s += ", "
		}
		if r.From == r.To {
			s += fmt.Sprint(r.From)
		} else {
			s += fmt.Sprintf("%d-%d", r.From, r.To)
		}
	}
	return s
}

// summarizeData returns the selector and length of call data, such as
// "0xa9059cbb (68 bytes)", or "-" if there is none.
func summarizeData(data []byte) string {
	if len(data) == 0 {
		return "-"
	}
	return fmt.Sprintf("%s (%d bytes)", ethrpc.Bytes(data[:min(len(data), 4)]), len(data))
}
