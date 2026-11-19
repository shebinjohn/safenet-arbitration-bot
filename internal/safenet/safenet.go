// Package safenet reads Safenet oracle requests and their arbitration from the
// chain.
//
// It is stateless: it keeps no index or cache, and every query reads what it
// needs from a Gnosis Chain node, as of a given block.
package safenet

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// ErrRequestNotFound is returned for a request ID that the oracle has no
// request for.
var ErrRequestNotFound = errors.New("request not found")

// SentinelOracle functions.
var (
	getRequestSelector         = solabi.Selector("getRequest(bytes32)")
	commitWindowSelector       = solabi.Selector("COMMIT_WINDOW()")
	arbitrationTimeoutSelector = solabi.Selector("ARBITRATION_TIMEOUT()")
	proposerSelector           = solabi.Selector("PROPOSER()")
	charterENSSelector         = solabi.Selector("charterEns()")
	feeTokenSelector           = solabi.Selector("FEE_TOKEN()")
	requestNotFoundSelector    = solabi.Selector("RequestNotFound()")
)

// SentinelOracle and Consensus events.
var (
	transactionProposedEvent = solabi.Event("TransactionProposed(bytes32,bytes32,address,uint64,bytes,(uint256,address,address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,uint256))")
	committedEvent           = solabi.Event("Committed(bytes32,address,uint96)")
	revealedEvent            = solabi.Event("Revealed(bytes32,address,bool,uint96,string)")
	disputeTriggeredEvent    = solabi.Event("DisputeTriggered(bytes32,uint64)")
	disputeResolvedEvent     = solabi.Event("DisputeResolved(bytes32,uint8,uint128,string)")
	disputeOutOfScopeEvent   = solabi.Event("DisputeOutOfScope(bytes32,string)")
	arbitrationTimedOutEvent = solabi.Event("ArbitrationTimedOut(bytes32)")
)

// arbitrationEvents are the events that freeze a request for arbitration, and
// that settle its arbitration.
var arbitrationEvents = []ethrpc.Hash{
	disputeTriggeredEvent,
	disputeResolvedEvent,
	disputeOutOfScopeEvent,
	arbitrationTimedOutEvent,
}

// Safenet reads the requests of a SentinelOracle.
type Safenet struct {
	dial      ethrpc.Dialer
	oracle    ethrpc.Address
	consensus ethrpc.Address
}

// New returns a Safenet that reads the SentinelOracle at oracle and the
// Consensus contract at consensus on Gnosis Chain, whose PROPOSER must be
// consensus. Each query connects to Gnosis Chain, and to Ethereum Mainnet and
// the Safes' chains as needed, with dial.
func New(dial ethrpc.Dialer, oracle, consensus ethrpc.Address) *Safenet {
	return &Safenet{dial: dial, oracle: oracle, consensus: consensus}
}

// session is a Safenet connected to Gnosis Chain, for one query, which reads
// the chain as of block.
type session struct {
	*Safenet
	eth   *ethrpc.Client
	block ethrpc.BlockNumber
}

// connect returns a session connected to Gnosis Chain, as of its latest block.
func (s *Safenet) connect(ctx context.Context) (*session, error) {
	eth, err := s.dial(ctx, ethrpc.Gnosis)
	if err != nil {
		return nil, fmt.Errorf("connecting to Gnosis Chain: %w", err)
	}
	block, err := eth.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting the latest Gnosis Chain block: %w", err)
	}
	return &session{Safenet: s, eth: eth, block: block}, nil
}

// FinalizedBlock returns the latest finalized Gnosis Chain block.
func (s *Safenet) FinalizedBlock(ctx context.Context) (uint64, error) {
	q, err := s.connect(ctx)
	if err != nil {
		return 0, err
	}
	block, err := q.eth.FinalizedBlock(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting the finalized Gnosis Chain block: %w", err)
	}
	return uint64(block.Number), nil
}

