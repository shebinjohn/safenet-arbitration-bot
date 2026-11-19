package safenet

import (
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// TestSafeID checks SafeID against the topics of the TransactionProposed logs
// of the proposals of requests a049284bbb54 and cbfadf6b9595, for Safes on
// Ethereum Mainnet.
func TestSafeID(t *testing.T) {
	for _, test := range []struct {
		safe ethrpc.Address
		want ethrpc.Hash
	}{
		{
			ethrpc.MustParseAddress("0xc4994fe4f501a02434ca7f25b14fd803f5d7a388"),
			ethrpc.MustParseHash("0x000000000000000000000001c4994fe4f501a02434ca7f25b14fd803f5d7a388"),
		},
		{
			ethrpc.MustParseAddress("0x47623c62f281807d615eeb4a2ceee9d97f9d3c49"),
			ethrpc.MustParseHash("0x00000000000000000000000147623c62f281807d615eeb4a2ceee9d97f9d3c49"),
		},
	} {
		if got := SafeID(ethrpc.Mainnet, test.safe); got != test.want {
			t.Errorf("SafeID(1, %s): got %s, want %s", test.safe, got, test.want)
		}
	}
	if got, want := SafeID(ethrpc.Gnosis, ethrpc.Address{19: 1}), ethrpc.MustParseHash("0x0000000000000000000000640000000000000000000000000000000000000001"); got != want {
		t.Errorf("SafeID(100, ...): got %s, want %s", got, want)
	}
}
