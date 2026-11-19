package solcmeta

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// code returns the runtime code in testdata/<name>.hex, which is the code of a
// deployed contract, copied from the end-to-end test artifacts.
func code(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".hex"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(data)), "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestRead(t *testing.T) {
	for _, tt := range []struct {
		name string
		want Metadata
		cid  string
	}{
		{
			// The Safe 1.3.0 singleton, compiled with solc 0.7.6.
			name: "safe-1.3.0",
			want: Metadata{Kind: IPFS, Hash: mustHex("12203874bcf92e1722cc7bfa0cef1a0985cf0dc3485ba0663db3747ccdf1605df534"), Compiler: "0.7.6", Offset: 22958 - 53, Length: 53},
			cid:  "QmS92nT8ttKhGLXSTyExBv23kFdgRMezYKzPicn9g3xv67",
		},
		{
			// The SentinelOracle of the default deployment, compiled with solc 0.8.30.
			name: "sentinel-oracle",
			want: Metadata{Kind: IPFS, Hash: mustHex("1220817d0aea5d71421b0aefc015d626f2082b83e2e248d1e6ac45deaae74ecffd35"), Compiler: "0.8.30", Offset: 16812 - 53, Length: 53},
			cid:  "QmX47tdDU9Rvi11hKXuRPmzyJUnpwhfNyeh1XGPwEr3eTW",
		},
		{
			// WETH9, compiled with solc 0.4, which appended a Swarm hash and no version.
			name: "weth9",
			want: Metadata{Kind: Bzzr0, Hash: mustHex("deb4c2ccab3c2fdca32ab3f46728389c2fe2c165d5fafa07661e4e004f6c344a"), Offset: 3124 - 43, Length: 43},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Read(code(t, tt.name))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.Kind != tt.want.Kind || !bytes.Equal(got.Hash, tt.want.Hash) || got.Compiler != tt.want.Compiler || got.Experimental || got.Offset != tt.want.Offset || got.Length != tt.want.Length {
				t.Errorf("Read: got %+v, want %+v", got, tt.want)
			}
			if cid := got.CID(); cid != tt.cid {
				t.Errorf("CID: got %q, want %q", cid, tt.cid)
			}
		})
	}
}

func TestReadVersionStringAndExperimental(t *testing.T) {
	// {"ipfs": h'1220' + 32 zero bytes, "experimental": true, "solc": "0.8.0-dev"}
	cbor := append([]byte{0xa3, 0x64}, "ipfs"...)
	cbor = append(append(cbor, 0x58, 0x22, 0x12, 0x20), make([]byte, 32)...)
	cbor = append(append(append(cbor, 0x6c), "experimental"...), 0xf5)
	cbor = append(append(append(append(cbor, 0x64), "solc"...), 0x69), "0.8.0-dev"...)
	code := append(append([]byte{0x60, 0x80}, cbor...), 0, byte(len(cbor)))
	got, err := Read(code)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Compiler != "0.8.0-dev" || !got.Experimental || got.Offset != 2 {
		t.Errorf("Read: got %+v, want version 0.8.0-dev, experimental, at offset 2", got)
	}
	if !strings.HasPrefix(got.CID(), "Qm") {
		t.Errorf("CID: got %q, want a version 0 CID", got.CID())
	}
}

func TestReadNoMetadata(t *testing.T) {
	safe := code(t, "safe-1.3.0")
	for name, code := range map[string][]byte{
		"empty":          nil,
		"no map":         {0x60, 0x80, 0x60, 0x40, 0x52, 0x00},
		"length too big": {0x60, 0x80, 0xff, 0xff},
		// The Safe's code without its last byte ends with the wrong length.
		"truncated": safe[:len(safe)-1],
		// Vyper appends a CBOR array, not a map.
		"array": {0x60, 0x80, 0x82, 0x01, 0x02, 0x00, 0x03},
		// A map without a hash, {"solc": h'000706'}.
		"no hash": append(append([]byte{0xa1, 0x64}, "solc"...), 0x43, 0, 7, 6, 0, 9),
	} {
		t.Run(name, func(t *testing.T) {
			if m, err := Read(code); !errors.Is(err, ErrNoMetadata) {
				t.Errorf("Read: got %+v, %v, want ErrNoMetadata", m, err)
			}
		})
	}
}

func TestBase58(t *testing.T) {
	for in, want := range map[string]string{"": "", "00": "1", "0000ff": "115Q", "61": "2g", "626262": "a3gV"} {
		if got := base58(mustHex(in)); got != want {
			t.Errorf("base58(%s): got %q, want %q", in, got, want)
		}
	}
}
