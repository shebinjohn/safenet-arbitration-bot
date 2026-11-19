// Package solcmeta reads the metadata that the Solidity compiler appends to a
// contract's runtime code: a CBOR map with the hash of the contract's metadata
// file, which names its sources and settings, and the compiler version,
// followed by the map's length in 2 bytes.
//
// Two contracts compiled from the same sources with the same settings have the
// same metadata hash, so it is a key that finds the build artifacts of the
// code, and a part of the code that a comparison of artifacts must match for
// the sources to be the same.
package solcmeta

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
)

// The kinds of metadata hash, by their key in the CBOR map.
const (
	// IPFS is the key of the IPFS multihash of the metadata file, which solc
	// appends by default since 0.6.0.
	IPFS = "ipfs"
	// Bzzr0 and Bzzr1 are the keys of the Swarm hashes of the metadata file, which
	// solc appended before 0.6.0, and still can with a setting.
	Bzzr0 = "bzzr0"
	Bzzr1 = "bzzr1"
)

// Metadata is the metadata that the compiler appended to a contract's code.
type Metadata struct {
	// Kind is the key of the hash in the CBOR map: IPFS, Bzzr0, or Bzzr1.
	Kind string
	// Hash is the value of the hash, as the map has it: for IPFS, a multihash of 34
	// bytes, 0x1220 and a SHA2-256 digest, and for Swarm, a 32-byte hash.
	Hash []byte
	// Compiler is the solc version, such as "0.7.6", or "" if the map has none, as
	// solc before 0.5.9 left it out.
	Compiler string
	// Experimental is whether the code was compiled with an experimental feature,
	// such as the ABI coder v2 before 0.6.0.
	Experimental bool
	// Offset is where the CBOR map starts in the code, and Length the length of the
	// map and the 2 bytes of its length, which end the code.
	Offset, Length int
}

// CID returns the IPFS CID of the metadata file, in its version 0 form, such as
// "Qm...", the base58btc encoding of the multihash, or "" for a Swarm hash.
func (m *Metadata) CID() string {
	if m.Kind != IPFS {
		return ""
	}
	return base58(m.Hash)
}

// ErrNoMetadata is the error for code that doesn't end with metadata that solc
// appends, such as code compiled with the metadata hash turned off, by another
// compiler, or written by hand.
var ErrNoMetadata = errors.New("no Solidity metadata")

// Read returns the metadata that ends the runtime code code, or an error that
// wraps ErrNoMetadata if the code doesn't end with a CBOR map with a metadata
// hash.
func Read(code []byte) (*Metadata, error) {
	if len(code) < 2 {
		return nil, fmt.Errorf("%w: the code is shorter than the length of a map", ErrNoMetadata)
	}
	length := int(binary.BigEndian.Uint16(code[len(code)-2:]))
	if length == 0 || length > len(code)-2 {
		return nil, fmt.Errorf("%w: the last 2 bytes give a map of %d bytes, which the code can't hold", ErrNoMetadata, length)
	}
	offset := len(code) - 2 - length
	fields, err := decodeMap(code[offset : len(code)-2])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoMetadata, err)
	}

	m := &Metadata{Offset: offset, Length: length + 2}
	for _, kind := range []string{IPFS, Bzzr1, Bzzr0} {
		if v, ok := fields[kind]; ok {
			hash, ok := v.([]byte)
			if !ok {
				return nil, fmt.Errorf("%w: the %s hash isn't a byte string", ErrNoMetadata, kind)
			}
			m.Kind, m.Hash = kind, hash
			break
		}
	}
	switch {
	case m.Kind == "":
		return nil, fmt.Errorf("%w: the map has no ipfs, bzzr0, or bzzr1 hash", ErrNoMetadata)
	case m.Kind == IPFS && (len(m.Hash) != 34 || m.Hash[0] != 0x12 || m.Hash[1] != 0x20):
		return nil, fmt.Errorf("%w: the ipfs hash isn't a SHA2-256 multihash", ErrNoMetadata)
	case m.Kind != IPFS && len(m.Hash) != 32:
		return nil, fmt.Errorf("%w: the %s hash has %d bytes, not 32", ErrNoMetadata, m.Kind, len(m.Hash))
	}
	// A release build stores its version as 3 bytes, and any other build as the
	// whole version string.
	switch v := fields["solc"].(type) {
	case []byte:
		if len(v) != 3 {
			return nil, fmt.Errorf("%w: the solc version has %d bytes, not 3", ErrNoMetadata, len(v))
		}
		m.Compiler = fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	case string:
		m.Compiler = v
	}
	if experimental, ok := fields["experimental"].(bool); ok {
		m.Experimental = experimental
	}
	return m, nil
}

