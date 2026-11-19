package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/vector"
)

// vectorCommand writes a request of a Sentinel test vector, which the commands
// that take -request-file run on.
func vectorCommand(_ context.Context, e *env, args []string) error {
	flags := e.flagSet("vector")
	asJSON := flags.Bool("json", false, "write the request as JSON, as arbot info -json writes one, for -request-file")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s vector [flags] <spec.json>\n\n", e.progname)
		fmt.Fprintf(flags.Output(), "Makes a request of a Sentinel test vector, with its Safe transaction, its block as the Safe block, and its rule as the disputed rule, without oracle fields, so that the commands that take -request-file run on it.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 1); err != nil {
		return err
	}
	path := flags.Arg(0)
	spec, err := vector.Read(path)
	if err != nil {
		return err
	}
	request := spec.SafenetRequest(path)
	if *asJSON {
		return writeJSON(e.stdout, request)
	}
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	writeVector(w, request)
	return w.Flush()
}

// writeVector writes the request of a vector as text to w, in sections of
// key-value rows.
func writeVector(w io.Writer, r *safenet.Request) {
	v := r.Vector
	fmt.Fprintf(w, "Vector\n")
	fmt.Fprintf(w, "  Spec\t%s\n", v.Path)
	fmt.Fprintf(w, "  Verdict\t%s\n", v.Verdict)
	rule := v.Rule
	if rule == "" {
		rule = "-"
	}
	fmt.Fprintf(w, "  Rule\t%s\n", rule)
	fmt.Fprintf(w, "  EOA transaction\t%t\n", v.EOA)
	if v.EOA {
		fmt.Fprintf(w, "  \tno Safe exists at the Safe address, so arbot classify finds it insecure under R-4.0\n")
	}

	p := r.Proposal
	tx := p.Transaction
	fmt.Fprintf(w, "\nSafe transaction\n")
	fmt.Fprintf(w, "  Safe block\t%d (chain %s)\n", p.SafeBlock, tx.ChainID)
	fmt.Fprintf(w, "  Safe tx hash\t%s\n", p.SafeTxHash)
	fmt.Fprintf(w, "  Safe\t%s\n", tx.Safe)
	fmt.Fprintf(w, "  To\t%s\n", tx.To)
	fmt.Fprintf(w, "  Value\t%s\n", tx.Value)
	fmt.Fprintf(w, "  Data\t%s\n", tx.Data)
	fmt.Fprintf(w, "  Operation\t%s\n", tx.Operation)
	fmt.Fprintf(w, "  Safe tx gas\t%s\n", tx.SafeTxGas)
	fmt.Fprintf(w, "  Base gas\t%s\n", tx.BaseGas)
	fmt.Fprintf(w, "  Gas price\t%s\n", tx.GasPrice)
	fmt.Fprintf(w, "  Gas token\t%s\n", tx.GasToken)
	fmt.Fprintf(w, "  Refund receiver\t%s\n", tx.RefundReceiver)
	fmt.Fprintf(w, "  Nonce\t%s\n", tx.Nonce)

	fmt.Fprintf(w, "\nDisputed rules\n  %s\n", formatRules(r.DisputedRules))
}
