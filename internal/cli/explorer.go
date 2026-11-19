package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/etherscan"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// explorerCommands are the subcommands of `arbot explorer`, by name.
var explorerCommands = map[string]struct {
	run         func(ctx context.Context, e *env, x *explorerRef, address ethrpc.Address, asJSON bool) error
	description string
}{
	"creation": {explorerCreation, "the deployer, factory, creation transaction, and block of a contract, confirmed onchain"},
	"txs":      {explorerTxs, "the normal transactions of an account up to the block"},
	"tokentx":  {explorerTokenTx, "the ERC-20 transfers of an account up to the block"},
	"source":   {explorerSource, "Etherscan's verification of a contract, as a second source next to Sourcify"},
}

// errEtherscanNotConfigured is the error of every explorer command without an
// Etherscan API key, which no other command needs.
var errEtherscanNotConfigured = errors.New(`arbot explorer needs an Etherscan API key: configure etherscan in the config file, such as {"etherscan": {"apiKey": "<key>"}}; without one, this evidence is a gap: record it as "Etherscan not configured"`)

// onchain is whether an offchain result is confirmed onchain: true, false, or
// "n/a" if nothing onchain can confirm it.
type onchain int

const (
	notConfirmed onchain = iota
	confirmed
	notApplicable
)

func (o onchain) MarshalJSON() ([]byte, error) {
	switch o {
	case confirmed:
		return []byte("true"), nil
	case notApplicable:
		return []byte(`"n/a"`), nil
	}
	return []byte("false"), nil
}

// confirmedText describes whether a result is confirmed onchain, for the text
// output.
func confirmedText(o onchain) string {
	switch o {
	case confirmed:
		return "confirmed"
	case notApplicable:
		return "n/a: nothing onchain confirms it"
	}
	return "not confirmed"
}

// explorerRef is the chain and block that an explorer command reads, and the
// client of Etherscan for the chain.
type explorerRef struct {
	// SafeChainID and SafeBlock are the request's Safe chain and Safe block.
	SafeChainID, SafeBlock uint64
	Safe                   ethrpc.Address
	// ChainID is the chain that the command reads: the Safe's, or -chain.
	ChainID uint64
	// Block is the cutoff on ChainID, which nothing after is evidence: the Safe
	// block on the Safe's chain, and the last block before the proposal's time on
	// another chain. BlockTime is its timestamp.
	Block     uint64
	BlockTime time.Time
	eth       *ethrpc.Client
	etherscan *etherscan.Client
}

// explorerHeader is the start of the JSON output of every explorer command: the
// chain and block that it read, the account, and the labels of its source.
type explorerHeader struct {
	ChainID     uint64         `json:"chainId"`
	SafeChainID uint64         `json:"safeChainId"`
	SafeBlock   uint64         `json:"safeBlock"`
	Block       uint64         `json:"block"`
	BlockTime   time.Time      `json:"blockTime"`
	Address     ethrpc.Address `json:"address"`
	// Source is always "etherscan", an offchain source (Charter § 3.4).
	Source string `json:"source"`
	etherscan.Observation
	ConfirmedOnchain onchain `json:"confirmedOnchain"`
}

func (x *explorerRef) header(address ethrpc.Address, obs etherscan.Observation, o onchain) explorerHeader {
	return explorerHeader{
		ChainID: x.ChainID, SafeChainID: x.SafeChainID, SafeBlock: x.SafeBlock, Block: x.Block, BlockTime: x.BlockTime,
		Address: address, Source: etherscan.Source, Observation: obs, ConfirmedOnchain: o,
	}
}

// explorerFlags returns the flag set of an explorer command, with its flags,
// which every subcommand shares.
func explorerFlags(e *env, name string) (flags *flag.FlagSet, asJSON *bool, requestFile *string, chain *uint64) {
	flags = e.flagSet(name)
	asJSON = flags.Bool("json", false, "write the result as JSON")
	requestFile = flags.String("request-file", "", "read the Safe, its chain, and the block from the request in the JSON file at `path`, as written by arbot info -json")
	chain = flags.Uint64("chain", 0, "read the network with this chain `id`, which the Charter covers, at its last block before the proposal's time (default: the Safe's chain, at the Safe block)")
	return flags, asJSON, requestFile, chain
}

