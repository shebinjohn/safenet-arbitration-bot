package etherscan

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// Creation is the creation of a contract, as Etherscan reports it.
type Creation struct {
	// Creator is the account that Etherscan names as the contract's creator: the
	// sender of the creation transaction.
	Creator ethrpc.Address
	// Factory is the contract that deployed it, if a contract did, such as a proxy
	// factory, or nil if the creation transaction itself did.
	Factory *ethrpc.Address
	TxHash  ethrpc.Hash
	Block   uint64
	// Time is the block's timestamp, as Etherscan reports it.
	Time time.Time
}

// Creation returns the creation of the contract at address that Etherscan
// indexed, or nil if it has none, such as for an account without code, a
// contract that it hasn't indexed, or one in the chain's genesis.
func (c *Client) Creation(ctx context.Context, address ethrpc.Address) (*Creation, Observation, error) {
	params := url.Values{"module": {"contract"}, "action": {"getcontractcreation"}, "contractaddresses": {address.String()}}
	var results []struct {
		ContractAddress ethrpc.Address `json:"contractAddress"`
		ContractCreator ethrpc.Address `json:"contractCreator"`
		TxHash          ethrpc.Hash    `json:"txHash"`
		BlockNumber     string         `json:"blockNumber"`
		Timestamp       string         `json:"timestamp"`
		ContractFactory string         `json:"contractFactory"`
	}
	obs, err := c.get(ctx, params, &results)
	if errors.Is(err, errEmpty) {
		return nil, obs, nil
	} else if err != nil {
		return nil, obs, err
	}
	for _, r := range results {
		if r.ContractAddress != address {
			continue
		}
		creation := &Creation{Creator: r.ContractCreator, TxHash: r.TxHash}
		var errs []error
		creation.Block, err = parseUint(r.BlockNumber)
		errs = append(errs, err)
		creation.Time, err = parseTime(r.Timestamp)
		errs = append(errs, err)
		if r.ContractFactory != "" {
			factory, err := ethrpc.ParseAddress(r.ContractFactory)
			errs = append(errs, err)
			creation.Factory = &factory
		}
		if err := errors.Join(errs...); err != nil {
			return nil, obs, c.invalid(obs, err)
		}
		return creation, obs, nil
	}
	// An answer about other contracts says nothing about this one.
	return nil, obs, c.invalid(obs, fmt.Errorf("no creation of %s in the result", address))
}

// Transaction is a transaction of an account, as Etherscan lists it.
type Transaction struct {
	Block            uint64         `json:"block"`
	BlockTime        time.Time      `json:"blockTime"`
	TxHash           ethrpc.Hash    `json:"txHash"`
	TransactionIndex uint64         `json:"transactionIndex"`
	Nonce            uint64         `json:"nonce"`
	From             ethrpc.Address `json:"from"`
	// To is nil for a transaction that creates a contract, whose address is
	// ContractAddress.
	To              *ethrpc.Address `json:"to"`
	ContractAddress *ethrpc.Address `json:"contractAddress"`
	Value           *big.Int        `json:"value"`
	// MethodID is the first 4 bytes of the transaction's data, or "0x" if it has
	// fewer, and FunctionName is the function that Etherscan names for it, or "",
	// which is its own decoding of the selector, not the contract's ABI.
	MethodID     string `json:"methodId"`
	FunctionName string `json:"functionName"`
	// InputSize is the size of the transaction's data, in bytes.
	InputSize int `json:"inputSize"`
	// Success is whether the transaction succeeded, as Etherscan reports it.
	Success bool `json:"success"`
}

