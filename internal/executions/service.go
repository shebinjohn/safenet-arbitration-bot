package executions

import (
	"cmp"
	"context"
	"fmt"
	"math/big"
	"slices"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// History is a Safe's history of executions, as the Safe Transaction Service
// lists them and the chain confirms them.
type History struct {
	// Executions are the confirmed executions, in the order of their logs.
	Executions []Execution
	// Rejected are the executions that the service lists in the blocks searched but
	// that the chain doesn't confirm.
	Rejected []Rejected
	// Nonce is the Safe's nonce at the last block searched, or nil if it couldn't
	// be read.
	Nonce *big.Int
	// Missing are the ranges of nonces below Nonce that no confirmed execution
	// used. It is nil if Nonce is.
	Missing []NonceRange
}

// Rejected is an execution that the Safe Transaction Service lists but the
// chain doesn't confirm, with the reason.
type Rejected struct {
	SafeTxHash ethrpc.Hash `json:"safeTxHash"`
	TxHash     ethrpc.Hash `json:"txHash"`
	Nonce      *big.Int    `json:"nonce"`
	Reason     string      `json:"reason"`
}

// NonceRange is a range of nonces, From to To, including both.
type NonceRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}

// FromService returns the Safe transactions that the Safe executed up to block
// to, as the Finder's Service lists them. It only returns an execution that the
// chain confirms: the transaction's fields hash to the Safe transaction hash
// that the service reports, and the receipt of the onchain transaction that the
// service reports has a log of the Safe, in a block up to to, that says it
// executed the transaction with that hash. The block, log, success, and payment
// are those of the log. An execution that the service reports in a later block
// isn't evidence of the history up to to, so it is left out, and so is one
// whose receipt is in a later block.
//
// Every execution uses one nonce, so the nonces below the Safe's nonce at to
// that no confirmed execution used are the executions that the service doesn't
// list, or lists wrongly. Executions by modules don't use nonces, and the
// service doesn't list them.
func (f *Finder) FromService(ctx context.Context, safe ethrpc.Address, to ethrpc.BlockNumber) (*History, error) {
	if f.Service == nil {
		return nil, fmt.Errorf("no Safe Transaction Service for chain %d", f.Eth.ChainID())
	}
	listed, err := f.Service.Executed(ctx, safe)
	if err != nil {
		return nil, fmt.Errorf("listing the executed transactions of %s from the Safe Transaction Service: %w", safe, err)
	}

	h := &History{Executions: []Execution{}, Rejected: []Rejected{}}
	receipts := make(map[ethrpc.Hash]*ethrpc.Receipt)
	for _, l := range listed {
		if l.Block > uint64(to) {
			continue
		}
		tx := l.Transaction
		reject := func(reason string) {
			h.Rejected = append(h.Rejected, Rejected{SafeTxHash: l.SafeTxHash, TxHash: l.TxHash, Nonce: tx.Nonce, Reason: reason})
		}
		if tx.Safe != safe {
			reject(fmt.Sprintf("the transaction is for the Safe %s", tx.Safe))
			continue
		}
		if !matches(tx, l.SafeTxHash) {
			reject("the transaction's fields don't hash to its Safe transaction hash")
			continue
		}
		receipt, ok := receipts[l.TxHash]
		if !ok {
			r, err := f.Eth.GetTransactionReceipt(ctx, l.TxHash)
			if err != nil {
				f.warn(fmt.Errorf("getting the receipt of %s: %w", l.TxHash, err))
			} else {
				receipt = &r
			}
			receipts[l.TxHash] = receipt
		}
		if receipt == nil {
			reject("the onchain transaction has no receipt")
			continue
		}
		if receipt.BlockNumber > to {
			continue
		}
		e, ok := executionIn(receipt, safe, l.SafeTxHash)
		if !ok {
			reject("the onchain transaction logged no execution of it by the Safe")
			continue
		}
		e.Source, e.Transaction = SourceService, tx
		h.Executions = append(h.Executions, e)
	}
	slices.SortFunc(h.Executions, func(a, b Execution) int {
		return cmp.Or(cmp.Compare(a.Block, b.Block), cmp.Compare(a.LogIndex, b.LogIndex))
	})

	result, err := f.Eth.Call(ctx, ethrpc.CallRequest{To: safe, Data: solabi.Call(nonceSelector)}, to)
	if err != nil {
		f.warn(fmt.Errorf("reading the nonce of %s at block %d: %w", safe, to, err))
		return h, nil
	}
	d := solabi.NewDecoder(result)
	nonce := d.Uint(0)
	if d.Err() != nil || !nonce.IsUint64() {
		f.warn(fmt.Errorf("reading the nonce of %s at block %d: invalid result %s", safe, to, result))
		return h, nil
	}
	h.Nonce, h.Missing = nonce, missing(h.Executions, nonce.Uint64())
	return h, nil
}