// Rulings returns the requests that the arbitrator ruled on or marked out of
// scope in blocks from to to, which must not be after the latest block, in the
// order of the rulings. Timed out arbitrations aren't rulings, so they are left
// out.
//
// The requests are read as of the latest block, as nodes may not have the state
// of block to. A ruling settles a request for good, so this gives the same
// requests as reading them as of block to would.
func (s *Safenet) Rulings(ctx context.Context, from, to uint64) ([]*Request, error) {
	q, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	if ethrpc.BlockNumber(to) > q.block {
		return nil, fmt.Errorf("block %d is after the latest Gnosis Chain block %d", to, q.block)
	}
	var ids []ethrpc.Hash
	logs := q.eth.ScanLogs(ctx, ethrpc.LogFilter{
		FromBlock: ethrpc.BlockNumber(from),
		ToBlock:   ethrpc.BlockNumber(to),
		Addresses: []ethrpc.Address{s.oracle},
		Topics:    [][]ethrpc.Hash{{disputeResolvedEvent, disputeOutOfScopeEvent}},
	})
	for log, err := range logs {
		if err != nil {
			return nil, fmt.Errorf("listing rulings: %w", err)
		}
		if log.Removed {
			continue
		}
		if len(log.Topics) != 2 {
			return nil, fmt.Errorf("listing rulings: log %d in block %d has %d topics, want 2", log.LogIndex, log.BlockNumber, len(log.Topics))
		}
		ids = append(ids, log.Topics[1])
	}

	requests := make([]*Request, len(ids))
	for i, id := range ids {
		if requests[i], err = q.request(ctx, id); err != nil {
			return nil, err
		}
	}
	return requests, nil
}

// Pending returns the disputes awaiting arbitration as of the latest block,
// ordered by deadline. These are the frozen requests that the arbitrator can
// still rule on before the deadline: as a dispute's deadline is the block it
// was frozen in plus the oracle's ARBITRATION_TIMEOUT, only that many blocks
// need scanning.
//
// Overdue disputes are left out, although the arbitrator can rule on them until
// someone calls timeoutArbitration, as sentinels generally call it promptly to
// reclaim their bonds.
func (s *Safenet) Pending(ctx context.Context) ([]Dispute, error) {
	q, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	return q.pending(ctx)
}

func (s *session) pending(ctx context.Context) ([]Dispute, error) {
	block := s.block
	timeout, err := s.callUint64(ctx, block, arbitrationTimeoutSelector)
	if err != nil {
		return nil, err
	}

	disputes := []Dispute{}
	settled := make(map[ethrpc.Hash]bool)
	logs := s.eth.ScanLogs(ctx, ethrpc.LogFilter{
		FromBlock: block - min(ethrpc.BlockNumber(timeout), block),
		ToBlock:   block,
		Addresses: []ethrpc.Address{s.oracle},
		Topics:    [][]ethrpc.Hash{arbitrationEvents},
	})
	for log, err := range logs {
		if err != nil {
			return nil, fmt.Errorf("listing disputes: %w", err)
		}
		if log.Removed {
			continue
		}
		if len(log.Topics) != 2 {
			return nil, fmt.Errorf("listing disputes: log %d in block %d has %d topics, want 2", log.LogIndex, log.BlockNumber, len(log.Topics))
		}
		id := log.Topics[1]
		if log.Topics[0] != disputeTriggeredEvent {
			settled[id] = true
			continue
		}
		d := solabi.NewDecoder(log.Data)
		dispute := Dispute{RequestID: id, FrozenBlock: uint64(log.BlockNumber), Deadline: d.Uint64(0)}
		if err := d.Err(); err != nil {
			return nil, fmt.Errorf("decoding DisputeTriggered for request %s: %w", id, err)
		}
		disputes = append(disputes, dispute)
	}

	disputes = slices.DeleteFunc(disputes, func(d Dispute) bool { return settled[d.RequestID] })
	slices.SortStableFunc(disputes, func(a, b Dispute) int { return cmp.Compare(a.Deadline, b.Deadline) })
	return disputes, nil
}

// Request returns the request with the given ID as of the latest block: the
// oracle's record of it, the transaction proposal it is for, the sentinels'
// votes, and its arbitration. It returns an error wrapping ErrRequestNotFound
// if the oracle has no request with the ID.
//
// The proposal is taken from the Consensus logs, and only accepted if hashing
// it gives both the logged Safe transaction hash and the request ID.
func (s *Safenet) Request(ctx context.Context, id ethrpc.Hash) (*Request, error) {
	q, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	return q.request(ctx, id)
}

