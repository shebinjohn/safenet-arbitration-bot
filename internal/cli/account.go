package cli

import (
	"context"
	"fmt"
	"math/big"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/erc20"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// accountReport is the JSON output of `arbot account`.
type accountReport struct {
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	Safe      ethrpc.Address `json:"safe"`
	checks.Account
	// Balance is the account's balance of the native currency, in wei.
	Balance *big.Int `json:"balance"`
	// Nonce is the account's nonce: the number of transactions that it sent, or of
	// contracts that it created if it is a contract.
	Nonce uint64 `json:"nonce"`
	// ERC20 is what the account returns as an ERC-20 token, with the Safe's balance
	// of it, or nil if it doesn't answer as one.
	ERC20 *tokenReport `json:"erc20"`
}

// tokenReport is an ERC-20 token in the output of `arbot account`.
type tokenReport struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
	// SafeBalance is the Safe's balance of the token, in its smallest unit.
	SafeBalance *big.Int `json:"safeBalance"`
}

// accountCommand shows an account on the Safe's chain of a request, as of its
// proposal.
func accountCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("account")
	asJSON := flags.Bool("json", false, "write the account as JSON")
	requestFile := flags.String("request-file", "", "read the Safe, its chain, and the block from the request in the JSON file at `path`, as written by arbot info -json")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s account [flags] -request-file <path> <address>\n\n", e.progname)
		fmt.Fprintf(out, "Shows the account at address on the Safe's chain of a request, as of the request's Safe block,\n")
		fmt.Fprintf(out, "without the transaction's own effects or anything later: its code hash and size, as arbot calls\n")
		fmt.Fprintf(out, "shows the accounts that a Safe calls, with whether it is the Safe itself and the contract of a § 2.1\n")
		fmt.Fprintf(out, "Safe version that it is, if any, its balance of the native currency in wei, and its nonce.\n\n")
		fmt.Fprintf(out, "If the account answers decimals() and balanceOf(address) as an ERC-20 token does, it also shows\n")
		fmt.Fprintf(out, "the name, symbol, and decimals that it returns, and the Safe's balance of it in its smallest unit.\n")
		fmt.Fprintf(out, "Any contract can return any name and symbol, so they say what the contract claims to be, not what\n")
		fmt.Fprintf(out, "it is. The name and symbol are empty if it doesn't return strings for them.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 1); err != nil {
		return err
	}
	address, err := ethrpc.ParseAddress(flags.Arg(0))
	if err != nil {
		return usageError(fmt.Sprintf("address %q: %v", flags.Arg(0), err))
	}
	safe, err := openSafe(ctx, e, *requestFile)
	if err != nil {
		return err
	}
	block := ethrpc.BlockNumber(safe.SafeBlock)

	report := accountReport{ChainID: safe.ChainID, SafeBlock: safe.SafeBlock, Safe: safe.Safe}
	if report.Account, err = checks.NewCallReader(safe.eth, safe.Safe, block).Account(ctx, address); err != nil {
		return fmt.Errorf("reading the code of %s at block %d: %w", address, block, err)
	}
	if report.Balance, err = safe.eth.GetBalance(ctx, address, block); err != nil {
		return fmt.Errorf("reading the balance of %s at block %d: %w", address, block, err)
	}
	if report.Nonce, err = safe.eth.GetTransactionCount(ctx, address, block); err != nil {
		return fmt.Errorf("reading the nonce of %s at block %d: %w", address, block, err)
	}
	if report.CodeSize > 0 {
		token, err := erc20.Read(ctx, safe.eth, address, safe.Safe, block)
		if err != nil {
			return err
		}
		if token != nil {
			report.ERC20 = &tokenReport{Name: token.Name, Symbol: token.Symbol, Decimals: token.Decimals, SafeBalance: token.Balance}
		}
	}

	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	fmt.Fprintf(e.stdout, "Account %s on chain %d, as of block %d\n", report.Address, report.ChainID, report.SafeBlock)
	if report.CodeSize == 0 {
		fmt.Fprintf(e.stdout, "  Code      none\n")
	} else {
		fmt.Fprintf(e.stdout, "  Code      %d bytes, hash %s\n", report.CodeSize, report.CodeHash)
	}
	if report.Self {
		fmt.Fprintf(e.stdout, "  Safe      the Safe itself\n")
	}
	if report.Contract != nil {
		fmt.Fprintf(e.stdout, "  Contract  %s\n", report.Contract)
	}
	fmt.Fprintf(e.stdout, "  Balance   %s wei\n", report.Balance)
	fmt.Fprintf(e.stdout, "  Nonce     %d\n", report.Nonce)
	if t := report.ERC20; t != nil {
		fmt.Fprintf(e.stdout, "\nERC-20 token\n")
		fmt.Fprintf(e.stdout, "  Name          %q\n", t.Name)
		fmt.Fprintf(e.stdout, "  Symbol        %q\n", t.Symbol)
		fmt.Fprintf(e.stdout, "  Decimals      %d\n", t.Decimals)
		fmt.Fprintf(e.stdout, "  Safe balance  %s\n", t.SafeBalance)
	}
	return nil
}
