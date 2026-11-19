package cli

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/simulate"
	"github.com/safe-research/safenet-arbitration-bot/internal/sourcify"
)

// simulateReport is the JSON output of `arbot simulate`.
type simulateReport struct {
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	Safe      ethrpc.Address `json:"safe"`
	// All is whether the whole Safe transaction was simulated, with -all, rather
	// than one of its calls.
	All bool `json:"all"`
	// Number is the number of the simulated call, from 1, in the order that arbot
	// calls lists them, left out with -all, and First is whether the simulation
	// runs on the state that the call would run on: for the transaction's first
	// call, and for the whole transaction.
	Number int  `json:"number,omitempty"`
	First  bool `json:"first"`
	// Call is the call, as arbot calls -json has it, left out with -all.
	Call *checks.Call `json:"call,omitempty"`
	// Transaction is the Safe transaction's call, with -all.
	Transaction *simulatedTransaction `json:"transaction,omitempty"`
	Result      *simulate.Result      `json:"result"`
	// Trace is the trace of the simulation, with -trace.
	Trace *simulate.Trace `json:"trace,omitempty"`
	// Notes are the limits of the simulation that the result doesn't show.
	Notes []string `json:"notes"`
}

// simulatedTransaction is the call of a Safe transaction that arbot simulate
// -all runs.
type simulatedTransaction struct {
	To        ethrpc.Address    `json:"to"`
	Value     *big.Int          `json:"value"`
	Operation safenet.Operation `json:"operation"`
	Data      ethrpc.Bytes      `json:"data"`
}

// simulateCommand runs a call that the Safe transaction of a request makes, or
// the whole transaction, from the Safe, on the state of the Safe's chain at its
// Safe block.
func simulateCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("simulate")
	asJSON := flags.Bool("json", false, "write the result as JSON")
	requestFile := flags.String("request-file", "", "read the request from the JSON file at `path`, as written by arbot info -json")
	number := flags.Int("call", 0, "simulate the call with this `number`, from 1, as arbot calls lists the calls (default 1)")
	all := flags.Bool("all", false, "simulate the whole Safe transaction, in the Safe's context, instead of one call")
	withTrace := flags.Bool("trace", false, "also trace the simulation with debug_traceCall and the callTracer")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s simulate [flags] -request-file <path>\n\n", e.progname)
		fmt.Fprintf(out, "Runs a call that the Safe transaction of a request makes, as arbot calls lists them, with eth_call:\n")
		fmt.Fprintf(out, "from the Safe, with the call's value and data, on the state of the Safe's chain at the request's\n")
		fmt.Fprintf(out, "Safe block. It reports whether the call returned or reverted, the data that it returned, and, from\n")
		fmt.Fprintf(out, "the ABI that Sourcify verified for the account that it calls, an offchain source that it reports\n")
		fmt.Fprintf(out, "with its URL and observation time, the function, its arguments, its returned addresses, booleans,\n")
		fmt.Fprintf(out, "unsigned integers, and fixed-size byte arrays, and the error that it reverted with: one of the\n")
		fmt.Fprintf(out, "ABI's errors, Error(string), or Panic(uint256).\n\n")
		fmt.Fprintf(out, "The simulation is of the call alone: the state doesn't have the effects of the calls that the\n")
		fmt.Fprintf(out, "transaction makes before it, so only the first call's simulation runs on the state that the call\n")
		fmt.Fprintf(out, "would run on, which \"first\" reports. A delegate call, the synthetic refund call, and a\n")
		fmt.Fprintf(out, "placeholder can't be simulated alone.\n\n")
		fmt.Fprintf(out, "With -all, it runs the whole Safe transaction instead, with no signatures: a call as above, and a\n")
		fmt.Fprintf(out, "delegate call, such as a MultiSend batch, with the Safe's simulateAndRevert, which the Safe\n")
		fmt.Fprintf(out, "singletons of § 2.1 versions have, and which delegate calls the target from the Safe, in its\n")
		fmt.Fprintf(out, "context, so that the batch's calls run in order, each on the effects of those before it. It\n")
		fmt.Fprintf(out, "doesn't simulate the gas refund, and doesn't check the signatures, nonce, guard, or safeTxGas.\n\n")
		fmt.Fprintf(out, "With -trace, it also runs debug_traceCall with the callTracer on the same call, and lists every call\n")
		fmt.Fprintf(out, "nested in it, and every contract that it creates with CREATE or CREATE2, or says why the node\n")
		fmt.Fprintf(out, "didn't serve the trace.\n\n")
		fmt.Fprintf(out, "The state is that of the Safe block, so the transaction can do otherwise when it is executed in a\n")
		fmt.Fprintf(out, "later block. It needs a node that serves eth_call at the Safe block.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 0); err != nil {
		return err
	}
	if *requestFile == "" {
		return usageError("-request-file is required")
	}
	callSet := false
	flags.Visit(func(f *flag.Flag) { callSet = callSet || f.Name == "call" })
	switch {
	case *all && callSet:
		return usageError("-all and -call can't be used together")
	case !callSet:
		*number = 1
	case *number < 1:
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
	block := ethrpc.BlockNumber(ref.SafeBlock)
	reader := checks.NewCallReader(ref.eth, ref.Safe, block).DeriveCreateNonces()
	tx := &request.Proposal.Transaction
	calls, err := reader.Calls(ctx, tx)
	if err != nil {
		return err
	}
	report := simulateReport{ChainID: ref.ChainID, SafeBlock: ref.SafeBlock, Safe: ref.Safe, All: *all}
	report.Notes = []string{fmt.Sprintf("the simulation ran on the state at block %d, the Safe block, not on the state that the transaction is executed on", ref.SafeBlock)}
	var sim simulate.Simulation
	if *all {
		if sim, err = transactionSimulation(ref.Safe, calls.Safe, tx); err != nil {
			return err
		}
		report.First = true
		report.Transaction = &simulatedTransaction{To: tx.To, Value: tx.Value, Operation: tx.Operation, Data: tx.Data}
		report.Notes = append(report.Notes,
			"the gas refund isn't simulated",
			"the signatures, nonce, guard, and safeTxGas aren't checked")
	} else {
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
		if sim, err = simulate.CallOf(ref.Safe, call.To.Address, call.Value, call.Data, call.Operation); err != nil {
			return fmt.Errorf("call %d: %w; simulate the whole transaction with -all", *number, err)
		}
		report.Number, report.First, report.Call = *number, *number == 1, &call
		if !report.First {
			report.Notes = append(report.Notes, "the call was simulated alone, so the state doesn't have the effects of the calls before it")
		}
	}
	code, err := reader.Code(ctx, sim.To)
	if err != nil {
		return err
	}
	repo := sourcify.NewClient(cmp.Or(e.cfg.Sourcify, sourcify.DefaultURL))
	if report.Result, err = simulate.Run(ctx, ref.eth, repo, sim, code, block); err != nil {
		return err
	}
	if *withTrace {
		if report.Trace, err = simulate.TraceCall(ctx, ref.eth, sim.Request, block); err != nil {
			return err
		}
		if sim.Method == simulate.MethodSimulateAndRevert && report.Trace.Available {
			report.Notes = append(report.Notes, "simulateAndRevert reverts by design, so the trace's top frame shows a revert whatever the outcome")
		}
	}

	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	printSimulation(e, &report)
	return nil
}

