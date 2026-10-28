// Package ethrpc is a minimal Ethereum JSON-RPC client over HTTP.
package ethrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/httpretry"
)

// Client sends JSON-RPC requests to a single Ethereum node over HTTP.
type Client struct {
	chainID uint64
	url     string
	http    *http.Client
	id      atomic.Uint64
}

// connectTimeout bounds how long NewClient waits for each default RPC node to
// report its chain ID, so that one unresponsive node doesn't stall it.
var connectTimeout = 1 * time.Second

// NewClient returns a client for the chain with the given ID, using the node at
// url. If url is empty, the client uses the chain's default RPC URLs (see
// DefaultRPCs): it tries them one at a time, in order, and uses the first node
// that answers with the expected chain ID.
//
// It checks that the node serves the expected chain (eth_chainId), since the
// node may come from an untrusted list, and chains share contract addresses and
// call encodings, so a wrong chain would go unnoticed otherwise.
func NewClient(ctx context.Context, chainID uint64, url string) (*Client, error) {
	if url != "" {
		c := &Client{chainID: chainID, url: url, http: httpretry.Client}
		if err := c.checkChainID(ctx); err != nil {
			return nil, err
		}
		return c, nil
	}

	urls, err := DefaultRPCs(ctx, chainID)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, url := range urls {
		c := &Client{chainID: chainID, url: url, http: httpretry.Client}
		checkCtx, cancel := context.WithTimeout(ctx, connectTimeout)
		err := c.checkChainID(checkCtx)
		cancel()
		if err == nil {
			return c, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("no default RPC node for chain %d: %w", chainID, errors.Join(errs...))
}

// checkChainID returns an error unless the client's node serves the client's
// chain.
func (c *Client) checkChainID(ctx context.Context) error {
	var chainID Quantity
	if err := c.RawRequest(ctx, &chainID, "eth_chainId"); err != nil {
		return fmt.Errorf("%s: %w", c.url, err)
	}
	if uint64(chainID) != c.chainID {
		return fmt.Errorf("%s: serves chain %d, want chain %d", c.url, chainID, c.chainID)
	}
	return nil
}

// ChainID returns the ID of the chain that the client's node serves.
func (c *Client) ChainID() uint64 {
	return c.chainID
}

// Call executes a message call against the state at the given block without
// creating a transaction (eth_call), and returns the call's return data.
func (c *Client) Call(ctx context.Context, call CallRequest, block BlockNumber) (Bytes, error) {
	var result Bytes
	err := c.RawRequest(ctx, &result, "eth_call", call, block)
	return result, err
}

// BlockNumber returns the number of the node's most recent block
// (eth_blockNumber).
func (c *Client) BlockNumber(ctx context.Context) (BlockNumber, error) {
	var result BlockNumber
	err := c.RawRequest(ctx, &result, "eth_blockNumber")
	return result, err
}

// GetCode returns the code of the contract at address as of block
// (eth_getCode).
func (c *Client) GetCode(ctx context.Context, address Address, block BlockNumber) (Bytes, error) {
	var result Bytes
	err := c.RawRequest(ctx, &result, "eth_getCode", address, block)
	return result, err
}

// GetBalance returns the balance of address, in wei, as of block
// (eth_getBalance).
func (c *Client) GetBalance(ctx context.Context, address Address, block BlockNumber) (*big.Int, error) {
	var result string
	if err := c.RawRequest(ctx, &result, "eth_getBalance", address, block); err != nil {
		return nil, err
	}
	digits, ok := strings.CutPrefix(result, "0x")
	balance, valid := new(big.Int).SetString(digits, 16)
	if !ok || digits == "" || !valid || balance.Sign() < 0 {
		return nil, fmt.Errorf("eth_getBalance: invalid quantity %q", result)
	}
	return balance, nil
}

// GetTransactionCount returns the nonce of address, the number of transactions
// that it sent, or contracts that it created if it is a contract, as of block
// (eth_getTransactionCount).
func (c *Client) GetTransactionCount(ctx context.Context, address Address, block BlockNumber) (uint64, error) {
	var result Quantity
	err := c.RawRequest(ctx, &result, "eth_getTransactionCount", address, block)
	return uint64(result), err
}

// GetStorageAt returns the value of the storage slot of the contract at address
// as of block (eth_getStorageAt).
func (c *Client) GetStorageAt(ctx context.Context, address Address, slot Hash, block BlockNumber) (Hash, error) {
	var result Hash
	err := c.RawRequest(ctx, &result, "eth_getStorageAt", address, slot, block)
	return result, err
}

// GetLogs returns the logs matching filter (eth_getLogs). Nodes commonly limit
// the block range of a single request, see ScanLogs for querying wider ranges.
func (c *Client) GetLogs(ctx context.Context, filter LogFilter) ([]Log, error) {
	var result []Log
	err := c.RawRequest(ctx, &result, "eth_getLogs", filter)
	return result, err
}

// GetTransactionByHash returns the transaction with the given hash
// (eth_getTransactionByHash).
func (c *Client) GetTransactionByHash(ctx context.Context, hash Hash) (Transaction, error) {
	var result *Transaction
	if err := c.RawRequest(ctx, &result, "eth_getTransactionByHash", hash); err != nil {
		return Transaction{}, err
	}
	if result == nil {
		return Transaction{}, fmt.Errorf("eth_getTransactionByHash: transaction %s not found", hash)
	}
	return *result, nil
}

// GetTransactionReceipt returns the receipt of the mined transaction with the
// given hash (eth_getTransactionReceipt).
func (c *Client) GetTransactionReceipt(ctx context.Context, hash Hash) (Receipt, error) {
	var result *Receipt
	if err := c.RawRequest(ctx, &result, "eth_getTransactionReceipt", hash); err != nil {
		return Receipt{}, err
	}
	if result == nil {
		return Receipt{}, fmt.Errorf("eth_getTransactionReceipt: transaction %s not found", hash)
	}
	return *result, nil
}

// GetBlockByNumber returns the header of the block with the given number
// (eth_getBlockByNumber).
func (c *Client) GetBlockByNumber(ctx context.Context, number BlockNumber) (Block, error) {
	var result *Block
	if err := c.RawRequest(ctx, &result, "eth_getBlockByNumber", number, false); err != nil {
		return Block{}, err
	}
	if result == nil {
		return Block{}, fmt.Errorf("eth_getBlockByNumber: block %d not found", number)
	}
	return *result, nil
}

// FinalizedBlock returns the header of the node's most recent finalized block
// (eth_getBlockByNumber with the "finalized" tag), which a reorg can't remove.
func (c *Client) FinalizedBlock(ctx context.Context) (Block, error) {
	var result *Block
	if err := c.RawRequest(ctx, &result, "eth_getBlockByNumber", "finalized", false); err != nil {
		return Block{}, err
	}
	if result == nil {
		return Block{}, errors.New("eth_getBlockByNumber: no finalized block")
	}
	return *result, nil
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

// RawRequest calls method with params, and decodes its result into result
// unless result is nil. It is for methods that the client has no function for,
// such as a node's own methods. A JSON-RPC error response is returned as an
// *Error.
func (c *Client) RawRequest(ctx context.Context, result any, method string, params ...any) error {
	if params == nil {
		params = []any{}
	}
	id := c.id.Add(1)
	body, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("%s: encoding request: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Some nodes reject requests with an HTTP error status and a JSON-RPC error in
		// the body, such as for a block range that is too large.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var res response
		if json.Unmarshal(body, &res) == nil && res.Error != nil {
			return fmt.Errorf("%s: HTTP %s: %w", method, resp.Status, res.Error)
		}
		return fmt.Errorf("%s: HTTP %s: %s", method, resp.Status, bytes.TrimSpace(body[:min(len(body), 512)]))
	}

	var res response
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return fmt.Errorf("%s: decoding response: %w", method, err)
	}
	if res.ID != id {
		return fmt.Errorf("%s: response ID %d does not match request ID %d", method, res.ID, id)
	}
	if res.Error != nil {
		return fmt.Errorf("%s: %w", method, res.Error)
	}
	if len(res.Result) == 0 {
		return fmt.Errorf("%s: response has neither result nor error", method)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(res.Result, result); err != nil {
		return fmt.Errorf("%s: decoding result: %w", method, err)
	}
	return nil
}
