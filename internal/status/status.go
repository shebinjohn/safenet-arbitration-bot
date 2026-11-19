// Package status reports the status of the Safe transaction that a Safenet
// request is for: whether its nonce was already used when it was proposed,
// whether and when it was executed, confirmed onchain, and when that was
// against the request's voting and arbitration.
//
// These are facts about the proposal, not evidence of its effects: what
// happened after the request's Safe block isn't evidence (Charter § 3.6). The
// package reports them, and never classifies a request by them.
package status

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/executions"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// NotEvidence is the note that every report starts with.
const NotEvidence = "Effects after the Safe block aren't evidence (Charter § 3.6): this status reports facts about the proposal, not evidence of its effects."

// The statuses of an execution.
const (
	// Executed means that the Safe executed the request's Safe transaction.
	Executed = "executed"
	// NotExecuted means that the Safe hasn't used the transaction's nonce yet, so
	// it hasn't executed the transaction.
	NotExecuted = "not-executed"
	// OtherTransaction means that the Safe used the transaction's nonce for another
	// transaction, so it can never execute this one.
	OtherTransaction = "other-transaction"
	// Unknown means that the chain read doesn't settle which transaction used the
	// nonce, for the reason given.
	Unknown = "unknown"
)

// The sources of an execution's status.
const (
	// SourceService is an execution that the Safe Transaction Service names, and
	// whose receipt has the Safe's log of it.
	SourceService = "service"
	// SourceLogs is an execution found in a scan of the Safe's execution logs.
	SourceLogs = "logs"
	// SourceNonce is a status that the Safe's nonce settles.
	SourceNonce = "nonce"
)

// The values of Timing, in the order of a request's life.
const (
	BeforeProposal  = "before-proposal"
	DuringVoting    = "during-voting"
	AfterVoting     = "after-voting"
	AfterFreeze     = "after-freeze"
	AfterResolution = "after-resolution"
)

// Report is the status of a request's Safe transaction.
type Report struct {
	Request ethrpc.Hash `json:"request"`
	// State is the request's state as of the latest Gnosis Chain block.
	State      safenet.State  `json:"state"`
	Safe       ethrpc.Address `json:"safe"`
	ChainID    uint64         `json:"chainId"`
	SafeBlock  uint64         `json:"safeBlock"`
	SafeTxHash ethrpc.Hash    `json:"safeTxHash"`
	Nonce      Nonce          `json:"nonce"`
	Execution  Execution      `json:"execution"`
	// Timing places the execution, or the other transaction's, against the
	// request's life, or is nil if there is none.
	Timing *Timing `json:"timing"`
	// Queue is the other proposals for the Safe, or nil if they weren't read.
	Queue *Queue `json:"queue"`
	// Notes are facts and caveats in plain words, the first being NotEvidence.
	Notes []string `json:"notes"`
}

// Nonce is the Safe transaction's nonce, against the Safe's.
type Nonce struct {
	Transaction *big.Int `json:"transaction"`
	// SafeAtSafeBlock and SafeAtLatest are the Safe's nonce at the Safe block and
	// at the latest block of its chain, LatestBlock.
	SafeAtSafeBlock *big.Int `json:"safeAtSafeBlock"`
	SafeAtLatest    *big.Int `json:"safeAtLatest"`
	LatestBlock     uint64   `json:"latestBlock"`
	// Stale is whether the transaction's nonce is below the Safe's at the Safe
	// block: the Safe had used it by the proposal, so the transaction could never
	// be executed after it.
	Stale bool `json:"stale"`
	// ConsumedAfterSafeBlock is whether the Safe used the transaction's nonce after
	// the Safe block, by the latest block.
	ConsumedAfterSafeBlock bool `json:"consumedAfterSafeBlock"`
}

