package executions

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// History is every Safe transaction that a Safe executed up to a block, as
// FindAll finds them.
type History struct {
	// Nonce is the Safe's nonce at the last block. Every execution uses the next
	// nonce, so the Safe executed this number of Safe transactions up to it, unless
	// a delegate call changed its nonce.
	Nonce *big.Int
	// Executions are the executions found, in the order of their logs.
	Executions []Execution
	// Missing are the ranges of nonces below Nonce that no execution found used, in
	// order. It is nil if Nonce is too large to list them.
	Missing []NonceRange
	// Scanned are the ranges of blocks whose logs were scanned for the executions
	// that the service doesn't list, in order.
	Scanned []BlockRange
	// Rejected are the executions that the service lists up to the last block but
	// that the chain doesn't confirm, in the order that the service lists them.
	Rejected []Rejected
	// Creation is the creation of the Safe's account, if the service has it and the
	// chain confirms it, or nil.
	Creation *Creation
}

// Complete reports whether the executions found account for every nonce below
// the Safe's nonce.
func (h *History) Complete() bool {
	return h.Missing != nil && len(h.Missing) == 0
}

// NonceRange is a range of nonces, From to To, including both.
type NonceRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}

// BlockRange is a range of blocks, including both ends.
type BlockRange struct {
	FromBlock uint64 `json:"fromBlock"`
	ToBlock   uint64 `json:"toBlock"`
}

// Rejected is an execution that the Safe Transaction Service lists but the
// chain doesn't confirm, with the reason.
type Rejected struct {
	SafeTxHash ethrpc.Hash `json:"safeTxHash"`
	// TxHash is the onchain transaction that the service says executed it, which is
	// the zero hash if it gives none.
	TxHash ethrpc.Hash `json:"txHash"`
	// Nonce is the nonce that the service gives the transaction, or nil if it gives
	// none.
	Nonce  *big.Int `json:"nonce"`
	Reason string   `json:"reason"`
}

// FindAll returns every Safe transaction that the Safe executed up to the block
// to, in order.
//
// A Safe's whole history is too long to scan the logs of on public nodes, so it
// lists the Safe's executions from the Safe Transaction Service, which must be
// set, and uses them to find the blocks to read. It reads the Safe's logs in
// each block that the service names up to to, and keeps the executions that the
// Safe logged there, whether the service lists them rightly or not, which it
// decodes like Find, taking a transaction that the service lists with its hash
// from the listing. A block whose logs have no execution of the Safe is read
// once more, since a node behind a load balancer now and then answers for a
// block that it hasn't caught up with.
//
// It only confirms an execution that the service lists if its fields hash to
// the Safe transaction hash that the service reports, and the Safe logged the
// execution of that hash in the block and the onchain transaction that the
// service reports. It rejects any other, with the reason. One that the service
// lists after to isn't evidence of the history up to to, so it is left out.
//
// The Safe's nonce at to is the number of executions up to it, and the
// transactions that the service lists have their nonces, once their hashes
// check out. So for the nonces that no execution found has, it scans the blocks
// between the executions with the nonces before and after them, or from the
// block that created the Safe for the first ones, or up to to for the last
// ones. A scan that fails, such as one that a public node times out, is a
// warning: its range isn't in Scanned, and its nonces stay missing.
//
// Executions by modules don't use nonces, and the service doesn't list them, so
// they aren't found. Any other request to the node that fails, such as for a
// rate limit, fails FindAll, since it says nothing about the history.
func (f *Finder) FindAll(ctx context.Context, safe ethrpc.Address, to ethrpc.BlockNumber) (*History, error) {
	if f.Service == nil {
		return nil, errors.New("finding every execution needs the Safe Transaction Service")
	}
	nonce, err := f.readNonce(ctx, safe, to)
	if err != nil {
		return nil, err
	}
	listed, err := f.Service.Executions(ctx, safe)
	switch {
	case errors.Is(err, txservice.ErrNotFound):
		// The nonces show what this leaves out, and the scans look for it.
		f.warn(fmt.Errorf("the Safe Transaction Service doesn't know %s", safe))
	case err != nil:
		return nil, fmt.Errorf("listing the executions of %s from the Safe Transaction Service: %w", safe, err)
	}
	creation, err := f.CreationFromService(ctx, safe, to)
	if err != nil {
		return nil, err
	}

	// The transactions that the service lists, by hash, if their hash checks out,
	// whatever it says about their execution, and the blocks up to to that it
	// names.
	transactions := make(map[ethrpc.Hash]*safenet.SafeTransaction)
	var blocks []uint64
	for _, x := range listed {
		if x.Transaction.Safe == safe && matches(x.Transaction, x.SafeTxHash) {
			transactions[x.SafeTxHash] = x.Transaction
		}
		if x.Block != 0 && x.Block <= uint64(to) {
			blocks = append(blocks, x.Block)
		}
	}
	slices.Sort(blocks)
	c := newCollector(f, safe)
	for _, block := range slices.Compact(blocks) {
		read, err := c.scan(ctx, ethrpc.BlockNumber(block), ethrpc.BlockNumber(block))
		if err == nil && read == 0 {
			_, err = c.scan(ctx, ethrpc.BlockNumber(block), ethrpc.BlockNumber(block))
		}
		if err != nil {
			return nil, err
		}
	}

	h := &History{Nonce: nonce, Scanned: []BlockRange{}, Rejected: []Rejected{}, Creation: creation}
	for _, r := range f.gaps(ctx, c, transactions, nonce, to, creation) {
		if _, err := c.scan(ctx, ethrpc.BlockNumber(r.FromBlock), ethrpc.BlockNumber(r.ToBlock)); err != nil {
			f.warn(fmt.Errorf("scanning blocks %d to %d for executions that the Safe Transaction Service doesn't list: %w", r.FromBlock, r.ToBlock, err))
			continue
		}
		h.Scanned = append(h.Scanned, r)
	}

	slices.SortFunc(c.executions, func(a, b Execution) int {
		return cmp.Or(cmp.Compare(a.Block, b.Block), cmp.Compare(a.LogIndex, b.LogIndex))
	})
	// The nonce is already read, and is that of the last execution, plus one, if
	// the history is complete.
	d := &decoder{
		Finder: f, safe: safe, to: to, executions: c.executions, events: c.events, listed: transactions,
		nonceRead: true, nonceNext: nonce,
	}
	if err := d.decodeAll(ctx); err != nil {
		return nil, err
	}
	h.Executions = c.executions

	for _, x := range listed {
		if x.Block > uint64(to) {
			continue
		}
		if reason := confirm(x, safe, h.Executions); reason != "" {
			h.Rejected = append(h.Rejected, Rejected{SafeTxHash: x.SafeTxHash, TxHash: x.TxHash, Nonce: x.Transaction.Nonce, Reason: reason})
		}
	}
	if nonce.IsUint64() {
		h.Missing = missing(h.Executions, nonce.Uint64())
	} else {
		f.warn(fmt.Errorf("the Safe's nonce %s is too large to list the nonces that no execution used", nonce))
	}
	return h, nil
}

