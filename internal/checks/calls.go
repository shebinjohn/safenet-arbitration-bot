package checks

import (
	"context"
	"errors"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/functions"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// TransactionCalls are the calls that the Safe of a request makes, as the
// checks judge them, with the Safe and the accounts that they call, as of the
// request's Safe block.
type TransactionCalls struct {
	ChainID   uint64      `json:"chainId"`
	SafeBlock uint64      `json:"safeBlock"`
	Safe      SafeAccount `json:"safe"`
	// Target is the account that the Safe transaction's own call is to.
	Target Account `json:"target"`
	Calls  []Call  `json:"calls"`
}

// SafeAccount is the Safe of a request, with what § 2.1 identifies a Safe by:
// the version of its proxy, and the singleton that the proxy delegates to.
type SafeAccount struct {
	Address  ethrpc.Address `json:"address"`
	CodeHash ethrpc.Hash    `json:"codeHash"`
	CodeSize int            `json:"codeSize"`
	// ProxyVersion is the proxy version of § 2.1 whose official proxy factory
	// deploys a SafeProxy with the account's code on its network, if any.
	ProxyVersion *string `json:"proxyVersion"`
	// Singleton is the address in the low 20 bytes of the account's storage slot 0,
	// which a SafeProxy delegates calls to.
	Singleton ethrpc.Address `json:"singleton"`
	// SingletonVersion is the singleton version of § 2.1 that Singleton is an
	// official deployment of, with "+L2" for SafeL2, if any.
	SingletonVersion *string `json:"singletonVersion"`
}

// Account is an account that a Safe calls.
type Account struct {
	Address  ethrpc.Address `json:"address"`
	CodeHash ethrpc.Hash    `json:"codeHash"`
	CodeSize int            `json:"codeSize"`
	// Self is whether the account is the Safe itself.
	Self bool `json:"self"`
	// Contract is the contract of a Safe version of § 2.1 that the account is an
	// official deployment of, if any.
	Contract *Contract `json:"contract"`
}

// Contract is a contract of a Safe version of § 2.1: MultiSend,
// MultiSendCallOnly, CreateCall, or the Safe or SafeL2 singleton.
type Contract struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (c Contract) String() string {
	return c.Name + " " + c.Version
}

// Call is a call that a Safe makes, as the checks judge it.
type Call struct {
	// Kind is "root" for the Safe transaction's own call, or a placeholder for it,
	// "batched" for a call that a MultiSend makes, and "refund" for the synthetic
	// call that pays the largest gas refund that the transaction allows.
	Kind string `json:"kind"`
	// Placeholder is set for a root call that stands for the Safe transaction's own
	// call, rather than being it: "reverting" for a call to a MultiSend that
	// reverts whatever the state, and "empty" for one that makes no calls.
	Placeholder string            `json:"placeholder,omitempty"`
	To          Account           `json:"to"`
	Value       *big.Int          `json:"value"`
	Operation   safenet.Operation `json:"operation"`
	Data        ethrpc.Bytes      `json:"data"`
	// Function is the data decoded as a well-known function, by its selector and
	// ABI layout only, if it is one. What the call does depends on the account that
	// it is to.
	Function *functions.Function `json:"function"`
	// Creates is the contract that a call to a CreateCall of a § 2.1 version
	// deploys, derived from its data, if the call is one, and left out otherwise.
	Creates *Creation `json:"creates,omitempty"`
	// ABI is the lookup of the account's ABI in Sourcify, for a call that
	// simulate.ABIDecoder looked up, and left out otherwise. See ABILookup.
	ABI *ABILookup `json:"abi,omitempty"`
}

// ABILookup is how a call's data decodes with the ABI that Sourcify, an
// offchain source (§ 3.4), verified for the account that the call is to, as of
// the account's code at the block.
type ABILookup struct {
	// Status is "verified" for an account whose contract Sourcify verified,
	// "unverified" for one that it hasn't, whose call isn't decoded, and "no-code"
	// for an account without code, which isn't looked up.
	Status string `json:"status"`
	// Source is "sourcify", and URL and ObservedAt are where and when the ABI was
	// fetched, for an account with code.
	Source     string     `json:"source"`
	URL        string     `json:"url,omitempty"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
	// Function is the function of the ABI whose selector the data starts with, or
	// nil if there is none, or the account isn't verified.
	Function *ABIFunction `json:"function"`
}

// ABIFunction is a call's data decoded with a function of a verified ABI.
type ABIFunction struct {
	Signature string `json:"signature"`
	// Inputs are the arguments, as arbot simulate decodes them, or nil if one has a
	// type that isn't decoded or the data is malformed.
	Inputs []functions.Arg `json:"inputs"`
}

// callKinds are the names of the kinds of calls, by callKind.
var callKinds = []string{Root: "root", Batched: "batched", Refund: "refund"}

// Calls returns the calls that the Safe of request makes, as Classify judges
// them: delegate calls to the MultiSend and MultiSendCallOnly contracts of §
// 2.1 versions are decoded into the calls that they make, a call that reverts
// whatever the state or makes no calls is replaced by a placeholder, and a
// synthetic call pays the largest gas refund that the transaction allows, if
// any. A call to a CreateCall of a § 2.1 version has the contract that it
// deploys, with CREATE2 or CREATE (see CallReader.DeriveCreateNonces). It reads
// the Safe and the accounts that it calls on the Safe's chain at the proposal's
// SafeBlock, connecting with dial.
//
// Contracts are identified by their address, since the contracts of § 2.1
// versions are deterministic deployments, and only on the networks that the
// Charter covers, which they are deployed on. An address without code isn't
// identified.
func Calls(ctx context.Context, dial ethrpc.Dialer, request *safenet.Request) (*TransactionCalls, error) {
	reader, err := RequestReader(ctx, dial, request)
	if err != nil {
		return nil, err
	}
	return reader.Calls(ctx, &request.Proposal.Transaction)
}

// RequestReader returns the reader of the calls of request's transaction, as
// Calls reads them: of its Safe on its chain, at its SafeBlock, connecting with
// dial, and deriving the addresses of contracts that CreateCall deploys with
// CREATE from the nonces at that block (see DeriveCreateNonces).
func RequestReader(ctx context.Context, dial ethrpc.Dialer, request *safenet.Request) (*CallReader, error) {
	tx := &request.Proposal.Transaction
	// A malformed transaction is an error before anything is read.
	if _, err := components(tx); err != nil {
		return nil, err
	}
	if !tx.ChainID.IsUint64() {
		return nil, errors.New("safe transaction has an invalid chainId")
	}
	if request.Proposal.SafeBlock == 0 {
		return nil, errors.New("proposal has no safeBlock")
	}
	client, err := dial(ctx, tx.ChainID.Uint64())
	if err != nil {
		return nil, err
	}
	return NewCallReader(client, tx.Safe, ethrpc.BlockNumber(request.Proposal.SafeBlock)).DeriveCreateNonces(), nil
}

// CallReader reads the calls of the Safe transactions of a Safe, as Calls does,
// such as those that the Safe executed before a proposal, as of one block. It
// reads each account once, so that it can read many transactions.
type CallReader struct {
	// createNonces is whether the reader derives the addresses of the contracts
	// that CreateCall deploys with CREATE, from the deployers' nonces at the block.
	createNonces bool
	client       *ethrpc.Client
	block        ethrpc.BlockNumber
	safe         ethrpc.Address
	chainID      uint64
	covered      bool
	codes        map[ethrpc.Address]ethrpc.Bytes
	// safeAccount is the Safe, once read.
	safeAccount *SafeAccount
}

// NewCallReader returns a reader of the calls of the Safe at safe, on the chain
// of client at block.
func NewCallReader(client *ethrpc.Client, safe ethrpc.Address, block ethrpc.BlockNumber) *CallReader {
	return &CallReader{
		client:  client,
		block:   block,
		safe:    safe,
		chainID: client.ChainID(),
		covered: slices.Contains(networks, client.ChainID()),
		codes:   map[ethrpc.Address]ethrpc.Bytes{},
	}
}

// Client returns the client of the node that the reader reads.
func (r *CallReader) Client() *ethrpc.Client {
	return r.client
}

// Calls returns the calls that tx makes, as the package-level Calls returns
// those of a request's transaction. tx must be a transaction of the reader's
// Safe, on its chain.
func (r *CallReader) Calls(ctx context.Context, tx *safenet.SafeTransaction) (*TransactionCalls, error) {
	components, err := components(tx)
	if err != nil {
		return nil, err
	}
	if tx.Safe != r.safe || !tx.ChainID.IsUint64() || tx.ChainID.Uint64() != r.chainID {
		return nil, errors.New("safe transaction is for another Safe or chain")
	}
	safe, err := r.safeOf(ctx)
	if err != nil {
		return nil, err
	}

	report := &TransactionCalls{ChainID: r.chainID, SafeBlock: uint64(r.block), Safe: *safe, Calls: []Call{}}
	if report.Target, err = r.account(ctx, tx.To); err != nil {
		return nil, err
	}
	own := call{to: tx.To, value: tx.Value, data: tx.Data, operation: tx.Operation, kind: Root}
	for _, c := range components.calls {
		to, err := r.account(ctx, c.to)
		if err != nil {
			return nil, err
		}
		reported := Call{Kind: callKinds[c.kind], To: to, Value: c.value, Operation: c.operation, Data: c.data}
		if reported.Data == nil {
			reported.Data = ethrpc.Bytes{}
		}
		reported.Function = functions.Decode(reported.Data)
		if c.kind == Root && !c.equal(own) {
			switch {
			case c.equal(revertingMultiSend(c.to)):
				reported.Placeholder = "reverting"
			case c.equal(emptyMultiSend(c.to)):
				reported.Placeholder = "empty"
			}
		}
		report.Calls = append(report.Calls, reported)
	}
	if err := r.creations(ctx, report.Calls); err != nil {
		return nil, err
	}
	return report, nil
}

// DeriveCreateNonces makes the reader derive the addresses of the contracts
// that the calls to a CreateCall deploy with CREATE, from the deployers' nonces
// at the reader's block, which are their nonces when a transaction runs only if
// it runs right after the block, such as a proposal's transaction at its Safe
// block. Without it, only the CREATE2 addresses are derived, since they don't
// depend on the state. It returns the reader.
func (r *CallReader) DeriveCreateNonces() *CallReader {
	r.createNonces = true
	return r
}

// Code returns the code of the account at address as of the reader's block,
// reading it once.
func (r *CallReader) Code(ctx context.Context, address ethrpc.Address) (ethrpc.Bytes, error) {
	return r.code(ctx, address)
}

// code returns the code of address, reading it once.
func (r *CallReader) code(ctx context.Context, address ethrpc.Address) (ethrpc.Bytes, error) {
	if c, ok := r.codes[address]; ok {
		return c, nil
	}
	c, err := r.client.GetCode(ctx, address, r.block)
	if err != nil {
		return nil, err
	}
	r.codes[address] = c
	return c, nil
}

// safeOf returns the reader's Safe, reading it once.
func (r *CallReader) safeOf(ctx context.Context) (*SafeAccount, error) {
	if r.safeAccount != nil {
		return r.safeAccount, nil
	}
	safeCode, err := r.code(ctx, r.safe)
	if err != nil {
		return nil, err
	}
	slot0, err := r.client.GetStorageAt(ctx, r.safe, ethrpc.Hash{}, r.block)
	if err != nil {
		return nil, err
	}
	singleton := ethrpc.Address(slot0[12:])
	safe := &SafeAccount{
		Address:      r.safe,
		CodeHash:     ethrpc.Hash(keccak256.Hash(safeCode)),
		CodeSize:     len(safeCode),
		ProxyVersion: optional(proxyVersion(safeCode, r.chainID)),
		Singleton:    singleton,
	}
	if r.covered {
		safe.SingletonVersion = optional(singletons[singleton])
	}
	r.safeAccount = safe
	return safe, nil
}

// Account returns the account at address as the calls that the reader decodes
// have it: its code, whether it is the reader's Safe, and the contract of a
// Safe version of § 2.1 that it is, if any.
func (r *CallReader) Account(ctx context.Context, address ethrpc.Address) (Account, error) {
	return r.account(ctx, address)
}

// Safe returns the reader's Safe as the calls that the reader decodes have it,
// with what § 2.1 identifies a Safe by: the proxy version of its code, and its
// singleton and the singleton's version. Either version is nil if the account
// isn't a Safe of a § 2.1 version, such as an account without code.
func (r *CallReader) Safe(ctx context.Context) (SafeAccount, error) {
	safe, err := r.safeOf(ctx)
	if err != nil {
		return SafeAccount{}, err
	}
	return *safe, nil
}

// account returns an account that the Safe calls.
func (r *CallReader) account(ctx context.Context, address ethrpc.Address) (Account, error) {
	c, err := r.code(ctx, address)
	if err != nil {
		return Account{}, err
	}
	a := Account{
		Address:  address,
		CodeHash: ethrpc.Hash(keccak256.Hash(c)),
		CodeSize: len(c),
		Self:     address == r.safe,
	}
	if r.covered && len(c) > 0 {
		a.Contract = contractAt(address)
		if cc, ok := isCreateCall(address, c); ok {
			a.Contract = &Contract{Name: "CreateCall", Version: cc.version}
		}
	}
	return a, nil
}

// contractAt returns the contract of a § 2.1 version at address, if any.
func contractAt(address ethrpc.Address) *Contract {
	if ms, ok := multiSends[address]; ok {
		name := "MultiSend"
		if ms.callOnly {
			name = "MultiSendCallOnly"
		}
		return &Contract{Name: name, Version: ms.version}
	}
	if version, ok := singletons[address]; ok {
		if v, ok := strings.CutSuffix(version, "+L2"); ok {
			return &Contract{Name: "SafeL2", Version: v}
		}
		return &Contract{Name: "Safe", Version: version}
	}
	return nil
}

// optional returns a pointer to s, or nil if s is empty.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
