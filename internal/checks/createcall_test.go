package checks

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/create"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// createCallFixture is testdata/createcall.json: the code of CreateCall 1.4.1
// on Ethereum Mainnet, and calls to it that real Safe transactions made, with
// the contracts that they created.
type createCallFixture struct {
	CreateCall     ethrpc.Address `json:"createCall"`
	CreateCallCode ethrpc.Bytes   `json:"createCallCode"`
	Calls          []struct {
		Request    string         `json:"request"`
		SafeTxHash ethrpc.Hash    `json:"safeTxHash"`
		Data       ethrpc.Bytes   `json:"data"`
		Created    ethrpc.Address `json:"created"`
	} `json:"calls"`
}

func loadCreateCallFixture(t *testing.T) *createCallFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/createcall.json")
	if err != nil {
		t.Fatal(err)
	}
	var f createCallFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return &f
}

var (
	callOnly130  = ethrpc.MustParseAddress("0x40A2aCCbd92BCA938b02010E17A5b8929b49130D")
	multiSend141 = ethrpc.MustParseAddress("0x38869bf66a61cF6bDB996A6aE40D5853Fd43B526")
)

// TestCreateCallCodeHash checks the code hash of CreateCall 1.4.1 against the
// code that the fixture recorded from Ethereum Mainnet.
func TestCreateCallCodeHash(t *testing.T) {
	f := loadCreateCallFixture(t)
	if c, ok := isCreateCall(f.CreateCall, f.CreateCallCode); !ok || c.version != "1.4.1" {
		t.Errorf("CreateCall %s: got %+v, %t; want version 1.4.1", f.CreateCall, c, ok)
	}
	if _, ok := isCreateCall(f.CreateCall, append(f.CreateCallCode, 0)); ok {
		t.Error("CreateCall with other code: identified")
	}
}

// TestCreate2Vectors derives the addresses of the contracts that real calls to
// CreateCall 1.4.1 created with CREATE2: those of the batch that the Safe of
// arbitration cbfadf6b9595 executed with nonce 24, which created the helper and
// the contracts that its proposal then calls, and the first call of the batch
// of arbitration 8581d6d57749.
func TestCreate2Vectors(t *testing.T) {
	f := loadCreateCallFixture(t)
	safe := ethrpc.MustParseAddress("0x47623C62f281807D615eeb4A2CEee9d97F9D3C49")
	node := &fakeNode{accounts: map[ethrpc.Address]account{
		safe:         proxy(safeProxy141, singletonOf("1.4.1")),
		f.CreateCall: {code: f.CreateCallCode, nonce: 1},
		callOnly130:  {code: ethrpc.Bytes{0x60, 0x80}},
	}}
	var batch []call
	for _, c := range f.Calls {
		batch = append(batch, call{to: f.CreateCall, value: new(big.Int), data: c.Data, operation: safenet.OperationCall})
	}
	tx := safenet.SafeTransaction{Safe: safe, To: callOnly130, Data: solabi.Call(multiSendSelector, pack(batch...)), Operation: safenet.OperationDelegateCall}
	got, err := Calls(t.Context(), node.dialer(t, testSafeBlock), request(tx))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Calls) != len(f.Calls) {
		t.Fatalf("got %d calls, want %d", len(got.Calls), len(f.Calls))
	}
	for i, c := range got.Calls {
		cr := c.Creates
		if c.To.Contract == nil || *c.To.Contract != (Contract{"CreateCall", "1.4.1"}) {
			t.Errorf("call %d (%s): got account %+v, want CreateCall 1.4.1", i+1, f.Calls[i].Request, c.To)
		}
		if cr == nil || cr.Address != f.Calls[i].Created || cr.Source != "derived" || cr.Method != "create2" ||
			cr.Deployer != f.CreateCall || cr.Salt == nil || cr.Nonce != nil {
			t.Errorf("call %d (%s): got creation %+v, want %s created by %s with CREATE2", i+1, f.Calls[i].Request, cr, f.Calls[i].Created, f.CreateCall)
			continue
		}
		initCode := c.Function.Args[1].Value.(ethrpc.Bytes)
		if cr.InitCodeHash != keccak256.Hash(initCode) || *cr.Salt != c.Function.Args[2].Value.(ethrpc.Hash) {
			t.Errorf("call %d (%s): got inputs %+v", i+1, f.Calls[i].Request, cr)
		}
	}
	// CREATE2 doesn't depend on the state, so no nonce is read.
	if n := node.reads.Load(); n != 4 {
		t.Errorf("reads: got %d, want the Safe's code and slot 0, and the code of two accounts", n)
	}
}