// confirm returns why the chain doesn't confirm the execution x that the
// service lists, given the executions of the Safe found, or "" if it does.
func confirm(x txservice.Execution, safe ethrpc.Address, executions []Execution) string {
	switch {
	case x.Block == 0 || x.TxHash == (ethrpc.Hash{}):
		return "the service gives no onchain transaction or block"
	case x.Transaction.Safe != safe:
		return fmt.Sprintf("the transaction is for the Safe %s", x.Transaction.Safe)
	case !matches(x.Transaction, x.SafeTxHash):
		return "the transaction's fields don't hash to its Safe transaction hash"
	}
	var in []Execution
	for _, e := range executions {
		if e.SafeTxHash == x.SafeTxHash && e.Block == x.Block {
			if e.TxHash == x.TxHash {
				return ""
			}
			in = append(in, e)
		}
	}
	if len(in) > 0 {
		return fmt.Sprintf("the Safe logged its execution in block %d in the transaction %s, not the one that the service reports", x.Block, in[0].TxHash)
	}
	return fmt.Sprintf("the Safe logged no execution of it in block %d", x.Block)
}

// missing returns the ranges of the nonces below next that none of executions
// used.
func missing(executions []Execution, next uint64) []NonceRange {
	var used []uint64
	for _, e := range executions {
		if tx := e.Transaction; tx != nil && tx.Nonce.IsUint64() && tx.Nonce.Uint64() < next {
			used = append(used, tx.Nonce.Uint64())
		}
	}
	slices.Sort(used)
	ranges := []NonceRange{}
	from := uint64(0)
	for _, n := range slices.Compact(used) {
		if n > from {
			ranges = append(ranges, NonceRange{From: from, To: n - 1})
		}
		from = n + 1
	}
	if from < next {
		ranges = append(ranges, NonceRange{From: from, To: next - 1})
	}
	return ranges
}

