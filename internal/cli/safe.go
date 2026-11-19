package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safesettings"
)

// controlNote says what a comparison of Safe settings is evidence of, which the
// text output of `arbot safe` prints with the settings of another account and
// with each comparison.
const controlNote = "Shared owners, or an identical configuration, are facts about who controls the Safes, not by\n" +
	"themselves evidence that a recipient is inside the expected target set of Charter § 2.4: the ruling\n" +
	"on 0x8b83aa12d81c160fc8f1b2dd5af6e4f8032e61a6b648ff9e2cdb80f6570c3b2d gave a recipient's shared\n" +
	"owners no weight.\n"

// safeReport is the JSON output of `arbot safe` without -address, -at, or
// -compare.
type safeReport struct {
	Safe      ethrpc.Address `json:"safe"`
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	*safesettings.Settings
}

// accountSettingsReport is the JSON output of `arbot safe` with -address or
// -at, and without -compare.
type accountSettingsReport struct {
	ChainID   uint64 `json:"chainId"`
	SafeBlock uint64 `json:"safeBlock"`
	*safesettings.Account
}

// compareReport is the JSON output of `arbot safe -compare`.
type compareReport struct {
	ChainID   uint64                `json:"chainId"`
	SafeBlock uint64                `json:"safeBlock"`
	A         *safesettings.Account `json:"a"`
	B         *safesettings.Account `json:"b"`
	// Diff is nil unless both accounts are Safes of § 2.1 versions.
	Diff *safesettings.Diff `json:"diff"`
}

