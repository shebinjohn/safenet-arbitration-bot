// Package vector reads the Sentinel test vectors of
// https://github.com/safe-research/sentinel-test-vectors, and makes requests of
// them that arbot's commands run on, as they run on a request that arbot info
// -json wrote.
//
// A vector is a Safe transaction, the block to judge it at, and the verdict and
// rule that a correct sentinel answers. It has no oracle request behind it, so
// its request has no oracle fields.
package vector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// The verdicts of a vector.
const (
	Secure   = "secure"
	Insecure = "insecure"
)

// eoaNote starts the note of a vector that is recast from a transaction of an
// externally owned account, as the vectors' README defines it.
const eoaNote = "EOA transaction."

// Spec is a vector, as its spec file has it, which the vectors' schema.json
// defines.
type Spec struct {
	Request struct {
		// Block is the block of the transaction's chain to judge it at: for a vector of
		// an executed transaction, the block before the one that included it.
		Block       ethrpc.Quantity `json:"block"`
		Transaction transaction     `json:"transaction"`
	} `json:"request"`
	Response struct {
		Verdict string `json:"verdict"`
		Rule    string `json:"rule"`
	} `json:"response"`
	Metadata struct {
		Note               string `json:"note"`
		ExampleTransaction string `json:"exampleTransaction"`
	} `json:"metadata"`
}

// transaction is a Safe transaction as a spec encodes it, with its numbers as
// hex quantities and its operation as a number.
type transaction struct {
	ChainID        *ethrpc.BigQuantity `json:"chainId"`
	Safe           ethrpc.Address      `json:"safe"`
	To             ethrpc.Address      `json:"to"`
	Value          *ethrpc.BigQuantity `json:"value"`
	Data           ethrpc.Bytes        `json:"data"`
	Operation      *uint8              `json:"operation"`
	SafeTxGas      *ethrpc.BigQuantity `json:"safeTxGas"`
	BaseGas        *ethrpc.BigQuantity `json:"baseGas"`
	GasPrice       *ethrpc.BigQuantity `json:"gasPrice"`
	GasToken       ethrpc.Address      `json:"gasToken"`
	RefundReceiver ethrpc.Address      `json:"refundReceiver"`
	Nonce          *ethrpc.BigQuantity `json:"nonce"`
}

// Read reads the spec in the file at path.
func Read(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	spec, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("reading vector %s: %w", path, err)
	}
	return spec, nil
}

// Parse parses a spec, and checks the parts of the schema that a request needs.
func Parse(data []byte) (*Spec, error) {
	// Reject unknown fields, as the schema does, so that a misspelled field fails
	// rather than being left empty.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var s Spec
	if err := decoder.Decode(&s); err != nil {
		return nil, err
	}
	tx := s.Request.Transaction
	for name, q := range map[string]*ethrpc.BigQuantity{
		"chainId": tx.ChainID, "value": tx.Value, "safeTxGas": tx.SafeTxGas, "baseGas": tx.BaseGas, "gasPrice": tx.GasPrice, "nonce": tx.Nonce,
	} {
		if q == nil {
			return nil, fmt.Errorf("the transaction has no %s", name)
		}
	}
	if !(*big.Int)(tx.ChainID).IsUint64() {
		return nil, errors.New("the chain ID is too large")
	}
	if tx.Operation == nil || *tx.Operation > uint8(safenet.OperationDelegateCall) {
		return nil, errors.New("the transaction has no operation of 0 or 1")
	}
	if s.Request.Block == 0 {
		return nil, errors.New("the request has no block")
	}
	switch {
	case s.Response.Verdict == Insecure && s.Response.Rule == "":
		return nil, errors.New("an insecure vector has no rule")
	case s.Response.Verdict == Secure && s.Response.Rule != "":
		return nil, errors.New("a secure vector has a rule")
	case s.Response.Verdict != Secure && s.Response.Verdict != Insecure:
		return nil, fmt.Errorf("the verdict is %q, not secure or insecure", s.Response.Verdict)
	}
	return &s, nil
}

// EOA reports whether the vector is recast from a transaction of an externally
// owned account, whose note starts with "EOA transaction.": its Safe address is
// the account's, at which no Safe exists.
func (s *Spec) EOA() bool {
	return strings.HasPrefix(s.Metadata.Note, eoaNote)
}

// Transaction returns the vector's Safe transaction.
func (s *Spec) Transaction() safenet.SafeTransaction {
	tx := s.Request.Transaction
	return safenet.SafeTransaction{
		ChainID:        bigInt(tx.ChainID),
		Safe:           tx.Safe,
		To:             tx.To,
		Value:          bigInt(tx.Value),
		Data:           tx.Data,
		Operation:      safenet.Operation(*tx.Operation),
		SafeTxGas:      bigInt(tx.SafeTxGas),
		BaseGas:        bigInt(tx.BaseGas),
		GasPrice:       bigInt(tx.GasPrice),
		GasToken:       tx.GasToken,
		RefundReceiver: tx.RefundReceiver,
		Nonce:          bigInt(tx.Nonce),
	}
}

// SafenetRequest returns a request of the vector, as arbot info -json writes
// one, for the spec file at path: its Safe block is the vector's block, its
// transaction and Safe transaction hash are the vector's, and its disputed
// rules are the vector's rule for an insecure vector, and none for a secure
// one. It has no oracle fields, such as an ID, terms, votes, or Charter name,
// and no Ethereum Mainnet block, so the commands that read the oracle or the
// Charter don't apply to it, and no block times, which the vector doesn't have.
func (s *Spec) SafenetRequest(path string) *safenet.Request {
	tx := s.Transaction()
	disputed := []string{}
	if s.Response.Verdict == Insecure {
		disputed = append(disputed, s.Response.Rule)
	}
	return &safenet.Request{
		Votes:         []safenet.Commitment{},
		DisputedRules: disputed,
		Proposal: safenet.Proposal{
			SafeBlock:   uint64(s.Request.Block),
			OracleData:  ethrpc.Bytes{},
			SafeTxHash:  tx.Hash(),
			Transaction: tx,
		},
		Vector: &safenet.Vector{Path: path, Verdict: s.Response.Verdict, Rule: s.Response.Rule, EOA: s.EOA()},
	}
}

func bigInt(q *ethrpc.BigQuantity) *big.Int {
	return new(big.Int).Set((*big.Int)(q))
}
