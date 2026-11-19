package ghsource

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/solcmeta"
)

// The formats of an artifact, by where it has the deployed bytecode.
const (
	// Hardhat is an artifact whose deployedBytecode is a hex string, as Hardhat's
	// artifacts/ and hardhat-deploy's deployments/ have it, and Truffle's.
	Hardhat = "hardhat"
	// Foundry is an artifact whose deployedBytecode is an object with the hex
	// string in object, and the immutableReferences, as Foundry's out/ has it.
	Foundry = "foundry"
	// Solc is the output of the compiler for a contract, whose deployed bytecode is
	// evm.deployedBytecode, an object as Foundry's.
	Solc = "solc"
)

// The kinds of match of an artifact's deployed bytecode and a contract's code,
// outside the metadata that ends the code.
const (
	// Exact is a match of every byte.
	Exact = "exact"
	// ImmutablesMasked is a match of every byte but those of the immutables, which
	// the constructor sets, and of the library addresses, which linking sets.
	ImmutablesMasked = "immutables-masked"
)

// Range is a range of bytes of code.
type Range struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

// Artifact is the deployed bytecode of a build artifact.
type Artifact struct {
	// Format is Hardhat, Foundry, or Solc.
	Format string
	// Code is the deployed bytecode, with zeros for the placeholders of library
	// addresses.
	Code []byte
	// Immutables are the ranges of the immutables, which the artifact's
	// immutableReferences give, if it has them, and Libraries those of the
	// placeholders of library addresses, such as __$<hash>$__.
	Immutables, Libraries []Range
	// HasImmutableReferences is whether the artifact has immutableReferences, even
	// empty ones, without which its immutables can't be masked.
	HasImmutableReferences bool
}

// bytecode is the deployed bytecode of Foundry's artifacts and of the solc
// output.
type bytecode struct {
	Object              *string            `json:"object"`
	ImmutableReferences map[string][]Range `json:"immutableReferences"`
}