// safeCommand shows the settings of the Safe of a request before its proposal,
// or of another account, or compares two of them.
func safeCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("safe")
	asJSON := flags.Bool("json", false, "write the settings as JSON")
	requestFile := flags.String("request-file", "", "read the Safe, its chain, and the block from the request in the JSON file at `path`, as written by arbot info -json")
	addressValue := flags.String("address", "", "show the settings of the account at `address` on the Safe's chain instead of the Safe's")
	at := flags.Uint64("at", 0, "read the settings as of block `N`, which can't be after the request's Safe block (default: the Safe block)")
	compareFlag := flags.String("compare", "", "compare the settings with those of the Safe at `address`, as of the same block, or with -at, compare that Safe's settings at -at with those at the Safe block")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s safe [flags] -request-file <path>\n\n", e.progname)
		fmt.Fprintf(out, "Shows the settings of the Safe of a request, as Charter § 2.10 lists them, read from the storage of\n")
		fmt.Fprintf(out, "the Safe on its chain as of the request's Safe block, without the transaction's own effects or\n")
		fmt.Fprintf(out, "anything later: its singleton, owners, threshold, nonce, enabled modules, fallback handler, guard,\n")
		fmt.Fprintf(out, "and module guard, which only Safe 1.5.0 has. The storage is read with the layout of the Safe versions\n")
		fmt.Fprintf(out, "of § 2.1, so only an account that arbot calls identifies as a Safe of one of them has settings: a\n")
		fmt.Fprintf(out, "SafeProxy of a § 2.1 proxy version on its network, whose singleton is a Safe singleton of a § 2.1\n")
		fmt.Fprintf(out, "version. The zero address means that a setting isn't set.\n\n")
		fmt.Fprintf(out, "With -address, it shows the settings of another account on the Safe's chain, such as a recipient,\n")
		fmt.Fprintf(out, "or reports that it has none and why: it has no code, its code isn't a § 2.1 Safe proxy, or its\n")
		fmt.Fprintf(out, "singleton isn't a § 2.1 singleton. With -at, it reads them as of an earlier block.\n\n")
		fmt.Fprintf(out, "With -compare, it compares the settings of the Safe, or of -address, with those of another Safe as of\n")
		fmt.Fprintf(out, "the Safe block, or with -at, those of the -compare Safe at -at with its own at the Safe block, field\n")
		fmt.Fprintf(out, "by field: which are equal, the owners and modules that both have and those that only one has, and\n")
		fmt.Fprintf(out, "whether every setting but the nonce is the same.\n\n")
		fmt.Fprintf(out, "%s\n", controlNote)
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 0); err != nil {
		return err
	}
	address, err := addressFlag("address", *addressValue)
	if err != nil {
		return err
	}
	other, err := addressFlag("compare", *compareFlag)
	if err != nil {
		return err
	}
	if other != nil && *at != 0 && address != nil {
		return usageError("-address doesn't apply with -compare and -at, which compare the -compare Safe at two blocks")
	}
	safe, err := openSafe(ctx, e, *requestFile)
	if err != nil {
		return err
	}
	if *at > safe.SafeBlock {
		return usageError(fmt.Sprintf("-at %d is after the request's Safe block %d", *at, safe.SafeBlock))
	}
	block := safe.SafeBlock
	if *at != 0 {
		block = *at
	}
	read := func(address ethrpc.Address, block uint64) (*safesettings.Account, error) {
		a, err := safesettings.ReadAccount(ctx, safe.eth, address, ethrpc.BlockNumber(block))
		if err != nil {
			return nil, fmt.Errorf("reading the settings of %s at block %d: %w", address, block, err)
		}
		return a, nil
	}

	if other != nil {
		report := compareReport{ChainID: safe.ChainID, SafeBlock: safe.SafeBlock}
		if *at != 0 {
			report.A, err = read(*other, block)
		} else {
			report.A, err = read(orAddress(address, safe.Safe), block)
		}
		if err != nil {
			return err
		}
		if report.B, err = read(*other, safe.SafeBlock); err != nil {
			return err
		}
		if report.A.Settings != nil && report.B.Settings != nil {
			report.Diff = safesettings.Compare(report.A.Settings, report.B.Settings)
		}
		if *asJSON {
			return writeJSON(e.stdout, report)
		}
		return printComparison(e.stdout, report)
	}

	account, err := read(orAddress(address, safe.Safe), block)
	if err != nil {
		return err
	}
	if address != nil || *at != 0 {
		if *asJSON {
			return writeJSON(e.stdout, accountSettingsReport{ChainID: safe.ChainID, SafeBlock: safe.SafeBlock, Account: account})
		}
		printAccountSettings(e.stdout, safe.ChainID, account)
		if account.Address != safe.Safe && account.Settings != nil {
			fmt.Fprintf(e.stdout, "\n%s", controlNote)
		}
		return nil
	}
	// The output without flags has the settings at its top level, so an account
	// that isn't a Safe fails rather than having empty settings.
	if account.Settings == nil {
		return fmt.Errorf("%s isn't a Safe of a § 2.1 version at block %d, so it has no settings: %s (%s)",
			account.Address, account.Block, safesettings.Reasons[account.Reason], account.Reason)
	}
	if *asJSON {
		return writeJSON(e.stdout, safeReport{Safe: safe.Safe, ChainID: safe.ChainID, SafeBlock: safe.SafeBlock, Settings: account.Settings})
	}
	printAccountSettings(e.stdout, safe.ChainID, account)
	return nil
}

// orAddress returns *address, or fallback if address is nil.
func orAddress(address *ethrpc.Address, fallback ethrpc.Address) ethrpc.Address {
	if address == nil {
		return fallback
	}
	return *address
}

