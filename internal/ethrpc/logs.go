package ethrpc

import (
	"context"
	"errors"
	"iter"
)

// Nodes commonly limit the block range of eth_getLogs, often to 10,000 blocks,
// and only report the limit in an error message, whose format varies. ScanLogs
// starts with chunks of maxLogRange blocks, and halves them down to minLogRange
// blocks while the node answers with JSON-RPC errors that aren't transient.
var (
	maxLogRange uint64 = 10_000
	minLogRange uint64 = 50
)

// ScanLogs returns the logs matching filter, in order, querying the block range
// in chunks that the node accepts. Iteration stops at the first error, which is
// yielded with a zero Log. Stopping early saves querying the remaining chunks.
func (c *Client) ScanLogs(ctx context.Context, filter LogFilter) iter.Seq2[Log, error] {
	return func(yield func(Log, error) bool) {
		step := maxLogRange
		for from := filter.FromBlock; from <= filter.ToBlock; {
			chunk := filter
			chunk.FromBlock = from
			chunk.ToBlock = min(from+BlockNumber(step-1), filter.ToBlock)
			logs, err := c.GetLogs(ctx, chunk)
			// A rate limit or a server error is not about the range, and a smaller range
			// only means more requests.
			if _, ok := errors.AsType[*Error](err); ok && !Transient(err) && step > minLogRange {
				step = max(step/2, minLogRange)
				continue
			}
			if err != nil {
				yield(Log{}, err)
				return
			}
			for _, log := range logs {
				if !yield(log, nil) {
					return
				}
			}
			if chunk.ToBlock == filter.ToBlock {
				return
			}
			from = chunk.ToBlock + 1
		}
	}
}
