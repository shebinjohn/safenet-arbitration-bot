package executions

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// decoder decodes the Safe transactions of executions, caching what several of
// them need: the Safe's nonce, and the transaction and trace of an onchain
// transaction that executes more than one.
type decoder struct {
	*Finder
	safe ethrpc.Address
	// to is the last block that the search covers.
	to         ethrpc.BlockNumber
	executions []Execution
	events     map[ethrpc.Hash][]*safenet.SafeTransaction
	// listed are the transactions that the service lists by Safe transaction hash,
	// each of which has its hash, so that the service isn't asked for them again.
	listed map[ethrpc.Hash]*safenet.SafeTransaction

	nonceRead bool
	// nonceNext is the Safe's nonce after the last execution, or nil if it couldn't
	// be read.
	nonceNext *big.Int
	txs       map[ethrpc.Hash]*ethrpc.Transaction
	traces    map[ethrpc.Hash][]ethrpc.Bytes
}

// decode returns the Safe transaction of executions[i] and where it is from, or
// nothing if no source has it.
func (d *decoder) decode(ctx context.Context, i int) (Source, *safenet.SafeTransaction) {
	e := d.executions[i]
	for _, tx := range d.events[e.TxHash] {
		if matches(tx, e.SafeTxHash) {
			return SourceEvent, tx
		}
	}

	// The calldata and traces have no nonce. Every execution uses one, and all of
	// the executions up to the last block are in the search, so the last one used
	// the Safe's nonce at that block, less one.
	if nonce := d.nonce(ctx, i); nonce != nil {
		if tx := d.fromCalldata(ctx, e, nonce); tx != nil {
			return SourceCalldata, tx
		}
		if tx := d.fromTrace(ctx, e, nonce); tx != nil {
			return SourceTrace, tx
		}
	}

	if tx, ok := d.listed[e.SafeTxHash]; ok {
		return SourceService, tx
	}
	if d.Service != nil {
		tx, err := d.Service.Transaction(ctx, e.SafeTxHash)
		if err != nil && !errors.Is(err, txservice.ErrNotFound) {
			d.warn(fmt.Errorf("getting Safe transaction %s from the Safe Transaction Service: %w", e.SafeTxHash, err))
		}
		if err == nil && matches(tx, e.SafeTxHash) {
			return SourceService, tx
		}
	}
	return "", nil
}

// nonce returns the nonce that executions[i] used, or nil if it is unknown.
func (d *decoder) nonce(ctx context.Context, i int) *big.Int {
	if !d.nonceRead {
		d.nonceRead = true
		next, err := d.readNonce(ctx, d.safe, d.to)
		if err != nil {
			d.warn(err)
		}
		d.nonceNext = next
	}
	if d.nonceNext == nil {
		return nil
	}
	nonce := new(big.Int).Sub(d.nonceNext, big.NewInt(int64(len(d.executions)-i)))
	if nonce.Sign() < 0 {
		return nil
	}
	return nonce
}

// fromCalldata decodes the Safe transaction of e from the calldata of its
// onchain transaction, if that calls the Safe's execTransaction with a
// transaction that used nonce and has the hash that e has.
func (d *decoder) fromCalldata(ctx context.Context, e Execution, nonce *big.Int) *safenet.SafeTransaction {
	tx, ok := d.txs[e.TxHash]
	if !ok {
		var err error
		result, err := d.Eth.GetTransactionByHash(ctx, e.TxHash)
		if err != nil {
			d.warn(fmt.Errorf("getting transaction %s: %w", e.TxHash, err))
		} else {
			tx = &result
		}
		if d.txs == nil {
			d.txs = make(map[ethrpc.Hash]*ethrpc.Transaction)
		}
		d.txs[e.TxHash] = tx
	}
	if tx == nil || tx.To == nil || *tx.To != d.safe {
		return nil
	}
	return d.candidate(tx.Input, e, nonce)
}

// fromTrace decodes the Safe transaction of e from the calls to the Safe's
// execTransaction in a trace of its onchain transaction.
func (d *decoder) fromTrace(ctx context.Context, e Execution, nonce *big.Int) *safenet.SafeTransaction {
	inputs, ok := d.traces[e.TxHash]
	if !ok {
		var root callFrame
		// Most public nodes don't serve traces, so failing to get one is not a warning.
		err := d.Eth.RawRequest(ctx, &root, "debug_traceTransaction", e.TxHash, map[string]any{"tracer": "callTracer"})
		if err == nil {
			inputs = root.callsTo(d.safe, nil)
		}
		if d.traces == nil {
			d.traces = make(map[ethrpc.Hash][]ethrpc.Bytes)
		}
		d.traces[e.TxHash] = inputs
	}
	for _, input := range inputs {
		if tx := d.candidate(input, e, nonce); tx != nil {
			return tx
		}
	}
	return nil
}

// candidate returns the Safe transaction that the calldata of a call to
// execTransaction has, given that it used nonce, if it has the hash that e has.
func (d *decoder) candidate(input []byte, e Execution, nonce *big.Int) *safenet.SafeTransaction {
	tx := decodeCall(d.safe, d.Eth.ChainID(), input)
	if tx == nil {
		return nil
	}
	tx.Nonce = nonce
	if !matches(tx, e.SafeTxHash) {
		return nil
	}
	return tx
}

func (d *decoder) warn(err error) {
	if d.Warn != nil {
		d.Warn(err)
	}
}

// callFrame is a call in the output of geth's callTracer.
type callFrame struct {
	Type  string          `json:"type"`
	To    *ethrpc.Address `json:"to"`
	Input ethrpc.Bytes    `json:"input"`
	Error string          `json:"error"`
	Calls []callFrame     `json:"calls"`
}

// callsTo appends to inputs the calldata of the calls to safe in the frame and
// the calls it makes, in the order that they start. A call that reverted, and
// the calls in it, are left out, since their effects were undone.
func (f *callFrame) callsTo(safe ethrpc.Address, inputs []ethrpc.Bytes) []ethrpc.Bytes {
	if f.Error != "" {
		return inputs
	}
	if f.Type == "CALL" && f.To != nil && *f.To == safe {
		inputs = append(inputs, f.Input)
	}
	for i := range f.Calls {
		inputs = f.Calls[i].callsTo(safe, inputs)
	}
	return inputs
}