// TestCreateNonces derives CREATE addresses from the deployers' nonces: the
// Safe's, for a delegate call to CreateCall, and the CreateCall's, for a call,
// each counting the deployments of the calls before it, CREATE2 included.
func TestCreateNonces(t *testing.T) {
	f := loadCreateCallFixture(t)
	safe := ethrpc.Address{0: 0x5a, 19: 0x01}
	other := ethrpc.Address{0: 0xc0, 19: 0x01}
	node := &fakeNode{accounts: map[ethrpc.Address]account{
		safe:         {code: safeProxy141, slot0: proxy(safeProxy141, singletonOf("1.4.1")).slot0, nonce: 5},
		f.CreateCall: {code: f.CreateCallCode, nonce: 1},
		multiSend141: {code: ethrpc.Bytes{0x60, 0x80}},
		// An account at another address with CreateCall's code isn't CreateCall.
		other: {code: f.CreateCallCode, nonce: 9},
	}}
	initCode := ethrpc.Bytes{0x60, 0x00, 0x60, 0x00, 0xf3}
	performCreate := solabi.Call(solabi.Selector("performCreate(uint256,bytes)"), new(big.Int), []byte(initCode))
	salt := ethrpc.Hash{31: 1}
	performCreate2 := solabi.Call(solabi.Selector("performCreate2(uint256,bytes,bytes32)"), new(big.Int), []byte(initCode), salt)
	batch := pack(
		call{to: f.CreateCall, value: new(big.Int), data: performCreate, operation: safenet.OperationDelegateCall},
		call{to: f.CreateCall, value: new(big.Int), data: performCreate2, operation: safenet.OperationCall},
		call{to: f.CreateCall, value: new(big.Int), data: performCreate, operation: safenet.OperationDelegateCall},
		call{to: f.CreateCall, value: new(big.Int), data: performCreate, operation: safenet.OperationCall},
		call{to: other, value: new(big.Int), data: performCreate, operation: safenet.OperationCall},
	)
	tx := safenet.SafeTransaction{Safe: safe, To: multiSend141, Data: solabi.Call(multiSendSelector, batch), Operation: safenet.OperationDelegateCall}
	codeHash := ethrpc.Hash(keccak256.Hash(initCode))
	nonce := func(n uint64) *uint64 { return &n }
	want := []*Creation{
		{Source: "derived", Address: create.Address(safe, 5), Method: "create", Deployer: safe, Nonce: nonce(5), InitCodeHash: codeHash},
		{Source: "derived", Address: create.Address2(f.CreateCall, salt, codeHash), Method: "create2", Deployer: f.CreateCall, Salt: &salt, InitCodeHash: codeHash},
		{Source: "derived", Address: create.Address(safe, 6), Method: "create", Deployer: safe, Nonce: nonce(6), InitCodeHash: codeHash},
		// The CREATE2 call before it incremented CreateCall's nonce.
		{Source: "derived", Address: create.Address(f.CreateCall, 2), Method: "create", Deployer: f.CreateCall, Nonce: nonce(2), InitCodeHash: codeHash},
		nil,
	}
	check := func(name string, got *TransactionCalls, want []*Creation) {
		t.Helper()
		for i, c := range got.Calls {
			g, _ := json.Marshal(c.Creates)
			w, _ := json.Marshal(want[i])
			if string(g) != string(w) {
				t.Errorf("%s: call %d: got creation %s, want %s", name, i+1, g, w)
			}
		}
	}
	got, err := Calls(t.Context(), node.dialer(t, testSafeBlock), request(tx))
	if err != nil {
		t.Fatal(err)
	}
	check("Calls", got, want)

	// A reader that doesn't derive CREATE nonces, such as one of a Safe's earlier
	// transactions, only derives the CREATE2 address.
	client, err := node.dialer(t, testSafeBlock)(t.Context(), ethrpc.Mainnet)
	if err != nil {
		t.Fatal(err)
	}
	r := request(tx)
	got, err = NewCallReader(client, safe, testSafeBlock).Calls(t.Context(), &r.Proposal.Transaction)
	if err != nil {
		t.Fatal(err)
	}
	check("CallReader without nonces", got, []*Creation{nil, want[1], nil, nil, nil})
}

// TestCreateNonceTransientError checks that a nonce that the node fails to
// answer fails the calls, rather than being read as some nonce.
func TestCreateNonceTransientError(t *testing.T) {
	f := loadCreateCallFixture(t)
	safe := ethrpc.Address{0: 0x5a, 19: 0x01}
	node := &fakeNode{
		accounts: map[ethrpc.Address]account{
			safe:         proxy(safeProxy141, singletonOf("1.4.1")),
			f.CreateCall: {code: f.CreateCallCode},
		},
		fail: "eth_getTransactionCount", failStatus: 500,
	}
	data := solabi.Call(solabi.Selector("performCreate(uint256,bytes)"), new(big.Int), []byte{0x00})
	tx := safenet.SafeTransaction{Safe: safe, To: f.CreateCall, Data: data}
	if _, err := Calls(t.Context(), node.dialer(t, testSafeBlock), request(tx)); err == nil || !ethrpc.Transient(err) {
		t.Errorf("Calls with eth_getTransactionCount failing: got error %v, want a transient one", err)
	}
}