func (s *session) request(ctx context.Context, id ethrpc.Hash) (*Request, error) {
	block := s.block
	request, progress, err := s.getRequest(ctx, id, block)
	if err != nil {
		return nil, err
	}
	commitWindow, err := s.callUint64(ctx, block, commitWindowSelector)
	if err != nil {
		return nil, err
	}
	proposer, err := s.callAddress(ctx, block, proposerSelector)
	if err != nil {
		return nil, err
	}
	if proposer != s.consensus {
		return nil, fmt.Errorf("SentinelOracle %s has PROPOSER %s, not Consensus %s", s.oracle, proposer, s.consensus)
	}
	if request.Charter, err = s.callString(ctx, block, charterENSSelector); err != nil {
		return nil, err
	}
	if request.FeeToken, err = s.feeToken(ctx, block); err != nil {
		return nil, err
	}

	// The oracle sets the commit deadline to the proposal block plus its
	// COMMIT_WINDOW.
	if commitWindow > request.Terms.CommitDeadline {
		return nil, fmt.Errorf("request %s: commit deadline %d is before COMMIT_WINDOW %d", id, request.Terms.CommitDeadline, commitWindow)
	}
	if request.Proposal, err = s.proposal(ctx, id, request.Terms.CommitDeadline-commitWindow); err != nil {
		return nil, err
	}
	if err := s.proposalBlocks(ctx, &request.Proposal); err != nil {
		return nil, fmt.Errorf("request %s: %w", id, err)
	}

	to := min(ethrpc.BlockNumber(request.Terms.RevealDeadline), block)
	if request.Votes, err = s.votes(ctx, id, ethrpc.BlockNumber(request.Proposal.Block), to); err != nil {
		return nil, err
	}
	if err := progress.check(request.Votes); err != nil {
		return nil, fmt.Errorf("request %s: %w", id, err)
	}
	request.DisputedRules = DisputedRules(request.Votes)

	if progress.arbitrationDeadline != 0 {
		timeout, err := s.callUint64(ctx, block, arbitrationTimeoutSelector)
		if err != nil {
			return nil, err
		}
		if request.Arbitration, err = s.arbitration(ctx, id, request.State, progress.arbitrationDeadline, timeout, block); err != nil {
			return nil, err
		}
	}
	return request, nil
}

// progress is the part of the oracle's record of a request that Request checks
// the logs against.
type progress struct {
	arbitrationDeadline uint64
	committed           int
	revealed            int
	approvals           int
	denials             int
}

// check returns an error unless votes match the vote counts.
func (p progress) check(votes []Commitment) error {
	var revealed, approvals, denials int
	for _, vote := range votes {
		switch vote.Vote {
		case VoteApproved:
			revealed, approvals = revealed+1, approvals+1
		case VoteDenied:
			revealed, denials = revealed+1, denials+1
		}
	}
	got := progress{p.arbitrationDeadline, len(votes), revealed, approvals, denials}
	if got != p {
		return fmt.Errorf("logs have %d commits, %d reveals, %d approvals, and %d denials, but the oracle counts %d, %d, %d, and %d",
			got.committed, got.revealed, got.approvals, got.denials, p.committed, p.revealed, p.approvals, p.denials)
	}
	return nil
}

