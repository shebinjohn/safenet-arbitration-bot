package vector

import (
	"slices"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

func TestSafenetRequest(t *testing.T) {
	for _, test := range []struct {
		file     string
		verdict  string
		disputed []string
		eoa      bool
		block    uint64
		safe     string
		op       safenet.Operation
		nonce    int64
	}{
		{"safenet_approve_stake_excess.json", Insecure, []string{"R-4.5"}, false, 0x18a64ef, "0x888614448Eb7c766864faFb1Dd20ff0b47988a87", safenet.OperationDelegateCall, 0x30},
		{"0xAb876791757b47D202B7d6DeF038940cE7A64fe4_35.json", Secure, []string{}, false, 0x18a7c66, "0xAb876791757b47D202B7d6DeF038940cE7A64fe4", safenet.OperationCall, 35},
		{"usdt-100k-near-match.json", Insecure, []string{"R-4.3"}, true, 0x173d7ee, "0xa492b4382186eb0f852669b0784f5a279c5ec00d", safenet.OperationCall, 1},
	} {
		t.Run(test.file, func(t *testing.T) {
			path := "testdata/" + test.file
			spec, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			r := spec.SafenetRequest(path)
			p, tx := r.Proposal, r.Proposal.Transaction
			if r.Vector == nil || r.Vector.Path != path || r.Vector.Verdict != test.verdict || r.Vector.EOA != test.eoa ||
				r.Vector.Rule != strings.Join(test.disputed, "") {
				t.Errorf("vector: got %+v", r.Vector)
			}
			if !slices.Equal(r.DisputedRules, test.disputed) || r.DisputedRules == nil || r.Votes == nil {
				t.Errorf("got disputed rules %#v and votes %#v, want %q and none", r.DisputedRules, r.Votes, test.disputed)
			}
			if p.SafeBlock != test.block || p.EthereumBlock != 0 || r.Charter != "" || r.ID != (ethrpc.Hash{}) || r.Arbitration != nil {
				t.Errorf("got Safe block %d, Ethereum block %d, Charter %q, ID %s, arbitration %+v; want block %d, and no oracle fields",
					p.SafeBlock, p.EthereumBlock, r.Charter, r.ID, r.Arbitration, test.block)
			}
			if tx.ChainID.Uint64() != 1 || tx.Safe != ethrpc.MustParseAddress(test.safe) || tx.Operation != test.op || tx.Nonce.Int64() != test.nonce {
				t.Errorf("got transaction %+v", tx)
			}
			if p.SafeTxHash != tx.Hash() {
				t.Errorf("got Safe transaction hash %s, want the transaction's, %s", p.SafeTxHash, tx.Hash())
			}
		})
	}
}

func TestParse(t *testing.T) {
	valid := `{"request": {"block": "0x10", "transaction": {"chainId": "0x1", "safe": "0x0000000000000000000000000000000000000001",
		"to": "0x0000000000000000000000000000000000000002", "value": "0x0", "data": "0x", "operation": 0, "safeTxGas": "0x0",
		"baseGas": "0x0", "gasPrice": "0x0", "gasToken": "0x0000000000000000000000000000000000000000",
		"refundReceiver": "0x0000000000000000000000000000000000000000", "nonce": "0x0"}},
		"response": {"verdict": "secure"}, "metadata": {"note": "EOA transaction. Sent ether."}}`
	spec, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if !spec.EOA() {
		t.Errorf("got EOA false for a note that starts with %q", eoaNote)
	}
	for name, edit := range map[string][2]string{
		"unknown field":         {`"metadata": {`, `"metadata": {"extra": 1, `},
		"no nonce":              {`, "nonce": "0x0"`, ``},
		"operation 2":           {`"operation": 0`, `"operation": 2`},
		"insecure without rule": {`"verdict": "secure"`, `"verdict": "insecure"`},
		"secure with a rule":    {`"verdict": "secure"`, `"verdict": "secure", "rule": "R-4.3"`},
		"other verdict":         {`"verdict": "secure"`, `"verdict": "out of scope"`},
		"no block":              {`"block": "0x10"`, `"block": "0x0"`},
		"decimal quantity":      {`"value": "0x0"`, `"value": "1"`},
	} {
		if _, err := Parse([]byte(strings.Replace(valid, edit[0], edit[1], 1))); err == nil {
			t.Errorf("%s: got no error", name)
		}
	}
}
