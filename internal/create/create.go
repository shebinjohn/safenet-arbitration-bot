// Package create derives the addresses of the contracts that the CREATE and
// CREATE2 opcodes deploy, as the Ethereum Yellow Paper and EIP-1014 define
// them.
//
// A derived address is arithmetic over its inputs, not a fact about the chain:
// the deployment can fail, and with CREATE, the deployer's nonce when it runs
// decides the address.
package create

import (
	"math/big"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
)

// Address returns the address of the contract that deployer creates with CREATE
// when its nonce is nonce: the last 20 bytes of the Keccak-256 hash of the RLP
// encoding of [deployer, nonce].
func Address(deployer ethrpc.Address, nonce uint64) ethrpc.Address {
	// The list's payload is the address, as a 20-byte string, and the nonce, as the
	// string of its big-endian bytes without leading zeros, which is empty for
	// zero. Both are shorter than 56 bytes, so each has a one-byte prefix, as the
	// list does.
	payload := append([]byte{0x80 + 20}, deployer[:]...)
	switch n := new(big.Int).SetUint64(nonce).Bytes(); {
	case len(n) == 1 && n[0] < 0x80:
		// A single byte below 0x80 is its own encoding.
		payload = append(payload, n[0])
	default:
		payload = append(append(payload, 0x80+byte(len(n))), n...)
	}
	hash := keccak256.Hash(append([]byte{0xc0 + byte(len(payload))}, payload...))
	return ethrpc.Address(hash[12:])
}

// Address2 returns the address of the contract that deployer creates with
// CREATE2, salt, and the init code whose Keccak-256 hash is initCodeHash: the
// last 20 bytes of keccak256(0xff ++ deployer ++ salt ++ initCodeHash), as
// EIP-1014 defines it.
func Address2(deployer ethrpc.Address, salt, initCodeHash ethrpc.Hash) ethrpc.Address {
	hash := keccak256.Hash([]byte{0xff}, deployer[:], salt[:], initCodeHash[:])
	return ethrpc.Address(hash[12:])
}
