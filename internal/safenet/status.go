package safenet

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// Progress is the state of a request as of a block: what may have changed since
// its proposal, without the proposal and the votes, which Request reads too.
type Progress struct {
	// Block is the Gnosis Chain block that the request was read at.
	Block uint64 `json:"block"`
	State State  `json:"state"`
	Terms Terms  `json:"terms"`
	// ArbitrationTimeout is the oracle's ARBITRATION_TIMEOUT.
	ArbitrationTimeout uint64 `json:"arbitrationTimeout"`
	// Arbitration is nil unless a split vote froze the request.
	Arbitration *Arbitration `json:"arbitration"`
}

// Progress returns the state, the terms, and the arbitration of the request
// with the given ID as of the latest block. It returns an error wrapping
// ErrRequestNotFound if the oracle has no request with the ID.
//
// It reads less than Request does, for a caller that has the request already,
// as arbot info wrote it, and needs what may have changed since, such as an
// arbitration that was pending then.
func (s *Safenet) Progress(ctx context.Context, id ethrpc.Hash) (*Progress, error) {
	q, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	request, progress, err := q.getRequest(ctx, id, q.block)
	if err != nil {
		return nil, err
	}
	timeout, err := q.callUint64(ctx, q.block, arbitrationTimeoutSelector)
	if err != nil {
		return nil, err
	}
	p := &Progress{Block: uint64(q.block), State: request.State, Terms: request.Terms, ArbitrationTimeout: timeout}
	if progress.arbitrationDeadline != 0 {
		if p.Arbitration, err = q.arbitration(ctx, id, request.State, progress.arbitrationDeadline, timeout, q.block); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// SafeProposal is a transaction proposal to the oracle for a Safe, as Proposals
// finds it.
type SafeProposal struct {
	// RequestID is the ID of the oracle request for the proposal.
	RequestID ethrpc.Hash `json:"request"`
	// Block and TxHash are the Gnosis Chain block and transaction of the proposal.
	Block      uint64      `json:"block"`
	TxHash     ethrpc.Hash `json:"txHash"`
	SafeTxHash ethrpc.Hash `json:"safeTxHash"`
	// ChainID and Nonce are those of the proposed Safe transaction.
	ChainID *big.Int `json:"chainId"`
	Nonce   *big.Int `json:"nonce"`
}

// Proposals returns the transaction proposals to the oracle for the Safe at
// safe on the chain with the ID chainID, in the Gnosis Chain blocks from to to,
// in order. It fails if to is after the latest block.
//
// Consensus logs the Safe's ID as the second topic of TransactionProposed: the
// chain ID in its upper 96 bits, and the address in its lower 160, so the logs
// are filtered by it. A proposal is only returned if it is for the Safe, and
// its Safe transaction hashes to the Safe transaction hash that it logged. Its
// request ID is the hash of the proposal, as the oracle computes it.
func (s *Safenet) Proposals(ctx context.Context, chainID uint64, safe ethrpc.Address, from, to uint64) ([]SafeProposal, error) {
	q, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	if ethrpc.BlockNumber(to) > q.block {
		return nil, fmt.Errorf("block %d is after the latest Gnosis Chain block %d", to, q.block)
	}
	proposals := []SafeProposal{}
	logs := q.eth.ScanLogs(ctx, ethrpc.LogFilter{
		FromBlock: ethrpc.BlockNumber(from),
		ToBlock:   ethrpc.BlockNumber(to),
		Addresses: []ethrpc.Address{s.consensus},
		Topics:    [][]ethrpc.Hash{{transactionProposedEvent}, nil, {SafeID(chainID, safe)}, {solabi.Address(s.oracle)}},
	})
	for log, err := range logs {
		if err != nil {
			return nil, fmt.Errorf("listing the proposals for %s: %w", safe, err)
		}
		if log.Removed || len(log.Topics) != 4 {
			continue
		}
		p, err := decodeProposal(log)
		if err != nil {
			return nil, fmt.Errorf("decoding the proposal in transaction %s: %w", log.TransactionHash, err)
		}
		if p.Transaction.Safe != safe || !p.Transaction.ChainID.IsUint64() || p.Transaction.ChainID.Uint64() != chainID || p.Transaction.Hash() != p.SafeTxHash {
			continue
		}
		proposals = append(proposals, SafeProposal{
			RequestID:  requestID(q.eth.ChainID(), s.consensus, p.Epoch, s.oracle, p.OracleData, p.SafeTxHash),
			Block:      p.Block,
			TxHash:     p.TxHash,
			SafeTxHash: p.SafeTxHash,
			ChainID:    p.Transaction.ChainID,
			Nonce:      p.Transaction.Nonce,
		})
	}
	return proposals, nil
}

// SafeID returns the ID that Consensus gives the Safe at safe on the chain with
// the ID chainID, in the topics of its TransactionProposed logs: the chain ID
// in its upper 96 bits, and the address in its lower 160.
func SafeID(chainID uint64, safe ethrpc.Address) ethrpc.Hash {
	id := solabi.Address(safe)
	binary.BigEndian.PutUint64(id[4:12], chainID)
	return id
}