// explorerCommand runs an explorer subcommand: it reads Etherscan's index of an
// account, which is an offchain source, as of the request's Safe block.
func explorerCommand(ctx context.Context, e *env, args []string) error {
	usage := func(flags *flag.FlagSet) {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s explorer <command> [flags] -request-file <path> <address>\n\n", e.progname)
		fmt.Fprintf(out, "Reads Etherscan's index of an account on the Safe's chain of a request, or with -chain, on another\n")
		fmt.Fprintf(out, "network that the Charter covers, cut at the request's Safe block, or the last block before the\n")
		fmt.Fprintf(out, "proposal's time on another chain: nothing after it is evidence (§ 3.6). Etherscan is an offchain\n")
		fmt.Fprintf(out, "source (§ 3.4), so every result has its URL, without the API key, when it was observed, and whether\n")
		fmt.Fprintf(out, "it is confirmed onchain. It needs an Etherscan API key, the etherscan setting of the config file:\n")
		fmt.Fprintf(out, "without one, it fails, and the evidence is a gap, \"Etherscan not configured\".\n\nCommands:\n")
		for _, name := range slices.Sorted(maps.Keys(explorerCommands)) {
			fmt.Fprintf(out, "  %-9s %s\n", name, explorerCommands[name].description)
		}
		fmt.Fprintf(out, "\nFlags:\n")
		flags.PrintDefaults()
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		flags, _, _, _ := explorerFlags(e, "explorer")
		flags.Usage = func() { usage(flags) }
		if err := parse(flags, args, -1); err != nil {
			return err
		}
		flags.Usage()
		return usageError("")
	}
	command, ok := explorerCommands[args[0]]
	if !ok {
		return usageError(fmt.Sprintf("unknown explorer command %q", args[0]))
	}
	flags, asJSON, requestFile, chain := explorerFlags(e, "explorer "+args[0])
	flags.Usage = func() { usage(flags) }
	if err := parse(flags, args[1:], 1); err != nil {
		return err
	}
	address, err := ethrpc.ParseAddress(flags.Arg(0))
	if err != nil {
		return usageError(fmt.Sprintf("address %q: %v", flags.Arg(0), err))
	}
	if e.cfg.Etherscan.APIKey == "" {
		return errEtherscanNotConfigured
	}
	x, err := openExplorer(ctx, e, *requestFile, *chain)
	if err != nil {
		return err
	}
	return command.run(ctx, e, x, address, *asJSON)
}

// openExplorer reads the request in the file at requestFile, and returns the
// chain to read, chain or else the Safe's, with its cutoff block.
func openExplorer(ctx context.Context, e *env, requestFile string, chain uint64) (*explorerRef, error) {
	if requestFile == "" {
		return nil, usageError("-request-file is required")
	}
	if chain != 0 && !checks.Covered(chain) {
		return nil, usageError(fmt.Sprintf("-chain %d isn't a network that the Charter covers", chain))
	}
	request, err := readRequest(requestFile)
	if err != nil {
		return nil, err
	}
	p := request.Proposal
	if p.Transaction.ChainID == nil || !p.Transaction.ChainID.IsUint64() || p.SafeBlock == 0 {
		return nil, fmt.Errorf("request in %s has no Safe chain ID or block", requestFile)
	}
	x := &explorerRef{SafeChainID: p.Transaction.ChainID.Uint64(), SafeBlock: p.SafeBlock, Safe: p.Transaction.Safe}
	x.ChainID = cmp.Or(chain, x.SafeChainID)
	if x.eth, err = ethrpc.NewDialer(e.cfg.RPCs)(ctx, x.ChainID); err != nil {
		return nil, err
	}
	switch {
	case x.ChainID == x.SafeChainID:
		x.Block = p.SafeBlock
	case x.ChainID == ethrpc.Mainnet && p.EthereumBlock != 0:
		// The request's Ethereum block is the last block before the proposal's time, as
		// for any other chain.
		x.Block = p.EthereumBlock
	case p.Time.IsZero():
		return nil, fmt.Errorf("request in %s has no proposal time, such as a test vector's, so it has no block on chain %d", requestFile, x.ChainID)
	default:
		block, err := x.eth.SearchBlock(ctx, p.Time)
		if err != nil {
			return nil, fmt.Errorf("finding the last block before the proposal's time on chain %d: %w", x.ChainID, err)
		}
		x.Block = uint64(block.Number)
	}
	if x.BlockTime, err = x.eth.BlockTime(ctx, ethrpc.BlockNumber(x.Block)); err != nil {
		return nil, fmt.Errorf("reading the time of block %d of chain %d: %w", x.Block, x.ChainID, err)
	}
	x.etherscan = etherscan.NewClient(cmp.Or(e.cfg.Etherscan.URL, etherscan.DefaultURL), e.cfg.Etherscan.APIKey, x.ChainID)
	return x, nil
}

