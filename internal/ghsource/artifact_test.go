package ghsource

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/solcmeta"
)

// runtimeCode returns the runtime code of a deployed contract in the testdata
// of solcmeta: "safe-1.3.0", the Safe 1.3.0 singleton, compiled with solc
// 0.7.6, or "sentinel-oracle", compiled with solc 0.8.30.
func runtimeCode(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "solcmeta", "testdata", name+".hex"))
	if err != nil {
		t.Fatal(err)
	}
	code, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(data)), "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func readMetadata(t *testing.T, code []byte) *solcmeta.Metadata {
	t.Helper()
	meta, err := solcmeta.Read(code)
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

// pushes returns the offsets of the values of the PUSH<size> instructions of
// code, before its metadata, walking its instructions so that the data of a
// push isn't read as an instruction.
func pushes(code []byte, end, size int) []int {
	var offsets []int
	for pc := 0; pc < end; pc++ {
		op := code[pc]
		if op < 0x60 || op > 0x7f {
			continue
		}
		n := int(op) - 0x5f
		if n == size && pc+1+n <= end {
			offsets = append(offsets, pc+1)
		}
		pc += n
	}
	return offsets
}

// hardhat returns a Hardhat artifact with the deployed bytecode code, in hex.
func hardhat(t *testing.T, code string, extra map[string]any) []byte {
	t.Helper()
	artifact := map[string]any{"_format": "hh-sol-artifact-1", "contractName": "Test", "bytecode": "0x", "deployedBytecode": code}
	for k, v := range extra {
		artifact[k] = v
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// foundry returns a Foundry artifact with the deployed bytecode code, and the
// immutables at the offsets of refs, of 32 bytes each.
func foundry(t *testing.T, code []byte, refs ...int) []byte {
	t.Helper()
	ranges := []Range{}
	for _, r := range refs {
		ranges = append(ranges, Range{Start: r, Length: 32})
	}
	artifact := map[string]any{
		"abi": []any{},
		"deployedBytecode": map[string]any{
			"object": "0x" + hex.EncodeToString(code), "sourceMap": "", "linkReferences": map[string]any{},
			"immutableReferences": map[string][]Range{"42": ranges},
		},
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// withZeros returns a copy of code with 32 zero bytes at each offset.
func withZeros(code []byte, offsets ...int) []byte {
	c := bytes.Clone(code)
	for _, o := range offsets {
		clear(c[o : o+32])
	}
	return c
}

func TestCompare(t *testing.T) {
	safe := runtimeCode(t, "safe-1.3.0")
	oracle := runtimeCode(t, "sentinel-oracle")
	safeMeta, oracleMeta := readMetadata(t, safe), readMetadata(t, oracle)
	push32 := pushes(oracle, oracleMeta.Offset, 32)
	push20 := pushes(safe, safeMeta.Offset, 20)
	if len(push32) < 2 || len(push20) == 0 {
		t.Fatalf("got %d PUSH32 values in the oracle and %d PUSH20 values in the Safe, want some", len(push32), len(push20))
	}
	// The oracle's immutables are where it pushes 32-byte values, which the test
	// pretends are immutables, set by the constructor.
	immutables := push32[:2]

	otherMetadata := bytes.Clone(safe)
	otherMetadata[safeMeta.Offset+10] ^= 0xff
	otherCode := bytes.Clone(safe)
	otherCode[100] ^= 0xff
	linked := hex.EncodeToString(safe[:push20[0]]) + "__$0123456789abcdef0123456789abcdef01$__" + hex.EncodeToString(safe[push20[0]+20:])

	for _, test := range []struct {
		name     string
		code     []byte
		meta     *solcmeta.Metadata
		artifact []byte
		format   string
		want     Match
		reason   string
	}{
		{
			name: "exact hardhat", code: safe, meta: safeMeta, artifact: hardhat(t, "0x"+hex.EncodeToString(safe), nil),
			format: Hardhat, want: Match{Kind: Exact, MetadataMatch: true},
		},
		{
			name: "exact foundry with immutables that the code has as zeros", code: withZeros(oracle, immutables...), meta: oracleMeta,
			artifact: foundry(t, withZeros(oracle, immutables...), immutables...),
			format:   Foundry, want: Match{Kind: Exact, MetadataMatch: true, Masked: Masked{Immutables: 64}},
		},
		{
			name: "immutables masked", code: oracle, meta: oracleMeta, artifact: foundry(t, withZeros(oracle, immutables...), immutables...),
			format: Foundry, want: Match{Kind: ImmutablesMasked, MetadataMatch: true, Masked: Masked{Immutables: 64}},
		},
		{
			name: "library linked", code: safe, meta: safeMeta, artifact: hardhat(t, linked, nil),
			format: Hardhat, want: Match{Kind: ImmutablesMasked, MetadataMatch: true, Masked: Masked{Libraries: 20}},
		},
		{
			name: "metadata mismatch", code: otherMetadata, meta: safeMeta, artifact: hardhat(t, hex.EncodeToString(safe), nil),
			format: Hardhat, want: Match{Kind: Exact, MetadataMatch: false},
		},
		{
			name: "solc output", code: safe, meta: safeMeta,
			artifact: []byte(`{"evm": {"deployedBytecode": {"object": "` + hex.EncodeToString(safe) + `", "immutableReferences": {}}}}`),
			format:   Solc, want: Match{Kind: Exact, MetadataMatch: true},
		},
		{
			name: "no match", code: otherCode, meta: safeMeta, artifact: hardhat(t, hex.EncodeToString(safe), nil),
			reason: "differs from the code at byte 100, and the artifact has no immutableReferences",
		},
		{
			name: "immutables not masked without references", code: oracle, meta: oracleMeta, artifact: hardhat(t, hex.EncodeToString(withZeros(oracle, immutables...)), nil),
			reason: "has no immutableReferences",
		},
		{
			name: "length", code: safe[:len(safe)-1], meta: safeMeta, artifact: hardhat(t, hex.EncodeToString(safe), nil),
			reason: "has 22958 bytes, and the code 22957",
		},
		{
			// An artifact can't mask the code that it claims to be: a range must be the
			// value of a PUSH32, zero in the artifact.
			name: "immutable not a push", code: oracle, meta: oracleMeta, artifact: foundry(t, withZeros(oracle, 10), 10),
			reason: "isn't the value of a PUSH32",
		},
		{
			name: "immutable not zero", code: oracle, meta: oracleMeta, artifact: foundry(t, oracle, immutables[0]),
			reason: "isn't zero",
		},
		{
			name: "immutable in the metadata", code: oracle, meta: oracleMeta, artifact: foundry(t, oracle, oracleMeta.Offset+1),
			reason: "isn't a 32-byte value in the code before its metadata",
		},
		{
			name: "no metadata", code: safe, artifact: hardhat(t, hex.EncodeToString(safe), nil),
			reason: "no Solidity metadata",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			artifact, err := ParseArtifact(test.artifact)
			if err != nil {
				t.Fatalf("ParseArtifact: %v", err)
			}
			if test.format != "" && artifact.Format != test.format {
				t.Errorf("ParseArtifact: got format %q, want %q", artifact.Format, test.format)
			}
			match, err := Compare(test.code, test.meta, artifact)
			if test.reason != "" {
				if err == nil || !strings.Contains(err.Error(), test.reason) {
					t.Errorf("Compare: got %+v, %v, want an error with %q", match, err, test.reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("Compare: %v", err)
			}
			if *match != test.want {
				t.Errorf("Compare: got %+v, want %+v", *match, test.want)
			}
		})
	}
}

func TestParseArtifactRejects(t *testing.T) {
	for name, data := range map[string]string{
		"README":                  "# Safe\n\nThe official address is 0x000000000000000000000000000000000000dEaD.\n",
		"ABI only":                `{"abi": [], "address": "0x000000000000000000000000000000000000dEaD"}`,
		"empty bytecode":          `{"deployedBytecode": "0x"}`,
		"foundry without object":  `{"deployedBytecode": {"sourceMap": ""}}`,
		"odd hex":                 `{"deployedBytecode": "0x608"}`,
		"invalid placeholder":     `{"deployedBytecode": "0x6080__$0123$__"}`,
		"not hex":                 `{"deployedBytecode": "0x60zz"}`,
		"deployedBytecode number": `{"deployedBytecode": 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if a, err := ParseArtifact([]byte(data)); err == nil {
				t.Errorf("ParseArtifact: got %+v, want an error", a)
			}
		})
	}
}
