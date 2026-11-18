package solabi

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// data decodes words written as hex, ignoring whitespace.
func data(t *testing.T, words string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(strings.Fields(words), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSelector(t *testing.T) {
	if got := Selector("transfer(address,uint256)"); got != [4]byte{0xa9, 0x05, 0x9c, 0xbb} {
		t.Errorf("Selector: got %x, want a9059cbb", got)
	}
}

func TestEvent(t *testing.T) {
	const want = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	if got := Event("Transfer(address,address,uint256)"); got.String() != want {
		t.Errorf("Event: got %s, want %s", got, want)
	}
}

func TestCall(t *testing.T) {
	got := Call(Selector("balanceOf(address)"), ethrpc.Address{19: 0xaa})
	want := data(t, `70a08231
		00000000000000000000000000000000000000000000000000000000000000aa`)
	if !bytes.Equal(got, want) {
		t.Errorf("Call: got %x, want %x", got, want)
	}
}

func TestCallDynamic(t *testing.T) {
	// The encoding from `cast calldata`.
	got := Call(
		Selector("f(address,bytes,(uint64,string),bool,bytes32)"),
		ethrpc.Address{19: 0xaa},
		[]byte{0x12, 0x34},
		Tuple{uint64(5), "hello"},
		true,
		Word{31: 1},
	)
	want := data(t, `a0ff2557
		00000000000000000000000000000000000000000000000000000000000000aa
		00000000000000000000000000000000000000000000000000000000000000a0
		00000000000000000000000000000000000000000000000000000000000000e0
		0000000000000000000000000000000000000000000000000000000000000001
		0000000000000000000000000000000000000000000000000000000000000001
		0000000000000000000000000000000000000000000000000000000000000002
		1234000000000000000000000000000000000000000000000000000000000000
		0000000000000000000000000000000000000000000000000000000000000005
		0000000000000000000000000000000000000000000000000000000000000040
		0000000000000000000000000000000000000000000000000000000000000005
		68656c6c6f000000000000000000000000000000000000000000000000000000`)
	if !bytes.Equal(got, want) {
		t.Errorf("Call: got %x, want %x", got, want)
	}
}

func TestEncodeAddresses(t *testing.T) {
	got := Encode([]ethrpc.Address{{19: 0xaa}, {19: 0xbb}}, uint64(7), []ethrpc.Address{})
	want := data(t, `0000000000000000000000000000000000000000000000000000000000000060
		0000000000000000000000000000000000000000000000000000000000000007
		00000000000000000000000000000000000000000000000000000000000000c0
		0000000000000000000000000000000000000000000000000000000000000002
		00000000000000000000000000000000000000000000000000000000000000aa
		00000000000000000000000000000000000000000000000000000000000000bb
		0000000000000000000000000000000000000000000000000000000000000000`)
	if !bytes.Equal(got, want) {
		t.Errorf("Encode: got %x, want %x", got, want)
	}
}

func TestEncodeUnsupported(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Encode: expected a panic for an int")
		}
	}()
	Encode(1)
}

func TestUint(t *testing.T) {
	x := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 96), big.NewInt(1))
	want := Word(data(t, "0000000000000000000000000000000000000000ffffffffffffffffffffffff"))
	if got := Uint(x); got != want {
		t.Errorf("Uint: got %x, want %x", got, want)
	}
	if got, want := Uint64(0x1234), (Word{30: 0x12, 31: 0x34}); got != want {
		t.Errorf("Uint64: got %x, want %x", got, want)
	}
}