// account reads address on the chain of x at its block, as arbot account does.
func (x *explorerRef) account(ctx context.Context, address ethrpc.Address) (checks.Account, error) {
	account, err := checks.NewCallReader(x.eth, x.Safe, ethrpc.BlockNumber(x.Block)).Account(ctx, address)
	if err != nil {
		return checks.Account{}, fmt.Errorf("reading the code of %s at block %d: %w", address, x.Block, err)
	}
	return account, nil
}

// writeExplorerJSON writes the JSON output of an explorer command, with the
// characters of its URLs, such as &, as they are.
func writeExplorerJSON(e *env, v any) error {
	encoder := json.NewEncoder(e.stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(v)
}

// printHeader prints the account, the chain and block, and the labels of the
// source of an explorer command's result.
func (e *env) printHeader(title string, h explorerHeader) {
	fmt.Fprintf(e.stdout, "%s of %s on chain %d, up to block %d (%s)\n", title, h.Address, h.ChainID, h.Block, h.BlockTime.Format(time.RFC3339))
	if h.ChainID != h.SafeChainID {
		fmt.Fprintf(e.stdout, "  The Safe's chain is %d, whose Safe block is %d; block %d is the last before the proposal's time\n", h.SafeChainID, h.SafeBlock, h.Block)
	}
	fmt.Fprintf(e.stdout, "  Source     %s (offchain, § 3.4), %s\n", h.Source, h.URL)
	fmt.Fprintf(e.stdout, "  Observed   %s\n", h.ObservedAt.Format(time.RFC3339))
	fmt.Fprintf(e.stdout, "  Onchain    %s\n", confirmedText(h.ConfirmedOnchain))
}

// printNotes prints the notes of an explorer command's result.
func (e *env) printNotes(notes []string) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(e.stdout, "\nNotes\n")
	for _, note := range notes {
		fmt.Fprintf(e.stdout, "  - %s\n", note)
	}
}

// creationReport is the JSON output of `arbot explorer creation`.
type creationReport struct {
	explorerHeader
	// Account is the account at the block, read onchain, as arbot account reads it.
	Account checks.Account `json:"account"`
	// Creation is the creation that Etherscan reports, or nil if it has none.
	Creation *creationJSON `json:"creation"`
	// Confirmation is what the chain says about the creation, or nil if it isn't
	// checked, for no creation or one after the block.
	Confirmation *etherscan.CreationCheck `json:"confirmation"`
	Notes        []string                 `json:"notes"`
}

// creationJSON is a creation that Etherscan reports.
type creationJSON struct {
	Creator   ethrpc.Address  `json:"creator"`
	Factory   *ethrpc.Address `json:"factory"`
	TxHash    ethrpc.Hash     `json:"txHash"`
	Block     uint64          `json:"block"`
	BlockTime time.Time       `json:"blockTime"`
	// AfterCutoff is whether the creation is after the block, so that it isn't
	// evidence (§ 3.6).
	AfterCutoff bool `json:"afterCutoff"`
}

