package simulate

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// TestParseTrace parses the callTracer's trace of the simulateAndRevert call
// that simulates the Safe transaction of arbitration cbfadf6b9595 at its Safe
// block, as an archive node returned it, with each frame's input shortened to
// 36 bytes and its output left out. Its batch calls a helper three times, which
// creates a contract with CREATE each time, nested in calls that delegate call
// the singleton and the MultiSendCallOnly.
func TestParseTrace(t *testing.T) {
	raw, err := os.ReadFile("testdata/trace-cbfadf6b9595.json")
	if err != nil {
		t.Fatal(err)
	}
	trace, err := ParseTrace(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !trace.Available || len(trace.Calls) != 16 {
		t.Fatalf("got %d frames, want 16", len(trace.Calls))
	}
	safe := ethrpc.MustParseAddress("0x47623C62f281807D615eeb4A2CEee9d97F9D3C49")
	helper := ethrpc.MustParseAddress("0xe530c247341e857042FdFf953E73Dd6c580f8D42")
	top, batch, create := trace.Calls[0], trace.Calls[2], trace.Calls[5]
	// simulateAndRevert reverts by design.
	if top.Depth != 0 || top.Type != "CALL" || top.To == nil || *top.To != safe || top.Error != "execution reverted" ||
		top.Selector.String() != "0xb4faba09" || top.Value == nil || top.Value.Sign() != 0 {
		t.Errorf("top frame: got %+v", top)
	}
	if batch.Depth != 2 || batch.Type != "DELEGATECALL" || batch.Selector.String() != "0x8d80ff0a" || batch.Error != "" {
		t.Errorf("batch frame: got %+v", batch)
	}
	if create.Depth != 4 || create.Type != "CREATE" || create.From != helper || create.Selector != nil || create.Error != "" {
		t.Errorf("creation frame: got %+v", create)
	}
	// A STATICCALL sends no value.
	if s := trace.Calls[6]; s.Type != "STATICCALL" || s.Value != nil || s.Selector.String() != "0x82c63066" {
		t.Errorf("static call frame: got %+v", s)
	}

	want := []string{
		"0x3Da548EE3C9d7fb614C869e53ffdaa1a07b8A0a0",
		"0x47C6CE915E24e50A5aa98e10d310968a554Ee8af",
		"0x2505Ac2e825f8a65e9B2393E65c4A940d52a9385",
	}
	if len(trace.Creations) != len(want) {
		t.Fatalf("got %d creations, want %d", len(trace.Creations), len(want))
	}
	for i, c := range trace.Creations {
		if c.Address == nil || *c.Address != ethrpc.MustParseAddress(want[i]) || c.Creator != helper || c.Type != "CREATE" ||
			c.Depth != 4 || c.InitCodeSize != 36 || c.Error != "" {
			t.Errorf("creation %d: got %+v, want %s created by %s", i, c, want[i], helper)
		}
	}
}

func TestParseTraceFailedCreation(t *testing.T) {
	// A CREATE2 that failed has no address, and a nested call its error and revert
	// reason.
	raw := `{"type": "CALL", "from": "0x0000000000000000000000000000000000000001", "to": "0x0000000000000000000000000000000000000002",
		"value": "0x10", "input": "0x12345678", "calls": [
			{"type": "CREATE2", "from": "0x0000000000000000000000000000000000000002", "value": "0x0", "input": "0x6000", "error": "contract address collision"},
			{"type": "CALL", "from": "0x0000000000000000000000000000000000000002", "to": "0x0000000000000000000000000000000000000003",
				"input": "0x01", "error": "execution reverted", "revertReason": "GS013"}
		]}`
	trace, err := ParseTrace(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Calls) != 3 || trace.Calls[0].Value.Int64() != 16 || len(trace.Creations) != 1 {
		t.Fatalf("got %+v", trace)
	}
	if c := trace.Creations[0]; c.Address != nil || c.Type != "CREATE2" || c.InitCodeSize != 2 || c.Error != "contract address collision" {
		t.Errorf("failed creation: got %+v", c)
	}
	if f := trace.Calls[2]; f.Selector != nil || f.InputSize != 1 || f.RevertReason != "GS013" || f.Depth != 1 {
		t.Errorf("reverted call: got %+v", f)
	}
	if _, err := ParseTrace(json.RawMessage(`{"type": "CALL", "value": "12"}`)); err == nil {
		t.Error("trace with a malformed value: expected an error")
	}
}

func TestTraceCall(t *testing.T) {
	req := ethrpc.CallRequest{From: safe, To: target}
	// A node that serves the trace.
	n := &node{trace: json.RawMessage(`{"type": "CALL", "from": "0x5a000000000000000000000000000000000000fe", "to": "0xc000000000000000000000000000000000000001", "input": "0x"}`)}
	trace, err := TraceCall(t.Context(), n.serve(t), req, 100)
	if err != nil || !trace.Available || len(trace.Calls) != 1 || len(trace.Creations) != 0 {
		t.Errorf("TraceCall: got %+v, %v", trace, err)
	}
	if n.last["to"] != strings.ToLower(target.String()) {
		t.Errorf("debug_traceCall: got %v", n.last)
	}
	// A node that doesn't serve it, or the block's state, answers so.
	for name, block := range map[string]ethrpc.BlockNumber{"method": 100, "block": 99} {
		trace, err := TraceCall(t.Context(), (&node{}).serve(t), req, block)
		if err != nil || trace.Available || trace.Reason == "" || trace.Calls == nil {
			t.Errorf("TraceCall from a node without the %s: got %+v, %v; want an unavailable trace with a reason", name, trace, err)
		}
	}
	// A node that fails to answer says nothing about whether it serves traces.
	if trace, err := TraceCall(t.Context(), (&node{status: 500}).serve(t), req, 100); err == nil || !ethrpc.Transient(err) {
		t.Errorf("TraceCall from a failing node: got %+v, %v; want a transient error", trace, err)
	}
}
