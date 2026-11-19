package etherscan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// CreationCheck is what the chain says about a creation that Etherscan reports:
// each check is nil if it couldn't be read, such as the code at an old block
// from a node that doesn't keep old states.
type CreationCheck struct {
	// Receipt is whether the creation transaction has a receipt in the creation's
	// block, with a status of success, and Sender whether its sender is the creator
	// that Etherscan names.
	Receipt *bool `json:"receipt"`
	Sender  *bool `json:"sender"`
	// CodeBefore and CodeAt are whether the account has code at the block before
	// the creation's block and at its block. A creation has none before and some at
	// the block.
	CodeBefore *bool `json:"codeBefore"`
	CodeAt     *bool `json:"codeAt"`
	// Confirmed is whether every check passed, and Reason, if not, says why, with
	// each check that failed or couldn't be read.
	Confirmed bool   `json:"-"`
	Reason    string `json:"reason,omitempty"`
}

// ConfirmCreation checks the creation c of the contract at address against the
// chain that eth reads: that the creation transaction succeeded in c's block,
// sent by c's creator, and that the account has no code at the block before it
// and code at it. That the factory, if any, deployed it isn't checked, since
// the receipt doesn't say which contract deployed an account. A request that
// fails rather than being answered is an error, since it says nothing about the
// chain, while one that the node answers with an error, such as for a state
// that it doesn't keep, leaves its check nil, with the error in Reason.
func ConfirmCreation(ctx context.Context, eth *ethrpc.Client, address ethrpc.Address, c *Creation) (*CreationCheck, error) {
	check := &CreationCheck{}
	var reasons []string
	fail := func(format string, args ...any) { reasons = append(reasons, fmt.Sprintf(format, args...)) }

	receipt, err := eth.GetTransactionReceipt(ctx, c.TxHash)
	switch {
	case ethrpc.Transient(err):
		return nil, fmt.Errorf("reading the receipt of the creation transaction %s: %w", c.TxHash, err)
	case errors.Is(err, ethrpc.ErrNotFound):
		check.Receipt = new(false)
		fail("the creation transaction %s has no receipt", c.TxHash)
	case err != nil:
		fail("the receipt of the creation transaction couldn't be read: %v", err)
	default:
		check.Receipt = new(uint64(receipt.BlockNumber) == c.Block && receipt.Status == 1)
		check.Sender = new(receipt.From == c.Creator)
		if uint64(receipt.BlockNumber) != c.Block {
			fail("the creation transaction is in block %d, not %d", receipt.BlockNumber, c.Block)
		} else if receipt.Status != 1 {
			fail("the creation transaction reverted")
		}
		if !*check.Sender {
			fail("the creation transaction was sent by %s, not the creator %s", receipt.From, c.Creator)
		}
	}

	code := func(block uint64) (*bool, error) {
		code, err := eth.GetCode(ctx, address, ethrpc.BlockNumber(block))
		switch {
		case ethrpc.Transient(err):
			return nil, fmt.Errorf("reading the code of %s at block %d: %w", address, block, err)
		case err != nil:
			fail("the code at block %d couldn't be read: %v", block, err)
			return nil, nil
		}
		return new(len(code) > 0), nil
	}
	if c.Block == 0 {
		fail("a creation in block 0 has no block before it")
	} else if check.CodeBefore, err = code(c.Block - 1); err != nil {
		return nil, err
	} else if check.CodeBefore != nil && *check.CodeBefore {
		fail("the account has code at block %d, before the creation", c.Block-1)
	}
	if check.CodeAt, err = code(c.Block); err != nil {
		return nil, err
	} else if check.CodeAt != nil && !*check.CodeAt {
		fail("the account has no code at block %d, the creation's", c.Block)
	}

	check.Confirmed = len(reasons) == 0
	check.Reason = strings.Join(reasons, "; ")
	return check, nil
}