func explorerCreation(ctx context.Context, e *env, x *explorerRef, address ethrpc.Address, asJSON bool) error {
	account, err := x.account(ctx, address)
	if err != nil {
		return err
	}
	creation, obs, err := x.etherscan.Creation(ctx, address)
	if err != nil {
		return err
	}
	report := creationReport{explorerHeader: x.header(address, obs, notApplicable), Account: account, Notes: []string{}}
	switch {
	case creation == nil && account.CodeSize > 0:
		report.Notes = append(report.Notes, "Etherscan reports no creation of the contract, such as for a contract that it hasn't indexed or one of the chain's genesis: its creator and age are a gap.")
	case creation == nil:
		report.Notes = append(report.Notes, fmt.Sprintf("The account has no code at block %d, and Etherscan reports creations of contracts only.", x.Block))
	default:
		report.Creation = &creationJSON{
			Creator: creation.Creator, Factory: creation.Factory, TxHash: creation.TxHash,
			Block: creation.Block, BlockTime: creation.Time, AfterCutoff: creation.Block > x.Block,
		}
		if report.Creation.AfterCutoff {
			report.Notes = append(report.Notes, fmt.Sprintf("Etherscan reports a creation in block %d, after block %d: it isn't evidence (§ 3.6), and isn't checked onchain.", creation.Block, x.Block))
			break
		}
		if account.CodeSize == 0 {
			report.Notes = append(report.Notes, fmt.Sprintf("The account has no code at block %d, after the creation, such as after a self-destruct.", x.Block))
		}
		if report.Confirmation, err = etherscan.ConfirmCreation(ctx, x.eth, address, creation); err != nil {
			return err
		}
		report.ConfirmedOnchain = notConfirmed
		if report.Confirmation.Confirmed {
			report.ConfirmedOnchain = confirmed
		}
		if creation.Factory != nil {
			report.Notes = append(report.Notes, "The factory is as Etherscan reports it: a receipt doesn't say which contract deployed an account, so it isn't checked onchain.")
		}
	}

	if asJSON {
		return writeExplorerJSON(e, report)
	}
	e.printHeader("Creation", report.explorerHeader)
	if account.CodeSize == 0 {
		fmt.Fprintf(e.stdout, "  Code       none at block %d\n", x.Block)
	} else {
		fmt.Fprintf(e.stdout, "  Code       %d bytes at block %d, hash %s\n", account.CodeSize, x.Block, account.CodeHash)
	}
	if c := report.Creation; c != nil {
		fmt.Fprintf(e.stdout, "\nCreation, as Etherscan reports it\n")
		fmt.Fprintf(e.stdout, "  Creator      %s\n", c.Creator)
		if c.Factory != nil {
			fmt.Fprintf(e.stdout, "  Factory      %s\n", c.Factory)
		} else {
			fmt.Fprintf(e.stdout, "  Factory      none\n")
		}
		fmt.Fprintf(e.stdout, "  Transaction  %s\n", c.TxHash)
		fmt.Fprintf(e.stdout, "  Block        %d (%s)\n", c.Block, c.BlockTime.Format(time.RFC3339))
		if check := report.Confirmation; check != nil {
			fmt.Fprintf(e.stdout, "\nOnchain confirmation\n")
			fmt.Fprintf(e.stdout, "  Receipt in the block       %s\n", checkText(check.Receipt))
			fmt.Fprintf(e.stdout, "  Sent by the creator        %s\n", checkText(check.Sender))
			fmt.Fprintf(e.stdout, "  No code the block before   %s\n", checkText(negate(check.CodeBefore)))
			fmt.Fprintf(e.stdout, "  Code at the block          %s\n", checkText(check.CodeAt))
			if check.Reason != "" {
				fmt.Fprintf(e.stdout, "  Not confirmed: %s\n", check.Reason)
			}
		}
	}
	e.printNotes(report.Notes)
	return nil
}

func negate(b *bool) *bool {
	if b == nil {
		return nil
	}
	return new(!*b)
}

// checkText returns the result of an onchain check, or "unread" if it couldn't
// be read.
func checkText(b *bool) string {
	switch {
	case b == nil:
		return "unread"
	case *b:
		return "yes"
	}
	return "no"
}

