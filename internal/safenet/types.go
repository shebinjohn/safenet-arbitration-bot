package safenet

import (
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// Dispute is a request that a split sentinel vote froze for arbitration.
type Dispute struct {
	RequestID ethrpc.Hash `json:"requestId"`
	// FrozenBlock is the block in which the request was frozen.
	FrozenBlock uint64 `json:"frozenBlock"`
	// Deadline is the last block in which the arbitrator can rule before anyone can
	// time out the arbitration.
	Deadline uint64 `json:"deadline"`
}

// Request is a SentinelOracle request, with the transaction proposal it is for,
// the sentinels' votes, and its arbitration.
type Request struct {
	ID     ethrpc.Hash    `json:"id"`
	Oracle ethrpc.Address `json:"oracle"`
	// Charter is the ENS name of the Safenet Arbitration Charter that the oracle
	// trusts.
	Charter string `json:"charter"`
	// FeeToken is the token of the fees, bonds, and slashed amounts.
	FeeToken Token `json:"feeToken"`
	State    State `json:"state"`
	Terms    Terms `json:"terms"`
	// Fee is the part of the sponsor's fee that the oracle holds for the request:
	// the full fee until the request resolves, then the part left for the winning
	// sentinels, and zero once it is refunded.
	Fee      *big.Int     `json:"fee"`
	Proposal Proposal     `json:"proposal"`
	Votes    []Commitment `json:"votes"`
	// DisputedRules are the rules that the DENIED votes cite, all of them together,
	// in the order of their first citation. The arbitration of a dispute considers
	// only these rules.
	DisputedRules []string `json:"disputedRules"`
	// Arbitration is nil unless a split vote froze the request.
	Arbitration *Arbitration `json:"arbitration"`
}

// Token is an ERC-20 token.
type Token struct {
	Address  ethrpc.Address `json:"address"`
	Symbol   string         `json:"symbol"`
	Decimals uint8          `json:"decimals"`
}

// Terms are the terms that the oracle set for a request when it was posted.
type Terms struct {
	// Sponsor is the account that paid the request's fee, and gets refunds.
	Sponsor ethrpc.Address `json:"sponsor"`
	// Bond is the amount that each sentinel bonds when committing a vote.
	Bond *big.Int `json:"bond"`
	// SlashAmount is the amount slashed from each sentinel on the losing side of a
	// ruling, and from each sentinel that does not reveal its vote.
	SlashAmount *big.Int `json:"slashAmount"`
	// DAOFeeShare is the DAO's share of the fee, in units of 1/100,000.
	DAOFeeShare uint32 `json:"daoFeeShare"`
	// CommitDeadline and RevealDeadline are the last blocks in which sentinels can
	// commit and reveal their votes.
	CommitDeadline uint64 `json:"commitDeadline"`
	RevealDeadline uint64 `json:"revealDeadline"`
}

// Proposal is the Consensus transaction proposal that a request is for.
type Proposal struct {
	Consensus ethrpc.Address `json:"consensus"`
	// Block and Time are the Gnosis Chain block that the proposal was made in, and
	// its timestamp.
	Block uint64    `json:"block"`
	Time  time.Time `json:"time"`
	// EthereumBlock and SafeBlock are the last blocks before Time on Ethereum
	// Mainnet and on the Safe's chain. Their state is what the proposal is judged
	// against, without its own effects or later ones (Charter § 2.8 and § 3.6), and
	// the Charter version to apply is the one at EthereumBlock.
	EthereumBlock uint64 `json:"ethereumBlock"`
	// EthereumBlockTime is the timestamp of EthereumBlock.
	EthereumBlockTime time.Time `json:"ethereumBlockTime,omitzero"`
	SafeBlock         uint64    `json:"safeBlock"`
	// SafeBlockTime is the timestamp of SafeBlock.
	SafeBlockTime time.Time `json:"safeBlockTime,omitzero"`
	// TxHash is the hash of the Gnosis Chain transaction that made the proposal.
	TxHash     ethrpc.Hash  `json:"txHash"`
	Epoch      uint64       `json:"epoch"`
	OracleData ethrpc.Bytes `json:"oracleData"`
	// SafeTxHash is the EIP-712 hash of Transaction, as the Safe computes it.
	SafeTxHash  ethrpc.Hash     `json:"safeTxHash"`
	Transaction SafeTransaction `json:"transaction"`
}

// SafeTransaction is a proposed Safe transaction, along with the Safe and the
// chain it is for.
type SafeTransaction struct {
	ChainID        *big.Int       `json:"chainId"`
	Safe           ethrpc.Address `json:"safe"`
	To             ethrpc.Address `json:"to"`
	Value          *big.Int       `json:"value"`
	Data           ethrpc.Bytes   `json:"data"`
	Operation      Operation      `json:"operation"`
	SafeTxGas      *big.Int       `json:"safeTxGas"`
	BaseGas        *big.Int       `json:"baseGas"`
	GasPrice       *big.Int       `json:"gasPrice"`
	GasToken       ethrpc.Address `json:"gasToken"`
	RefundReceiver ethrpc.Address `json:"refundReceiver"`
	Nonce          *big.Int       `json:"nonce"`
}

// Commitment is a sentinel's vote on a request.
type Commitment struct {
	Sentinel ethrpc.Address `json:"sentinel"`
	Bond     *big.Int       `json:"bond"`
	// Vote is PENDING until the sentinel reveals it.
	Vote Vote `json:"vote"`
	// Reason is the reason the sentinel gave when revealing its vote. By
	// convention, it is empty for approvals, and a comma-separated list of the
	// Charter rules that the transaction fails for denials, such as "R-4.1, R-4.2".
	Reason string `json:"reason"`
	// Rules are the rule identifiers that the reason of a DENIED vote cites, as
	// CitedRules parses them, and are empty for other votes.
	Rules []string `json:"rules"`
}

// Arbitration is the arbitration of a frozen request.
type Arbitration struct {
	FrozenBlock uint64 `json:"frozenBlock"`
	// Deadline is the last block in which the arbitrator can rule before anyone can
	// time out the arbitration.
	Deadline uint64  `json:"deadline"`
	Outcome  Outcome `json:"outcome"`
	// Slashed is the total amount slashed from the losing side of a secure or
	// insecure ruling.
	Slashed *big.Int `json:"slashed,omitempty"`
	// Context is the arbitrator's rationale for a ruling or an out-of-scope
	// decision: the text itself, or an IPFS CID pointing to it.
	Context string `json:"context,omitempty"`
	// Record is the log that settled the arbitration, or nil while it is pending.
	Record *Record `json:"record,omitempty"`
}

// Record identifies the Gnosis Chain transaction that emitted a log.
type Record struct {
	Block  uint64      `json:"block"`
	TxHash ethrpc.Hash `json:"txHash"`
}

// State is the state of a request, SentinelOracleRequest.State.
type State uint8

const (
	StateNone State = iota
	StatePending
	StateFrozen
	StateResolvedApproved
	StateResolvedDenied
	StateTimedOut
)

var stateNames = []string{"NONE", "PENDING", "FROZEN", "RESOLVED_APPROVED", "RESOLVED_DENIED", "TIMED_OUT"}

func (s State) String() string {
	return enumName(stateNames, s)
}

func (s State) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *State) UnmarshalText(text []byte) error {
	return parseEnum(stateNames, text, s)
}

