// Package simulate runs a call that a Safe transaction makes, or the whole
// transaction, with eth_call, on the state of the Safe's chain at a block, and
// decodes its arguments, what it returns, and why it reverts, with the ABI that
// Sourcify verified for the account that it calls.
//
// A simulation shows what the call would do on that state, which is onchain
// data (Charter § 2.8), not the effects of the transaction after the proposal.
// A call simulated alone runs on a state without the effects of the calls that
// the transaction makes before it, so only a simulation of the transaction's
// first call, or of the whole transaction, is on the state that the call would
// run on.
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

// The methods of a simulation.
const (
	// MethodCall is a call from the Safe with eth_call, which runs the called
	// account's code in its own context, as a Safe transaction's call does.
	MethodCall = "eth_call"
	// MethodSimulateAndRevert is an eth_call of the Safe's simulateAndRevert, which
	// delegate calls the target from the Safe, in the Safe's context, as a Safe
	// transaction's delegate call does, and reverts with the outcome.
	MethodSimulateAndRevert = "simulateAndRevert"
)

// Result is the outcome of a simulated call.
type Result struct {
	// Method is MethodCall or MethodSimulateAndRevert.
	Method string `json:"method"`
	// Block is the block whose state the call ran on.
	Block  uint64 `json:"block"`
	Status Status `json:"status"`
	// ReturnData is the data that the call returned, or its revert data, if the
	// node reports it.
	ReturnData ethrpc.Bytes `json:"returnData"`
	// RevertReason is the node's message for a call with eth_call that reverted.
	RevertReason string `json:"revertReason,omitempty"`
	// Error is the revert data of a call that reverted, decoded, or nil if it
	// returned, or reverted without data.
	Error *RevertError `json:"error"`
	// Function is the called function and its decoded return values, from the ABI
	// that Sourcify verified for the account, or nil if the account isn't verified
	// or its ABI has no function with the call's selector.
	Function *Function `json:"function"`
	// Source is Sourcify's record of the account, for the ABI, or nil if the
	// account has no code.
	Source *sourcify.Record `json:"source"`
}

// Function is a called function, with the arguments that the call passes and
// the values that it returned.
type Function struct {
	Signature string `json:"signature"`
	// Inputs are the arguments that the call's data passes, decoded with the
	// function's ABI, or nil if an argument has a type that isn't decoded or the
	// data is malformed. Addresses, booleans, unsigned integers, fixed-size byte
	// arrays, bytes, strings, and dynamic arrays of the static types are decoded.
	Inputs []Value `json:"inputs"`
	// Outputs are the returned values, or nil if the call reverted or a value has a
	// type that isn't decoded: only addresses, booleans, unsigned integers, and
	// fixed-size byte arrays are.
	Outputs []Value `json:"outputs"`
}

// Value is an argument that a call passes or a value that a function returned.
type Value struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Simulation is a call to simulate with eth_call: the request, and the account
// whose code it runs, with the data that it runs it with, for decoding.
type Simulation struct {
	Method  string
	Request ethrpc.CallRequest
	// To is the account whose code the simulation runs, and Data its call data: the
	// request's for MethodCall, and the delegate call's for
	// MethodSimulateAndRevert.
	To   ethrpc.Address
	Data ethrpc.Bytes
}

// simulateAndRevertSelector is the selector of the simulateAndRevert of the
// StorageAccessible contract that the Safe singletons of § 2.1 inherit.
var simulateAndRevertSelector = solabi.Selector("simulateAndRevert(address,bytes)")

// CallOf returns the simulation of a call from safe to to with value and data.
// A delegate call can't be simulated this way, since eth_call runs the code of
// the account that it calls in that account's context, not in the Safe's.
func CallOf(safe, to ethrpc.Address, value *big.Int, data ethrpc.Bytes, operation safenet.Operation) (Simulation, error) {
	if operation != safenet.OperationCall {
		return Simulation{}, errors.New("a delegate call can't be simulated on its own")
	}
	req := ethrpc.CallRequest{From: safe, To: to, Data: data}
	if value != nil && value.Sign() > 0 {
		req.Value = (*ethrpc.BigQuantity)(value)
	}
	return Simulation{Method: MethodCall, Request: req, To: to, Data: data}, nil
}