// listingReport is the start of the JSON output of `arbot explorer txs` and
// `arbot explorer tokentx`, which their results follow.
type listingReport struct {
	explorerHeader
	Count int `json:"count"`
	// Capped is whether the listing has MaxResults results, so that it may be
	// incomplete.
	Capped     bool     `json:"capped"`
	MaxResults int      `json:"maxResults"`
	Notes      []string `json:"notes"`
}

// listingNotes returns the notes of a listing of results of a kind, which is
// capped or not.
func listingNotes(capped bool, kind string) []string {
	notes := []string{fmt.Sprintf("Etherscan's listing of %s isn't checked onchain: confirm one that matters, such as with arbot events on the Safe's chain.", kind)}
	if capped {
		notes = append(notes, fmt.Sprintf("The listing has %d results, as many as Etherscan returns, starting from the oldest: it may be incomplete, and those after the last result are missing.", etherscan.MaxResults))
	}
	return notes
}

func explorerTxs(ctx context.Context, e *env, x *explorerRef, address ethrpc.Address, asJSON bool) error {
	listing, obs, err := x.etherscan.Transactions(ctx, address, x.Block)
	if err != nil {
		return err
	}
	notes := listingNotes(listing.Capped, "transactions")
	notes = append(notes, "Etherscan lists the transactions that the account sent or that were sent to it, not internal calls; a function name is Etherscan's decoding of the selector.")
	report := struct {
		listingReport
		Transactions []etherscan.Transaction `json:"transactions"`
	}{
		listingReport{
			explorerHeader: x.header(address, obs, notConfirmed), Count: len(listing.Results),
			Capped: listing.Capped, MaxResults: etherscan.MaxResults, Notes: notes,
		},
		listing.Results,
	}
	if asJSON {
		return writeExplorerJSON(e, report)
	}
	e.printHeader("Transactions", report.explorerHeader)
	fmt.Fprintf(e.stdout, "  Count      %d%s\n\n", report.Count, cappedText(listing.Capped))
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BLOCK\tTIME\tTRANSACTION\tNONCE\tFROM\tTO\tVALUE\tMETHOD\tRESULT")
	for _, t := range listing.Results {
		to := "-"
		if t.To != nil {
			to = t.To.String()
		} else if t.ContractAddress != nil {
			to = "creates " + t.ContractAddress.String()
		}
		method := t.MethodID
		if t.FunctionName != "" {
			method += " " + strings.SplitN(t.FunctionName, "(", 2)[0]
		}
		result := "success"
		if !t.Success {
			result = "failed"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n", t.Block, t.BlockTime.Format(time.RFC3339), t.TxHash, t.Nonce, t.From, to, t.Value, method, result)
	}
	w.Flush()
	e.printNotes(report.Notes)
	return nil
}

func explorerTokenTx(ctx context.Context, e *env, x *explorerRef, address ethrpc.Address, asJSON bool) error {
	listing, obs, err := x.etherscan.TokenTransfers(ctx, address, x.Block)
	if err != nil {
		return err
	}
	notes := listingNotes(listing.Capped, "transfers")
	notes = append(notes, "Any contract can log a Transfer event, such as a fake token in an address poisoning attack, so the token matters: its name, symbol, and decimals are what the contract claims.")
	report := struct {
		listingReport
		Transfers []etherscan.TokenTransfer `json:"transfers"`
	}{
		listingReport{
			explorerHeader: x.header(address, obs, notConfirmed), Count: len(listing.Results),
			Capped: listing.Capped, MaxResults: etherscan.MaxResults, Notes: notes,
		},
		listing.Results,
	}
	if asJSON {
		return writeExplorerJSON(e, report)
	}
	e.printHeader("ERC-20 transfers", report.explorerHeader)
	fmt.Fprintf(e.stdout, "  Count      %d%s\n\n", report.Count, cappedText(listing.Capped))
	w := tabwriter.NewWriter(e.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BLOCK\tTIME\tTRANSACTION\tTOKEN\tCLAIMED SYMBOL\tFROM\tTO\tVALUE")
	for _, t := range listing.Results {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", t.Block, t.BlockTime.Format(time.RFC3339), t.TxHash, t.Token, fmt.Sprintf("%q", t.TokenSymbol), t.From, t.To, t.Value)
	}
	w.Flush()
	e.printNotes(report.Notes)
	return nil
}

func cappedText(capped bool) string {
	if capped {
		return fmt.Sprintf(", capped at %d: the history may be incomplete", etherscan.MaxResults)
	}
	return ""
}

// sourceReport is the JSON output of `arbot explorer source`.
type sourceReport struct {
	explorerHeader
	// Account is the account at the block, read onchain.
	Account  checks.Account `json:"account"`
	Verified bool           `json:"verified"`
	// Contract is what Etherscan reports about the verified contract, or nil if it
	// isn't verified.
	Contract       *sourceContract `json:"contract"`
	Proxy          bool            `json:"proxy"`
	Implementation *ethrpc.Address `json:"implementation"`
	Notes          []string        `json:"notes"`
}

// sourceContract is a contract that Etherscan verified.
type sourceContract struct {
	Name         string `json:"name"`
	FileName     string `json:"fileName"`
	Compiler     string `json:"compiler"`
	CompilerType string `json:"compilerType"`
	Optimization bool   `json:"optimization"`
	Runs         uint64 `json:"runs"`
	EVMVersion   string `json:"evmVersion"`
	License      string `json:"license"`
}

func explorerSource(ctx context.Context, e *env, x *explorerRef, address ethrpc.Address, asJSON bool) error {
	account, err := x.account(ctx, address)
	if err != nil {
		return err
	}
	contract, obs, err := x.etherscan.Contract(ctx, address)
	if err != nil {
		return err
	}
	report := sourceReport{
		explorerHeader: x.header(address, obs, notApplicable), Account: account,
		Verified: contract.Verified, Proxy: contract.Proxy, Implementation: contract.Implementation,
		Notes: []string{"Etherscan's record is of the address as of when it was observed, not of the code at the block, and has no verification time: a contract verified after the proposal is later evidence (§ 3.6). Nothing onchain confirms it: arbot signatures and contract-source check Sourcify's against the code."},
	}
	if contract.Verified {
		report.Contract = &sourceContract{
			Name: contract.Name, FileName: contract.FileName, Compiler: contract.Compiler, CompilerType: contract.CompilerType,
			Optimization: contract.Optimization, Runs: contract.Runs, EVMVersion: contract.EVMVersion, License: contract.License,
		}
		if account.CodeSize == 0 {
			report.Notes = append(report.Notes, fmt.Sprintf("The account has no code at block %d, so the verified contract is code that was deployed after it.", x.Block))
		}
	}
	if contract.Proxy {
		report.Notes = append(report.Notes, "Etherscan reads the contract as a proxy, and its implementation as of when it was observed, not as of the block.")
	}
	if asJSON {
		return writeExplorerJSON(e, report)
	}
	e.printHeader("Verification", report.explorerHeader)
	if account.CodeSize == 0 {
		fmt.Fprintf(e.stdout, "  Code       none at block %d\n", x.Block)
	} else {
		fmt.Fprintf(e.stdout, "  Code       %d bytes at block %d, hash %s\n", account.CodeSize, x.Block, account.CodeHash)
	}
	if c := report.Contract; c != nil {
		fmt.Fprintf(e.stdout, "\nVerified, as Etherscan reports it\n")
		fmt.Fprintf(e.stdout, "  Contract   %s\n", printable(c.Name))
		if c.FileName != "" {
			fmt.Fprintf(e.stdout, "  File       %s\n", printable(c.FileName))
		}
		fmt.Fprintf(e.stdout, "  Compiler   %s %s, optimization %t, %d runs, EVM %s\n", printable(c.CompilerType), printable(c.Compiler), c.Optimization, c.Runs, printable(c.EVMVersion))
		fmt.Fprintf(e.stdout, "  License    %s\n", printable(cmp.Or(c.License, "-")))
	} else {
		fmt.Fprintf(e.stdout, "\nNot verified on Etherscan\n")
	}
	if report.Proxy {
		implementation := "none named"
		if report.Implementation != nil {
			implementation = report.Implementation.String()
		}
		fmt.Fprintf(e.stdout, "  Proxy      yes, implementation %s\n", implementation)
	}
	e.printNotes(report.Notes)
	return nil
}
