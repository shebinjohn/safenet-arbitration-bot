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
)

// History is every Safe transaction that a Safe executed up to a block, as
// FindAll finds them.
type History struct {
	// Nonce is the Safe's nonce at the last block. Every execution uses the next
	// nonce, so the Safe executed this number of Safe transactions up to it, unless
	// a delegate call changed its nonce.
	Nonce *big.Int
	// Executions are the executions found, in order.
	Executions []Execution
	// Scanned are the ranges of blocks whose logs were scanned for the executions
	// that the service doesn't list, in order.
	Scanned []BlockRange
}

// Complete reports whether the history has as many executions as the Safe's
// nonce says that it executed.
func (h *History) Complete() bool {
	return h.Nonce.IsInt64() && h.Nonce.Int64() == int64(len(h.Executions))
}

// BlockRange is a range of blocks, including both ends.
type BlockRange struct {
	FromBlock uint64 `json:"fromBlock"`
	ToBlock   uint64 `json:"toBlock"`
}

// FindAll returns every Safe transaction that the Safe executed up to the block
// to, in order.
//
// A Safe's whole history is too long to scan the logs of, so it lists the
// Safe's executions from the Safe Transaction Service, which must be set. It
// only uses them to find the blocks to read: it reads the logs of each block
// that the service names, and keeps the executions that the Safe logged there,
// which it decodes like Find. The Safe's nonce at to is the number of
// executions up to it, and the transactions that the service lists have their
// nonces, once their hashes check out. So for the nonces that no execution
// found has, it scans the blocks between the executions with the nonces before
// and after them, or from the block that the Safe was deployed in for the first
// ones, or up to to for the last ones. A scan that fails is a warning, and
// leaves the history incomplete.
func (f *Finder) FindAll(ctx context.Context, safe ethrpc.Address, to ethrpc.BlockNumber) (*History, error) {
	if f.Service == nil {
		return nil, errors.New("finding every execution needs the Safe Transaction Service")
	}
	nonce, err := f.readNonce(ctx, safe, to)
	if err != nil {
		return nil, err
	}
	listed, err := f.Service.Executions(ctx, safe)
	if err != nil {
		return nil, fmt.Errorf("listing the executions of %s from the Safe Transaction Service: %w", safe, err)
	}

	// The transactions that the service lists, by hash, if their hash checks out.
	transactions := make(map[ethrpc.Hash]*safenet.SafeTransaction)
	var blocks []uint64
	for _, x := range listed {
		if x.Block > uint64(to) {
			continue
		}
		if matches(x.Transaction, x.SafeTxHash) {
			transactions[x.SafeTxHash] = x.Transaction
		}
		blocks = append(blocks, x.Block)
	}
	slices.Sort(blocks)
	c := newCollector(f, safe)
	for _, block := range slices.Compact(blocks) {
		if err := c.scan(ctx, ethrpc.BlockNumber(block), ethrpc.BlockNumber(block)); err != nil {
			return nil, err
		}
	}

	h := &History{Nonce: nonce, Scanned: []BlockRange{}}
	for _, r := range f.missing(ctx, c, transactions, nonce, to) {
		if err := c.scan(ctx, ethrpc.BlockNumber(r.FromBlock), ethrpc.BlockNumber(r.ToBlock)); err != nil {
			f.warnf("scanning blocks %d to %d for executions that the Safe Transaction Service doesn't list: %w", r.FromBlock, r.ToBlock, err)
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
	for i := range c.executions {
		e := &c.executions[i]
		e.Source, e.Transaction = d.decode(ctx, i)
	}
	h.Executions = c.executions
	return h, nil
}

// missing returns the ranges of blocks to scan for the executions with the
// nonces, below nonce, that no execution that c collected has, given the
// transactions by hash whose hashes check out. Each range is between the blocks
// of the executions with the nonces before and after the missing ones, which
// are where they must be, since nonces only increase.
func (f *Finder) missing(ctx context.Context, c *collector, transactions map[ethrpc.Hash]*safenet.SafeTransaction, nonce *big.Int, to ethrpc.BlockNumber) []BlockRange {
	if !nonce.IsInt64() {
		f.warnf("the Safe's nonce %s is too large to find the executions that it doesn't have", nonce)
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
	next := nonce.Int64()
	blocks[next] = uint64(to)
	nonces := slices.Sorted(maps.Keys(blocks))

	var ranges []BlockRange
	// The execution before the first is the Safe's deployment, at a block that is
	// only searched for if an execution before the first one found is missing.
	prev, prevBlock := int64(-1), uint64(0)
	for _, n := range nonces {
		// Nonces that a delegate call changed may be out of the order of their blocks,
		// which leaves no range between them.
		if n > prev+1 && prevBlock <= blocks[n] {
			if prev == -1 {
				block, err := f.deployment(ctx, c.safe, ethrpc.BlockNumber(blocks[n]))
				if err != nil {
					f.warnf("finding the block that %s was deployed in: %w", c.safe, err)
					return nil
				}
				prevBlock = block
			}
			ranges = append(ranges, BlockRange{FromBlock: prevBlock, ToBlock: blocks[n]})
		}
		prev, prevBlock = n, blocks[n]
	}
	return ranges
}

// deployment returns the first block, up to to, at which safe has code, which a
// Safe proxy has from its deployment on. It searches the blocks by bisection,
// which needs a node that serves the code of old blocks.
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

func (f *Finder) warnf(format string, args ...any) {
	if f.Warn != nil {
		f.Warn(fmt.Errorf(format, args...))
	}
}