// TransactionOf returns the simulation of the whole Safe transaction tx of
// safe, without its signatures: a call, as CallOf simulates it, or a delegate
// call with the Safe's simulateAndRevert, which delegate calls tx.To with
// tx.Data from the Safe. A Safe transaction's delegate call sends no value, so
// tx.Value is ignored for it, as the Safe ignores it.
//
// simulateAndRevert is a function of the StorageAccessible contract that the
// Safe and SafeL2 singletons of 1.3.0, 1.4.1, and 1.5.0 inherit, so it needs a
// Safe whose singleton is one of them. Anyone can call it, and it has no
// effects, since it reverts.
func TransactionOf(safe ethrpc.Address, tx *safenet.SafeTransaction) (Simulation, error) {
	if tx.Operation == safenet.OperationCall {
		return CallOf(safe, tx.To, tx.Value, tx.Data, tx.Operation)
	}
	if tx.Operation != safenet.OperationDelegateCall {
		return Simulation{}, fmt.Errorf("the Safe transaction's operation %d is neither a call nor a delegate call", tx.Operation)
	}
	data := solabi.Call(simulateAndRevertSelector, tx.To, []byte(tx.Data))
	return Simulation{
		Method:  MethodSimulateAndRevert,
		Request: ethrpc.CallRequest{To: safe, Data: data},
		To:      tx.To, Data: tx.Data,
	}, nil
}

// Call runs a call from safe to to, with value and data, on the state at block,
// and decodes its result with the ABI that repo verified for to, whose code at
// block is code. A delegate call can't be run, since eth_call can't run code in
// the Safe's context; see TransactionOf.
func Call(ctx context.Context, eth *ethrpc.Client, repo sourcify.Repository, safe, to ethrpc.Address, value *big.Int, data ethrpc.Bytes, operation safenet.Operation, code ethrpc.Bytes, block ethrpc.BlockNumber) (*Result, error) {
	sim, err := CallOf(safe, to, value, data, operation)
	if err != nil {
		return nil, err
	}
	return Run(ctx, eth, repo, sim, code, block)
}

// Run runs sim on the state at block, and decodes its result with the ABI that
// repo verified for the account that it runs the code of, whose code at block
// is code.
//
// It fails if a request to the node fails rather than being answered
// (ethrpc.Transient), so that a node that doesn't answer is never read as a
// call that reverted.
func Run(ctx context.Context, eth *ethrpc.Client, repo sourcify.Repository, sim Simulation, code ethrpc.Bytes, block ethrpc.BlockNumber) (*Result, error) {
	result := &Result{Method: sim.Method, Block: uint64(block), Status: Success, ReturnData: ethrpc.Bytes{}}
	out, err := eth.Call(ctx, sim.Request, block)
	revertData, message, reverted := revertOf(err)
	if err != nil && !reverted {
		return nil, fmt.Errorf("simulating the call to %s: %w", sim.Request.To, err)
	}
	switch {
	case sim.Method == MethodSimulateAndRevert && !reverted:
		return nil, fmt.Errorf("simulateAndRevert of %s returned instead of reverting, so its singleton isn't a Safe that has it", sim.Request.To)
	case sim.Method == MethodSimulateAndRevert:
		if len(revertData) == 0 {
			return nil, fmt.Errorf("the node didn't return the revert data of simulateAndRevert (%q), which has the outcome of the simulation", message)
		}
		success, data, err := DecodeSimulateAndRevert(revertData)
		if err != nil {
			return nil, err
		}
		result.ReturnData = data
		if !success {
			result.Status = Reverted
		}
	case reverted:
		result.Status, result.RevertReason, result.ReturnData = Reverted, message, revertData
	default:
		result.ReturnData = out
	}

	lookup, contract, err := sourcify.Fetch(ctx, repo, eth.ChainID(), sim.To, code, sourcify.ABI)
	if err != nil {
		return nil, err
	}
	result.Source = lookup.Source
	var abi []solabi.ABIEntry
	if contract != nil {
		abi = contract.ABI
	}
	if result.Status == Reverted {
		result.Error = DecodeRevert(result.ReturnData, abi)
	}
	if result.Function = DecodeFunction(abi, sim.Data); result.Function != nil && result.Status == Success {
		result.Function.Outputs = decodeOutputs(abi, sim.Data, result.ReturnData)
	}
	return result, nil
}

