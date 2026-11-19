package simulate

import (
	"context"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/functions"
	"github.com/safe-research/safenet-arbitration-bot/internal/sourcify"
)

// ABIDecoder decodes the data of the calls that a Safe makes with the ABI that
// Sourcify verified for the account that each calls, as of the account's code
// at the block of a call reader. It looks up each account once.
type ABIDecoder struct {
	repo    sourcify.Repository
	reader  *checks.CallReader
	chainID uint64
	lookups map[ethrpc.Address]abiLookup
}

// abiLookup is the lookup of an account in Sourcify, and its verified contract,
// or nil if it isn't verified.
type abiLookup struct {
	lookup   *sourcify.Lookup
	contract *sourcify.Contract
}

// NewABIDecoder returns a decoder of the calls that reader reads, on the chain
// with chainID, with the ABIs that repo verified.
func NewABIDecoder(repo sourcify.Repository, reader *checks.CallReader, chainID uint64) *ABIDecoder {
	return &ABIDecoder{repo: repo, reader: reader, chainID: chainID, lookups: map[ethrpc.Address]abiLookup{}}
}

// Decode sets the ABI of each of calls whose data isn't a well-known function
// but has a selector, as the verified ABI of the account that it calls decodes
// it, with the arguments decoded as Run decodes them. The refund call and
// placeholders aren't decoded, since they aren't calls of the transaction's
// data.
func (d *ABIDecoder) Decode(ctx context.Context, calls []checks.Call) error {
	for i := range calls {
		c := &calls[i]
		if c.Function != nil || len(c.Data) < 4 || c.Kind == "refund" || c.Placeholder != "" {
			continue
		}
		l, err := d.lookup(ctx, c.To.Address)
		if err != nil {
			return err
		}
		c.ABI = &checks.ABILookup{Status: l.lookup.Status, Source: "sourcify"}
		if s := l.lookup.Source; s != nil {
			observed := s.ObservedAt
			c.ABI.URL, c.ABI.ObservedAt = s.URL, &observed
		}
		if l.contract == nil {
			continue
		}
		if f := DecodeFunction(l.contract.ABI, c.Data); f != nil {
			c.ABI.Function = &checks.ABIFunction{Signature: f.Signature}
			if f.Inputs != nil {
				c.ABI.Function.Inputs = make([]functions.Arg, len(f.Inputs))
				for j, in := range f.Inputs {
					c.ABI.Function.Inputs[j] = functions.Arg{Name: in.Name, Type: in.Type, Value: in.Value}
				}
			}
		}
	}
	return nil
}

// lookup returns the lookup of the account at address, looking it up once.
func (d *ABIDecoder) lookup(ctx context.Context, address ethrpc.Address) (abiLookup, error) {
	if l, ok := d.lookups[address]; ok {
		return l, nil
	}
	code, err := d.reader.Code(ctx, address)
	if err != nil {
		return abiLookup{}, err
	}
	lookup, contract, err := sourcify.Fetch(ctx, d.repo, d.chainID, address, code, sourcify.ABI)
	if err != nil {
		return abiLookup{}, err
	}
	l := abiLookup{lookup: lookup, contract: contract}
	d.lookups[address] = l
	return l, nil
}