// transactionSimulation returns the simulation of the whole Safe transaction tx
// of safe, which is account. A delegate call needs the Safe's
// simulateAndRevert, which only the singletons of § 2.1 versions are known to
// have.
func transactionSimulation(safe ethrpc.Address, account checks.SafeAccount, tx *safenet.SafeTransaction) (simulate.Simulation, error) {
	if tx.Operation == safenet.OperationDelegateCall && account.SingletonVersion == nil {
		return simulate.Simulation{}, fmt.Errorf("can't simulate the delegate call: the Safe's singleton %s isn't a Safe singleton of a § 2.1 version, so it isn't known to have simulateAndRevert", account.Singleton)
	}
	return simulate.TransactionOf(safe, tx)
}

// printSimulation writes the text output of arbot simulate.
func printSimulation(e *env, report *simulateReport) {
	result := report.Result
	method := "eth_call from the Safe"
	if result.Method == simulate.MethodSimulateAndRevert {
		method = "simulateAndRevert of the Safe, which delegate calls the target in the Safe's context"
	}
	if report.All {
		tx := report.Transaction
		fmt.Fprintf(e.stdout, "The Safe transaction of %s on chain %d, simulated on the state at block %d\n", report.Safe, report.ChainID, report.SafeBlock)
		fmt.Fprintf(e.stdout, "  To        %s\n", tx.To)
		fmt.Fprintf(e.stdout, "  Value     %s\n", tx.Value)
		fmt.Fprintf(e.stdout, "  Operation %s\n", tx.Operation)
		fmt.Fprintf(e.stdout, "  Data      %s\n", summarizeData(tx.Data))
	} else {
		call := report.Call
		fmt.Fprintf(e.stdout, "Call %d of the Safe transaction of %s on chain %d, simulated on the state at block %d\n", report.Number, report.Safe, report.ChainID, report.SafeBlock)
		fmt.Fprintf(e.stdout, "  To        %s (%s)\n", call.To.Address, accountText(call.To))
		fmt.Fprintf(e.stdout, "  Value     %s\n", call.Value)
		fmt.Fprintf(e.stdout, "  Data      %s\n", summarizeData(call.Data))
	}
	fmt.Fprintf(e.stdout, "  Method    %s\n", method)
	if s := result.Source; s != nil {
		fmt.Fprintf(e.stdout, "  ABI       %s, observed %s\n", s.URL, s.ObservedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(e.stdout, "\nResult      %s\n", result.Status)
	if result.RevertReason != "" {
		fmt.Fprintf(e.stdout, "  Reason    %q\n", result.RevertReason)
	}
	if r := result.Error; r != nil {
		fmt.Fprintf(e.stdout, "  Error     %s\n", revertText(r))
		for _, a := range r.Args {
			fmt.Fprintf(e.stdout, "  error  %s %s = %s\n", a.Type, cmp.Or(a.Name, "-"), valueText(a.Type, a.Value))
		}
	}
	if f := result.Function; f != nil {
		fmt.Fprintf(e.stdout, "  Function  %s\n", f.Signature)
		for _, in := range f.Inputs {
			fmt.Fprintf(e.stdout, "  input  %s %s = %s\n", in.Type, cmp.Or(in.Name, "-"), valueText(in.Type, in.Value))
		}
		for _, o := range f.Outputs {
			fmt.Fprintf(e.stdout, "  output %s %s = %s\n", o.Type, cmp.Or(o.Name, "-"), valueText(o.Type, o.Value))
		}
	}
	fmt.Fprintf(e.stdout, "  Returned  %s\n", wordsText(result.ReturnData))
	if t := report.Trace; t != nil {
		printTrace(e, t)
	}
	fmt.Fprintf(e.stdout, "\nNotes\n")
	for _, n := range report.Notes {
		fmt.Fprintf(e.stdout, "  %s\n", n)
	}
}

// revertText describes a decoded revert error.
func revertText(r *simulate.RevertError) string {
	if r.Signature == "" {
		return r.Selector.String() + " (unknown error)"
	}
	return fmt.Sprintf("%s %s (%s)", r.Selector, r.Signature, r.Source)
}

// printTrace writes the trace of a simulation.
func printTrace(e *env, t *simulate.Trace) {
	if !t.Available {
		fmt.Fprintf(e.stdout, "\nTrace unavailable: %s\n", t.Reason)
		return
	}
	fmt.Fprintf(e.stdout, "\nTrace (debug_traceCall, callTracer)\n\n")
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tDEPTH\tTYPE\tFROM\tTO\tVALUE\tSELECTOR\tINPUT\tERROR")
	for i, f := range t.Calls {
		to, value, selector := "-", "-", "-"
		if f.To != nil {
			to = f.To.String()
		}
		if f.Value != nil {
			value = f.Value.String()
		}
		if f.Selector != nil {
			selector = f.Selector.String()
		}
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%d bytes\t%s\n", i+1, f.Depth, f.Type, f.From, to, value, selector, f.InputSize, errorText(f.Error, f.RevertReason))
	}
	w.Flush()
	fmt.Fprintf(e.stdout, "\nCreated contracts\n\n")
	if len(t.Creations) == 0 {
		fmt.Fprintf(e.stdout, "  none\n")
		return
	}
	w = tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DEPTH\tTYPE\tADDRESS\tCREATOR\tINIT CODE\tERROR")
	for _, c := range t.Creations {
		address := "-"
		if c.Address != nil {
			address = c.Address.String()
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d bytes, hash %s\t%s\n", c.Depth, c.Type, address, c.Creator, c.InitCodeSize, c.InitCodeHash, errorText(c.Error, ""))
	}
	w.Flush()
}

// errorText describes the error of a trace frame, or "-" for none.
func errorText(err, reason string) string {
	switch {
	case err == "":
		return "-"
	case reason != "":
		return fmt.Sprintf("%s: %q", err, reason)
	}
	return err
}

// valueText formats a decoded value of type typ: a string quoted, bytes longer
// than 32 bytes shortened to their first 4 bytes and length, and an array's
// elements in brackets.
func valueText(typ string, value any) string {
	switch v := value.(type) {
	case ethrpc.Bytes:
		if len(v) > 32 {
			return summarizeData(v)
		}
		return v.String()
	case string:
		if typ == "string" {
			return strconv.Quote(v)
		}
		return v
	case []any:
		elem := strings.TrimSuffix(typ, "[]")
		elems := make([]string, len(v))
		for i, e := range v {
			elems[i] = valueText(elem, e)
		}
		return "[" + strings.Join(elems, ", ") + "]"
	}
	return fmt.Sprint(value)
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