// revertOf returns the revert data and the node's message of err, if it is the
// node's answer that a call reverted. A request that failed transiently is no
// answer, even if its error mentions a revert.
func revertOf(err error) (ethrpc.Bytes, string, bool) {
	if err == nil || ethrpc.Transient(err) {
		return nil, "", false
	}
	rpcErr, ok := errors.AsType[*ethrpc.Error](err)
	// Geth and the nodes that follow it answer a revert with code 3, and others
	// with a message that says so.
	if !ok || (rpcErr.Code != 3 && !strings.Contains(strings.ToLower(rpcErr.Message), "revert")) {
		return nil, "", false
	}
	var data ethrpc.Bytes
	if len(rpcErr.Data) > 0 && json.Unmarshal(rpcErr.Data, &data) != nil {
		data = nil
	}
	if data == nil {
		data = ethrpc.Bytes{}
	}
	return data, rpcErr.Message, true
}

// DecodeSimulateAndRevert decodes the revert data of the Safe's
// simulateAndRevert: whether the delegate call succeeded, and the data that it
// returned or reverted with. The Safe versions of § 2.1 revert with
// abi.encodePacked(success, returnData.length, returnData), as their
// StorageAccessible's assembly writes it, which its comments call
// abi.encode(success, returnData) but which has no offset of the bytes: a word
// that is 0 or 1, a word with the length, and the data, unpadded.
func DecodeSimulateAndRevert(data []byte) (bool, ethrpc.Bytes, error) {
	d := solabi.NewDecoder(data)
	success := d.Bool(0)
	length := d.Uint(1)
	if d.Err() != nil || !length.IsUint64() || length.Uint64() != uint64(len(data))-64 {
		return false, nil, fmt.Errorf("the revert data of simulateAndRevert, %s, isn't its success flag, length, and data", summarize(data))
	}
	return success, ethrpc.Bytes(data[64:]), nil
}

// summarize returns data as hex, shortened to its first 68 bytes.
func summarize(data []byte) string {
	if len(data) <= 68 {
		return ethrpc.Bytes(data).String()
	}
	return fmt.Sprintf("%s... (%d bytes)", ethrpc.Bytes(data[:68]), len(data))
}

// RevertError is the revert data of a call, decoded.
type RevertError struct {
	Selector ethrpc.Bytes `json:"selector"`
	// Signature is the error's signature, such as "Error(string)", or empty for an
	// error that isn't Error(string), Panic(uint256), or one of the ABI's.
	Signature string `json:"signature,omitempty"`
	// Args are the error's arguments, or nil if one has a type that isn't decoded,
	// or the error is unknown.
	Args []Value `json:"args"`
	// Source is "builtin" for Error(string) and Panic(uint256), which Solidity
	// reverts with, and "sourcify" for an error of the verified ABI.
	Source string `json:"source,omitempty"`
}

// builtinErrors are the errors that Solidity reverts with for require and
// revert with a message, and for a failed assertion or arithmetic.
var builtinErrors = []solabi.ABIEntry{
	{Type: "error", Name: "Error", Inputs: []solabi.ABIParam{{Name: "message", Type: "string"}}},
	{Type: "error", Name: "Panic", Inputs: []solabi.ABIParam{{Name: "code", Type: "uint256"}}},
}

// DecodeRevert decodes the revert data of a call as Error(string),
// Panic(uint256), or one of the errors of abi. It returns nil for data without
// a selector, such as that of revert() or of running out of gas.
func DecodeRevert(data []byte, abi []solabi.ABIEntry) *RevertError {
	if len(data) < 4 {
		return nil
	}
	revert := &RevertError{Selector: ethrpc.Bytes(data[:4])}
	for _, set := range []struct {
		source  string
		entries []solabi.ABIEntry
	}{{"builtin", builtinErrors}, {"sourcify", abi}} {
		for _, entry := range set.entries {
			if entry.Type != "error" {
				continue
			}
			signature, err := entry.Signature()
			if err != nil || solabi.Selector(signature) != [4]byte(data[:4]) {
				continue
			}
			revert.Signature, revert.Source = signature, set.source
			revert.Args = decodeInputs(entry.Inputs, data[4:])
			return revert
		}
	}
	return revert
}

