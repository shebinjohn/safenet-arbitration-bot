package simulate

import (
	"context"
	"math/big"
	"reflect"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/functions"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
	"github.com/safe-research/safenet-arbitration-bot/internal/sourcify"
)

// countingRepository serves contract for the addresses in verified, and counts
// the lookups.
type countingRepository struct {
	repository
	verified map[ethrpc.Address]bool
	lookups  int
}

func (r *countingRepository) Contract(ctx context.Context, chainID uint64, address ethrpc.Address, fields ...sourcify.Field) (*sourcify.Contract, error) {
	r.lookups++
	if !r.verified[address] {
		return nil, sourcify.ErrNotFound
	}
	return r.repository.Contract(ctx, chainID, address, fields...)
}

func TestABIDecoder(t *testing.T) {
	unverified, eoa := ethrpc.Address{0: 0xc0, 19: 2}, ethrpc.Address{0: 0xe0, 19: 3}
	n := &node{codes: map[ethrpc.Address]ethrpc.Bytes{target: code, unverified: code}}
	eth := n.serve(t)
	repo := &countingRepository{repository: *verified(t, factoryABI), verified: map[ethrpc.Address]bool{target: true}}
	reader := checks.NewCallReader(eth, safe, 100)

	createData := solabi.Call(create, ethrpc.Address{19: 9})
	transfer := solabi.Call(solabi.Selector("transfer(address,uint256)"), safe, big.NewInt(1))
	call := func(kind string, to ethrpc.Address, data ethrpc.Bytes) checks.Call {
		return checks.Call{Kind: kind, To: checks.Account{Address: to}, Value: new(big.Int), Data: data, Function: functions.Decode(data)}
	}
	calls := []checks.Call{
		call("batched", target, createData),
		call("batched", target, append(createData[:4:4], 1)),
		call("batched", target, ethrpc.Bytes{0xde, 0xad, 0xbe, 0xef}),
		call("batched", unverified, createData),
		call("batched", eoa, createData),
		call("batched", target, transfer),
		call("batched", target, ethrpc.Bytes{}),
		call("refund", target, createData),
	}
	if err := NewABIDecoder(repo, reader, ethrpc.Mainnet).Decode(t.Context(), calls); err != nil {
		t.Fatal(err)
	}

	// The verified account is decoded, with its arguments if they are well-formed,
	// and an unverified account, one without code, a well-known function, data
	// without a selector, and the refund aren't.
	want := []struct {
		status    string
		signature string
		inputs    []functions.Arg
	}{
		{sourcify.Verified, "create(address)", []functions.Arg{{Name: "gauge", Type: "address", Value: ethrpc.Address{19: 9}}}},
		{sourcify.Verified, "create(address)", nil},
		{sourcify.Verified, "", nil},
		{sourcify.Unverified, "", nil},
		{sourcify.NoCode, "", nil},
	}
	for i, w := range want {
		a := calls[i].ABI
		if a == nil || a.Status != w.status || a.Source != "sourcify" {
			t.Errorf("call %d: got lookup %+v, want status %s", i+1, a, w.status)
			continue
		}
		if (a.URL == "" || a.ObservedAt == nil) != (w.status == sourcify.NoCode) {
			t.Errorf("call %d: got URL %q and observation time %v", i+1, a.URL, a.ObservedAt)
		}
		switch {
		case w.signature == "" && a.Function != nil:
			t.Errorf("call %d: got function %+v, want none", i+1, a.Function)
		case w.signature != "" && (a.Function == nil || a.Function.Signature != w.signature || !reflect.DeepEqual(a.Function.Inputs, w.inputs)):
			t.Errorf("call %d: got function %+v, want %s with inputs %+v", i+1, a.Function, w.signature, w.inputs)
		}
	}
	for i, c := range calls[len(want):] {
		if c.ABI != nil {
			t.Errorf("call %d: got lookup %+v, want none", len(want)+i+1, c.ABI)
		}
	}
	// Each account with code is looked up once.
	if repo.lookups != 2 {
		t.Errorf("lookups: got %d, want 2", repo.lookups)
	}

	// A node that fails to answer fails the decoding, rather than leaving the
	// account undecoded.
	failing := checks.NewCallReader((&node{status: 500}).serve(t), safe, 100)
	if err := NewABIDecoder(repo, failing, ethrpc.Mainnet).Decode(t.Context(), []checks.Call{call("batched", target, createData)}); err == nil {
		t.Error("Decode with a failing node: expected an error")
	}
}
