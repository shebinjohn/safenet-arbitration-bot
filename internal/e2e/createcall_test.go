package e2e_test

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
	"github.com/safe-research/safenet-arbitration-bot/internal/solabi"
)

// createCall141 is CreateCall 1.4.1, at its address and with its code on
// Ethereum Mainnet.
var (
	createCall141     = ethrpc.MustParseAddress("0x9b35Af71d77eaf8d7e40252370304687390A1A52")
	createCall141Code = "0x608060405234801561001057600080fd5b50600436106100365760003560e01c80634847be6f1461003b5780634c8c9ea114610134575b600080fd5b6101086004803603606081101561005157600080fd5b81019080803590602001909291908035906020019064010000000081111561007857600080fd5b82018360208201111561008a57600080fd5b803590602001918460018302840111640100000000831117156100ac57600080fd5b91908080601f016020809104026020016040519081016040528093929190818152602001838380828437600081840152601f19601f82011690508083019250505050505050919291929080359060200190929190505050610223565b604051808273ffffffffffffffffffffffffffffffffffffffff16815260200191505060405180910390f35b6101f76004803603604081101561014a57600080fd5b81019080803590602001909291908035906020019064010000000081111561017157600080fd5b82018360208201111561018357600080fd5b803590602001918460018302840111640100000000831117156101a557600080fd5b91908080601f016020809104026020016040519081016040528093929190818152602001838380828437600081840152601f19601f82011690508083019250505050505050919291929050505061031d565b604051808273ffffffffffffffffffffffffffffffffffffffff16815260200191505060405180910390f35b60008183518460200186f59050600073ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1614156102d3576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004018080602001828103825260198152602001807f436f756c64206e6f74206465706c6f7920636f6e74726163740000000000000081525060200191505060405180910390fd5b8073ffffffffffffffffffffffffffffffffffffffff167f4db17dd5e4732fb6da34a148104a592783ca119a1e7bb8829eba6cbadef0b51160405160405180910390a29392505050565b600081516020830184f09050600073ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1614156103cc576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004018080602001828103825260198152602001807f436f756c64206e6f74206465706c6f7920636f6e74726163740000000000000081525060200191505060405180910390fd5b8073ffffffffffffffffffffffffffffffffffffffff167f4db17dd5e4732fb6da34a148104a592783ca119a1e7bb8829eba6cbadef0b51160405160405180910390a29291505056fea26469706673582212204f5b682b785a4bde69d22ce13d07ddd6c58ec565b71a1a95733b0ee584b5e47864736f6c63430007060033"
)

// TestCallsCreateCall runs `arbot calls` on a MultiSendCallOnly batch that
// calls CreateCall 1.4.1 twice on the Mainnet node, with CREATE2 and then
// CREATE, then executes the batch, and checks that the contracts are deployed
// at the addresses that arbot derived.
func TestCallsCreateCall(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	mainnet, a := n.Mainnet, n.Artifacts
	for address, hex := range map[ethrpc.Address]string{createCall141: createCall141Code, multiSendCallOnly130: multiSendCallOnly130Code} {
		var code ethrpc.Bytes
		if err := code.UnmarshalText([]byte(hex)); err != nil {
			t.Fatal(err)
		}
		mainnet.RPC(nil, "anvil_setCode", address, code)
	}
	// A contract's nonce starts at 1 (EIP-161), which anvil_setCode doesn't set.
	mainnet.RPC(nil, "anvil_setNonce", createCall141, "0x1")

	// Init code that deploys the one-byte code 0x01.
	initCode := []byte{0x60, 0x01, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xf3}
	salt := ethrpc.Hash{31: 0x5a}
	var transactions []byte
	for _, data := range [][]byte{
		solabi.Call(solabi.Selector("performCreate2(uint256,bytes,bytes32)"), new(big.Int), initCode, salt),
		solabi.Call(solabi.Selector("performCreate(uint256,bytes)"), new(big.Int), initCode),
	} {
		transactions = append(append(transactions, 0), createCall141[:]...)
		transactions = append(append(transactions, solabi.Encode(uint64(0), uint64(len(data)))...), data...)
	}
	r := n.ProposeTransaction(func(tx *safenet.SafeTransaction) {
		tx.ChainID, tx.To, tx.Value, tx.Operation = big.NewInt(ethrpc.Mainnet), multiSendCallOnly130, new(big.Int), safenet.OperationDelegateCall
		tx.Data = solabi.Call(solabi.Selector("multiSend(bytes)"), transactions)
		tx.SafeTxGas, tx.BaseGas, tx.GasPrice = new(big.Int), new(big.Int), new(big.Int)
	})
	owner := ethrpc.Address{0: 0x0e, 19: 1}
	ether := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	mainnet.SetBalance(owner, ether)
	n.SetupSafe(mainnet, r.Transaction.Safe, a.SafeSingleton, owner)
	mainnet.MineUntil(n.Header(r.Block).Time())

	arbot := n.Arbot()
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, arbot.Run(t, "info", "-json", r.ID.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	type creation struct {
		Source   string         `json:"source"`
		Address  ethrpc.Address `json:"address"`
		Method   string         `json:"method"`
		Deployer ethrpc.Address `json:"deployer"`
		Nonce    *uint64        `json:"nonce"`
	}
	got := decode[struct {
		Calls []struct {
			To struct {
				Contract *struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"contract"`
			} `json:"to"`
			Creates *creation `json:"creates"`
		} `json:"calls"`
	}](t, arbot.Run(t, "calls", "-json", "-request-file", path))
	if len(got.Calls) != 2 {
		t.Fatalf("calls: got %+v, want two calls", got)
	}
	for i, method := range []string{"create2", "create"} {
		c := got.Calls[i]
		if c.To.Contract == nil || c.To.Contract.Name != "CreateCall" || c.To.Contract.Version != "1.4.1" || c.Creates == nil ||
			c.Creates.Source != "derived" || c.Creates.Method != method || c.Creates.Deployer != createCall141 {
			t.Errorf("calls: call %d: got %+v, want a CreateCall 1.4.1 that deploys with %s", i+1, c, method)
		}
	}
	// CreateCall's nonce is 1, and the CREATE2 call increments it.
	if c := got.Calls[1].Creates; c == nil || c.Nonce == nil || *c.Nonce != 2 {
		t.Errorf("calls: call 2: got creation %+v, want nonce 2", c)
	}

	// The contracts are deployed at the derived addresses when the batch runs.
	tx := r.Transaction
	mainnet.Transact(owner, tx.Safe, n.ExecCalldata(tx, owner))
	for i, c := range got.Calls {
		if c.Creates == nil {
			continue
		}
		var code ethrpc.Bytes
		mainnet.RPC(&code, "eth_getCode", c.Creates.Address, "latest")
		if code.String() != "0x01" {
			t.Errorf("call %d: the code at %s is %s, want 0x01", i+1, c.Creates.Address, code)
		}
	}
}
