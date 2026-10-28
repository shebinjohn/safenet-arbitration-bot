package cli

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"text/tabwriter"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/executions"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
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
	// Nonce is the Safe's nonce at the Safe block, which is the number of Safe
	// transactions that it executed up to it, since each execution uses the next
	// nonce, unless a delegate call changed its nonce.
	Nonce *big.Int `json:"nonce"`
	// Complete is whether the number of executions is the nonce.
	Complete bool `json:"complete"`
	// Scanned are the ranges of blocks whose logs were scanned for the executions
	// that the Safe Transaction Service doesn't list.
	Scanned []executions.BlockRange `json:"scanned"`
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
		fmt.Fprintf(out, "Safe's logs can't reach on public nodes. It lists the Safe's executed transactions from the Safe\n")
		fmt.Fprintf(out, "Transaction Service, which it needs, and only uses them to find the blocks to read: it reads the logs\n")
		fmt.Fprintf(out, "of each block that the service names, and keeps the executions that the Safe logged there, up to the\n")
		fmt.Fprintf(out, "Safe block, which it decodes as above. Each execution uses the next nonce, so the Safe's nonce at the\n")
		fmt.Fprintf(out, "Safe block is the number of executions up to it, unless a delegate call changed it. For the nonces\n")
		fmt.Fprintf(out, "that no execution found has, it scans the blocks between the executions before and after them, from\n")
		fmt.Fprintf(out, "the Safe's deployment, which it finds from the code of old blocks, or up to the Safe block, and\n")
		fmt.Fprintf(out, "reports the ranges. It says whether the history is complete: whether it found as many executions as\n")
		fmt.Fprintf(out, "the nonce, and warns if not.\n\n")
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
		Eth:  ref.eth,
		Warn: func(err error) { fmt.Fprintf(e.stderr, "%s: warning: %v\n", e.progname, err) },
	}
	baseURL, ok := e.cfg.TxServices[ref.ChainID]
	if !ok {
		baseURL = txservice.DefaultURLs[ref.ChainID]
	}
	if baseURL != "" {
		finder.Service = txservice.NewClient(baseURL, ref.ChainID)
	}
	report := executionsReport{Safe: ref.Safe, ChainID: ref.ChainID, FromBlock: fromBlock, ToBlock: ref.SafeBlock}
	var found []executions.Execution
	if *all {
		if finder.Service == nil {
			return fmt.Errorf("-all needs the Safe Transaction Service of chain %d, which the txServices setting leaves out", ref.ChainID)
		}
		history, err := finder.FindAll(ctx, ref.Safe, ethrpc.BlockNumber(ref.SafeBlock))
		if err != nil {
			return err
		}
		found = history.Executions
		report.History = &historyReport{Nonce: history.Nonce, Complete: history.Complete(), Scanned: history.Scanned}
		if !history.Complete() {
			finder.Warn(fmt.Errorf("found %d executions up to block %d, but the Safe's nonce there is %s", len(found), ref.SafeBlock, history.Nonce))
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
	for i, x := range found {
		report.Executions[i].Execution = x
		if reader == nil || x.Transaction == nil {
			continue
		}
		calls, err := reader.Calls(ctx, x.Transaction)
		if err != nil {
			return fmt.Errorf("decoding the calls of Safe transaction %s: %w", x.SafeTxHash, err)
		}
		report.Executions[i].Target, report.Executions[i].Calls = &calls.Target, calls.Calls
	}

	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	if h := report.History; h != nil {
		fmt.Fprintf(e.stdout, "Safe transactions executed by %s on chain %d, up to block %d\n\n", report.Safe, report.ChainID, report.ToBlock)
		completeness := "complete"
		if !h.Complete {
			completeness = "incomplete"
		}
		fmt.Fprintf(e.stdout, "Found %d executions, and the Safe's nonce at block %d is %s: %s.\n", len(found), report.ToBlock, h.Nonce, completeness)
		fmt.Fprintf(e.stdout, "Listed by the Safe Transaction Service, and read from the Safe's logs in the blocks that it names.\n")
		for _, r := range h.Scanned {
			fmt.Fprintf(e.stdout, "Scanned blocks %d to %d for executions that the service doesn't list.\n", r.FromBlock, r.ToBlock)
		}
		fmt.Fprintln(e.stdout)
	} else {
		fmt.Fprintf(e.stdout, "Safe transactions executed by %s on chain %d, in blocks %d to %d\n\n", report.Safe, report.ChainID, report.FromBlock, report.ToBlock)
	}
	if len(found) == 0 {
		_, err := fmt.Fprintln(e.stdout, "none")
		return err
	}
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
	if err := w.Flush(); err != nil || reader == nil {
		return err
	}

	fmt.Fprintf(e.stdout, "\nCalls\n\n")
	w = tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NONCE\t#\tKIND\tTO\tACCOUNT\tOPERATION\tVALUE\tFUNCTION")
	for _, x := range report.Executions {
		for i, c := range x.Calls {
			fmt.Fprintf(w, "%s\t%d\t%s\n", x.Transaction.Nonce, i+1, callText(c))
		}
	}
	return w.Flush()
}

// summarizeData returns the selector and length of call data, such as
// "0xa9059cbb (68 bytes)", or "-" if there is none.
func summarizeData(data []byte) string {
	if len(data) == 0 {
		return "-"
	}
	return fmt.Sprintf("%s (%d bytes)", ethrpc.Bytes(data[:min(len(data), 4)]), len(data))
}
