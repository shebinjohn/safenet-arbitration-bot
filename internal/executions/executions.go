// Package executions finds the Safe transactions that a Safe executed onchain,
// and decodes them.
//
// A Safe logs an ExecutionSuccess or ExecutionFailure event with the hash of
// each Safe transaction it executes, but the event doesn't say what the
// transaction was. The package decodes it from the first of these sources that
// yields a transaction with that hash:
//
//  1. The SafeMultiSigTransaction event, which SafeL2 logs before executing.
//  2. The calldata of the onchain transaction, if it calls the Safe's
//     execTransaction directly.
//  3. A trace of the onchain transaction, for the calls to execTransaction that
//     it makes, such as by a relayer or a batch.
//  4. The Safe Transaction Service.
//
// The calldata and traces don't have the Safe transaction's nonce, so it is
// worked out from the Safe's nonce, since every execution increases it by one.
// A decoded transaction is only ever reported if its EIP-712 hash is the one
// that the Safe logged, so a source can't misreport what was executed, however
// much it is trusted.
package executions

import (
	"context"
	"fmt"
	"math/big"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// The events and function of Safe 1.3.0, 1.4.1, and 1.5.0 that the package
// reads. The signatures of ExecutionSuccess and ExecutionFailure are the same
// in all of them, but 1.3.0 doesn't index their transaction hash.
var (
	executionSuccessEvent = solabi.Event("ExecutionSuccess(bytes32,uint256)")
	executionFailureEvent = solabi.Event("ExecutionFailure(bytes32,uint256)")
	multiSigEvent         = solabi.Event("SafeMultiSigTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes,bytes)")
	execTransaction       = solabi.Selector("execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)")
	nonceSelector         = solabi.Selector("nonce()")
)

// Source is where a transaction was decoded from.
type Source string

const (
	// SourceEvent is the SafeMultiSigTransaction event of SafeL2.
	SourceEvent Source = "event"
	// SourceCalldata is the calldata of the onchain transaction.
	SourceCalldata Source = "calldata"
	// SourceTrace is a trace of the onchain transaction.
	SourceTrace Source = "trace"
	// SourceService is the Safe Transaction Service.
	SourceService Source = "service"
)

// Execution is a Safe transaction that a Safe executed.
type Execution struct {
	Block uint64 `json:"block"`
	// TxHash is the hash of the onchain transaction that executed it, and LogIndex
	// the index in its block of the log that says so.
	TxHash   ethrpc.Hash `json:"txHash"`
	LogIndex uint64      `json:"logIndex"`
	// SafeTxHash is the hash of the Safe transaction, from the Safe's log.
	SafeTxHash ethrpc.Hash `json:"safeTxHash"`
	// Success is whether the Safe transaction's call succeeded. A Safe transaction
	// whose call fails is executed all the same, and uses its nonce.
	Success bool `json:"success"`
	// Payment is the refund that the Safe paid, in the gas token.
	Payment *big.Int `json:"payment"`
	// Source is where Transaction was decoded from. It is empty if the transaction
	// is unknown.
	Source Source `json:"source,omitempty"`
	// Transaction is nil if no source has a transaction with the hash SafeTxHash.
	Transaction *safenet.SafeTransaction `json:"transaction"`
}

// Finder finds the Safe transactions that a Safe executed on a chain.
type Finder struct {
	// Eth is a client for the chain. The node must serve eth_call at the last block
	// that Find searches for the calldata and trace sources, which are otherwise
	// skipped, and debug_traceTransaction for the trace source.
	Eth *ethrpc.Client
	// Service is a client for the chain's Safe Transaction Service, or nil to leave
	// it out.
	Service *txservice.Client
	// Warn is called with the errors of sources that failed for a reason other than
	// not having the transaction, such as an unreachable service. It may be nil.
	Warn func(error)
}

// Find returns the Safe transactions that the Safe executed in the blocks from
// to to, including both, in order.
func (f *Finder) Find(ctx context.Context, safe ethrpc.Address, from, to ethrpc.BlockNumber) ([]Execution, error) {
	c := newCollector(f, safe)
	if err := c.scan(ctx, from, to); err != nil {
		return nil, err
	}
	d := &decoder{Finder: f, safe: safe, to: to, executions: c.executions, events: c.events}
	for i := range c.executions {
		e := &c.executions[i]
		e.Source, e.Transaction = d.decode(ctx, i)
	}
	return c.executions, nil
}

// collector collects the executions of a Safe, and its SafeMultiSigTransaction
// events, from its logs.
type collector struct {
	*Finder
	safe       ethrpc.Address
	executions []Execution
	// events are the SafeMultiSigTransaction logs by onchain transaction, whose
	// hash matches the transaction of the execution that follows.
	events map[ethrpc.Hash][]*safenet.SafeTransaction
	// seen are the logs collected, by block and log index, so that a block that is
	// scanned again adds nothing.
	seen map[[2]uint64]bool
}

func newCollector(f *Finder, safe ethrpc.Address) *collector {
	return &collector{
		Finder: f, safe: safe, executions: []Execution{},
		events: make(map[ethrpc.Hash][]*safenet.SafeTransaction), seen: make(map[[2]uint64]bool),
	}
}

// scan collects the executions and events of the Safe in the blocks from to to,
// including both.
func (c *collector) scan(ctx context.Context, from, to ethrpc.BlockNumber) error {
	logs := c.Eth.ScanLogs(ctx, ethrpc.LogFilter{
		FromBlock: from,
		ToBlock:   to,
		Addresses: []ethrpc.Address{c.safe},
		Topics:    [][]ethrpc.Hash{{executionSuccessEvent, executionFailureEvent, multiSigEvent}},
	})
	for log, err := range logs {
		if err != nil {
			return fmt.Errorf("listing Safe transactions of %s: %w", c.safe, err)
		}
		key := [2]uint64{uint64(log.BlockNumber), uint64(log.LogIndex)}
		if log.Removed || len(log.Topics) == 0 || c.seen[key] {
			continue
		}
		c.seen[key] = true
		if log.Topics[0] == multiSigEvent {
			if tx := c.decodeEvent(c.safe, log.Data); tx != nil {
				c.events[log.TransactionHash] = append(c.events[log.TransactionHash], tx)
			}
			continue
		}
		if e, ok := decodeExecution(log); ok {
			c.executions = append(c.executions, e)
		}
	}
	return nil
}

// readNonce returns the nonce of the Safe at block.
func (f *Finder) readNonce(ctx context.Context, safe ethrpc.Address, block ethrpc.BlockNumber) (*big.Int, error) {
	result, err := f.Eth.Call(ctx, ethrpc.CallRequest{To: safe, Data: solabi.Call(nonceSelector)}, block)
	if err != nil {
		return nil, fmt.Errorf("reading the nonce of %s at block %d: %w", safe, block, err)
	}
	d := solabi.NewDecoder(result)
	nonce := d.Uint(0)
	if err := d.Err(); err != nil {
		return nil, fmt.Errorf("reading the nonce of %s at block %d: %w", safe, block, err)
	}
	return nonce, nil
}

// decodeExecution decodes an ExecutionSuccess or ExecutionFailure log. Safe
// 1.3.0 has its transaction hash and payment in the data, and later versions
// have the transaction hash as a topic, and the payment in the data.
func decodeExecution(log ethrpc.Log) (Execution, bool) {
	e := Execution{
		Block:    uint64(log.BlockNumber),
		TxHash:   log.TransactionHash,
		LogIndex: uint64(log.LogIndex),
		Success:  log.Topics[0] == executionSuccessEvent,
	}
	switch {
	case len(log.Topics) == 1 && len(log.Data) == 64:
		e.SafeTxHash = ethrpc.Hash(log.Data[:32])
		e.Payment = new(big.Int).SetBytes(log.Data[32:])
	case len(log.Topics) == 2 && len(log.Data) == 32:
		e.SafeTxHash = log.Topics[1]
		e.Payment = new(big.Int).SetBytes(log.Data)
	default:
		return Execution{}, false
	}
	return e, true
}

// decodeEvent decodes the data of a SafeMultiSigTransaction log of the Safe,
// which has the Safe transaction's fields, its signatures, and the nonce, the
// sender, and the threshold as additional information. It returns nil if the
// data is malformed.
func (f *Finder) decodeEvent(safe ethrpc.Address, data []byte) *safenet.SafeTransaction {
	d := solabi.NewDecoder(data)
	tx := decodeFields(d, safe, f.Eth.ChainID())
	info := d.Bytes(10)
	if d.Err() != nil || len(info) < 32 {
		return nil
	}
	tx.Nonce = new(big.Int).SetBytes(info[:32])
	return tx
}

// decodeFields decodes the first nine values of a tuple of the Safe
// transaction's fields, as they are in the arguments of execTransaction and in
// the data of SafeMultiSigTransaction: the transaction's target, value, data,
// and operation, and its gas and refund parameters. It leaves the nonce unset.
func decodeFields(d *solabi.Decoder, safe ethrpc.Address, chainID uint64) *safenet.SafeTransaction {
	tx := &safenet.SafeTransaction{ChainID: new(big.Int).SetUint64(chainID), Safe: safe}
	tx.To = d.Address(0)
	tx.Value = d.Uint(1)
	tx.Data = d.Bytes(2)
	operation := d.Uint64(3)
	tx.SafeTxGas = d.Uint(4)
	tx.BaseGas = d.Uint(5)
	tx.GasPrice = d.Uint(6)
	tx.GasToken = d.Address(7)
	tx.RefundReceiver = d.Address(8)
	if operation > uint64(safenet.OperationDelegateCall) && d.Err() == nil {
		// The Safe rejects the operation, which is an enum.
		tx.Operation = safenet.Operation(255)
	} else {
		tx.Operation = safenet.Operation(operation)
	}
	return tx
}

// decodeCall decodes the calldata of a call to execTransaction on the Safe. It
// returns nil if the calldata is for another function or is malformed. The
// nonce is unset.
func decodeCall(safe ethrpc.Address, chainID uint64, input []byte) *safenet.SafeTransaction {
	if len(input) < 4 || [4]byte(input[:4]) != execTransaction {
		return nil
	}
	d := solabi.NewDecoder(input[4:])
	tx := decodeFields(d, safe, chainID)
	d.Bytes(9)
	if d.Err() != nil {
		return nil
	}
	return tx
}

// matches reports whether tx is a Safe transaction with the hash want. It
// doesn't hash a transaction with a missing or oversized field, which the ABI
// encoding of the hash doesn't allow.
func matches(tx *safenet.SafeTransaction, want ethrpc.Hash) bool {
	if tx == nil || tx.Operation > safenet.OperationDelegateCall {
		return false
	}
	for _, n := range []*big.Int{tx.ChainID, tx.Value, tx.SafeTxGas, tx.BaseGas, tx.GasPrice, tx.Nonce} {
		if n == nil || n.Sign() < 0 || n.BitLen() > 256 {
			return false
		}
	}
	return tx.Hash() == want
}