// DecodeFunction decodes call data with the function of abi whose selector it
// starts with, or returns nil if there is none.
func DecodeFunction(abi []solabi.ABIEntry, data []byte) *Function {
	entry, signature := functionOf(abi, data)
	if entry == nil {
		return nil
	}
	return &Function{Signature: signature, Inputs: decodeInputs(entry.Inputs, data[4:])}
}

// functionOf returns the function of abi whose selector data starts with, and
// its signature, or nil if there is none.
func functionOf(abi []solabi.ABIEntry, data []byte) (*solabi.ABIEntry, string) {
	if len(data) < 4 {
		return nil, ""
	}
	for i, entry := range abi {
		if entry.Type != "function" {
			continue
		}
		signature, err := entry.Signature()
		if err == nil && solabi.Selector(signature) == [4]byte(data[:4]) {
			return &abi[i], signature
		}
	}
	return nil, ""
}

// decodeOutputs decodes the values that the function of abi that data calls
// returned in returnData, or returns nil if one has a type that isn't decoded
// or the data is malformed.
func decodeOutputs(abi []solabi.ABIEntry, data, returnData []byte) []Value {
	entry, _ := functionOf(abi, data)
	if entry == nil {
		return nil
	}
	d := solabi.NewDecoder(returnData)
	outputs := make([]Value, len(entry.Outputs))
	for i, p := range entry.Outputs {
		value, ok := decodeStatic(d, i, p.Type)
		if !ok {
			return nil
		}
		outputs[i] = Value{Name: p.Name, Type: p.Type, Value: value}
	}
	if d.Err() != nil {
		return nil
	}
	return outputs
}

// decodeInputs decodes the arguments of a call from its data after the
// selector, or returns nil if one has a type that isn't decoded or the data is
// malformed.
func decodeInputs(params []solabi.ABIParam, data []byte) []Value {
	d := solabi.NewDecoder(data)
	inputs := make([]Value, len(params))
	for i, p := range params {
		var value any
		switch elem, isArray := strings.CutSuffix(p.Type, "[]"); {
		case p.Type == "bytes":
			value = ethrpc.Bytes(d.Bytes(i))
		case p.Type == "string":
			value = d.String(i)
		case isArray && !strings.ContainsAny(elem, "[("):
			n, elems := d.Array(i)
			values := make([]any, n)
			for j := range n {
				v, ok := decodeStatic(elems, j, elem)
				if !ok {
					return nil
				}
				values[j] = v
			}
			value = values
		default:
			v, ok := decodeStatic(d, i, p.Type)
			if !ok {
				return nil
			}
			value = v
		}
		inputs[i] = Value{Name: p.Name, Type: p.Type, Value: value}
	}
	if d.Err() != nil {
		return nil
	}
	return inputs
}

// decodeStatic decodes the i-th value of d, of an address, boolean, unsigned
// integer, or fixed-size byte array type, and reports whether typ is one of
// them.
func decodeStatic(d *solabi.Decoder, i int, typ string) (any, bool) {
	switch {
	case typ == "address":
		return d.Address(i), true
	case typ == "bool":
		return d.Bool(i), true
	case strings.HasPrefix(typ, "uint") && !strings.Contains(typ, "["):
		return d.Uint(i).String(), true
	case strings.HasPrefix(typ, "bytes") && len(typ) > len("bytes") && !strings.Contains(typ, "["):
		return ethrpc.Bytes(sizedBytes(d.Bytes32(i), typ)), true
	}
	return nil, false
}

// sizedBytes returns the first bytes of a word that a bytesN value has.
func sizedBytes(word [32]byte, typ string) []byte {
	var n int
	if _, err := fmt.Sscanf(typ, "bytes%d", &n); err != nil || n < 1 || n > 32 {
		return word[:]
	}
	return word[:n]
}
