package cli

import (
	"context"
	"fmt"
	"math/big"
	"text/tabwriter"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/executions"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// historyReport is the JSON output of `arbot history`.
type historyReport struct {
	Safe      ethrpc.Address `json:"safe"`
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	// Service is the Safe Transaction Service that the executions are from, an
	// offchain source (§ 3.4), and when it was observed.
	Service serviceSource `json:"service"`
	// Nonce is the Safe's nonce at the Safe block, and Missing the ranges of nonces
	// below it that no listed execution used. Both are null if the nonce couldn't
	// be read.
	Nonce   *big.Int                `json:"nonce"`
	Missing []executions.NonceRange `json:"missingNonces"`
	// ProposalExecution is the execution of the request's own Safe transaction, if
	// it was executed by the Safe block. It isn't in Executions, since the
	// transaction's own effects aren't evidence (§ 3.6).
	ProposalExecution *execution            `json:"proposalExecution"`
	Executions        []execution           `json:"executions"`
	Rejected          []executions.Rejected `json:"rejected"`
}

// serviceSource is the Safe Transaction Service that `arbot history` read.
type serviceSource struct {
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
}

// historyCommand lists the Safe transactions that the Safe of a request
// executed before its proposal, as the Safe Transaction Service lists them.
func historyCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("history")
	asJSON := flags.Bool("json", false, "write the history as JSON")
	requestFile := flags.String("request-file", "", "read the Safe, its chain, and the block from the request in the JSON file at `path`, as written by arbot info -json")
	withCalls := flags.Bool("calls", false, "also list the calls that each transaction makes, as arbot calls lists them")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s history [flags] -request-file <path>\n\n", e.progname)
		fmt.Fprintf(out, "Lists the Safe transactions that the Safe of a request executed up to the request's Safe block, as\n")
		fmt.Fprintf(out, "the Safe Transaction Service lists them (see the txServices setting), an offchain source that it\n")
		fmt.Fprintf(out, "reports with its URL and when it was observed. Unlike arbot executions, it doesn't scan the chain's\n")
		fmt.Fprintf(out, "logs, so it covers the Safe's whole history quickly.\n\n")
		fmt.Fprintf(out, "It lists an execution only if the chain confirms it: the transaction's fields hash to the Safe\n")
		fmt.Fprintf(out, "transaction hash that the service reports, and the onchain transaction that the service reports\n")
		fmt.Fprintf(out, "logged the Safe's execution of that hash, in a block up to the Safe block. The block, result, and\n")
		fmt.Fprintf(out, "payment are those of the Safe's log. An execution that the service lists but the chain doesn't\n")
		fmt.Fprintf(out, "confirm is rejected, with the reason. Executions after the Safe block aren't listed.\n\n")
		fmt.Fprintf(out, "Every execution uses one nonce, so the command reads the Safe's nonce at the Safe block, and reports\n")
		fmt.Fprintf(out, "the nonces below it that no listed execution used: the executions that the service doesn't have, or\n")
		fmt.Fprintf(out, "has wrongly. None missing means the history of the Safe's own executions is complete. Executions by\n")
		fmt.Fprintf(out, "modules don't use nonces, and the service doesn't list them.\n\n")
		fmt.Fprintf(out, "The execution of the request's own Safe transaction, if it was executed by the Safe block, is reported\n")
		fmt.Fprintf(out, "apart from the others, since its effects aren't evidence (§ 3.6).\n\n")
		fmt.Fprintf(out, "With -calls, it also lists the calls that each transaction makes, as arbot executions -calls does.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 0); err != nil {
		return err
	}
	ref, err := openSafe(ctx, e, *requestFile)
	if err != nil {
		return err
	}
	request, err := readRequest(*requestFile)
	if err != nil {
		return err
	}
	baseURL, ok := e.cfg.TxServices[ref.ChainID]
	if !ok {
		baseURL = txservice.DefaultURLs[ref.ChainID]
	}
	if baseURL == "" {
		return fmt.Errorf("no Safe Transaction Service for chain %d (see the txServices setting)", ref.ChainID)
	}
	finder := &executions.Finder{
		Eth:     ref.eth,
		Service: txservice.NewClient(baseURL, ref.ChainID),
		Warn:    func(err error) { fmt.Fprintf(e.stderr, "%s: warning: %v\n", e.progname, err) },
	}
	observed := time.Now().UTC().Truncate(time.Second)
	history, err := finder.FromService(ctx, ref.Safe, ethrpc.BlockNumber(ref.SafeBlock))
	if err != nil {
		return err
	}

	report := historyReport{
		Safe: ref.Safe, ChainID: ref.ChainID, SafeBlock: ref.SafeBlock,
		Service: serviceSource{URL: baseURL, ObservedAt: observed},
		Nonce:   history.Nonce, Missing: history.Missing,
		Executions: []execution{}, Rejected: history.Rejected,
	}
	var reader *checks.CallReader
	if *withCalls {
		reader = checks.NewCallReader(ref.eth, ref.Safe, ethrpc.BlockNumber(ref.SafeBlock))
	}
	for _, x := range history.Executions {
		item := execution{Execution: x}
		if reader != nil {
			calls, err := reader.Calls(ctx, x.Transaction)
			if err != nil {
				return fmt.Errorf("decoding the calls of Safe transaction %s: %w", x.SafeTxHash, err)
			}
			item.Target, item.Calls = &calls.Target, calls.Calls
		}
		if x.SafeTxHash == request.Proposal.SafeTxHash {
			report.ProposalExecution = &item
			continue
		}
		report.Executions = append(report.Executions, item)
	}

	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	fmt.Fprintf(e.stdout, "Safe transactions executed by %s on chain %d, up to block %d\n", report.Safe, report.ChainID, report.SafeBlock)
	fmt.Fprintf(e.stdout, "Source: Safe Transaction Service %q, observed %s\n\n", report.Service.URL, report.Service.ObservedAt.Format(time.RFC3339))
	if report.Nonce == nil {
		fmt.Fprintf(e.stdout, "Nonce at block %d: unknown\n", report.SafeBlock)
	} else {
		fmt.Fprintf(e.stdout, "Nonce at block %d: %s\n", report.SafeBlock, report.Nonce)
		fmt.Fprintf(e.stdout, "Missing nonces: %s\n", nonceRanges(report.Missing))
	}
	if x := report.ProposalExecution; x != nil {
		fmt.Fprintf(e.stdout, "The request's own Safe transaction was executed in block %d, transaction %s (not listed below)\n", x.Block, x.TxHash)
	}
	fmt.Fprintln(e.stdout)

	if len(report.Executions) == 0 {
		fmt.Fprintln(e.stdout, "none")
	} else {
		w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "BLOCK\tTRANSACTION\tRESULT\tSAFE TX HASH\tNONCE\tTO\tVALUE\tOPERATION\tDATA")
		for _, x := range report.Executions {
			result := "failure"
			if x.Success {
				result = "success"
			}
			tx := x.Transaction
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", x.Block, x.TxHash, result, x.SafeTxHash, tx.Nonce, tx.To, tx.Value, tx.Operation, summarizeData(tx.Data))
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}

	if len(report.Rejected) > 0 {
		fmt.Fprintf(e.stdout, "\nRejected\n\n")
		w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NONCE\tSAFE TX HASH\tTRANSACTION\tREASON")
		for _, r := range report.Rejected {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Nonce, r.SafeTxHash, r.TxHash, r.Reason)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}

	if reader == nil || len(report.Executions) == 0 {
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