// executionIn returns the execution of the Safe transaction with the hash
// safeTxHash that a log of the Safe in a receipt records.
func executionIn(receipt *ethrpc.Receipt, safe ethrpc.Address, safeTxHash ethrpc.Hash) (Execution, bool) {
	for _, log := range receipt.Logs {
		if log.Removed || log.Address != safe || len(log.Topics) == 0 ||
			(log.Topics[0] != executionSuccessEvent && log.Topics[0] != executionFailureEvent) {
			continue
		}
		if e, ok := decodeExecution(log); ok && e.SafeTxHash == safeTxHash {
			return e, true
		}
	}
	return Execution{}, false
}

// missing returns the ranges of the nonces below next that none of executions
// used.
func missing(executions []Execution, next uint64) []NonceRange {
	used := make(map[uint64]bool)
	for _, e := range executions {
		if n := e.Transaction.Nonce; n.IsUint64() {
			used[n.Uint64()] = true
		}
	}
	ranges := []NonceRange{}
	for n := uint64(0); n < next; n++ {
		if used[n] {
			continue
		}
		if len(ranges) > 0 && ranges[len(ranges)-1].To == n-1 {
			ranges[len(ranges)-1].To = n
		} else {
			ranges = append(ranges, NonceRange{From: n, To: n})
		}
	}
	return ranges
}

// proxyCreationEvent is the ProxyCreation event of the Safe proxy factories,
// which index the proxy from 1.4.1 and have it in the data in 1.3.0.
var proxyCreationEvent = solabi.Event("ProxyCreation(address,address)")

// Creation is the creation of a Safe's account.
type Creation struct {
	// Block is the block whose transaction gave the account its code: it has no
	// code at the block before, and has code at Block.
	Block  uint64      `json:"block"`
	TxHash ethrpc.Hash `json:"txHash"`
	// Factory is the contract that logged the ProxyCreation event of the account in
	// the transaction, or nil if none did.
	Factory *ethrpc.Address `json:"factory"`
}

// CreationFromService returns the creation of the Safe's account by block to,
// if the Finder's Service has the transaction that created it and the chain
// confirms it: the transaction's receipt is in a block up to to, the account
// has no code at the block before it, and has code at its block. It returns
// nil, with a warning, if the service doesn't have it or the chain doesn't
// confirm it, such as for a node that doesn't serve the code at old blocks.
func (f *Finder) CreationFromService(ctx context.Context, safe ethrpc.Address, to ethrpc.BlockNumber) *Creation {
	if f.Service == nil {
		return nil
	}
	hash, err := f.Service.Creation(ctx, safe)
	if err != nil {
		f.warn(fmt.Errorf("getting the creation of %s from the Safe Transaction Service: %w", safe, err))
		return nil
	}
	receipt, err := f.Eth.GetTransactionReceipt(ctx, hash)
	if err != nil {
		f.warn(fmt.Errorf("getting the receipt of the creation %s: %w", hash, err))
		return nil
	}
	block := receipt.BlockNumber
	if block > to || block == 0 {
		return nil
	}
	before, err := f.Eth.GetCode(ctx, safe, block-1)
	if err == nil && len(before) == 0 {
		var after ethrpc.Bytes
		if after, err = f.Eth.GetCode(ctx, safe, block); err == nil && len(after) == 0 {
			err = fmt.Errorf("the account has no code at block %d", block)
		}
	} else if err == nil {
		err = fmt.Errorf("the account has code at block %d, before it", block-1)
	}
	if err != nil {
		f.warn(fmt.Errorf("confirming the creation %s of %s: %w", hash, safe, err))
		return nil
	}
	c := &Creation{Block: uint64(block), TxHash: hash}
	for _, log := range receipt.Logs {
		if log.Removed || len(log.Topics) == 0 || log.Topics[0] != proxyCreationEvent {
			continue
		}
		var proxy ethrpc.Address
		switch {
		case len(log.Topics) == 2:
			proxy = ethrpc.Address(log.Topics[1][12:])
		case len(log.Topics) == 1 && len(log.Data) == 64:
			proxy = ethrpc.Address(log.Data[12:32])
		}
		if proxy == safe {
			factory := log.Address
			c.Factory = &factory
			break
		}
	}
	return c
}
