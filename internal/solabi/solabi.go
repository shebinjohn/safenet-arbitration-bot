// Package solabi encodes and decodes Solidity ABI data: function calls, return
// values, and event data.
package solabi

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"slices"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
)

// Word is a 32-byte ABI word, the encoding of a single static value.
type Word = [32]byte

// Selector returns the function selector of a function signature, such as
// "getRequest(bytes32)".
func Selector(signature string) [4]byte {
	hash := keccak256.Hash([]byte(signature))
	return [4]byte(hash[:4])
}

// Event returns the topic that identifies an event signature, such as
// "DisputeTriggered(bytes32,uint64)".
func Event(signature string) ethrpc.Hash {
	return keccak256.Hash([]byte(signature))
}

// Call returns the calldata of a call to the function with the given selector,
// with args. See Encode for the types of arguments.
func Call(selector [4]byte, args ...any) ethrpc.Bytes {
	return append(slices.Clone(selector[:]), Encode(args...)...)
}

// Tuple is a tuple of values, such as a struct, for Encode.
type Tuple []any

// Encode returns the ABI encoding of the tuple of values. The Go types of
// values determine their Solidity types:
//
//   - Words and hashes are bytes32.
//   - Addresses are addresses.
//   - Bools are bools.
//   - uint64s and *big.Ints are uint256, which covers every smaller unsigned
//     integer type as well.
//   - []bytes are bytes, and strings are strings.
//   - []ethrpc.Addresses are arrays of addresses.
//   - Tuples are tuples.
//
// It panics for other types, and for *big.Ints that aren't uint256s.
func Encode(values ...any) []byte {
	var head, tail []byte
	for _, value := range values {
		if isDynamic(value) {
			offset := Uint64(uint64(32*len(values) + len(tail)))
			head = append(head, offset[:]...)
			tail = append(tail, encode(value)...)
		} else {
			head = append(head, encode(value)...)
		}
	}
	return append(head, tail...)
}

// isDynamic reports whether value is encoded after the head of its tuple.
func isDynamic(value any) bool {
	switch value := value.(type) {
	case []byte, ethrpc.Bytes, string, []ethrpc.Address:
		return true
	case Tuple:
		return slices.ContainsFunc(value, isDynamic)
	}
	return false
}

func encode(value any) []byte {
	var word Word
	switch value := value.(type) {
	case Word:
		word = value
	case ethrpc.Hash:
		word = value
	case ethrpc.Address:
		word = Address(value)
	case bool:
		if value {
			word[31] = 1
		}
	case uint64:
		word = Uint64(value)
	case *big.Int:
		word = Uint(value)
	case []byte:
		length := Uint64(uint64(len(value)))
		padded := make([]byte, (len(value)+31)/32*32)
		copy(padded, value)
		return append(length[:], padded...)
	case ethrpc.Bytes:
		return encode([]byte(value))
	case string:
		return encode([]byte(value))
	case []ethrpc.Address:
		encoded := Uint64(uint64(len(value)))
		result := encoded[:]
		for _, address := range value {
			word := Address(address)
			result = append(result, word[:]...)
		}
		return result
	case Tuple:
		return Encode(value...)
	default:
		panic(fmt.Sprintf("solabi: cannot encode %T", value))
	}
	return word[:]
}

// Uint64 encodes an unsigned integer.
func Uint64(x uint64) Word {
	var w Word
	binary.BigEndian.PutUint64(w[24:], x)
	return w
}

// Uint encodes an unsigned integer of up to 256 bits. It panics if x is
// negative or wider than 256 bits.
func Uint(x *big.Int) Word {
	if x.Sign() < 0 || x.BitLen() > 256 {
		panic(fmt.Sprintf("solabi: %d is not a uint256", x))
	}
	var w Word
	x.FillBytes(w[:])
	return w
}

// Address encodes an address.
func Address(a ethrpc.Address) Word {
	var w Word
	copy(w[12:], a[:])
	return w
}

// Decoder reads the values of ABI-encoded tuple data, such as return values or
// event data. Each value has one word in the head of the tuple, in order: a
// static value is encoded in its word, and a dynamic value is encoded after the
// head, at the offset its word holds.
//
// The first error is sticky: once a read fails, later reads (including through
// decoders for nested tuples) return zero values, and Err returns the error.
type Decoder struct {
	data []byte
	err  *error
}

// NewDecoder returns a decoder for the tuple encoded in data.
func NewDecoder(data []byte) *Decoder {
	return &Decoder{data: data, err: new(error)}
}