// getRequest reads the oracle's record of a request (getRequest).
func (s *session) getRequest(ctx context.Context, id ethrpc.Hash, block ethrpc.BlockNumber) (*Request, progress, error) {
	result, err := s.eth.Call(ctx, ethrpc.CallRequest{To: s.oracle, Data: solabi.Call(getRequestSelector, id)}, block)
	if isRevert(err, requestNotFoundSelector) {
		return nil, progress{}, fmt.Errorf("request %s: %w", id, ErrRequestNotFound)
	}
	if err != nil {
		return nil, progress{}, fmt.Errorf("getting request %s: %w", id, err)
	}

	// SentinelOracleRequest.T is a static struct of Terms and Progress, so it is
	// encoded as the sequence of their fields, including padding.
	d := solabi.NewDecoder(result)
	request := &Request{
		ID:     id,
		Oracle: s.oracle,
		Terms: Terms{
			CommitDeadline: d.Uint64(0),
			DAOFeeShare:    uint32(d.Uint64(1)),
			RevealDeadline: d.Uint64(2),
			Bond:           d.Uint(3),
			Sponsor:        d.Address(5),
			SlashAmount:    d.Uint(6),
		},
		State: State(d.Uint64(7)),
		Fee:   d.Uint(8),
	}
	p := progress{
		arbitrationDeadline: d.Uint64(9),
		committed:           int(d.Uint64(10)),
		revealed:            int(d.Uint64(11)),
		approvals:           int(d.Uint64(12)),
		denials:             int(d.Uint64(13)),
	}
	if err := d.Err(); err != nil {
		return nil, progress{}, fmt.Errorf("decoding request %s: %w", id, err)
	}
	return request, p, nil
}

// proposal finds the transaction proposal for request id, which was made in
// block.
func (s *session) proposal(ctx context.Context, id ethrpc.Hash, block uint64) (Proposal, error) {
	logs, err := s.eth.GetLogs(ctx, ethrpc.LogFilter{
		FromBlock: ethrpc.BlockNumber(block),
		ToBlock:   ethrpc.BlockNumber(block),
		Addresses: []ethrpc.Address{s.consensus},
		Topics:    [][]ethrpc.Hash{{transactionProposedEvent}, nil, nil, {solabi.Address(s.oracle)}},
	})
	if err != nil {
		return Proposal{}, fmt.Errorf("getting proposal for request %s: %w", id, err)
	}

	for _, log := range logs {
		if log.Removed || len(log.Topics) != 4 {
			continue
		}
		proposal, err := decodeProposal(log)
		if err != nil {
			return Proposal{}, fmt.Errorf("decoding proposal in transaction %s: %w", log.TransactionHash, err)
		}
		if requestID(s.eth.ChainID(), s.consensus, proposal.Epoch, s.oracle, proposal.OracleData, proposal.SafeTxHash) != id {
			continue
		}
		if hash := proposal.Transaction.Hash(); hash != proposal.SafeTxHash {
			return Proposal{}, fmt.Errorf("proposal in transaction %s: Safe transaction hashes to %s, but the log has %s", log.TransactionHash, hash, proposal.SafeTxHash)
		}
		header, err := s.eth.GetBlockByNumber(ctx, log.BlockNumber)
		if err != nil {
			return Proposal{}, fmt.Errorf("getting proposal block: %w", err)
		}
		proposal.Consensus = s.consensus
		proposal.Time = header.Time()
		return proposal, nil
	}
	return Proposal{}, fmt.Errorf("request %s: no proposal in block %d hashes to the request ID", id, block)
}

