package cli

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// info shows a Safenet request, its proposal, votes, and arbitration.
func info(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("info")
	asJSON := flags.Bool("json", false, "write the request as JSON")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s info [flags] <request-id>\n\n", e.progname)
		fmt.Fprintf(flags.Output(), "Shows a Safenet request, the transaction proposal it is for, the sentinels' votes, and its arbitration.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 1); err != nil {
		return err
	}
	id, err := ethrpc.ParseHash(flags.Arg(0))
	if err != nil {
		return usageError(fmt.Sprintf("request ID %q: %v", flags.Arg(0), err))
	}

	request, err := openSafenet(e.cfg, ethrpc.NewDialer(e.cfg.RPCs)).Request(ctx, id)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(e.stdout, request)
	}
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	writeRequest(w, request)
	return w.Flush()
}

// writeRequest writes request as text to w, in sections of key-value rows.
// Strings that come from the chain are quoted, so that they can't contain
// terminal escape sequences.
func writeRequest(w io.Writer, r *safenet.Request) {
	fmt.Fprintf(w, "Request\n")
	fmt.Fprintf(w, "  ID\t%s\n", r.ID)
	fmt.Fprintf(w, "  Oracle\t%s\n", r.Oracle)
	fmt.Fprintf(w, "  Charter\t%q\n", r.Charter)
	fmt.Fprintf(w, "  State\t%s\n", r.State)
	fmt.Fprintf(w, "  Sponsor\t%s\n", r.Terms.Sponsor)
	fmt.Fprintf(w, "  Fee token\t%s (%s, %d decimals)\n", r.FeeToken.Address, symbol(r.FeeToken), r.FeeToken.Decimals)
	fmt.Fprintf(w, "  Fee\t%s\n", formatAmount(r.Fee, r.FeeToken))
	fmt.Fprintf(w, "  Bond\t%s\n", formatAmount(r.Terms.Bond, r.FeeToken))
	fmt.Fprintf(w, "  Slash amount\t%s\n", formatAmount(r.Terms.SlashAmount, r.FeeToken))
	fmt.Fprintf(w, "  DAO fee share\t%s%%\n", strconv.FormatFloat(float64(r.Terms.DAOFeeShare)/1000, 'f', -1, 64))
	fmt.Fprintf(w, "  Commit deadline\tblock %d\n", r.Terms.CommitDeadline)
	fmt.Fprintf(w, "  Reveal deadline\tblock %d\n", r.Terms.RevealDeadline)

	p := r.Proposal
	fmt.Fprintf(w, "\nProposal\n")
	fmt.Fprintf(w, "  Consensus\t%s\n", p.Consensus)
	fmt.Fprintf(w, "  Block\t%d (%s)\n", p.Block, p.Time.Format(time.RFC3339))
	fmt.Fprintf(w, "  Ethereum block before\t%d (%s)\n", p.EthereumBlock, formatTime(p.EthereumBlockTime))
	fmt.Fprintf(w, "  Safe chain block before\t%d (chain %s, %s)\n", p.SafeBlock, p.Transaction.ChainID, formatTime(p.SafeBlockTime))
	fmt.Fprintf(w, "  Transaction\t%s\n", p.TxHash)
	fmt.Fprintf(w, "  Epoch\t%d\n", p.Epoch)
	fmt.Fprintf(w, "  Oracle data\t%s\n", p.OracleData)
	fmt.Fprintf(w, "  Safe tx hash\t%s\n", p.SafeTxHash)

	tx := p.Transaction
	fmt.Fprintf(w, "\nSafe transaction\n")
	fmt.Fprintf(w, "  Chain ID\t%s\n", tx.ChainID)
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

	fmt.Fprintf(w, "\nVotes\n")
	if len(r.Votes) == 0 {
		fmt.Fprintf(w, "  none\n")
	}
	for _, v := range r.Votes {
		fmt.Fprintf(w, "  %s\t%s\tbond %s\treason %q\trules %s\n", v.Sentinel, v.Vote, formatAmount(v.Bond, r.FeeToken), v.Reason, formatRules(v.Rules))
	}
	fmt.Fprintf(w, "\nDisputed rules\n  %s\n", formatRules(r.DisputedRules))

	a := r.Arbitration
	if a == nil {
		return
	}
	fmt.Fprintf(w, "\nArbitration\n")
	fmt.Fprintf(w, "  Frozen\tblock %d\n", a.FrozenBlock)
	fmt.Fprintf(w, "  Deadline\tblock %d\n", a.Deadline)
	fmt.Fprintf(w, "  Outcome\t%s\n", a.Outcome)
	if a.Slashed != nil {
		fmt.Fprintf(w, "  Slashed\t%s\n", formatAmount(a.Slashed, r.FeeToken))
	}
	switch a.Outcome {
	case safenet.OutcomeSecure, safenet.OutcomeInsecure, safenet.OutcomeOutOfScope:
		fmt.Fprintf(w, "  Context\t%q\n", a.Context)
	}
	if a.Record != nil {
		fmt.Fprintf(w, "  Record\tblock %d, transaction %s\n", a.Record.Block, a.Record.TxHash)
	}
}

// formatTime formats a block's timestamp in RFC 3339, or "time unknown" for the
// zero time of a request file written before arbot reported block times.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "time unknown"
	}
	return t.UTC().Format(time.RFC3339)
}

// formatRules formats a list of rule identifiers, such as "R-4.1, R-4.2", or
// "-" if it is empty. CitedRules only returns rule identifiers, so they need no
// quoting.
func formatRules(rules []string) string {
	if len(rules) == 0 {
		return "-"
	}
	return strings.Join(rules, ", ")
}

// formatAmount formats an amount of token in the token's units, followed by its
// symbol, such as "0.4 MTK".
func formatAmount(amount *big.Int, token safenet.Token) string {
	return formatUnits(amount, token.Decimals) + " " + symbol(token)
}

// symbol returns the token's symbol, quoted unless it is a plain word, as it
// comes from the chain and could contain terminal escape sequences.
func symbol(token safenet.Token) string {
	plain := token.Symbol != "" && !strings.ContainsFunc(token.Symbol, func(r rune) bool {
		return !unicode.IsPrint(r) || unicode.IsSpace(r)
	})
	if plain {
		return token.Symbol
	}
	return strconv.Quote(token.Symbol)
}

// formatUnits formats amount as a decimal number with the given number of
// decimals, without trailing zeros: 400000000000000000 with 18 decimals is
// "0.4".
func formatUnits(amount *big.Int, decimals uint8) string {
	digits := new(big.Int).Abs(amount).String()
	d := int(decimals)
	if len(digits) <= d {
		digits = strings.Repeat("0", d-len(digits)+1) + digits
	}
	whole, fraction := digits[:len(digits)-d], strings.TrimRight(digits[len(digits)-d:], "0")
	if amount.Sign() < 0 {
		whole = "-" + whole
	}
	if fraction == "" {
		return whole
	}
	return whole + "." + fraction
}
