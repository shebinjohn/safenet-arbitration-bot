package safesettings

import (
	"slices"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// Comparison is the result of comparing a setting of two Safes: Equal or
// Different.
type Comparison string

const (
	Equal     Comparison = "equal"
	Different Comparison = "different"
)

// compare returns Equal if equal is true, and Different otherwise.
func compare(equal bool) Comparison {
	if equal {
		return Equal
	}
	return Different
}

// Diff compares the settings of two Safes, A and B, such as two Safes at the
// same block, or one Safe at two blocks, field by field.
type Diff struct {
	Singleton  Comparison `json:"singleton"`
	Owners     ListDiff   `json:"owners"`
	OwnerCount Comparison `json:"ownerCount"`
	Threshold  Comparison `json:"threshold"`
	Nonce      Comparison `json:"nonce"`
	Modules    ListDiff   `json:"modules"`
	// FallbackHandler, Guard, and ModuleGuard compare addresses, where the zero
	// address means that the setting isn't set.
	FallbackHandler Comparison `json:"fallbackHandler"`
	Guard           Comparison `json:"guard"`
	ModuleGuard     Comparison `json:"moduleGuard"`
	// SameConfiguration is whether every setting but the nonce is equal, with the
	// lists compared as sets. The nonce counts the Safe's executions rather than
	// configuring it, so two Safes with the same configuration usually differ in
	// it.
	SameConfiguration bool `json:"sameConfiguration"`
}

// ListDiff compares two lists of addresses, such as the owners of two Safes, as
// sets: the order of a linked list is the order that its entries were added in,
// which doesn't change what they can do.
type ListDiff struct {
	Result Comparison `json:"result"`
	// Shared are the addresses in both lists, in A's order.
	Shared []ethrpc.Address `json:"shared"`
	// OnlyA are the addresses only in A's list, in A's order, and OnlyB those only
	// in B's list, in B's order.
	OnlyA []ethrpc.Address `json:"onlyA"`
	OnlyB []ethrpc.Address `json:"onlyB"`
}

// Compare compares the settings a and b field by field.
func Compare(a, b *Settings) *Diff {
	d := &Diff{
		Singleton:       compare(a.Singleton == b.Singleton),
		Owners:          compareLists(a.Owners, b.Owners),
		OwnerCount:      compare(a.OwnerCount.Cmp(b.OwnerCount) == 0),
		Threshold:       compare(a.Threshold.Cmp(b.Threshold) == 0),
		Nonce:           compare(a.Nonce.Cmp(b.Nonce) == 0),
		Modules:         compareLists(a.Modules, b.Modules),
		FallbackHandler: compare(a.FallbackHandler == b.FallbackHandler),
		Guard:           compare(a.Guard == b.Guard),
		ModuleGuard:     compare(a.ModuleGuard == b.ModuleGuard),
	}
	d.SameConfiguration = !slices.Contains([]Comparison{
		d.Singleton, d.Owners.Result, d.OwnerCount, d.Threshold, d.Modules.Result, d.FallbackHandler, d.Guard, d.ModuleGuard,
	}, Different)
	return d
}

// compareLists compares the lists a and b as sets.
func compareLists(a, b []ethrpc.Address) ListDiff {
	d := ListDiff{Shared: []ethrpc.Address{}, OnlyA: []ethrpc.Address{}, OnlyB: []ethrpc.Address{}}
	for _, address := range a {
		if slices.Contains(b, address) {
			d.Shared = append(d.Shared, address)
		} else {
			d.OnlyA = append(d.OnlyA, address)
		}
	}
	for _, address := range b {
		if !slices.Contains(a, address) {
			d.OnlyB = append(d.OnlyB, address)
		}
	}
	d.Result = compare(len(d.OnlyA) == 0 && len(d.OnlyB) == 0)
	return d
}
