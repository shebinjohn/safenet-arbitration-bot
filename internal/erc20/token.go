package erc20

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// The view functions of ERC-20 that Read calls.
var (
	nameSelector      = solabi.Selector("name()")
	symbolSelector    = solabi.Selector("symbol()")
	decimalsSelector  = solabi.Selector("decimals()")
	balanceOfSelector = solabi.Selector("balanceOf(address)")
)

// Token is what an ERC-20 token contract returns about itself, and a holder's
// balance of it. Any contract can return any name and symbol, so they say what
// the contract claims to be, not what it is.
type Token struct {
	// Name and Symbol are empty if the contract doesn't return a string for them,
	// since they are optional in ERC-20.
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
	// Balance is the holder's balance, in the token's smallest unit.
	Balance *big.Int `json:"balance"`
}

// Read calls the ERC-20 view functions of the account token as of block, for
// its name, symbol, decimals, and the balance of holder. It returns nil if the
// account doesn't answer decimals and balanceOf as an ERC-20 token does, such
// as an account without code, or one whose calls revert.
func Read(ctx context.Context, eth *ethrpc.Client, token, holder ethrpc.Address, block ethrpc.BlockNumber) (*Token, error) {
	call := func(data []byte) (*solabi.Decoder, bool, error) {
		result, err := eth.Call(ctx, ethrpc.CallRequest{To: token, Data: data}, block)
		if err != nil {
			if reverted(err) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("calling %s at block %d: %w", token, block, err)
		}
		return solabi.NewDecoder(result), true, nil
	}

	d, ok, err := call(solabi.Call(decimalsSelector))
	if err != nil || !ok {
		return nil, err
	}
	decimals := d.Uint64(0)
	if d.Err() != nil || decimals > 255 {
		return nil, nil
	}
	if d, ok, err = call(solabi.Call(balanceOfSelector, holder)); err != nil || !ok {
		return nil, err
	}
	t := &Token{Decimals: uint8(decimals), Balance: d.Uint(0)}
	if d.Err() != nil {
		return nil, nil
	}
	for _, s := range []struct {
		selector [4]byte
		value    *string
	}{{nameSelector, &t.Name}, {symbolSelector, &t.Symbol}} {
		d, ok, err := call(solabi.Call(s.selector))
		if err != nil {
			return nil, err
		}
		if ok {
			if value := d.String(0); d.Err() == nil {
				*s.value = value
			}
		}
	}
	return t, nil
}

// reverted reports whether err is the error of a call that reverted, which
// nodes report as JSON-RPC error 3, or with a message about the revert.
func reverted(err error) bool {
	var rpcErr *ethrpc.Error
	return errors.As(err, &rpcErr) && (rpcErr.Code == 3 || strings.Contains(strings.ToLower(rpcErr.Message), "revert"))
}