// Execution is whether, when, and by what the Safe transaction's nonce was
// used.
type Execution struct {
	// Status is Executed, NotExecuted, OtherTransaction, or Unknown.
	Status string `json:"status"`
	// TxHash, Block, BlockTime, and Success are the onchain transaction, the block
	// of the Safe's chain, its time, and whether the Safe transaction's call
	// succeeded, of the execution of this transaction or of the other one, and nil
	// otherwise. SafeTxHash is the Safe transaction hash of the execution, which is
	// nil for another transaction whose hash is unknown.
	TxHash     *ethrpc.Hash `json:"txHash"`
	Block      *uint64      `json:"block"`
	BlockTime  *time.Time   `json:"blockTime"`
	Success    *bool        `json:"success"`
	SafeTxHash *ethrpc.Hash `json:"safeTxHash"`
	// Source is SourceService, SourceLogs, or SourceNonce, or empty for Unknown.
	Source string `json:"source"`
	// Reason says how the status was found, or why it is unknown.
	Reason string `json:"reason"`
	// Service is the Safe Transaction Service that was asked, an offchain source (§
	// 3.4), or nil if the chain has none.
	Service *Service `json:"service"`
	// Scanned is the range of blocks of the Safe's chain whose execution logs were
	// scanned, or nil if none were.
	Scanned *executions.BlockRange `json:"scanned"`
}

// Service is a Safe Transaction Service, and when it was observed.
type Service struct {
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
}

// Timing places an execution against the request's life, by the timestamps of
// the blocks: those of the Safe's chain for the execution, and of Gnosis Chain
// for the rest.
type Timing struct {
	// Value is BeforeProposal, DuringVoting, AfterVoting, AfterFreeze, or
	// AfterResolution.
	Value     string `json:"value"`
	Execution Point  `json:"execution"`
	Proposal  Point  `json:"proposal"`
	// CommitDeadline and RevealDeadline are the last blocks for committing and
	// revealing votes.
	CommitDeadline Deadline `json:"commitDeadline"`
	RevealDeadline Deadline `json:"revealDeadline"`
	// Frozen is the block that the request was frozen in, or nil if it wasn't.
	Frozen *Point `json:"frozen"`
	// Resolved is the block that settled the arbitration, or nil if it isn't
	// settled.
	Resolved *Resolution `json:"resolved"`
	// Now is the latest Gnosis Chain block.
	Now Point `json:"now"`
}

// Point is a block and its timestamp.
type Point struct {
	Block uint64    `json:"block"`
	Time  time.Time `json:"time"`
}

// Deadline is a deadline block, which may not exist yet.
type Deadline struct {
	Block uint64 `json:"block"`
	// Reached is whether the block exists, and Time is its timestamp, or nil if it
	// doesn't.
	Reached bool       `json:"reached"`
	Time    *time.Time `json:"time"`
}

// Resolution is the block that settled an arbitration, with its outcome.
type Resolution struct {
	Point
	Outcome safenet.Outcome `json:"outcome"`
}

// Queue is the proposals for the same Safe, on the same chain, that Consensus
// logged in a range of Gnosis Chain blocks, other than the request's own.
type Queue struct {
	FromBlock uint64     `json:"fromBlock"`
	ToBlock   uint64     `json:"toBlock"`
	Proposals []Proposal `json:"proposals"`
}

// Proposal is another proposal for the Safe.
type Proposal struct {
	safenet.SafeProposal
	// SameNonce is whether its Safe transaction has the same nonce as the
	// request's, so that at most one of them can be executed.
	SameNonce bool `json:"sameNonce"`
}

// Checker reads the status of a request's Safe transaction.
type Checker struct {
	// Gnosis is a client for Gnosis Chain, and Safe one for the Safe's chain, which
	// may be the same.
	Gnosis *ethrpc.Client
	Safe   *ethrpc.Client
	// Service is a client for the Safe Transaction Service of the Safe's chain, or
	// nil if it has none.
	Service *txservice.Client
	// Blocks bounds the scan of the Safe's execution logs.
	Blocks uint64
	// Proposals lists the proposals for the Safe in the Gnosis Chain blocks from to
	// to, or is nil to leave the queue out.
	Proposals func(ctx context.Context, from, to uint64) ([]safenet.SafeProposal, error)
	// Warn is called with the errors of sources that failed for a reason other than
	// not having what was asked for, such as an unreachable service. It may be nil.
	Warn func(error)
}