// ParseArtifact parses a build artifact, a JSON file with a contract's deployed
// bytecode, in any of the formats. It fails for a file that isn't one, such as
// a README, or for one whose deployed bytecode is empty, as an interface's is.
func ParseArtifact(data []byte) (*Artifact, error) {
	var fields struct {
		DeployedBytecode    json.RawMessage    `json:"deployedBytecode"`
		ImmutableReferences map[string][]Range `json:"immutableReferences"`
		EVM                 *struct {
			DeployedBytecode *bytecode `json:"deployedBytecode"`
		} `json:"evm"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("not a JSON build artifact: %w", err)
	}

	a := &Artifact{}
	var code string
	var refs map[string][]Range
	switch raw := bytes.TrimSpace(fields.DeployedBytecode); {
	case len(raw) > 0 && raw[0] == '"':
		a.Format = Hardhat
		if err := json.Unmarshal(raw, &code); err != nil {
			return nil, fmt.Errorf("deployedBytecode: %w", err)
		}
		// Truffle has the immutable references next to the bytecode.
		refs = fields.ImmutableReferences
		a.HasImmutableReferences = fields.ImmutableReferences != nil
	case len(raw) > 0 && raw[0] == '{':
		a.Format = Foundry
		var b bytecode
		if err := json.Unmarshal(raw, &b); err != nil || b.Object == nil {
			return nil, errors.New("deployedBytecode has no object")
		}
		code, refs, a.HasImmutableReferences = *b.Object, b.ImmutableReferences, b.ImmutableReferences != nil
	case fields.EVM != nil && fields.EVM.DeployedBytecode != nil && fields.EVM.DeployedBytecode.Object != nil:
		a.Format = Solc
		b := fields.EVM.DeployedBytecode
		code, refs, a.HasImmutableReferences = *b.Object, b.ImmutableReferences, b.ImmutableReferences != nil
	default:
		return nil, errors.New("not a build artifact: it has no deployedBytecode")
	}

	var err error
	if a.Code, a.Libraries, err = decodeBytecode(code); err != nil {
		return nil, fmt.Errorf("deployedBytecode: %w", err)
	}
	if len(a.Code) == 0 {
		return nil, errors.New("the deployedBytecode is empty, as an interface's or an abstract contract's is")
	}
	for _, ranges := range refs {
		a.Immutables = append(a.Immutables, ranges...)
	}
	slices.SortFunc(a.Immutables, func(x, y Range) int { return x.Start - y.Start })
	return a, nil
}

// placeholderSize is the size of a library placeholder in hex digits: 20 bytes,
// such as __$<34 hex digits>$__, or __<library name padded with _>__ before
// solc 0.5.
const placeholderSize = 40

// decodeBytecode decodes hex bytecode, with or without 0x, whose library
// placeholders it replaces with zeros, and returns their ranges.
func decodeBytecode(s string) ([]byte, []Range, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 != 0 {
		return nil, nil, errors.New("odd number of hex digits")
	}
	var placeholders []Range
	digits := []byte(s)
	for i := 0; i < len(digits); i += 2 {
		if digits[i] != '_' {
			continue
		}
		end := i + placeholderSize
		if end > len(digits) || !strings.HasPrefix(s[i:], "__") || s[end-2:end] != "__" {
			return nil, nil, fmt.Errorf("invalid library placeholder at byte %d", i/2)
		}
		for j := i; j < end; j++ {
			digits[j] = '0'
		}
		placeholders = append(placeholders, Range{Start: i / 2, Length: placeholderSize / 2})
		i = end - 2
	}
	code, err := hex.DecodeString(string(digits))
	if err != nil {
		return nil, nil, err
	}
	return code, placeholders, nil
}

// The opcodes that push the values that a range masks.
const (
	push20 = 0x73
	push32 = 0x7f
)

// Match is how an artifact's deployed bytecode matches a contract's code.
type Match struct {
	// Kind is Exact or ImmutablesMasked: how the code matches outside its metadata.
	Kind string `json:"match"`
	// MetadataMatch is whether the metadata that ends the code matches too, which
	// means the same sources and settings. Without it, only the code matches.
	MetadataMatch bool `json:"metadataMatch"`
	// Masked are the numbers of bytes that weren't compared.
	Masked Masked `json:"masked"`
}

// Masked are the numbers of bytes of a match that weren't compared, since the
// deployment sets them.
type Masked struct {
	// Immutables are the bytes of the immutables, and of the address that a
	// library's code holds of itself.
	Immutables int `json:"immutables"`
	// Libraries are the bytes of the addresses of linked libraries.
	Libraries int `json:"libraries"`
}

// Compare compares the deployed bytecode of an artifact with code, whose
// metadata is meta, and returns how they match, or an error with the reason if
// they don't. The bytes of a range are masked only where the deployment sets
// them: an immutable is the 32 zero bytes of a PUSH32, a library address the 20
// bytes of a PUSH20, and a library's own address the 20 zero bytes of the
// PUSH20 that its code starts with, so that an artifact can't mask the code
// that it claims to be.
func Compare(code []byte, meta *solcmeta.Metadata, a *Artifact) (*Match, error) {
	if meta == nil {
		return nil, errors.New("the code has no Solidity metadata, so no artifact is a reference for it")
	}
	if len(a.Code) != len(code) {
		return nil, fmt.Errorf("the artifact's deployed bytecode has %d bytes, and the code %d", len(a.Code), len(code))
	}
	end := meta.Offset
	mask := make([]bool, len(code))
	m := &Match{Kind: Exact}
	apply := func(r Range, push byte, size int, zero bool, what string) error {
		if r.Length != size || r.Start < 1 || r.Start+r.Length > end {
			return fmt.Errorf("the artifact's %s at byte %d of %d bytes isn't a %d-byte value in the code before its metadata", what, r.Start, r.Length, size)
		}
		if code[r.Start-1] != push || a.Code[r.Start-1] != push {
			return fmt.Errorf("the artifact's %s at byte %d isn't the value of a PUSH%d", what, r.Start, size)
		}
		if zero && !isZero(a.Code[r.Start:r.Start+r.Length]) {
			return fmt.Errorf("the artifact's %s at byte %d isn't zero in its deployed bytecode", what, r.Start)
		}
		for i := r.Start; i < r.Start+r.Length; i++ {
			mask[i] = true
		}
		return nil
	}
	for _, r := range a.Immutables {
		if err := apply(r, push32, 32, true, "immutable"); err != nil {
			return nil, err
		}
		m.Masked.Immutables += r.Length
	}
	for _, r := range a.Libraries {
		if err := apply(r, push20, 20, true, "library placeholder"); err != nil {
			return nil, err
		}
		m.Masked.Libraries += r.Length
	}
	// A library's code starts with PUSH20 of its own address, which the deployed
	// bytecode has as zeros, and the deployment sets.
	self := Range{Start: 1, Length: 20}
	if end > 21 && a.Code[0] == push20 && isZero(a.Code[1:21]) && !bytes.Equal(code[1:21], a.Code[1:21]) {
		if err := apply(self, push20, 20, true, "library address"); err != nil {
			return nil, err
		}
		m.Masked.Immutables += self.Length
	}

	for i := range end {
		if code[i] == a.Code[i] {
			continue
		}
		if !mask[i] {
			reason := fmt.Sprintf("the artifact's deployed bytecode differs from the code at byte %d", i)
			if !a.HasImmutableReferences {
				reason += ", and the artifact has no immutableReferences, so its immutables, if it has any, can't be masked"
			}
			return nil, errors.New(reason)
		}
		m.Kind = ImmutablesMasked
	}
	if len(a.Libraries) > 0 {
		m.Kind = ImmutablesMasked
	}
	m.MetadataMatch = bytes.Equal(code[end:], a.Code[end:])
	return m, nil
}

func isZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