// Err returns the first error that a read from the decoder, or from a decoder
// for one of its nested tuples, ran into.
func (d *Decoder) Err() error {
	return *d.err
}

func (d *Decoder) fail(format string, args ...any) {
	if *d.err == nil {
		*d.err = fmt.Errorf("solabi: "+format, args...)
	}
}

// word returns the word at offset, or nil after an error.
func (d *Decoder) word(offset uint64) []byte {
	if *d.err != nil {
		return nil
	}
	if offset > uint64(len(d.data)) || uint64(len(d.data))-offset < 32 {
		d.fail("data of %d bytes has no word at offset %d", len(d.data), offset)
		return nil
	}
	return d.data[offset : offset+32]
}

// head returns the i-th word of the head, or nil after an error.
func (d *Decoder) head(i int) []byte {
	return d.word(uint64(i) * 32)
}

// Uint returns the i-th value as an unsigned integer of up to 256 bits.
func (d *Decoder) Uint(i int) *big.Int {
	w := d.head(i)
	return new(big.Int).SetBytes(w)
}

// Uint64 returns the i-th value as an unsigned integer that must fit in 64
// bits.
func (d *Decoder) Uint64(i int) uint64 {
	return d.uint64(d.head(i), fmt.Sprintf("value %d", i))
}

func (d *Decoder) uint64(w []byte, what string) uint64 {
	if w == nil {
		return 0
	}
	if !isZero(w[:24]) {
		d.fail("%s 0x%x does not fit in 64 bits", what, w)
		return 0
	}
	return binary.BigEndian.Uint64(w[24:])
}

// Address returns the i-th value as an address.
func (d *Decoder) Address(i int) ethrpc.Address {
	w := d.head(i)
	if w == nil {
		return ethrpc.Address{}
	}
	if !isZero(w[:12]) {
		d.fail("value %d 0x%x is not an address", i, w)
		return ethrpc.Address{}
	}
	return ethrpc.Address(w[12:])
}

// Bool returns the i-th value as a boolean.
func (d *Decoder) Bool(i int) bool {
	w := d.head(i)
	if w == nil {
		return false
	}
	if !isZero(w[:31]) || w[31] > 1 {
		d.fail("value %d 0x%x is not a bool", i, w)
		return false
	}
	return w[31] == 1
}

// Bytes32 returns the i-th value as a bytes32.
func (d *Decoder) Bytes32(i int) [32]byte {
	w := d.head(i)
	if w == nil {
		return [32]byte{}
	}
	return [32]byte(w)
}

// Bytes returns the i-th value as dynamic bytes.
func (d *Decoder) Bytes(i int) []byte {
	offset := d.Uint64(i)
	length := d.uint64(d.word(offset), fmt.Sprintf("length of value %d", i))
	if *d.err != nil {
		return nil
	}
	start := offset + 32
	if length > uint64(len(d.data))-start {
		d.fail("value %d of %d bytes exceeds data of %d bytes", i, length, len(d.data))
		return nil
	}
	return slices.Clone(d.data[start : start+length])
}

// String returns the i-th value as a string.
func (d *Decoder) String(i int) string {
	return string(d.Bytes(i))
}

// Tuple returns a decoder for the i-th value, a dynamic tuple. A static tuple
// has no offset: its values are part of the enclosing head.
func (d *Decoder) Tuple(i int) *Decoder {
	offset := d.Uint64(i)
	if *d.err == nil && offset > uint64(len(d.data)) {
		d.fail("value %d at offset %d exceeds data of %d bytes", i, offset, len(d.data))
	}
	if *d.err != nil {
		return &Decoder{err: d.err}
	}
	return &Decoder{data: d.data[offset:], err: d.err}
}

// Array returns the length of the i-th value, a dynamic array, and a decoder
// for its elements, whose head has one word for each element.
func (d *Decoder) Array(i int) (int, *Decoder) {
	offset := d.Uint64(i)
	length := d.uint64(d.word(offset), fmt.Sprintf("length of value %d", i))
	if *d.err != nil {
		return 0, &Decoder{err: d.err}
	}
	start := offset + 32
	if length > (uint64(len(d.data))-start)/32 {
		d.fail("value %d of %d elements exceeds data of %d bytes", i, length, len(d.data))
		return 0, &Decoder{err: d.err}
	}
	return int(length), &Decoder{data: d.data[start:], err: d.err}
}

func isZero(b []byte) bool {
	return !slices.ContainsFunc(b, func(x byte) bool { return x != 0 })
}
