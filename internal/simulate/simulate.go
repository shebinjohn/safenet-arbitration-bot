// Package simulate runs a call that a Safe transaction makes with eth_call,
// from the Safe, on the state of the Safe's chain at a block, and decodes what
// it returns with the ABI that Sourcify verified for the account that it calls.
//
// A simulation shows what the call would do on that state, which is onchain
// data (Charter § 2.8), not the effects of the transaction after the proposal.
// It runs the call alone: the state doesn't have the effects of the calls that
// the transaction makes before it, so only a simulation of the transaction's
// first call is on the state that the call would run on.
package simulate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
	"github.com/safe-research/safenet-arbitration-bot/internal/sourcify"
)

// Status is the outcome of a simulated call.
type Status string

const (
	// Success is the status of a call that returned.
	Success Status = "success"
	// Reverted is the status of a call that reverted.
	Reverted Status = "reverted"
)

// Result is the outcome of a simulated call.
type Result struct {
	Status Status `json:"status"`
	// ReturnData is the data that the call returned, or its revert data, if the
	// node reports it.
	ReturnData ethrpc.Bytes `json:"returnData"`
	// RevertReason is the node's message for a call that reverted.
	RevertReason string `json:"revertReason,omitempty"`
	// Function is the called function and its decoded return values, from the ABI
	// that Sourcify verified for the account, or nil if the account isn't verified
	// or its ABI has no function with the call's selector.
	Function *Function `json:"function"`
	// Source is Sourcify's record of the account, for the ABI, or nil if the
	// account has no code.
	Source *sourcify.Record `json:"source"`
}

// Function is a called function, with the values that it returned.
type Function struct {
	Signature string `json:"signature"`
	// Outputs are the returned values, or nil if the call reverted or a value has a
	// type that isn't decoded: only addresses, booleans, unsigned integers, and
	// fixed-size byte arrays are.
	Outputs []Output `json:"outputs"`
}

// Output is a value that a function returned.
type Output struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Call runs a call from safe to to, with value and data, on the state at block,
// and decodes its result with the ABI that repo verified for to, whose code at
// block is code. A delegate call can't be run, since eth_call can't run code in
// the Safe's context.
func Call(ctx context.Context, eth *ethrpc.Client, repo sourcify.Repository, safe, to ethrpc.Address, value *big.Int, data ethrpc.Bytes, operation safenet.Operation, code ethrpc.Bytes, block ethrpc.BlockNumber) (*Result, error) {
	if operation != safenet.OperationCall {
		return nil, errors.New("a delegate call can't be simulated")
	}
	req := ethrpc.CallRequest{From: safe, To: to, Data: data}
	if value != nil && value.Sign() > 0 {
		req.Value = (*ethrpc.BigQuantity)(value)
	}
	result := &Result{Status: Success, ReturnData: ethrpc.Bytes{}}
	out, err := eth.Call(ctx, req, block)
	if rpcErr, ok := errors.AsType[*ethrpc.Error](err); ok && strings.Contains(strings.ToLower(rpcErr.Message), "revert") {
		result.Status, result.RevertReason = Reverted, rpcErr.Message
		var revertData ethrpc.Bytes
		if len(rpcErr.Data) > 0 && json.Unmarshal(rpcErr.Data, &revertData) == nil {
			result.ReturnData = revertData
		}
	} else if err != nil {
		return nil, fmt.Errorf("simulating the call to %s: %w", to, err)
	} else {
		result.ReturnData = out
	}

	lookup, contract, err := sourcify.Fetch(ctx, repo, eth.ChainID(), to, code, sourcify.ABI)
	if err != nil {
		return nil, err
	}
	result.Source = lookup.Source
	if contract == nil || len(data) < 4 {
		return result, nil
	}
	for _, entry := range contract.ABI {
		if entry.Type != "function" {
			continue
		}
		signature, err := entry.Signature()
		if err != nil || solabi.Selector(signature) != [4]byte(data[:4]) {
			continue
		}
		result.Function = &Function{Signature: signature}
		if result.Status == Success {
			result.Function.Outputs = decodeOutputs(entry.Outputs, result.ReturnData)
		}
		break
	}
	return result, nil
}

// decodeOutputs decodes the values of a function's outputs from its return
// data, or returns nil if one has a type that isn't decoded or the data is
// malformed.
func decodeOutputs(params []solabi.ABIParam, data []byte) []Output {
	d := solabi.NewDecoder(data)
	outputs := make([]Output, len(params))
	for i, p := range params {
		outputs[i] = Output{Name: p.Name, Type: p.Type}
		switch {
		case p.Type == "address":
			outputs[i].Value = d.Address(i)
		case p.Type == "bool":
			outputs[i].Value = d.Bool(i)
		case strings.HasPrefix(p.Type, "uint") && !strings.Contains(p.Type, "["):
			outputs[i].Value = d.Uint(i).String()
		case strings.HasPrefix(p.Type, "bytes") && len(p.Type) > len("bytes") && !strings.Contains(p.Type, "["):
			outputs[i].Value = ethrpc.Bytes(sizedBytes(d.Bytes32(i), p.Type))
		default:
			return nil
		}
	}
	if d.Err() != nil {
		return nil
	}
	return outputs
}

// sizedBytes returns the first bytes of a word that a bytesN value has.
func sizedBytes(word [32]byte, typ string) []byte {
	var n int
	if _, err := fmt.Sscanf(typ, "bytes%d", &n); err != nil || n < 1 || n > 32 {
		return word[:]
	}
	return word[:n]
}
