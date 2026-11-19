package checks

import (
	"context"

	"github.com/safe-research/safenet-arbitration-bot/internal/create"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// createCall is a deployment of the CreateCall contract of a Safe version that
// § 2.1 of the Charter lists, with the Keccak-256 hash of its code.
type createCall struct {
	version  string
	codeHash ethrpc.Hash
}

// createCalls are the CreateCall deployments of the Safe versions that § 2.1
// lists, as listed in the safe-deployments repository at
// https://github.com/safe-global/safe-deployments, in
// src/assets/<version>/create_call.json, with their code hashes, which it lists
// too. It lists each of them on every network that the Charter covers, where
// each has that code. 1.3.0 has two: the canonical deployment and the one made
// with an EIP-155 transaction, which have the same code.
//
// CreateCall deploys a contract with the init code that it is given, with
// CREATE (performCreate) or CREATE2 (performCreate2), from its own address, or
// from the Safe's if the Safe delegate calls it, and reverts if the deployment
// fails.
var createCalls = map[ethrpc.Address]createCall{
	ethrpc.MustParseAddress("0x7cbB62EaA69F79e6873cD1ecB2392971036cFAa4"): {"1.3.0", createCall130CodeHash},
	ethrpc.MustParseAddress("0xB19D6FFc2182150F8Eb585b79D4ABcd7C5640A9d"): {"1.3.0", createCall130CodeHash},
	ethrpc.MustParseAddress("0x9b35Af71d77eaf8d7e40252370304687390A1A52"): {"1.4.1", ethrpc.MustParseHash("0x2b3060c55fcb8275653e99ad511a71f67ba76934ed66a7d74d6e68b52afff889")},
	ethrpc.MustParseAddress("0x2Ef5ECfbea521449E4De05EDB1ce63B75eDA90B4"): {"1.5.0", ethrpc.MustParseHash("0x6b7d8d29bdf7004c4617d95041923774f3f7e74b056bff55c1861c9ec92ce54f")},
}

var createCall130CodeHash = ethrpc.MustParseHash("0x8155d988823a4f6f1bcbc76a64af8e510c4ce68819290d43cf24956bd24dee82")

// Creation is the contract that a call to a CreateCall would deploy, derived
// from the call's data and the chain's state: arithmetic over onchain data, not
// a deployment that the chain shows.
type Creation struct {
	// Source is "derived".
	Source  string         `json:"source"`
	Address ethrpc.Address `json:"address"`
	// Method is "create" for performCreate and "create2" for performCreate2.
	Method string `json:"method"`
	// Deployer is the account that deploys the contract: the CreateCall for a call,
	// and the Safe for a delegate call.
	Deployer ethrpc.Address `json:"deployer"`
	// Salt is the CREATE2 salt, and Nonce the deployer's CREATE nonce.
	Salt         *ethrpc.Hash `json:"salt,omitempty"`
	Nonce        *uint64      `json:"nonce,omitempty"`
	InitCodeHash ethrpc.Hash  `json:"initCodeHash"`
}

// isCreateCall reports whether the account at address, whose code is code, is a
// CreateCall of a § 2.1 version.
func isCreateCall(address ethrpc.Address, code []byte) (createCall, bool) {
	c, ok := createCalls[address]
	return c, ok && ethrpc.Hash(keccak256.Hash(code)) == c.codeHash
}

// creations derives the contracts that the calls to a CreateCall of a § 2.1
// version among calls would deploy, and sets them as the calls' Creates.
//
// A CREATE2 address only depends on the call. A CREATE address depends on the
// deployer's nonce when the call runs: it is the deployer's nonce at the
// reader's block, plus one for each deployment by the same deployer that an
// earlier call to a CreateCall makes, since CREATE and CREATE2 both increment
// it. It doesn't count deployments that other calls make, such as those of a
// contract that the Safe delegate calls, which the calls don't show. The nonce
// is only that of the transaction's first call if the transaction runs right
// after the block, so CREATE addresses are only derived with createNonces, and
// left out otherwise.
func (r *CallReader) creations(ctx context.Context, calls []Call) error {
	// deployed counts the deployments of each deployer in the earlier calls, and
	// nonces are the deployers' nonces at the block.
	deployed, nonces := map[ethrpc.Address]uint64{}, map[ethrpc.Address]uint64{}
	for i := range calls {
		c := &calls[i]
		f := c.Function
		// The account is a CreateCall only on the networks that the Charter covers,
		// with its code.
		toCreateCall := c.To.Contract != nil && c.To.Contract.Name == "CreateCall"
		if !toCreateCall || c.Kind == "refund" || c.Placeholder != "" || f == nil || (f.Name != "performCreate" && f.Name != "performCreate2") {
			continue
		}
		deployer := c.To.Address
		if c.Operation == safenet.OperationDelegateCall {
			deployer = r.safe
		}
		initCode := f.Args[1].Value.(ethrpc.Bytes)
		creation := &Creation{Source: "derived", Deployer: deployer, InitCodeHash: keccak256.Hash(initCode)}
		if f.Name == "performCreate2" {
			salt := f.Args[2].Value.(ethrpc.Hash)
			creation.Method, creation.Salt = "create2", &salt
			creation.Address = create.Address2(deployer, salt, creation.InitCodeHash)
		} else {
			if !r.createNonces {
				deployed[deployer]++
				continue
			}
			base, ok := nonces[deployer]
			if !ok {
				var err error
				if base, err = r.client.GetTransactionCount(ctx, deployer, r.block); err != nil {
					return err
				}
				nonces[deployer] = base
			}
			nonce := base + deployed[deployer]
			creation.Method, creation.Nonce = "create", &nonce
			creation.Address = create.Address(deployer, nonce)
		}
		deployed[deployer]++
		c.Creates = creation
	}
	return nil
}