// Check returns the status of the Safe transaction of r, whose state, terms,
// and arbitration as of the latest Gnosis Chain block are p.
//
// A request to a node that fails transiently (see ethrpc.Transient) fails it,
// since it says nothing about the chain. A scan of the Safe's logs that the
// node rejects for another reason leaves the execution unknown, with the
// reason.
func (c *Checker) Check(ctx context.Context, r *safenet.Request, p *safenet.Progress) (*Report, error) {
	tx := r.Proposal.Transaction
	if tx.ChainID == nil || !tx.ChainID.IsUint64() || tx.ChainID.Uint64() != c.Safe.ChainID() {
		return nil, fmt.Errorf("the request's Safe transaction is for chain %v, not %d", tx.ChainID, c.Safe.ChainID())
	}
	if tx.Nonce == nil {
		return nil, errors.New("the request's Safe transaction has no nonce")
	}
	report := &Report{
		Request: r.ID, State: p.State, Safe: tx.Safe, ChainID: tx.ChainID.Uint64(),
		SafeBlock: r.Proposal.SafeBlock, SafeTxHash: r.Proposal.SafeTxHash, Notes: []string{NotEvidence},
	}

	latest, err := c.Safe.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting the latest block of chain %d: %w", report.ChainID, err)
	}
	if uint64(latest) < report.SafeBlock {
		return nil, fmt.Errorf("the latest block %d of chain %d is before the Safe block %d", latest, report.ChainID, report.SafeBlock)
	}
	n := &report.Nonce
	n.Transaction, n.LatestBlock = tx.Nonce, uint64(latest)
	if n.SafeAtSafeBlock, err = c.nonce(ctx, tx.Safe, ethrpc.BlockNumber(report.SafeBlock)); err != nil {
		return nil, err
	}
	if n.SafeAtLatest, err = c.nonce(ctx, tx.Safe, latest); err != nil {
		return nil, err
	}
	n.Stale = tx.Nonce.Cmp(n.SafeAtSafeBlock) < 0
	n.ConsumedAfterSafeBlock = !n.Stale && tx.Nonce.Cmp(n.SafeAtLatest) < 0
	switch {
	case n.Stale:
		report.note("The transaction's nonce %s is below the Safe's nonce %s at the Safe block %d: the Safe had used it by the proposal, so the transaction could never be executed after it. Charter issue #20 on such proposals is open, so this is reported, not classified.",
			tx.Nonce, n.SafeAtSafeBlock, report.SafeBlock)
	case tx.Nonce.Cmp(n.SafeAtSafeBlock) > 0:
		report.note("The transaction's nonce %s is above the Safe's nonce %s at the Safe block %d, so the Safe had to execute transactions with the nonces between first.",
			tx.Nonce, n.SafeAtSafeBlock, report.SafeBlock)
	}
	if n.ConsumedAfterSafeBlock {
		report.note("The Safe used the transaction's nonce %s after the Safe block %d: its nonce is %s at the latest block %d.", tx.Nonce, report.SafeBlock, n.SafeAtLatest, latest)
	}

	if err := c.execution(ctx, report); err != nil {
		return nil, err
	}
	if x := report.Execution; x.Block != nil {
		if report.Timing, err = c.timing(ctx, report, r, p); err != nil {
			return nil, err
		}
	}
	if c.Proposals != nil {
		if err := c.queue(ctx, report, r, p); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// note adds a note to the report.
func (r *Report) note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// execution sets the report's execution: from the Safe Transaction Service, if
// the chain confirms what it says, or else from the Safe's nonce, or else from
// a scan of the Safe's execution logs.
func (c *Checker) execution(ctx context.Context, report *Report) error {
	x := &report.Execution
	if c.Service != nil {
		x.Service = &Service{URL: c.Service.URL(), ObservedAt: time.Now().UTC().Truncate(time.Second)}
		done, err := c.fromService(ctx, report)
		if err != nil || done {
			return err
		}
	}

	nonce, n := report.Nonce.Transaction, &report.Nonce
	if n.SafeAtLatest.Cmp(nonce) <= 0 {
		x.Status, x.Source = NotExecuted, SourceNonce
		x.Reason = fmt.Sprintf("The Safe's nonce is %s at the latest block %d, at most the transaction's nonce %s, so the Safe hasn't executed a transaction with that nonce yet: every execution uses the Safe's nonce, and increases it by one.",
			n.SafeAtLatest, n.LatestBlock, nonce)
		return nil
	}
	return c.scan(ctx, report)
}

// fromService sets the report's execution from the Safe Transaction Service,
// and reports whether it did: only if the service names the onchain transaction
// that executed it, and its receipt has the Safe's log of the execution.
// Anything else that the service says is a note.
func (c *Checker) fromService(ctx context.Context, report *Report) (bool, error) {
	listed, err := c.Service.Transaction(ctx, report.SafeTxHash)
	switch {
	case errors.Is(err, txservice.ErrNotFound):
		report.note("The Safe Transaction Service doesn't have the Safe transaction.")
		return false, nil
	case err != nil:
		// The service is an offchain source, which the chain stands in for.
		c.warn(fmt.Errorf("getting Safe transaction %s from the Safe Transaction Service: %w", report.SafeTxHash, err))
		report.note("The Safe Transaction Service failed, so the chain was read without it: %v.", err)
		return false, nil
	case !listed.Executed:
		report.note("The Safe Transaction Service doesn't list the Safe transaction as executed.")
		return false, nil
	case listed.TxHash == (ethrpc.Hash{}):
		report.note("The Safe Transaction Service lists the Safe transaction as executed, but names no onchain transaction.")
		return false, nil
	}
	e, reason, err := c.confirm(ctx, report.Safe, report.SafeTxHash, listed.TxHash)
	if err != nil {
		return false, err
	}
	if reason != "" {
		report.note("The Safe Transaction Service says that the Safe transaction was executed in transaction %s, which the chain doesn't confirm: %s.", listed.TxHash, reason)
		return false, nil
	}
	if err := c.setExecution(ctx, report, Executed, SourceService, e); err != nil {
		return false, err
	}
	report.Execution.Reason = fmt.Sprintf("The Safe Transaction Service names transaction %s, whose receipt has the Safe's %s log of the Safe transaction hash, in block %d.",
		e.TxHash, executionEvent(e.Success), e.Block)
	return true, nil
}

// confirm returns the execution of the Safe transaction with the hash
// safeTxHash that safe logged in the onchain transaction txHash, or why its
// receipt doesn't confirm one.
func (c *Checker) confirm(ctx context.Context, safe ethrpc.Address, safeTxHash, txHash ethrpc.Hash) (executions.Execution, string, error) {
	// A node behind a load balancer now and then answers with a receipt without its
	// logs, so one without them is read once more.
	var receipt ethrpc.Receipt
	var err error
	for range 2 {
		receipt, err = c.Safe.GetTransactionReceipt(ctx, txHash)
		if err != nil || len(receipt.Logs) > 0 {
			break
		}
	}
	switch {
	case ethrpc.Transient(err):
		return executions.Execution{}, "", fmt.Errorf("getting the receipt of transaction %s: %w", txHash, err)
	case err != nil:
		return executions.Execution{}, fmt.Sprintf("getting its receipt failed: %v", err), nil
	case receipt.TransactionHash != txHash:
		return executions.Execution{}, fmt.Sprintf("the node returned the receipt of transaction %s", receipt.TransactionHash), nil
	}
	e, ok := executions.InReceipt(receipt, safe, safeTxHash)
	if !ok {
		return executions.Execution{}, "its receipt has no log of the Safe's execution of it", nil
	}
	return e, "", nil
}

// scan sets the report's execution from a scan of the Safe's execution logs, in
// the blocks where the Safe used the transaction's nonce: up to the Safe block
// for a stale nonce, and after it otherwise, bounded by the Checker's Blocks.
func (c *Checker) scan(ctx context.Context, report *Report) error {
	x, n := &report.Execution, &report.Nonce
	nonce, blocks := n.Transaction, max(c.Blocks, 1)
	var from, to uint64
	if n.Stale {
		to = report.SafeBlock
		from = to + 1 - min(blocks, to+1)
	} else {
		from = report.SafeBlock + 1
		to = min(n.LatestBlock, report.SafeBlock+blocks)
	}
	x.Scanned = &executions.BlockRange{FromBlock: from, ToBlock: to}
	finder := &executions.Finder{Eth: c.Safe, Service: c.Service, Warn: c.Warn}
	found, err := finder.Find(ctx, report.Safe, ethrpc.BlockNumber(from), ethrpc.BlockNumber(to))
	if ethrpc.Transient(err) {
		return err
	}
	if err != nil {
		x.Status = Unknown
		x.Reason = fmt.Sprintf("Scanning the Safe's execution logs in blocks %d to %d failed: %v.", from, to, err)
		return nil
	}
	for _, e := range found {
		if e.SafeTxHash == report.SafeTxHash {
			if err := c.setExecution(ctx, report, Executed, SourceLogs, e); err != nil {
				return err
			}
			x.Reason = fmt.Sprintf("The Safe logged its %s of the Safe transaction hash in block %d, transaction %s, in a scan of blocks %d to %d.",
				executionEvent(e.Success), e.Block, e.TxHash, from, to)
			return nil
		}
	}
	for _, e := range found {
		if e.Transaction != nil && e.Transaction.Nonce.Cmp(nonce) == 0 {
			if err := c.setExecution(ctx, report, OtherTransaction, SourceLogs, e); err != nil {
				return err
			}
			x.Reason = fmt.Sprintf("The Safe executed the Safe transaction %s with the same nonce %s in block %d, transaction %s, decoded from its %s, so the request's Safe transaction can never be executed.",
				e.SafeTxHash, nonce, e.Block, e.TxHash, e.Source)
			return nil
		}
	}

	// The Safe's nonces before and after the blocks scanned show whether it used
	// the nonce in them. Every execution uses the Safe's nonce and increases it by
	// one, so if the Safe logged as many executions in them as its nonce rose by,
	// each used the next nonce.
	before, after := n.SafeAtSafeBlock, n.SafeAtLatest
	switch {
	case n.Stale:
		after = n.SafeAtSafeBlock
		before = new(big.Int)
		if from > 0 {
			if before, err = c.nonceOrUnknown(ctx, report, ethrpc.BlockNumber(from-1)); before == nil {
				return err
			}
		}
	case to < n.LatestBlock:
		if after, err = c.nonceOrUnknown(ctx, report, ethrpc.BlockNumber(to)); after == nil {
			return err
		}
	}
	if nonce.Cmp(before) < 0 || nonce.Cmp(after) >= 0 {
		x.Status = Unknown
		x.Reason = fmt.Sprintf("The Safe's nonce is %s before block %d and %s at block %d, so it used the nonce %s outside the blocks scanned; run with more -blocks.",
			before, from, after, to, nonce)
		return nil
	}
	// An execution in that position whose transaction is known has another nonce,
	// which the count can't explain, so it settles nothing.
	rose := new(big.Int).Sub(after, before)
	if rose.IsInt64() && rose.Int64() == int64(len(found)) && found[new(big.Int).Sub(nonce, before).Int64()].Transaction == nil {
		e := found[new(big.Int).Sub(nonce, before).Int64()]
		if err := c.setExecution(ctx, report, OtherTransaction, SourceLogs, e); err != nil {
			return err
		}
		x.Reason = fmt.Sprintf("The Safe logged %d executions in blocks %d to %d, as many as its nonce rose by, from %s to %s, so the one in block %d, transaction %s, of the Safe transaction %s, used the nonce %s; no source has that transaction, so its nonce is derived from its position.",
			len(found), from, to, before, after, e.Block, e.TxHash, e.SafeTxHash, nonce)
		return nil
	}
	x.Status, x.Source = OtherTransaction, SourceLogs
	x.Reason = fmt.Sprintf("The Safe's nonce rose from %s to %s in blocks %d to %d, so it used the nonce %s in them, but it logged no execution of the request's Safe transaction there. It logged %d executions, so which one used the nonce is unknown.",
		before, after, from, to, nonce, len(found))
	return nil
}

// nonceOrUnknown returns the Safe's nonce at block. If the node can't serve it
// for a reason other than a transient failure, such as a node that doesn't keep
// the state of old blocks, it sets the execution to Unknown and returns nil.
func (c *Checker) nonceOrUnknown(ctx context.Context, report *Report, block ethrpc.BlockNumber) (*big.Int, error) {
	nonce, err := c.nonce(ctx, report.Safe, block)
	if ethrpc.Transient(err) {
		return nil, err
	}
	if err != nil {
		report.Execution.Status = Unknown
		report.Execution.Reason = fmt.Sprintf("The Safe logged no execution of the request's Safe transaction in blocks %d to %d, but its nonce at block %d, which shows whether it used the nonce there, couldn't be read: %v.",
			report.Execution.Scanned.FromBlock, report.Execution.Scanned.ToBlock, block, err)
		return nil, nil
	}
	return nonce, nil
}

// setExecution sets the report's execution to the status, the source, and the
// execution e, with the time of its block.
func (c *Checker) setExecution(ctx context.Context, report *Report, status, source string, e executions.Execution) error {
	t, err := c.Safe.BlockTime(ctx, ethrpc.BlockNumber(e.Block))
	if err != nil {
		return fmt.Errorf("reading the time of block %d of chain %d: %w", e.Block, report.ChainID, err)
	}
	x := &report.Execution
	x.Status, x.Source = status, source
	x.TxHash, x.Block, x.BlockTime, x.Success, x.SafeTxHash = &e.TxHash, &e.Block, &t, &e.Success, &e.SafeTxHash
	return nil
}

// timing places the report's execution against the request's life.
func (c *Checker) timing(ctx context.Context, report *Report, r *safenet.Request, p *safenet.Progress) (*Timing, error) {
	x := report.Execution
	t := &Timing{Execution: Point{Block: *x.Block, Time: *x.BlockTime}}
	latest, err := c.Gnosis.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting the latest Gnosis Chain block: %w", err)
	}
	// The latest block may be behind the one that the request was read at, on
	// another node behind a load balancer.
	latest = max(latest, ethrpc.BlockNumber(p.Block))
	point := func(block uint64) (Point, error) {
		t, err := c.Gnosis.BlockTime(ctx, ethrpc.BlockNumber(block))
		if err != nil {
			return Point{}, fmt.Errorf("reading the time of Gnosis Chain block %d: %w", block, err)
		}
		return Point{Block: block, Time: t}, nil
	}
	deadline := func(block uint64) (Deadline, error) {
		d := Deadline{Block: block}
		if ethrpc.BlockNumber(block) > latest {
			return d, nil
		}
		pt, err := point(block)
		d.Reached, d.Time = true, &pt.Time
		return d, err
	}
	if t.Now, err = point(uint64(latest)); err != nil {
		return nil, err
	}
	if t.Proposal, err = point(r.Proposal.Block); err != nil {
		return nil, err
	}
	if t.CommitDeadline, err = deadline(p.Terms.CommitDeadline); err != nil {
		return nil, err
	}
	if t.RevealDeadline, err = deadline(p.Terms.RevealDeadline); err != nil {
		return nil, err
	}
	if a := p.Arbitration; a != nil {
		frozen, err := point(a.FrozenBlock)
		if err != nil {
			return nil, err
		}
		t.Frozen = &frozen
		if a.Record != nil {
			resolved, err := point(a.Record.Block)
			if err != nil {
				return nil, err
			}
			t.Resolved = &Resolution{Point: resolved, Outcome: a.Outcome}
		}
	}

	// The execution is after an event only if its block's timestamp is later, since
	// the order of blocks of two chains with the same timestamp is unknown.
	at := t.Execution.Time
	tie := func(name string, other time.Time) {
		if at.Equal(other) {
			report.note("The execution's block has the same timestamp as the %s, so which came first is unknown; it is placed before the %s.", name, name)
		}
	}
	tie("proposal", t.Proposal.Time)
	switch {
	case at.Before(t.Proposal.Time):
		t.Value = BeforeProposal
	case t.Resolved != nil && at.After(t.Resolved.Time):
		t.Value = AfterResolution
	case t.Frozen != nil && at.After(t.Frozen.Time):
		t.Value = AfterFreeze
		if t.Resolved != nil {
			tie("resolution", t.Resolved.Time)
		}
	case t.Frozen == nil && t.RevealDeadline.Reached && at.After(*t.RevealDeadline.Time):
		t.Value = AfterVoting
	default:
		t.Value = DuringVoting
		if t.Frozen != nil {
			tie("freeze", t.Frozen.Time)
		} else if t.RevealDeadline.Reached {
			tie("reveal deadline", *t.RevealDeadline.Time)
		}
	}
	what := "The Safe transaction was executed"
	if x.Status == OtherTransaction {
		what = "The Safe transaction that used the nonce was executed"
	}
	report.note("%s at %s, %s: the proposal was at %s, on Gnosis Chain.", what, at.Format(time.RFC3339), t.Value, t.Proposal.Time.Format(time.RFC3339))
	return t, nil
}

