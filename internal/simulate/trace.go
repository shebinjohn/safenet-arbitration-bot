package simulate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
)

// Trace is the trace of a simulated call, with debug_traceCall and the
// callTracer: every call that it makes, nested to any depth, and every contract
// that it creates.
type Trace struct {
	// Available is whether the node served the trace, and Reason is its error if it
	// didn't.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// Calls are the frames of the trace, the simulated call first, each followed by
	// the frames of the calls that it makes, in order.
	Calls []Frame `json:"calls"`
	// Creations are the frames of the CREATE and CREATE2 calls, in the same order.
	Creations []Creation `json:"creations"`
}

// Frame is a call in a trace.
type Frame struct {
	// Depth is 0 for the simulated call, and one more for each call that a call
	// makes.
	Depth int `json:"depth"`
	// Type is the call's opcode, such as CALL, DELEGATECALL, STATICCALL, CREATE, or
	// CREATE2.
	Type string         `json:"type"`
	From ethrpc.Address `json:"from"`
	// To is the account called, or created, or nil for a creation that failed.
	To *ethrpc.Address `json:"to"`
	// Value is the native currency that the call sends, or nil for a call that
	// can't send any, such as a DELEGATECALL.
	Value *big.Int `json:"value"`
	// Selector is the first 4 bytes of the call's data, or nil for a creation,
	// whose data is init code, or data shorter than that.
	Selector  ethrpc.Bytes `json:"selector"`
	InputSize int          `json:"inputSize"`
	// Error is the node's error of a call that failed, such as "execution
	// reverted", and RevertReason the message that it reverted with, if any.
	Error        string `json:"error,omitempty"`
	RevertReason string `json:"revertReason,omitempty"`
}

// Creation is a CREATE or CREATE2 frame of a trace.
type Creation struct {
	Depth int    `json:"depth"`
	Type  string `json:"type"`
	// Address is the created contract, or nil if the creation failed.
	Address *ethrpc.Address `json:"address"`
	// Creator is the account that ran the CREATE or CREATE2 opcode.
	Creator      ethrpc.Address `json:"creator"`
	InitCodeSize int            `json:"initCodeSize"`
	InitCodeHash ethrpc.Hash    `json:"initCodeHash"`
	Error        string         `json:"error,omitempty"`
}

// callFrame is a frame of the callTracer's result.
type callFrame struct {
	Type         string              `json:"type"`
	From         ethrpc.Address      `json:"from"`
	To           *ethrpc.Address     `json:"to"`
	Value        *ethrpc.BigQuantity `json:"value"`
	Input        ethrpc.Bytes        `json:"input"`
	Error        string              `json:"error"`
	RevertReason string              `json:"revertReason"`
	Calls        []callFrame         `json:"calls"`
}

// maxTraceDepth bounds the depth of the frames that ParseTrace reads, past the
// call depth limit of 1024 of the EVM, so that a malformed trace can't make it
// recurse without bound.
const maxTraceDepth = 1100

// TraceCall traces req on the state at block with debug_traceCall and the
// callTracer. A node that answers that it can't, such as one that doesn't serve
// the method or the block's state, gives a trace that isn't available, with the
// node's error as its reason. It fails if the request fails rather than being
// answered (ethrpc.Transient), since that says nothing about whether the node
// serves traces.
func TraceCall(ctx context.Context, eth *ethrpc.Client, req ethrpc.CallRequest, block ethrpc.BlockNumber) (*Trace, error) {
	var raw json.RawMessage
	err := eth.RawRequest(ctx, &raw, "debug_traceCall", req, block, map[string]any{"tracer": "callTracer"})
	if err != nil {
		_, rpcErr := errors.AsType[*ethrpc.Error](err)
		_, httpErr := errors.AsType[*ethrpc.HTTPError](err)
		if ethrpc.Transient(err) || (!rpcErr && !httpErr) {
			return nil, fmt.Errorf("tracing the call to %s: %w", req.To, err)
		}
		return &Trace{Reason: err.Error(), Calls: []Frame{}, Creations: []Creation{}}, nil
	}
	return ParseTrace(raw)
}

// ParseTrace parses the result of the callTracer.
func ParseTrace(raw json.RawMessage) (*Trace, error) {
	var top callFrame
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("decoding the callTracer's trace: %w", err)
	}
	trace := &Trace{Available: true, Calls: []Frame{}, Creations: []Creation{}}
	if err := trace.add(&top, 0); err != nil {
		return nil, err
	}
	return trace, nil
}

// add adds f, at depth, and the frames of the calls that it makes.
func (t *Trace) add(f *callFrame, depth int) error {
	if depth > maxTraceDepth {
		return fmt.Errorf("the callTracer's trace is deeper than %d calls", maxTraceDepth)
	}
	typ := strings.ToUpper(f.Type)
	frame := Frame{Depth: depth, Type: typ, From: f.From, InputSize: len(f.Input), Error: f.Error, RevertReason: f.RevertReason}
	if f.To != nil && (*f.To != ethrpc.Address{} || !isCreate(typ)) {
		to := *f.To
		frame.To = &to
	}
	if f.Value != nil {
		frame.Value = new(big.Int).Set((*big.Int)(f.Value))
	}
	if !isCreate(typ) && len(f.Input) >= 4 {
		frame.Selector = ethrpc.Bytes(f.Input[:4])
	}
	t.Calls = append(t.Calls, frame)
	if isCreate(typ) {
		t.Creations = append(t.Creations, Creation{
			Depth: depth, Type: typ, Address: frame.To, Creator: f.From,
			InitCodeSize: len(f.Input), InitCodeHash: keccak256.Hash(f.Input), Error: f.Error,
		})
	}
	for i := range f.Calls {
		if err := t.add(&f.Calls[i], depth+1); err != nil {
			return err
		}
	}
	return nil
}

// isCreate reports whether a frame's type is a creation.
func isCreate(typ string) bool {
	return typ == "CREATE" || typ == "CREATE2"
}