// TokenTransfer is an ERC-20 Transfer event that involves an account, as
// Etherscan lists it. Any contract can log a Transfer event, so the token
// matters, and its name, symbol, and decimals are what the contract claims.
type TokenTransfer struct {
	Block     uint64         `json:"block"`
	BlockTime time.Time      `json:"blockTime"`
	TxHash    ethrpc.Hash    `json:"txHash"`
	Token     ethrpc.Address `json:"token"`
	// TokenName, TokenSymbol, and TokenDecimals are what the token's contract
	// returned to Etherscan. TokenDecimals is nil if it returned none.
	TokenName     string         `json:"tokenName"`
	TokenSymbol   string         `json:"tokenSymbol"`
	TokenDecimals *uint64        `json:"tokenDecimals"`
	From          ethrpc.Address `json:"from"`
	To            ethrpc.Address `json:"to"`
	// Value is the amount in the token's smallest unit.
	Value *big.Int `json:"value"`
}

// MaxResults is the most results that Etherscan returns for a listing: the page
// number times the page size can't exceed it.
const MaxResults = 10_000

// pageSize is the number of results that a listing asks for in each request.
const pageSize = 1_000

// Listing is the results of a listing of an account's history up to a block.
type Listing[T any] struct {
	Results []T
	// Capped is whether the listing has MaxResults results, so that Etherscan may
	// have more that it doesn't return, and the history may be incomplete.
	Capped bool
}

// Transactions returns the normal transactions that Etherscan lists for
// address, sent by it or to it, in blocks up to endBlock, in the order of their
// blocks and positions, as many as MaxResults.
func (c *Client) Transactions(ctx context.Context, address ethrpc.Address, endBlock uint64) (*Listing[Transaction], Observation, error) {
	type row struct {
		BlockNumber      string         `json:"blockNumber"`
		TimeStamp        string         `json:"timeStamp"`
		Hash             ethrpc.Hash    `json:"hash"`
		Nonce            string         `json:"nonce"`
		TransactionIndex string         `json:"transactionIndex"`
		From             ethrpc.Address `json:"from"`
		To               string         `json:"to"`
		ContractAddress  string         `json:"contractAddress"`
		Value            string         `json:"value"`
		Input            string         `json:"input"`
		MethodID         string         `json:"methodId"`
		FunctionName     string         `json:"functionName"`
		IsError          string         `json:"isError"`
	}
	block := func(t Transaction) uint64 { return t.Block }
	listing, obs, err := list(ctx, c, "txlist", address, endBlock, block, func(r row) (Transaction, error) {
		t := Transaction{TxHash: r.Hash, From: r.From, FunctionName: r.FunctionName}
		var errs []error
		var err error
		t.Block, err = parseUint(r.BlockNumber)
		errs = append(errs, err)
		t.BlockTime, err = parseTime(r.TimeStamp)
		errs = append(errs, err)
		t.Nonce, err = parseUint(r.Nonce)
		errs = append(errs, err)
		t.TransactionIndex, err = parseUint(r.TransactionIndex)
		errs = append(errs, err)
		t.To, err = optionalAddress(r.To)
		errs = append(errs, err)
		t.ContractAddress, err = optionalAddress(r.ContractAddress)
		errs = append(errs, err)
		t.Value, err = parseBig(r.Value)
		errs = append(errs, err)
		var input ethrpc.Bytes
		errs = append(errs, input.UnmarshalText([]byte(r.Input)))
		t.InputSize = len(input)
		t.MethodID = "0x"
		if len(input) >= 4 {
			t.MethodID = ethrpc.Bytes(input[:4]).String()
		}
		switch r.IsError {
		case "0":
			t.Success = true
		case "1":
		default:
			errs = append(errs, fmt.Errorf("invalid isError %q", r.IsError))
		}
		return t, errors.Join(errs...)
	})
	if listing != nil {
		slices.SortStableFunc(listing.Results, func(a, b Transaction) int {
			return cmp.Or(cmp.Compare(a.Block, b.Block), cmp.Compare(a.TransactionIndex, b.TransactionIndex))
		})
	}
	return listing, obs, err
}