// queue sets the report's queue to the other proposals for the Safe, on its
// chain, in ARBITRATION_TIMEOUT Gnosis Chain blocks around the proposal, half
// of them before and half after, up to the latest: as many as arbot pending
// scans. The request's own proposal must be among them, which checks how they
// were found; the queue is left out if it isn't.
func (c *Checker) queue(ctx context.Context, report *Report, r *safenet.Request, p *safenet.Progress) error {
	block, half := r.Proposal.Block, p.ArbitrationTimeout/2
	from := block - min(half, block)
	to := min(block+half, p.Block)
	proposals, err := c.Proposals(ctx, from, to)
	if ethrpc.Transient(err) {
		return err
	}
	if err != nil {
		report.note("The other proposals for the Safe couldn't be listed: %v.", err)
		return nil
	}
	q := &Queue{FromBlock: from, ToBlock: to, Proposals: []Proposal{}}
	own := false
	for _, proposal := range proposals {
		if proposal.RequestID == r.ID {
			own = true
			continue
		}
		if proposal.ChainID == nil || !proposal.ChainID.IsUint64() || proposal.ChainID.Uint64() != report.ChainID {
			continue
		}
		q.Proposals = append(q.Proposals, Proposal{SafeProposal: proposal, SameNonce: proposal.Nonce.Cmp(report.Nonce.Transaction) == 0})
	}
	if !own {
		report.note("The other proposals for the Safe are left out: the request's own proposal isn't among those found in Gnosis Chain blocks %d to %d.", from, to)
		return nil
	}
	report.Queue = q
	for _, proposal := range q.Proposals {
		if proposal.SameNonce {
			report.note("Request %s, proposed in Gnosis Chain block %d, is for another Safe transaction %s with the same nonce, so at most one of them can be executed.",
				proposal.RequestID, proposal.Block, proposal.SafeTxHash)
		}
	}
	return nil
}

// nonceSelector is the selector of the nonce function of the Safe versions of §
// 2.1.
var nonceSelector = solabi.Selector("nonce()")

// nonce returns the nonce of the Safe at block.
func (c *Checker) nonce(ctx context.Context, safe ethrpc.Address, block ethrpc.BlockNumber) (*big.Int, error) {
	result, err := c.Safe.Call(ctx, ethrpc.CallRequest{To: safe, Data: solabi.Call(nonceSelector)}, block)
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

// executionEvent returns the name of the event that a Safe logs for an
// execution that succeeded or failed.
func executionEvent(success bool) string {
	if success {
		return "ExecutionSuccess"
	}
	return "ExecutionFailure"
}

func (c *Checker) warn(err error) {
	if c.Warn != nil {
		c.Warn(err)
	}
}
