package simulate

import (
	"encoding/hex"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// recordedSimulateAndRevert is the revert data of the eth_call of
// simulateAndRevert that simulates the Safe transaction of arbitration
// cbfadf6b9595, a MultiSendCallOnly batch, at its Safe block 26432178 on
// Ethereum Mainnet, as an archive node answered it: the batch succeeded, and
// returned no data.
const recordedSimulateAndRevert = "0x" +
	"0000000000000000000000000000000000000000000000000000000000000001" +
	"0000000000000000000000000000000000000000000000000000000000000000"

// packed returns the revert data of simulateAndRevert for a delegate call that
// succeeded or not, with data, as the Safe's assembly writes it.
func packed(success bool, data []byte) string {
	flag := uint64(0)
	if success {
		flag = 1
	}
	return "0x" + hex.EncodeToString(append(solabi.Encode(flag, uint64(len(data))), data...))
}

func TestDecodeSimulateAndRevert(t *testing.T) {
	message := solabi.Call(solabi.Selector("Error(string)"), "GS013")
	for _, test := range []struct {
		name    string
		data    string
		success bool
		want    string
	}{
		{"recorded", recordedSimulateAndRevert, true, "0x"},
		{"success with data", packed(true, []byte{1, 2, 3}), true, "0x010203"},
		{"revert with a message", packed(false, message), false, ethrpc.Bytes(message).String()},
	} {
		var data ethrpc.Bytes
		if err := data.UnmarshalText([]byte(test.data)); err != nil {
			t.Fatal(err)
		}
		success, got, err := DecodeSimulateAndRevert(data)
		if err != nil || success != test.success || got.String() != test.want {
			t.Errorf("%s: got %t, %s, %v; want %t, %s", test.name, success, got, err, test.success, test.want)
		}
	}

	// Data that isn't a flag, a length, and that many bytes is malformed, such as
	// abi.encode(success, data), which has the offset of the data.
	for name, data := range map[string][]byte{
		"empty":           nil,
		"no length":       solabi.Encode(uint64(1)),
		"flag that is 2":  solabi.Encode(uint64(2), uint64(0)),
		"short data":      solabi.Encode(uint64(1), uint64(3)),
		"long data":       append(solabi.Encode(uint64(1), uint64(0)), 1),
		"ABI-encoded":     solabi.Encode(true, []byte{}),
		"huge length":     solabi.Encode(uint64(1), new(big.Int).Lsh(big.NewInt(1), 255)),
		"length overflow": append(solabi.Encode(uint64(1), ^uint64(0)), make([]byte, 32)...),
	} {
		if _, _, err := DecodeSimulateAndRevert(data); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDecodeRevert(t *testing.T) {
	abi := []solabi.ABIEntry{
		{Type: "function", Name: "DeploymentFailed"},
		{Type: "error", Name: "DeploymentFailed"},
		{Type: "error", Name: "Unauthorized", Inputs: []solabi.ABIParam{{Name: "caller", Type: "address"}}},
	}
	caller := ethrpc.Address{19: 7}
	unauthorized := solabi.Selector("Unauthorized(address)")
	for _, test := range []struct {
		name string
		data []byte
		want *RevertError
	}{
		{"no data", nil, nil},
		{"short", []byte{1, 2, 3}, nil},
		{
			"Error(string)", solabi.Call(solabi.Selector("Error(string)"), "GS013"),
			&RevertError{Selector: ethrpc.Bytes{0x08, 0xc3, 0x79, 0xa0}, Signature: "Error(string)", Source: "builtin", Args: []Value{{"message", "string", "GS013"}}},
		},
		{
			"Panic(uint256)", solabi.Call(solabi.Selector("Panic(uint256)"), uint64(0x11)),
			&RevertError{Selector: ethrpc.Bytes{0x4e, 0x48, 0x7b, 0x71}, Signature: "Panic(uint256)", Source: "builtin", Args: []Value{{"code", "uint256", "17"}}},
		},
		{
			// The error that the helper of arbitration cbfadf6b9595 reverted with when its
			// third call was simulated alone.
			"error of the ABI", ethrpc.Bytes{0x30, 0x11, 0x64, 0x25},
			&RevertError{Selector: ethrpc.Bytes{0x30, 0x11, 0x64, 0x25}, Signature: "DeploymentFailed()", Source: "sourcify", Args: []Value{}},
		},
		{
			"error with an argument", solabi.Call(unauthorized, caller),
			&RevertError{Selector: ethrpc.Bytes(unauthorized[:]), Signature: "Unauthorized(address)", Source: "sourcify", Args: []Value{{"caller", "address", caller}}},
		},
		{"unknown error", []byte{0xde, 0xad, 0xbe, 0xef, 0}, &RevertError{Selector: ethrpc.Bytes{0xde, 0xad, 0xbe, 0xef}}},
	} {
		if got := DecodeRevert(test.data, abi); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s: got %+v, want %+v", test.name, got, test.want)
		}
	}
}

// batch is a delegate call to a MultiSend whose ABI is multiSendABI.
var (
	multiSend    = ethrpc.MustParseAddress("0x40A2aCCbd92BCA938b02010E17A5b8929b49130D")
	multiSendABI = `[{"type": "function", "name": "multiSend", "inputs": [{"name": "transactions", "type": "bytes"}],
		"outputs": [], "stateMutability": "payable"},
		{"type": "error", "name": "DeploymentFailed", "inputs": []}]`
)

func TestRunSimulateAndRevert(t *testing.T) {
	batchData := solabi.Call(solabi.Selector("multiSend(bytes)"), []byte{1, 2})
	tx := &safenet.SafeTransaction{Safe: safe, To: multiSend, Value: big.NewInt(9), Data: batchData, Operation: safenet.OperationDelegateCall}
	sim, err := TransactionOf(safe, tx)
	if err != nil {
		t.Fatal(err)
	}
	// The call is to the Safe's simulateAndRevert, with the target and its data,
	// and no value, which a delegate call can't send.
	wantData := solabi.Call(solabi.Selector("simulateAndRevert(address,bytes)"), multiSend, []byte(batchData))
	if sim.Method != MethodSimulateAndRevert || sim.Request.To != safe || sim.Request.Value != nil ||
		sim.Request.Data.String() != wantData.String() || sim.To != multiSend || sim.Data.String() != batchData.String() {
		t.Fatalf("TransactionOf: got %+v", sim)
	}

	n := &node{revert: true, revertData: recordedSimulateAndRevert}
	eth := n.serve(t)
	got, err := Run(t.Context(), eth, verified(t, multiSendABI), sim, code, 100)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Method != MethodSimulateAndRevert || got.Block != 100 || got.Status != Success || len(got.ReturnData) != 0 ||
		got.RevertReason != "" || got.Error != nil || got.Function == nil || got.Function.Signature != "multiSend(bytes)" || got.Source == nil {
		t.Errorf("Run of a batch that succeeds: got %+v", got)
	}
	if _, ok := n.last["from"]; ok || n.last["to"] != strings.ToLower(safe.String()) {
		t.Errorf("eth_call: got %v, want a call to the Safe with no sender", n.last)
	}

	// A batch that reverts with an error of the target's ABI.
	n.revertData = packed(false, []byte{0x30, 0x11, 0x64, 0x25})
	got, err = Run(t.Context(), eth, verified(t, multiSendABI), sim, code, 100)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Status != Reverted || got.ReturnData.String() != "0x30116425" || got.Error == nil ||
		got.Error.Signature != "DeploymentFailed()" || got.Function == nil || got.Function.Outputs != nil {
		t.Errorf("Run of a batch that reverts: got %+v with error %+v", got, got.Error)
	}

	// simulateAndRevert that returns, reverts without data, or with malformed data,
	// gives no outcome.
	for name, n := range map[string]*node{
		"returned":     {result: ethrpc.Bytes{}},
		"no data":      {revert: true, revertData: "0x"},
		"no flag":      {revert: true, revertData: "0x08c379a0"},
		"server error": {status: 500},
	} {
		if got, err := Run(t.Context(), n.serve(t), verified(t, multiSendABI), sim, code, 100); err == nil {
			t.Errorf("%s: got %+v, want an error", name, got)
		}
	}
}

// TestRunTransient checks that a node that fails to answer, even with a body
// that says that the call reverted, is never read as a call that reverted.
func TestRunTransient(t *testing.T) {
	// Statuses that httpretry retries, such as 503, would make the test wait.
	for _, status := range []int{500, 501} {
		n := &node{status: status}
		_, err := Call(t.Context(), n.serve(t), verified(t, factoryABI), safe, target, nil, solabi.Call(create, ethrpc.Address{}), safenet.OperationCall, code, 100)
		if err == nil || !ethrpc.Transient(err) {
			t.Errorf("HTTP %d: got error %v, want a transient one", status, err)
		}
	}
}

func TestTransactionOf(t *testing.T) {
	tx := &safenet.SafeTransaction{Safe: safe, To: target, Value: big.NewInt(3), Data: ethrpc.Bytes{1}, Operation: safenet.OperationCall}
	sim, err := TransactionOf(safe, tx)
	if err != nil || sim.Method != MethodCall || sim.Request.From != safe || sim.Request.To != target ||
		(*big.Int)(sim.Request.Value).Int64() != 3 || sim.To != target {
		t.Errorf("TransactionOf a call: got %+v, %v", sim, err)
	}
	tx.Operation = 2
	if _, err := TransactionOf(safe, tx); err == nil {
		t.Error("TransactionOf an operation that is neither: expected an error")
	}
	if _, err := CallOf(safe, target, nil, nil, safenet.OperationDelegateCall); err == nil {
		t.Error("CallOf a delegate call: expected an error")
	}
}