// gaps returns the ranges of blocks to scan for the executions with the nonces,
// below nonce, that no execution that c collected has, given the transactions
// by hash whose hashes check out. Each range is between the blocks of the
// executions with the nonces before and after the missing ones, which are where
// they must be, since nonces only increase. The range before the first
// execution found starts at the Safe's creation, or, if it is unknown, at the
// first block with the Safe's code.
func (f *Finder) gaps(ctx context.Context, c *collector, transactions map[ethrpc.Hash]*safenet.SafeTransaction, nonce *big.Int, to ethrpc.BlockNumber, creation *Creation) []BlockRange {
	if !nonce.IsInt64() {
		f.warn(fmt.Errorf("the Safe's nonce %s is too large to find the executions that it doesn't have", nonce))
		return nil
	}
	// The blocks of the executions by their nonce, which only an execution whose
	// transaction checks out has. A nonce that a delegate call reused has the block
	// of its first execution.
	blocks := make(map[int64]uint64)
	for _, e := range c.executions {
		tx := transactions[e.SafeTxHash]
		if tx == nil {
			for _, event := range c.events[e.TxHash] {
				if matches(event, e.SafeTxHash) {
					tx = event
					break
				}
			}
		}
		if tx == nil || tx.Nonce.Cmp(nonce) >= 0 {
			continue
		}
		if b, ok := blocks[tx.Nonce.Int64()]; !ok || e.Block < b {
			blocks[tx.Nonce.Int64()] = e.Block
		}
	}
	// The Safe's nonce is that of the next execution, after the last block.
	blocks[nonce.Int64()] = uint64(to)

	var ranges []BlockRange
	// The execution before the first is the Safe's creation, at a block that is
	// only searched for if an execution before the first one found is missing.
	prev, prevBlock := int64(-1), uint64(0)
	for _, n := range slices.Sorted(maps.Keys(blocks)) {
		// Nonces that a delegate call changed may be out of the order of their blocks,
		// which leaves no range between them.
		if n > prev+1 && prevBlock <= blocks[n] {
			from, ok := prevBlock, true
			if prev == -1 {
				from, ok = f.firstBlock(ctx, c.safe, ethrpc.BlockNumber(blocks[n]), creation)
			}
			if ok && from <= blocks[n] {
				ranges = append(ranges, BlockRange{FromBlock: from, ToBlock: blocks[n]})
			}
		}
		prev, prevBlock = n, blocks[n]
	}
	return ranges
}

// firstBlock returns the block that the Safe's history starts at: its creation,
// if known, or else the first block, up to to, at which it has code, which a
// Safe proxy has from its deployment on. It warns and returns false if it can't
// find it.
func (f *Finder) firstBlock(ctx context.Context, safe ethrpc.Address, to ethrpc.BlockNumber, creation *Creation) (uint64, bool) {
	if creation != nil {
		return creation.Block, true
	}
	block, err := f.deployment(ctx, safe, to)
	if err != nil {
		f.warn(fmt.Errorf("finding the block that %s was deployed in: %w", safe, err))
		return 0, false
	}
	return block, true
}

// deployment returns the first block, up to to, at which safe has code. It
// searches the blocks by bisection, which needs a node that serves the code of
// old blocks.
func (f *Finder) deployment(ctx context.Context, safe ethrpc.Address, to ethrpc.BlockNumber) (uint64, error) {
	code, err := f.Eth.GetCode(ctx, safe, to)
	if err != nil {
		return 0, err
	}
	if len(code) == 0 {
		return 0, fmt.Errorf("%s has no code at block %d", safe, to)
	}
	lo, hi := uint64(0), uint64(to)
	for lo < hi {
		mid := lo + (hi-lo)/2
		code, err := f.Eth.GetCode(ctx, safe, ethrpc.BlockNumber(mid))
		if err != nil {
			return 0, err
		}
		if len(code) > 0 {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
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
// has no code at the block before it, and has code at its block. It returns nil
// if the service doesn't have it, and nil with a warning if the service fails
// or the chain doesn't confirm it, such as for a node that doesn't serve the
// code at old blocks. It fails if a node request fails transiently, such as for
// a rate limit, since that says nothing about the creation.
func (f *Finder) CreationFromService(ctx context.Context, safe ethrpc.Address, to ethrpc.BlockNumber) (*Creation, error) {
	if f.Service == nil {
		return nil, nil
	}
	hash, err := f.Service.Creation(ctx, safe)
	if errors.Is(err, txservice.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		f.warn(fmt.Errorf("getting the creation of %s from the Safe Transaction Service: %w", safe, err))
		return nil, nil
	}
	receipt, err := f.Eth.GetTransactionReceipt(ctx, hash)
	if ethrpc.Transient(err) {
		return nil, fmt.Errorf("getting the receipt of the creation %s: %w", hash, err)
	}
	if err != nil {
		f.warn(fmt.Errorf("getting the receipt of the creation %s: %w", hash, err))
		return nil, nil
	}
	block := receipt.BlockNumber
	if block > to || block == 0 {
		return nil, nil
	}
	before, err := f.Eth.GetCode(ctx, safe, block-1)
	var after ethrpc.Bytes
	if err == nil {
		after, err = f.Eth.GetCode(ctx, safe, block)
	}
	if ethrpc.Transient(err) {
		return nil, fmt.Errorf("confirming the creation %s of %s: %w", hash, safe, err)
	}
	switch {
	case err != nil:
	case len(before) != 0:
		err = fmt.Errorf("the account has code at block %d, before it", block-1)
	case len(after) == 0:
		err = fmt.Errorf("the account has no code at block %d", block)
	}
	if err != nil {
		f.warn(fmt.Errorf("confirming the creation %s of %s: %w", hash, safe, err))
		return nil, nil
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
	return c, nil
}