// proposalBlocks sets the blocks of p that are the last before its time on
// Ethereum Mainnet and on the Safe's chain. It searches the chains
// concurrently.
func (s *session) proposalBlocks(ctx context.Context, p *Proposal) error {
	if !p.Transaction.ChainID.IsUint64() {
		return fmt.Errorf("the Safe transaction's chain ID %s is out of range", p.Transaction.ChainID)
	}
	safeChain := p.Transaction.ChainID.Uint64()
	chains := []uint64{ethrpc.Mainnet}
	if safeChain != ethrpc.Mainnet && safeChain != s.eth.ChainID() {
		chains = append(chains, safeChain)
	}
	blocks := make([]ethrpc.Block, len(chains))
	errs := make([]error, len(chains))
	var wg sync.WaitGroup
	for i, chain := range chains {
		wg.Go(func() { blocks[i], errs[i] = s.blockBefore(ctx, chain, p.Time) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}

	p.EthereumBlock, p.EthereumBlockTime = uint64(blocks[0].Number), blocks[0].Time()
	safeBlock := blocks[0]
	switch safeChain {
	case s.eth.ChainID():
		block, err := s.parentBlock(ctx, p)
		if err != nil {
			return err
		}
		safeBlock = block
	case ethrpc.Mainnet:
	default:
		safeBlock = blocks[1]
	}
	p.SafeBlock, p.SafeBlockTime = uint64(safeBlock.Number), safeBlock.Time()
	return nil
}

// parentBlock returns the last block before p's time on the chain of the
// proposal. Gnosis Chain gives each block a later timestamp than its parent, so
// this is the proposal block's parent. The search is for chains that don't.
func (s *session) parentBlock(ctx context.Context, p *Proposal) (ethrpc.Block, error) {
	parent, err := s.eth.GetBlockByNumber(ctx, ethrpc.BlockNumber(p.Block-1))
	if err != nil {
		return ethrpc.Block{}, fmt.Errorf("getting the block before the proposal: %w", err)
	}
	if !parent.Time().Before(p.Time) {
		if parent, err = s.eth.SearchBlock(ctx, p.Time); err != nil {
			return ethrpc.Block{}, err
		}
	}
	return parent, nil
}

// blockBefore returns the last block before t on the chain with the given ID.
func (s *session) blockBefore(ctx context.Context, chainID uint64, t time.Time) (ethrpc.Block, error) {
	eth, err := s.dial(ctx, chainID)
	if err != nil {
		return ethrpc.Block{}, fmt.Errorf("connecting to chain %d: %w", chainID, err)
	}
	block, err := eth.SearchBlock(ctx, t)
	if err != nil {
		return ethrpc.Block{}, fmt.Errorf("chain %d: %w", chainID, err)
	}
	return block, nil
}

// decodeProposal decodes a TransactionProposed log.
func decodeProposal(log ethrpc.Log) (Proposal, error) {
	d := solabi.NewDecoder(log.Data)
	tx := d.Tuple(2)
	proposal := Proposal{
		Block:      uint64(log.BlockNumber),
		TxHash:     log.TransactionHash,
		Epoch:      d.Uint64(0),
		OracleData: d.Bytes(1),
		SafeTxHash: log.Topics[1],
		Transaction: SafeTransaction{
			ChainID:        tx.Uint(0),
			Safe:           tx.Address(1),
			To:             tx.Address(2),
			Value:          tx.Uint(3),
			Data:           tx.Bytes(4),
			Operation:      Operation(tx.Uint64(5)),
			SafeTxGas:      tx.Uint(6),
			BaseGas:        tx.Uint(7),
			GasPrice:       tx.Uint(8),
			GasToken:       tx.Address(9),
			RefundReceiver: tx.Address(10),
			Nonce:          tx.Uint(11),
		},
	}
	if err := d.Err(); err != nil {
		return Proposal{}, err
	}
	if proposal.Transaction.Operation > OperationDelegateCall {
		return Proposal{}, fmt.Errorf("invalid operation %d", proposal.Transaction.Operation)
	}
	return proposal, nil
}

// votes returns the sentinels' votes on request id, in commit order, from the
// oracle logs in blocks from to to.
func (s *session) votes(ctx context.Context, id ethrpc.Hash, from, to ethrpc.BlockNumber) ([]Commitment, error) {
	votes := []Commitment{}
	index := make(map[ethrpc.Address]int)
	logs := s.eth.ScanLogs(ctx, ethrpc.LogFilter{
		FromBlock: from,
		ToBlock:   to,
		Addresses: []ethrpc.Address{s.oracle},
		Topics:    [][]ethrpc.Hash{{committedEvent, revealedEvent}, {id}},
	})
	for log, err := range logs {
		if err != nil {
			return nil, fmt.Errorf("getting votes on request %s: %w", id, err)
		}
		if log.Removed {
			continue
		}
		if len(log.Topics) != 3 {
			return nil, fmt.Errorf("getting votes on request %s: log %d in block %d has %d topics, want 3", id, log.LogIndex, log.BlockNumber, len(log.Topics))
		}
		d := solabi.NewDecoder(log.Data)
		sentinel := ethrpc.Address(log.Topics[2][12:])
		i, committed := index[sentinel]
		if log.Topics[0] == committedEvent {
			if committed {
				return nil, fmt.Errorf("request %s: sentinel %s committed twice", id, sentinel)
			}
			index[sentinel] = len(votes)
			votes = append(votes, Commitment{Sentinel: sentinel, Bond: d.Uint(0), Vote: VotePending, Rules: []string{}})
		} else {
			if !committed || votes[i].Vote != VotePending {
				return nil, fmt.Errorf("request %s: sentinel %s revealed a vote it didn't commit", id, sentinel)
			}
			votes[i].Vote = VoteDenied
			if d.Bool(0) {
				votes[i].Vote = VoteApproved
			}
			votes[i].Reason = d.String(2)
			if votes[i].Vote == VoteDenied {
				votes[i].Rules = CitedRules(votes[i].Reason)
			}
		}
		if err := d.Err(); err != nil {
			return nil, fmt.Errorf("decoding vote by %s on request %s: %w", sentinel, id, err)
		}
	}
	return votes, nil
}

// outcomeStates are the states that the outcomes of an arbitration leave the
// request in. Declining to rule and timing out both refund the request, like a
// request that no sentinel revealed a vote on.
var outcomeStates = map[Outcome]State{
	OutcomePending:    StateFrozen,
	OutcomeSecure:     StateResolvedApproved,
	OutcomeInsecure:   StateResolvedDenied,
	OutcomeOutOfScope: StateTimedOut,
	OutcomeTimedOut:   StateTimedOut,
}

// arbitration returns the arbitration of the frozen request id, as of block.
// The request was frozen in the block ARBITRATION_TIMEOUT blocks before its
// deadline, and a request that is no longer frozen was settled after that.
func (s *session) arbitration(ctx context.Context, id ethrpc.Hash, state State, deadline, timeout uint64, block ethrpc.BlockNumber) (*Arbitration, error) {
	if timeout > deadline {
		return nil, fmt.Errorf("request %s: arbitration deadline %d is before ARBITRATION_TIMEOUT %d", id, deadline, timeout)
	}
	arbitration := &Arbitration{FrozenBlock: deadline - timeout, Deadline: deadline, Outcome: OutcomePending}
	to := block
	if state == StateFrozen {
		to = ethrpc.BlockNumber(arbitration.FrozenBlock)
	}

	var frozen bool
	logs := s.eth.ScanLogs(ctx, ethrpc.LogFilter{
		FromBlock: ethrpc.BlockNumber(arbitration.FrozenBlock),
		ToBlock:   to,
		Addresses: []ethrpc.Address{s.oracle},
		Topics:    [][]ethrpc.Hash{arbitrationEvents, {id}},
	})
	for log, err := range logs {
		if err != nil {
			return nil, fmt.Errorf("getting arbitration of request %s: %w", id, err)
		}
		if log.Removed {
			continue
		}
		if len(log.Topics) != 2 {
			return nil, fmt.Errorf("getting arbitration of request %s: log %d in block %d has %d topics, want 2", id, log.LogIndex, log.BlockNumber, len(log.Topics))
		}
		d := solabi.NewDecoder(log.Data)
		switch log.Topics[0] {
		case disputeTriggeredEvent:
			frozen = true
			if logged := d.Uint64(0); d.Err() == nil && logged != deadline {
				return nil, fmt.Errorf("request %s: DisputeTriggered has deadline %d, but the oracle has %d", id, logged, deadline)
			}
		case disputeResolvedEvent:
			switch State(d.Uint64(0)) {
			case StateResolvedApproved:
				arbitration.Outcome = OutcomeSecure
			case StateResolvedDenied:
				arbitration.Outcome = OutcomeInsecure
			default:
				return nil, fmt.Errorf("request %s: DisputeResolved has invalid outcome %d", id, d.Uint64(0))
			}
			arbitration.Slashed = d.Uint(1)
			arbitration.Context = d.String(2)
		case disputeOutOfScopeEvent:
			arbitration.Outcome = OutcomeOutOfScope
			arbitration.Context = d.String(0)
		case arbitrationTimedOutEvent:
			arbitration.Outcome = OutcomeTimedOut
		}
		if err := d.Err(); err != nil {
			return nil, fmt.Errorf("decoding arbitration log of request %s: %w", id, err)
		}
		if arbitration.Outcome != OutcomePending {
			arbitration.Record = &Record{Block: uint64(log.BlockNumber), TxHash: log.TransactionHash}
			break
		}
	}

	if !frozen {
		return nil, fmt.Errorf("request %s: no DisputeTriggered log in block %d", id, arbitration.FrozenBlock)
	}
	if outcomeStates[arbitration.Outcome] != state {
		return nil, fmt.Errorf("request %s is %s, but its arbitration logs have outcome %s", id, state, arbitration.Outcome)
	}
	return arbitration, nil
}

// ERC-20 functions.
var (
	symbolSelector   = solabi.Selector("symbol()")
	decimalsSelector = solabi.Selector("decimals()")
)

// feeToken returns the oracle's FEE_TOKEN, with its symbol and decimals.
func (s *session) feeToken(ctx context.Context, block ethrpc.BlockNumber) (Token, error) {
	address, err := s.callAddress(ctx, block, feeTokenSelector)
	if err != nil {
		return Token{}, err
	}
	call := func(selector [4]byte) (*solabi.Decoder, error) {
		result, err := s.eth.Call(ctx, ethrpc.CallRequest{To: address, Data: solabi.Call(selector)}, block)
		return solabi.NewDecoder(result), err
	}
	token := Token{Address: address}
	d, err := call(symbolSelector)
	if err == nil {
		token.Symbol = d.String(0)
		err = d.Err()
	}
	if err != nil {
		return Token{}, fmt.Errorf("getting the symbol of fee token %s: %w", address, err)
	}
	d, err = call(decimalsSelector)
	if err == nil {
		decimals := d.Uint64(0)
		if err = d.Err(); err == nil && decimals > 255 {
			err = fmt.Errorf("%d decimals", decimals)
		}
		token.Decimals = uint8(decimals)
	}
	if err != nil {
		return Token{}, fmt.Errorf("getting the decimals of fee token %s: %w", address, err)
	}
	return token, nil
}

// call calls a SentinelOracle function without arguments, and returns a decoder
// for its return data.
func (s *session) call(ctx context.Context, block ethrpc.BlockNumber, selector [4]byte) (*solabi.Decoder, error) {
	result, err := s.eth.Call(ctx, ethrpc.CallRequest{To: s.oracle, Data: solabi.Call(selector)}, block)
	if err != nil {
		return nil, fmt.Errorf("calling SentinelOracle %s: %w", s.oracle, err)
	}
	return solabi.NewDecoder(result), nil
}

func (s *session) callUint64(ctx context.Context, block ethrpc.BlockNumber, selector [4]byte) (uint64, error) {
	d, err := s.call(ctx, block, selector)
	if err != nil {
		return 0, err
	}
	value := d.Uint64(0)
	if err := d.Err(); err != nil {
		return 0, fmt.Errorf("calling SentinelOracle %s: %w", s.oracle, err)
	}
	return value, nil
}

func (s *session) callAddress(ctx context.Context, block ethrpc.BlockNumber, selector [4]byte) (ethrpc.Address, error) {
	d, err := s.call(ctx, block, selector)
	if err != nil {
		return ethrpc.Address{}, err
	}
	value := d.Address(0)
	if err := d.Err(); err != nil {
		return ethrpc.Address{}, fmt.Errorf("calling SentinelOracle %s: %w", s.oracle, err)
	}
	return value, nil
}

func (s *session) callString(ctx context.Context, block ethrpc.BlockNumber, selector [4]byte) (string, error) {
	d, err := s.call(ctx, block, selector)
	if err != nil {
		return "", err
	}
	value := d.String(0)
	if err := d.Err(); err != nil {
		return "", fmt.Errorf("calling SentinelOracle %s: %w", s.oracle, err)
	}
	return value, nil
}

// isRevert reports whether err is a JSON-RPC error for a call that reverted
// with the custom error that selector identifies.
func isRevert(err error, selector [4]byte) bool {
	rpcErr, ok := errors.AsType[*ethrpc.Error](err)
	if !ok {
		return false
	}
	var data ethrpc.Bytes
	if json.Unmarshal(rpcErr.Data, &data) != nil {
		return false
	}
	return bytes.Equal(data, selector[:])
}