// TokenTransfers returns the ERC-20 transfers that Etherscan lists for address,
// from it or to it, in blocks up to endBlock, in the order of their blocks, as
// many as MaxResults.
func (c *Client) TokenTransfers(ctx context.Context, address ethrpc.Address, endBlock uint64) (*Listing[TokenTransfer], Observation, error) {
	type row struct {
		BlockNumber     string         `json:"blockNumber"`
		TimeStamp       string         `json:"timeStamp"`
		Hash            ethrpc.Hash    `json:"hash"`
		ContractAddress ethrpc.Address `json:"contractAddress"`
		TokenName       string         `json:"tokenName"`
		TokenSymbol     string         `json:"tokenSymbol"`
		TokenDecimal    string         `json:"tokenDecimal"`
		From            ethrpc.Address `json:"from"`
		To              ethrpc.Address `json:"to"`
		Value           string         `json:"value"`
	}
	block := func(t TokenTransfer) uint64 { return t.Block }
	listing, obs, err := list(ctx, c, "tokentx", address, endBlock, block, func(r row) (TokenTransfer, error) {
		t := TokenTransfer{
			TxHash: r.Hash, Token: r.ContractAddress, TokenName: r.TokenName, TokenSymbol: r.TokenSymbol, From: r.From, To: r.To,
		}
		var errs []error
		var err error
		t.Block, err = parseUint(r.BlockNumber)
		errs = append(errs, err)
		t.BlockTime, err = parseTime(r.TimeStamp)
		errs = append(errs, err)
		t.Value, err = parseBig(r.Value)
		errs = append(errs, err)
		if r.TokenDecimal != "" {
			decimals, err := parseUint(r.TokenDecimal)
			errs = append(errs, err)
			t.TokenDecimals = &decimals
		}
		return t, errors.Join(errs...)
	})
	if listing != nil {
		slices.SortStableFunc(listing.Results, func(a, b TokenTransfer) int { return cmp.Compare(a.Block, b.Block) })
	}
	return listing, obs, err
}

// list returns the results of the account listing action for address in blocks
// up to endBlock, decoding each row with decode, from pages of pageSize up to
// MaxResults. The observation is that of the first page, whose URL names the
// query. A result whose block, as block returns it, is after endBlock, which
// Etherscan shouldn't return, is an error, since it isn't evidence of the
// account as of endBlock, and an answer that has it can't be trusted to cut the
// listing there.
func list[R, T any](ctx context.Context, c *Client, action string, address ethrpc.Address, endBlock uint64, block func(T) uint64, decode func(R) (T, error)) (*Listing[T], Observation, error) {
	listing := &Listing[T]{Results: []T{}}
	var first Observation
	for page := 1; page*pageSize <= MaxResults; page++ {
		params := url.Values{
			"module": {"account"}, "action": {action}, "address": {address.String()},
			"startblock": {"0"}, "endblock": {strconv.FormatUint(endBlock, 10)},
			"page": {strconv.Itoa(page)}, "offset": {strconv.Itoa(pageSize)}, "sort": {"asc"},
		}
		var rows []R
		obs, err := c.get(ctx, params, &rows)
		if page == 1 {
			first = obs
		}
		if errors.Is(err, errEmpty) {
			return listing, first, nil
		} else if err != nil {
			return nil, first, err
		}
		for _, r := range rows {
			t, err := decode(r)
			if err == nil && block(t) > endBlock {
				err = fmt.Errorf("a result in block %d, after the end block %d", block(t), endBlock)
			}
			if err != nil {
				return nil, first, c.invalid(obs, err)
			}
			listing.Results = append(listing.Results, t)
		}
		if len(rows) < pageSize {
			return listing, first, nil
		}
	}
	listing.Capped = true
	return listing, first, nil
}