// decodeMap decodes data, which must be exactly one CBOR map whose keys are
// text strings and whose values are byte strings, text strings, unsigned
// integers, or booleans, the types that solc writes.
func decodeMap(data []byte) (map[string]any, error) {
	d := decoder{data: data}
	major, n, err := d.head()
	if err != nil {
		return nil, err
	}
	if major != majorMap {
		return nil, fmt.Errorf("the CBOR item has major type %d, not a map", major)
	}
	fields := map[string]any{}
	for range n {
		key, err := d.item()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("a key of the map isn't a text string")
		}
		if _, ok := fields[name]; ok {
			return nil, fmt.Errorf("the map has the key %q twice", name)
		}
		if fields[name], err = d.item(); err != nil {
			return nil, err
		}
	}
	if len(d.data) != 0 {
		return nil, fmt.Errorf("%d bytes after the map", len(d.data))
	}
	return fields, nil
}

// The CBOR major types that solc writes.
const (
	majorUint   = 0
	majorBytes  = 2
	majorText   = 3
	majorMap    = 5
	majorSimple = 7
)

// The CBOR simple values of booleans.
const (
	simpleFalse = 20
	simpleTrue  = 21
)

// decoder decodes CBOR items from the start of data.
type decoder struct {
	data []byte
}

// head decodes the head of an item: its major type and its argument, which is
// the value of an integer, the length of a string, or the number of pairs of a
// map. Indefinite lengths aren't supported, since solc doesn't write them.
func (d *decoder) head() (major byte, n uint64, err error) {
	if len(d.data) == 0 {
		return 0, 0, errors.New("the CBOR data ends before an item")
	}
	major, info := d.data[0]>>5, d.data[0]&0x1f
	d.data = d.data[1:]
	if info < 24 {
		return major, uint64(info), nil
	}
	size := map[byte]int{24: 1, 25: 2, 26: 4, 27: 8}[info]
	if size == 0 {
		return 0, 0, fmt.Errorf("unsupported CBOR argument encoding %d", info)
	}
	if len(d.data) < size {
		return 0, 0, errors.New("the CBOR data ends in the head of an item")
	}
	n = new(big.Int).SetBytes(d.data[:size]).Uint64()
	d.data = d.data[size:]
	return major, n, nil
}

// item decodes an item that isn't a map.
func (d *decoder) item() (any, error) {
	major, n, err := d.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case majorUint:
		return n, nil
	case majorBytes, majorText:
		if n > uint64(len(d.data)) {
			return nil, errors.New("the CBOR data ends in a string")
		}
		s := d.data[:n]
		d.data = d.data[n:]
		if major == majorText {
			return string(s), nil
		}
		return s, nil
	case majorSimple:
		switch n {
		case simpleFalse:
			return false, nil
		case simpleTrue:
			return true, nil
		}
	}
	return nil, fmt.Errorf("unsupported CBOR item of major type %d", major)
}

// base58Alphabet is the alphabet of base58btc, which IPFS uses for CIDs of
// version 0.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58 returns the base58btc encoding of data: the digits of data as a
// big-endian number in base 58, with a "1" for each leading zero byte.
func base58(data []byte) string {
	n := new(big.Int).SetBytes(data)
	base, mod := big.NewInt(58), new(big.Int)
	var digits []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		digits = append(digits, base58Alphabet[mod.Int64()])
	}
	for _, b := range data {
		if b != 0 {
			break
		}
		digits = append(digits, base58Alphabet[0])
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