// printAccountSettings prints the settings of an account, or why it has none.
func printAccountSettings(w io.Writer, chainID uint64, a *safesettings.Account) {
	s := a.Settings
	if s == nil {
		fmt.Fprintf(w, "Account %s on chain %d, as of block %d\n", a.Address, chainID, a.Block)
		if a.CodeSize == 0 {
			fmt.Fprintf(w, "  Code      none\n")
		} else {
			fmt.Fprintf(w, "  Code      %s (%d bytes), %s\n", a.CodeHash, a.CodeSize, versionText("Safe proxy", a.ProxyVersion))
			fmt.Fprintf(w, "  Slot 0    %s, %s\n", a.Singleton, versionText("Safe singleton", a.SingletonVersion))
		}
		fmt.Fprintf(w, "  Settings  none: %s\n", safesettings.Reasons[a.Reason])
		return
	}
	fmt.Fprintf(w, "Safe %s on chain %d, as of block %d\n", a.Address, chainID, a.Block)
	fmt.Fprintf(w, "  Code              %s (%d bytes), %s\n", a.CodeHash, a.CodeSize, versionText("Safe proxy", a.ProxyVersion))
	fmt.Fprintf(w, "  Singleton         %s, %s\n", s.Singleton, versionText("Safe singleton", a.SingletonVersion))
	fmt.Fprintf(w, "  Threshold         %s of %s owners\n", s.Threshold, s.OwnerCount)
	fmt.Fprintf(w, "  Nonce             %s\n", s.Nonce)
	fmt.Fprintf(w, "  Fallback handler  %s\n", s.FallbackHandler)
	fmt.Fprintf(w, "  Guard             %s\n", s.Guard)
	fmt.Fprintf(w, "  Module guard      %s\n", s.ModuleGuard)
	for _, list := range []struct {
		name    string
		entries []ethrpc.Address
	}{{"Owners", s.Owners}, {"Modules", s.Modules}} {
		fmt.Fprintf(w, "\n%s\n", list.name)
		if len(list.entries) == 0 {
			fmt.Fprintf(w, "  none\n")
		}
		for _, address := range list.entries {
			fmt.Fprintf(w, "  %s\n", address)
		}
	}
}

// printComparison prints a comparison of the settings of two accounts.
func printComparison(w io.Writer, r compareReport) error {
	fmt.Fprintf(w, "Settings compared on chain %d, read from storage\n", r.ChainID)
	for _, side := range []struct {
		name    string
		account *safesettings.Account
	}{{"A", r.A}, {"B", r.B}} {
		a := side.account
		if a.Settings == nil {
			fmt.Fprintf(w, "  %s  %s as of block %d, no settings: %s\n", side.name, a.Address, a.Block, safesettings.Reasons[a.Reason])
		} else {
			fmt.Fprintf(w, "  %s  %s as of block %d, %s, %s\n", side.name, a.Address, a.Block,
				versionText("Safe proxy", a.ProxyVersion), versionText("Safe singleton", a.SingletonVersion))
		}
	}
	if r.Diff == nil {
		fmt.Fprintf(w, "\nNot compared, since only Safes of § 2.1 versions have settings.\n")
		return nil
	}

	a, b, d := r.A.Settings, r.B.Settings, r.Diff
	fmt.Fprintln(w)
	t := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(t, "SETTING\tRESULT\tA\tB")
	for _, row := range []struct {
		name   string
		result safesettings.Comparison
		a, b   any
	}{
		{"singleton", d.Singleton, a.Singleton, b.Singleton},
		{"owner count", d.OwnerCount, a.OwnerCount, b.OwnerCount},
		{"threshold", d.Threshold, a.Threshold, b.Threshold},
		{"nonce", d.Nonce, a.Nonce, b.Nonce},
		{"fallback handler", d.FallbackHandler, a.FallbackHandler, b.FallbackHandler},
		{"guard", d.Guard, a.Guard, b.Guard},
		{"module guard", d.ModuleGuard, a.ModuleGuard, b.ModuleGuard},
	} {
		fmt.Fprintf(t, "%s\t%s\t%v\t%v\n", row.name, row.result, row.a, row.b)
	}
	if err := t.Flush(); err != nil {
		return err
	}
	configuration := "different"
	if d.SameConfiguration {
		configuration = "the same"
	}
	fmt.Fprintf(w, "\nEvery setting but the nonce, with the lists compared as sets, is %s.\n", configuration)

	for _, list := range []struct {
		name string
		diff safesettings.ListDiff
	}{{"Owners", d.Owners}, {"Modules", d.Modules}} {
		fmt.Fprintf(w, "\n%s: %s as sets\n", list.name, list.diff.Result)
		t := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, group := range []struct {
			name    string
			entries []ethrpc.Address
		}{{"both", list.diff.Shared}, {"only A", list.diff.OnlyA}, {"only B", list.diff.OnlyB}} {
			entries := make([]string, len(group.entries))
			for i, address := range group.entries {
				entries[i] = address.String()
			}
			if len(entries) == 0 {
				entries = []string{"none"}
			}
			fmt.Fprintf(t, "  %s\t%s\n", group.name, strings.Join(entries, ", "))
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintf(w, "\n%s", controlNote)
	return nil
}