func TestDecoder(t *testing.T) {
	// The encoding of (uint64, address, bool, bytes, string, (uint256, bytes)).
	d := NewDecoder(data(t, `
		000000000000000000000000000000000000000000000000000000000000002a
		00000000000000000000000000000000000000000000000000000000000000aa
		0000000000000000000000000000000000000000000000000000000000000001
		00000000000000000000000000000000000000000000000000000000000000c0
		0000000000000000000000000000000000000000000000000000000000000100
		0000000000000000000000000000000000000000000000000000000000000140
		0000000000000000000000000000000000000000000000000000000000000002
		cafe000000000000000000000000000000000000000000000000000000000000
		0000000000000000000000000000000000000000000000000000000000000005
		68656c6c6f000000000000000000000000000000000000000000000000000000
		0100000000000000000000000000000000000000000000000000000000000000
		0000000000000000000000000000000000000000000000000000000000000040
		0000000000000000000000000000000000000000000000000000000000000000
	`))

	if got := d.Uint64(0); got != 42 {
		t.Errorf("Uint64: got %d, want 42", got)
	}
	if got := d.Address(1); got != (ethrpc.Address{19: 0xaa}) {
		t.Errorf("Address: got %s", got)
	}
	if got := d.Bool(2); !got {
		t.Errorf("Bool: got false, want true")
	}
	if got := d.Bytes(3); !bytes.Equal(got, []byte{0xca, 0xfe}) {
		t.Errorf("Bytes: got %x, want cafe", got)
	}
	if got := d.String(4); got != "hello" {
		t.Errorf("String: got %q, want hello", got)
	}
	tuple := d.Tuple(5)
	if got, want := tuple.Uint(0), new(big.Int).Lsh(big.NewInt(1), 248); got.Cmp(want) != 0 {
		t.Errorf("Uint: got %d, want %d", got, want)
	}
	if got := tuple.Bytes(1); len(got) != 0 {
		t.Errorf("Bytes: got %x, want none", got)
	}
	if err := d.Err(); err != nil {
		t.Errorf("Err: %v", err)
	}
}

func TestDecoderArray(t *testing.T) {
	d := NewDecoder(Encode([]ethrpc.Address{{19: 0xaa}, {19: 0xbb}}, uint64(7), []ethrpc.Address{}))
	n, elems := d.Array(0)
	if n != 2 || elems.Address(0) != (ethrpc.Address{19: 0xaa}) || elems.Address(1) != (ethrpc.Address{19: 0xbb}) {
		t.Errorf("Array(0): got %d elements, %s and %s", n, elems.Address(0), elems.Address(1))
	}
	if got := d.Uint64(1); got != 7 {
		t.Errorf("Uint64: got %d, want 7", got)
	}
	if n, _ := d.Array(2); n != 0 {
		t.Errorf("Array(2): got %d elements, want none", n)
	}
	if err := d.Err(); err != nil {
		t.Errorf("Err: %v", err)
	}
}

func TestDecoderErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   string
		decode func(d *Decoder)
	}{
		{"missing word", "", func(d *Decoder) { d.Uint(0) }},
		{"uint64 overflow", "0000000000000000000000000000000000000000000000010000000000000000",
			func(d *Decoder) { d.Uint64(0) }},
		{"dirty address", "00000000000000000000000100000000000000000000000000000000000000aa",
			func(d *Decoder) { d.Address(0) }},
		{"invalid bool", "0000000000000000000000000000000000000000000000000000000000000002",
			func(d *Decoder) { d.Bool(0) }},
		{"offset out of range", "0000000000000000000000000000000000000000000000000000000000000040",
			func(d *Decoder) { d.Bytes(0) }},
		{"length out of range", `
			0000000000000000000000000000000000000000000000000000000000000020
			0000000000000000000000000000000000000000000000000000000000000021
			cafe000000000000000000000000000000000000000000000000000000000000`,
			func(d *Decoder) { d.Bytes(0) }},
		{"huge length", `
			0000000000000000000000000000000000000000000000000000000000000020
			000000000000000000000000000000000000000000000000ffffffffffffffff`,
			func(d *Decoder) { d.String(0) }},
		{"array length out of range", `
			0000000000000000000000000000000000000000000000000000000000000020
			0000000000000000000000000000000000000000000000000000000000000002
			00000000000000000000000000000000000000000000000000000000000000aa`,
			func(d *Decoder) { d.Array(0) }},
		{"huge array length", `
			0000000000000000000000000000000000000000000000000000000000000020
			000000000000000000000000000000000000000000000000ffffffffffffffff`,
			func(d *Decoder) { d.Array(0) }},
		{"tuple out of range", "0000000000000000000000000000000000000000000000000000000000000040",
			func(d *Decoder) { d.Tuple(0).Uint(0) }},
		{"nested error", "0000000000000000000000000000000000000000000000000000000000000020",
			func(d *Decoder) { d.Tuple(0).Uint(0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDecoder(data(t, tc.data))
			tc.decode(d)
			if d.Err() == nil {
				t.Fatal("Err: expected an error")
			}
		})
	}
}
