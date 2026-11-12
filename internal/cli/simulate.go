package cli

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/simulate"
	"github.com/safe-research/safenet-arbitration-bot/internal/sourcify"
)

// simulateReport is the JSON output of `arbot simulate`.
type simulateReport struct {
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	Safe      ethrpc.Address `json:"safe"`
	// Number is the number of the simulated call, from 1, in the order that arbot
	// calls lists them, and First is whether it is the transaction's first call,
	// the only one whose simulation runs on the state that the call would run on.
	Number int  `json:"number"`
	First  bool `json:"first"`
	// Call is the call, as arbot calls -json has it.
	Call   checks.Call      `json:"call"`
	Result *simulate.Result `json:"result"`
}

// simulateCommand runs a call that the Safe transaction of a request makes,
// from the Safe, on the state of the Safe's chain at its Safe block.
func simulateCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("simulate")
	asJSON := flags.Bool("json", false, "write the result as JSON")
	requestFile := flags.String("request-file", "", "read the request from the JSON file at `path`, as written by arbot info -json")
	number := flags.Int("call", 1, "simulate the call with this `number`, from 1, as arbot calls lists the calls")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s simulate [flags] -request-file <path>\n\n", e.progname)
		fmt.Fprintf(out, "Runs a call that the Safe transaction of a request makes, as arbot calls lists them, with eth_call:\n")
		fmt.Fprintf(out, "from the Safe, with the call's value and data, on the state of the Safe's chain at the request's\n")
		fmt.Fprintf(out, "Safe block. It reports whether the call returned or reverted, the data that it returned, and, from\n")
		fmt.Fprintf(out, "the ABI that Sourcify verified for the account that it calls, an offchain source that it reports\n")
		fmt.Fprintf(out, "with its URL and observation time, the function and its returned addresses, booleans, unsigned\n")
		fmt.Fprintf(out, "integers, and fixed-size byte arrays.\n\n")
		fmt.Fprintf(out, "The simulation is of the call alone: the state doesn't have the effects of the calls that the\n")
		fmt.Fprintf(out, "transaction makes before it, so only the first call's simulation runs on the state that the call\n")
		fmt.Fprintf(out, "would run on, which \"first\" reports. The state is that of the Safe block, so the call can do\n")
		fmt.Fprintf(out, "otherwise when the transaction is executed in a later block. A delegate call, the synthetic refund\n")
		fmt.Fprintf(out, "call, and a placeholder can't be simulated. It needs a node that serves eth_call at the Safe block.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 0); err != nil {
		return err
	}
	if *requestFile == "" {
		return usageError("-request-file is required")
	}
	if *number < 1 {
		return usageError("-call must be at least 1")
	}
	request, err := readRequest(*requestFile)
	if err != nil {
		return err
	}
	ref, err := openSafe(ctx, e, *requestFile)
	if err != nil {
		return err
	}
	calls, err := checks.NewCallReader(ref.eth, ref.Safe, ethrpc.BlockNumber(ref.SafeBlock)).Calls(ctx, &request.Proposal.Transaction)
	if err != nil {
		return err
	}
	if *number > len(calls.Calls) {
		return fmt.Errorf("the transaction makes %d calls, so there is no call %d", len(calls.Calls), *number)
	}
	call := calls.Calls[*number-1]
	switch {
	case call.Kind == "refund":
		return fmt.Errorf("call %d is the synthetic refund call, which can't be simulated", *number)
	case call.Placeholder != "":
		return fmt.Errorf("call %d is a %s placeholder, which can't be simulated", *number, call.Placeholder)
	}
	code, err := ref.eth.GetCode(ctx, call.To.Address, ethrpc.BlockNumber(ref.SafeBlock))
	if err != nil {
		return err
	}
	repo := sourcify.NewClient(cmp.Or(e.cfg.Sourcify, sourcify.DefaultURL))
	result, err := simulate.Call(ctx, ref.eth, repo, ref.Safe, call.To.Address, call.Value, call.Data, call.Operation, code, ethrpc.BlockNumber(ref.SafeBlock))
	if err != nil {
		return err
	}
	report := simulateReport{
		ChainID: ref.ChainID, SafeBlock: ref.SafeBlock, Safe: ref.Safe,
		Number: *number, First: *number == 1, Call: call, Result: result,
	}

	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	fmt.Fprintf(e.stdout, "Call %d of the Safe transaction of %s on chain %d, simulated at block %d\n", report.Number, report.Safe, report.ChainID, report.SafeBlock)
	if !report.First {
		fmt.Fprintf(e.stdout, "Not the first call: the state doesn't have the effects of the calls before it\n")
	}
	fmt.Fprintf(e.stdout, "  To        %s (%s)\n", call.To.Address, accountText(call.To))
	fmt.Fprintf(e.stdout, "  Value     %s\n", call.Value)
	fmt.Fprintf(e.stdout, "  Data      %s\n", summarizeData(call.Data))
	if s := result.Source; s != nil {
		fmt.Fprintf(e.stdout, "  ABI       %s, observed %s\n", s.URL, s.ObservedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(e.stdout, "\nResult      %s\n", result.Status)
	if result.RevertReason != "" {
		fmt.Fprintf(e.stdout, "  Reason    %q\n", result.RevertReason)
	}
	if f := result.Function; f != nil {
		fmt.Fprintf(e.stdout, "  Function  %s\n", f.Signature)
		for _, o := range f.Outputs {
			fmt.Fprintf(e.stdout, "  %s %s = %v\n", o.Type, cmp.Or(o.Name, "-"), o.Value)
		}
	}
	fmt.Fprintf(e.stdout, "  Returned  %s\n", wordsText(result.ReturnData))
	return nil
}

// wordsText formats data as its 32-byte words, one per line, or "-" for none.
func wordsText(data []byte) string {
	if len(data) == 0 {
		return "-"
	}
	var lines []string
	for i := 0; i < len(data); i += 32 {
		lines = append(lines, ethrpc.Bytes(data[i:min(i+32, len(data))]).String())
	}
	return strings.Join(lines, "\n            ")
}