// Contract is what Etherscan reports about the verification of the contract at
// an address. It is Etherscan's record as of when it was observed, not as of a
// block, and it is of the address, not of a code hash.
type Contract struct {
	// Verified is whether Etherscan has the source code of the contract. The other
	// fields are empty if it hasn't.
	Verified bool
	// Name is the contract's name, and FileName the path of its file in the
	// compilation, if Etherscan reports one.
	Name, FileName string
	// Compiler is the compiler version, such as "v0.8.28+commit.7893614a", and
	// CompilerType the compiler, such as "solc" or "vyper", as Etherscan reports
	// them.
	Compiler, CompilerType string
	// Optimization is whether the compilation was optimized, with Runs runs, and
	// EVMVersion is its EVM version, as Etherscan reports them.
	Optimization bool
	Runs         uint64
	EVMVersion   string
	// License is the license that Etherscan records for the source.
	License string
	// Proxy is whether Etherscan says that the contract is a proxy, and
	// Implementation the implementation that it names for it, or nil. They are
	// Etherscan's reading, as of when it was observed.
	Proxy          bool
	Implementation *ethrpc.Address
}

// Contract returns what Etherscan reports about the verification of the
// contract at address. An account that Etherscan hasn't verified, such as one
// without code, isn't Verified. It doesn't return the source files.
func (c *Client) Contract(ctx context.Context, address ethrpc.Address) (*Contract, Observation, error) {
	params := url.Values{"module": {"contract"}, "action": {"getsourcecode"}, "address": {address.String()}}
	var results []struct {
		SourceCode       string `json:"SourceCode"`
		ABI              string `json:"ABI"`
		ContractName     string `json:"ContractName"`
		CompilerVersion  string `json:"CompilerVersion"`
		CompilerType     string `json:"CompilerType"`
		OptimizationUsed string `json:"OptimizationUsed"`
		Runs             string `json:"Runs"`
		EVMVersion       string `json:"EVMVersion"`
		ContractFileName string `json:"ContractFileName"`
		LicenseType      string `json:"LicenseType"`
		Proxy            string `json:"Proxy"`
		Implementation   string `json:"Implementation"`
	}
	obs, err := c.get(ctx, params, &results)
	if err != nil {
		// An empty answer isn't one that getsourcecode gives, even for an account
		// without code, so it says nothing about the contract either.
		if errors.Is(err, errEmpty) {
			err = c.invalid(obs, errors.New("no result"))
		}
		return nil, obs, err
	}
	if len(results) != 1 {
		return nil, obs, c.invalid(obs, fmt.Errorf("%d results, not 1", len(results)))
	}
	r := results[0]
	contract := &Contract{Verified: r.SourceCode != ""}
	if r.Proxy == "1" {
		contract.Proxy = true
		implementation, err := optionalAddress(r.Implementation)
		if err != nil {
			return nil, obs, c.invalid(obs, err)
		}
		contract.Implementation = implementation
	}
	if !contract.Verified {
		return contract, obs, nil
	}
	contract.Name, contract.FileName = r.ContractName, r.ContractFileName
	contract.Compiler, contract.CompilerType = r.CompilerVersion, r.CompilerType
	contract.Optimization = r.OptimizationUsed == "1"
	if r.Runs != "" {
		if contract.Runs, err = parseUint(r.Runs); err != nil {
			return nil, obs, c.invalid(obs, err)
		}
	}
	contract.EVMVersion, contract.License = r.EVMVersion, r.LicenseType
	return contract, obs, nil
}

// invalid returns the error of an answer that isn't what the API documents,
// which is an error rather than an answer.
func (c *Client) invalid(obs Observation, err error) error {
	return &Error{message: fmt.Sprintf("Etherscan: %s: invalid result: %s", obs.URL, c.redact(err.Error()))}
}

func parseUint(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", s)
	}
	return n, nil
}

func parseBig(s string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 {
		return nil, fmt.Errorf("invalid amount %q", s)
	}
	return n, nil
}

// parseTime parses a Unix timestamp in seconds.
func parseTime(s string) (time.Time, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", s)
	}
	return time.Unix(n, 0).UTC(), nil
}

// optionalAddress parses an address that Etherscan leaves empty if there is
// none.
func optionalAddress(s string) (*ethrpc.Address, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	a, err := ethrpc.ParseAddress(s)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q", s)
	}
	return &a, nil
}
