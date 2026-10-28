// Package txservice is a client for the Safe Transaction Service, which indexes
// the Safe transactions that owners have signed and submitted, executed or not.
//
// The service is not trusted: it is an offchain source (Charter § 3.4). Callers
// check what it returns against onchain data, such as the hash of the Safe
// transaction.
package txservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/httpretry"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// DefaultURLs are the base URLs of Safe's public transaction services, by chain
// ID, for the networks that the Charter covers. They need no API key.
var DefaultURLs = map[uint64]string{
	ethrpc.Mainnet:     "https://api.safe.global/tx-service/eth",
	ethrpc.ArbitrumOne: "https://api.safe.global/tx-service/arb1",
	ethrpc.Gnosis:      "https://api.safe.global/tx-service/gno",
}

// maxResponseSize is the largest response that the client accepts. A Safe
// transaction has calldata, which is bounded by the size of a block.
const maxResponseSize = 8 << 20

// ErrNotFound is the error for a transaction that the service doesn't have.
var ErrNotFound = errors.New("transaction not found")

// Client fetches Safe transactions from a Safe Transaction Service.
type Client struct {
	http    *http.Client
	baseURL string
	chainID *big.Int
}

// NewClient returns a client for the service at the base URL baseURL, such as
// "https://api.safe.global/tx-service/eth", which serves the Safes of the chain
// with the given ID.
func NewClient(baseURL string, chainID uint64) *Client {
	return &Client{http: httpretry.Client, baseURL: baseURL, chainID: new(big.Int).SetUint64(chainID)}
}

// Transaction returns the Safe transaction that the service has with the Safe
// transaction hash safeTxHash, or ErrNotFound. The transaction's fields are as
// the service reports them, so it may not have the hash it was asked for.
func (c *Client) Transaction(ctx context.Context, safeTxHash ethrpc.Hash) (*safenet.SafeTransaction, error) {
	u, err := url.JoinPath(c.baseURL, "api", "v1", "multisig-transactions", safeTxHash.String())
	if err != nil {
		return nil, err
	}
	// The service redirects paths without a trailing slash.
	var t transaction
	if err := c.get(ctx, u+"/", &t); err != nil {
		return nil, err
	}
	return t.safeTransaction(c.chainID), nil
}

// Execution is a Safe transaction that the service lists as executed, with the
// onchain transaction and block that it says executed it.
type Execution struct {
	SafeTxHash  ethrpc.Hash
	TxHash      ethrpc.Hash
	Block       uint64
	Transaction *safenet.SafeTransaction
}

// pageSize is the number of executions or transfers that Executions and
// Transfers ask for in each request, and maxExecutions bounds the number of
// executions that Executions lists.
const (
	pageSize      = 100
	maxExecutions = 100_000
)

// Executions returns the Safe transactions of safe that the service lists as
// executed, as it reports them: none of their fields, hashes, or blocks are
// checked.
func (c *Client) Executions(ctx context.Context, safe ethrpc.Address) ([]Execution, error) {
	base, err := url.JoinPath(c.baseURL, "api", "v1", "safes", safe.String(), "multisig-transactions")
	if err != nil {
		return nil, err
	}
	var executions []Execution
	for offset := 0; ; offset += pageSize {
		if offset >= maxExecutions {
			return nil, fmt.Errorf("the service lists more than %d executions", maxExecutions)
		}
		// The pages are by offset rather than the service's next links, so that every
		// request is to the service.
		query := url.Values{
			"executed": {"true"}, "ordering": {"nonce"},
			"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)},
		}
		var page struct {
			Results []struct {
				transaction
				SafeTxHash      ethrpc.Hash  `json:"safeTxHash"`
				TransactionHash *ethrpc.Hash `json:"transactionHash"`
				BlockNumber     *uint64      `json:"blockNumber"`
			} `json:"results"`
		}
		if err := c.get(ctx, base+"/?"+query.Encode(), &page); err != nil {
			return nil, err
		}
		for _, r := range page.Results {
			if r.TransactionHash == nil || r.BlockNumber == nil {
				return nil, fmt.Errorf("executed Safe transaction %s has no transaction hash or block", r.SafeTxHash)
			}
			executions = append(executions, Execution{
				SafeTxHash: r.SafeTxHash, TxHash: *r.TransactionHash, Block: *r.BlockNumber,
				Transaction: r.safeTransaction(c.chainID),
			})
		}
		if len(page.Results) < pageSize {
			return executions, nil
		}
	}
}