// Vote is a sentinel's vote, SentinelOracleCommitment.Vote.
type Vote uint8

const (
	VoteNone Vote = iota
	VotePending
	VoteApproved
	VoteDenied
)

var voteNames = []string{"NONE", "PENDING", "APPROVED", "DENIED"}

func (v Vote) String() string {
	return enumName(voteNames, v)
}

func (v Vote) MarshalText() ([]byte, error) {
	return []byte(v.String()), nil
}

func (v *Vote) UnmarshalText(text []byte) error {
	return parseEnum(voteNames, text, v)
}

// Operation is the operation type of a Safe transaction.
type Operation uint8

const (
	OperationCall Operation = iota
	OperationDelegateCall
)

var operationNames = []string{"CALL", "DELEGATECALL"}

func (o Operation) String() string {
	return enumName(operationNames, o)
}

func (o Operation) MarshalText() ([]byte, error) {
	return []byte(o.String()), nil
}

func (o *Operation) UnmarshalText(text []byte) error {
	return parseEnum(operationNames, text, o)
}

// Outcome is the outcome of an arbitration.
type Outcome string

const (
	// OutcomePending means that the arbitrator hasn't ruled yet, and the
	// arbitration hasn't timed out.
	OutcomePending Outcome = "pending"
	// OutcomeSecure and OutcomeInsecure are rulings that the transaction is secure
	// (the approving sentinels win) or insecure (the denying ones win).
	OutcomeSecure   Outcome = "secure"
	OutcomeInsecure Outcome = "insecure"
	// OutcomeOutOfScope means that the arbitrator declined to rule.
	OutcomeOutOfScope Outcome = "out-of-scope"
	// OutcomeTimedOut means that the arbitrator didn't rule by the deadline, and
	// someone timed out the arbitration.
	OutcomeTimedOut Outcome = "timed-out"
)

func enumName[T ~uint8](names []string, value T) string {
	if int(value) < len(names) {
		return names[value]
	}
	return fmt.Sprintf("UNKNOWN(%d)", value)
}

func parseEnum[T ~uint8](names []string, text []byte, value *T) error {
	i := slices.Index(names, string(text))
	if i < 0 {
		return fmt.Errorf("unknown value %q", text)
	}
	*value = T(i)
	return nil
}