// Transfer is a transfer into or out of a Safe that the service lists.
type Transfer struct {
	// Type is the service's type of the transfer, such as "ERC20_TRANSFER",
	// "ETHER_TRANSFER", or "ERC721_TRANSFER".
	Type   string
	Block  uint64
	TxHash ethrpc.Hash
	// Token is the token's contract, or nil for the native currency.
	Token *ethrpc.Address
	From  ethrpc.Address
	To    ethrpc.Address
	// Value is the amount in the token's or the currency's smallest unit, or nil
	// for a transfer without one, such as of an ERC-721 token.
	Value *big.Int
}

// maxTransfers bounds the number of transfers that Transfers lists.
const maxTransfers = 100_000

// Transfers returns the transfers into and out of safe that the service lists,
// as it reports them: none of them are checked.
func (c *Client) Transfers(ctx context.Context, safe ethrpc.Address) ([]Transfer, error) {
	base, err := url.JoinPath(c.baseURL, "api", "v1", "safes", safe.String(), "transfers")
	if err != nil {
		return nil, err
	}
	var transfers []Transfer
	for offset := 0; ; offset += pageSize {
		if offset >= maxTransfers {
			return nil, fmt.Errorf("the service lists more than %d transfers", maxTransfers)
		}
		query := url.Values{"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		var page struct {
			Results []struct {
				Type            string          `json:"type"`
				BlockNumber     uint64          `json:"blockNumber"`
				TransactionHash ethrpc.Hash     `json:"transactionHash"`
				TokenAddress    *ethrpc.Address `json:"tokenAddress"`
				From            ethrpc.Address  `json:"from"`
				To              ethrpc.Address  `json:"to"`
				Value           *number         `json:"value"`
			} `json:"results"`
		}
		if err := c.get(ctx, base+"/?"+query.Encode(), &page); err != nil {
			return nil, err
		}
		for _, r := range page.Results {
			t := Transfer{Type: r.Type, Block: r.BlockNumber, TxHash: r.TransactionHash, Token: r.TokenAddress, From: r.From, To: r.To}
			if r.Value != nil {
				t.Value = r.Value.Int
			}
			transfers = append(transfers, t)
		}
		if len(page.Results) < pageSize {
			return transfers, nil
		}
	}
}

// get fetches the JSON document at u, and decodes it into v. It returns
// ErrNotFound if the service doesn't have it.
func (c *Client) get(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("HTTP %s: %q", resp.Status, snippet(body))
	case len(body) > maxResponseSize:
		return fmt.Errorf("response exceeds %d bytes", maxResponseSize)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// transaction is a Safe transaction as the service encodes it.
type transaction struct {
	Safe           ethrpc.Address `json:"safe"`
	To             ethrpc.Address `json:"to"`
	Value          number         `json:"value"`
	Data           *ethrpc.Bytes  `json:"data"`
	Operation      uint8          `json:"operation"`
	SafeTxGas      number         `json:"safeTxGas"`
	BaseGas        number         `json:"baseGas"`
	GasPrice       number         `json:"gasPrice"`
	GasToken       ethrpc.Address `json:"gasToken"`
	RefundReceiver ethrpc.Address `json:"refundReceiver"`
	Nonce          number         `json:"nonce"`
}

// safeTransaction returns t as a Safe transaction on the chain with the ID
// chainID.
func (t *transaction) safeTransaction(chainID *big.Int) *safenet.SafeTransaction {
	tx := &safenet.SafeTransaction{
		ChainID:        chainID,
		Safe:           t.Safe,
		To:             t.To,
		Value:          t.Value.Int,
		Operation:      safenet.Operation(t.Operation),
		SafeTxGas:      t.SafeTxGas.Int,
		BaseGas:        t.BaseGas.Int,
		GasPrice:       t.GasPrice.Int,
		GasToken:       t.GasToken,
		RefundReceiver: t.RefundReceiver,
		Nonce:          t.Nonce.Int,
	}
	if t.Data != nil {
		tx.Data = *t.Data
	}
	return tx
}

// number is an integer that the service encodes as a JSON number or as a
// string, depending on the field.
type number struct {
	*big.Int
}

func (n *number) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		s = string(bytes.TrimSpace(data))
	}
	n.Int = new(big.Int)
	if _, ok := n.SetString(s, 10); !ok || n.Sign() < 0 {
		return fmt.Errorf("invalid integer %q", s)
	}
	return nil
}

// snippet returns the start of a response body, for an error message, which
// quotes it since it isn't trusted.
func snippet(body []byte) string {
	const limit = 200
	if len(body) > limit {
		body = body[:limit]
	}
	return string(bytes.TrimSpace(body))
}
