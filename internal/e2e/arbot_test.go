package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// TestPendingAndInfo runs `arbot pending`, `arbot info` and `arbot classify`
// against the default Safenet deployment's contracts on anvil, with requests in
// every state and with every arbitration outcome.
func TestPendingAndInfo(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	s1, s2, s3 := n.Artifacts.Sentinels[0], n.Artifacts.Sentinels[1], n.Artifacts.Sentinels[2]

	// A dispute that the arbitrator lets time out, which comes first so that its
	// timeout has passed by the time the pending dispute below is frozen.
	timedOut := n.Propose(nil)
	n.Vote(timedOut.Approve(s1), timedOut.Deny(s2, "R-4.1"))
	n.Mine(n.ArbitrationTimeout)
	timedOut.TimeoutArbitration()

	// Two proposals in the same block, one ruled insecure and one out of scope.
	pair := n.ProposeInOneBlock([]byte("insecure"), []byte("out of scope"))
	insecure, outOfScope := pair[0], pair[1]
	n.Vote(insecure.Approve(s1), insecure.Deny(s2, "R-4.1"), outOfScope.Approve(s2), outOfScope.Deny(s3, "R-4.2"))
	insecure.Resolve(false, "ipfs://bafkreidivzimqfqtoqxkrpge6bjyhlvxqs3rhe73owtmdulaxr5do5in7u")
	outOfScope.MarkOutOfScope("Not a Safe transaction.")

	// A dispute ruled secure, where the denials cite several rules.
	secure := n.Propose(nil)
	n.Vote(secure.Deny(s1, "R-4.4, R-4.5"), secure.Approve(s2), secure.Deny(s3, "R-4.5,R-4.6, not a rule"))
	secure.Resolve(true, "A reasonable allowance.")

	// A unanimous approval, which is never arbitrated.
	unanimous := n.Propose(nil)
	n.Vote(unanimous.Approve(s1), unanimous.Approve(s3))

	// A request that times out without a revealed vote.
	unrevealed := n.Propose(nil)
	n.Commit(unrevealed.Deny(s2, "R-4.1"))
	n.MineTo(unrevealed.RevealDeadline + 1)
	unrevealed.Finalize()

	// The dispute awaiting arbitration.
	frozen := n.Propose(nil)
	n.Vote(frozen.Approve(s1), frozen.Deny(s2, "R-4.1"), frozen.Approve(s3))

	// A request that the sentinels are still voting on.
	pending := n.Propose(nil)
	n.Commit(pending.Approve(s3))

	arbot := n.Arbot()

	t.Run("pending", func(t *testing.T) {
		deadline := frozen.FrozenBlock + n.ArbitrationTimeout
		want := []safenet.Dispute{{RequestID: frozen.ID, FrozenBlock: frozen.FrozenBlock, Deadline: deadline}}
		if got := decode[[]safenet.Dispute](t, arbot.Run(t, "pending", "-json")); !reflect.DeepEqual(got, want) {
			t.Errorf("pending: got %+v, want %+v", got, want)
		}
		text := arbot.Run(t, "pending")
		if !regexp.MustCompile(fmt.Sprintf(`(?m)^%s +%d +%d$`, frozen.ID, frozen.FrozenBlock, deadline)).Match(text) {
			t.Errorf("pending: output doesn't list the dispute:\n%s", text)
		}
	})

	tests := []struct {
		name    string
		request *e2e.Request
		state   string
		// outcome is empty for a request without an arbitration.
		outcome       string
		disputedRules []string
	}{
		{"arbitration timed out", timedOut, "TIMED_OUT", "timed-out", []string{"R-4.1"}},
		{"insecure", insecure, "RESOLVED_DENIED", "insecure", []string{"R-4.1"}},
		{"out of scope", outOfScope, "TIMED_OUT", "out-of-scope", []string{"R-4.2"}},
		{"secure", secure, "RESOLVED_APPROVED", "secure", []string{"R-4.4", "R-4.5", "R-4.6"}},
		{"unanimous", unanimous, "RESOLVED_APPROVED", "", []string{}},
		{"unrevealed", unrevealed, "TIMED_OUT", "", []string{}},
		{"frozen", frozen, "FROZEN", "pending", []string{"R-4.1"}},
		{"pending", pending, "PENDING", "", []string{}},
	}
	for _, test := range tests {
		t.Run("info "+test.name, func(t *testing.T) {
			r := test.request
			got := decode[requestInfo](t, arbot.Run(t, "info", "-json", r.ID.String()))
			if got.State != test.state {
				t.Errorf("state: got %s, want %s", got.State, test.state)
			}
			// The configuration that fetch-artifacts sets up: a fee of one token, bond and
			// slashing multipliers of 10 and 1, and a 10% DAO fee share.
			ether := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
			terms := got.Terms
			if got.Charter != "charter.safenet-gov.eth" || terms.Bond.Cmp(new(big.Int).Mul(ether, big.NewInt(10))) != 0 ||
				terms.SlashAmount.Cmp(ether) != 0 || terms.DAOFeeShare != 10_000 {
				t.Errorf("terms: got Charter %q, bond %s, slash amount %s, and DAO fee share %d, want the fetch-artifacts configuration",
					got.Charter, terms.Bond, terms.SlashAmount, terms.DAOFeeShare)
			}

			a := n.Artifacts
			p := got.Proposal
			if p.Consensus != a.Consensus || p.Block != r.Block || p.TxHash != r.TxHash || p.Epoch != a.Epoch {
				t.Errorf("proposal: got Consensus %s, block %d, transaction %s, and epoch %d, want %s, %d, %s, and %d",
					p.Consensus, p.Block, p.TxHash, p.Epoch, a.Consensus, r.Block, r.TxHash, a.Epoch)
			}
			if !bytes.Equal(p.OracleData, r.OracleData) || p.SafeTxHash != r.SafeTxHash {
				t.Errorf("proposal: got oracle data %s and Safe transaction hash %s, want %s and %s",
					p.OracleData, p.SafeTxHash, ethrpc.Bytes(r.OracleData), r.SafeTxHash)
			}
			if want := canonical(t, r.Transaction); !reflect.DeepEqual(canonical(t, p.Transaction), want) {
				t.Errorf("Safe transaction: got %s, want %s", p.Transaction, want)
			}
			if p.Time != n.Header(r.Block).Time() {
				t.Errorf("proposal: got time %s, want that of block %d", p.Time, r.Block)
			}
			checkBlockBefore(t, n.Mainnet, "Ethereum", p.EthereumBlock, p.Time)
			safeNode := n.Mainnet
			if r.Transaction.ChainID.Uint64() == ethrpc.Gnosis {
				checkBlockBefore(t, n.Anvil, "Gnosis Chain", p.SafeBlock, p.Time)
				safeNode = n.Anvil
			} else if p.SafeBlock != p.EthereumBlock {
				t.Errorf("proposal: got Safe chain block %d, want Ethereum block %d", p.SafeBlock, p.EthereumBlock)
			}
			if want := n.Mainnet.Header(p.EthereumBlock).Time(); !p.EthereumBlockTime.Equal(want) {
				t.Errorf("proposal: got Ethereum block time %s, want %s", p.EthereumBlockTime, want)
			}
			if want := safeNode.Header(p.SafeBlock).Time(); !p.SafeBlockTime.Equal(want) {
				t.Errorf("proposal: got Safe chain block time %s, want %s", p.SafeBlockTime, want)
			}

			// The rules that each reason of a denial cites.
			citedRules := map[string][]string{
				"R-4.1":                   {"R-4.1"},
				"R-4.2":                   {"R-4.2"},
				"R-4.4, R-4.5":            {"R-4.4", "R-4.5"},
				"R-4.5,R-4.6, not a rule": {"R-4.5", "R-4.6"},
			}
			var votes []vote
			for _, b := range r.Ballots {
				v := vote{Sentinel: b.Sentinel, Vote: "PENDING", Rules: []string{}}
				if r.Revealed[b.Sentinel] {
					v.Vote, v.Reason = "DENIED", b.Reason
					if b.Approve {
						v.Vote = "APPROVED"
					} else {
						v.Rules = citedRules[b.Reason]
					}
				}
				votes = append(votes, v)
			}
			if !reflect.DeepEqual(got.Votes, votes) {
				t.Errorf("votes: got %+v, want %+v", got.Votes, votes)
			}
			if !reflect.DeepEqual(got.DisputedRules, test.disputedRules) {
				t.Errorf("disputed rules: got %q, want %q", got.DisputedRules, test.disputedRules)
			}

			if test.outcome == "" {
				if got.Arbitration != nil {
					t.Errorf("arbitration: got %+v, want none", got.Arbitration)
				}
			} else if arb := got.Arbitration; arb == nil {
				t.Errorf("arbitration: got none, want outcome %s", test.outcome)
			} else {
				want := arbitration{
					FrozenBlock: r.FrozenBlock,
					Deadline:    r.FrozenBlock + n.ArbitrationTimeout,
					Outcome:     test.outcome,
					Context:     r.Context,
				}
				if r.Record != nil {
					want.Record = &record{Block: uint64(r.Record.BlockNumber), TxHash: r.Record.TransactionHash}
				}
				if !reflect.DeepEqual(*arb, want) {
					t.Errorf("arbitration: got %+v, want %+v", *arb, want)
				}
			}

			// The text shows the state, the fee token's amounts in its units, checksummed
			// addresses, and the disputed rules.
			disputedRules := "-"
			if len(test.disputedRules) > 0 {
				disputedRules = regexp.QuoteMeta(strings.Join(test.disputedRules, ", "))
			}
			text := arbot.Run(t, "info", r.ID.String())
			for _, line := range []string{
				`  State +` + test.state,
				`  Fee token +` + a.FeeToken.String() + ` \(WETH, 18 decimals\)`,
				`  Bond +10 WETH`,
				`  Slash amount +1 WETH`,
				`  Sponsor +` + n.Sponsor.String(),
				`Disputed rules\n  ` + disputedRules,
			} {
				if !regexp.MustCompile(`(?m)^` + line + `$`).Match(text) {
					t.Errorf("info: output has no line matching %q:\n%s", line, text)
				}
			}
		})
	}

	t.Run("info unknown request", func(t *testing.T) {
		stderr := arbot.Fail(t, 1, "info", ethrpc.Hash{31: 1}.String())
		if !bytes.Contains(stderr, []byte("request not found")) {
			t.Errorf("info: got error %q, want one saying that the request was not found", stderr)
		}
	})

	// The Safe is a Safe 1.3.0, and no check decides its transaction.
	t.Run("classify", func(t *testing.T) {
		got := decode[map[string]any](t, arbot.Run(t, "classify", "-json", frozen.ID.String()))
		if want := map[string]any{"verdict": nil}; !reflect.DeepEqual(got, want) {
			t.Errorf("classify: got %v, want %v", got, want)
		}
		if got := string(arbot.Run(t, "classify", frozen.ID.String())); got != "unclassified\n" {
			t.Errorf("classify: got output %q, want %q", got, "unclassified\n")
		}
		stderr := arbot.Fail(t, 1, "classify", ethrpc.Hash{31: 1}.String())
		if !bytes.Contains(stderr, []byte("request not found")) {
			t.Errorf("classify: got error %q, want one saying that the request was not found", stderr)
		}

		// A request that `arbot info -json` wrote classifies the same.
		path := filepath.Join(t.TempDir(), "request.json")
		if err := os.WriteFile(path, arbot.Run(t, "info", "-json", frozen.ID.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := string(arbot.Run(t, "classify", "-request-file", path)); got != "unclassified\n" {
			t.Errorf("classify -request-file: got output %q, want %q", got, "unclassified\n")
		}

		// The same transaction by an account that isn't a Safe is insecure.
		var request map[string]any
		if err := json.Unmarshal(arbot.Run(t, "info", "-json", frozen.ID.String()), &request); err != nil {
			t.Fatal(err)
		}
		request["proposal"].(map[string]any)["transaction"].(map[string]any)["safe"] = ethrpc.Address{19: 1}.String()
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		const want = "insecure  R-4.0  account that isn't a Safe of a version that the Charter covers\n"
		if got := string(arbot.Run(t, "classify", "-request-file", path)); got != want {
			t.Errorf("classify -request-file for another account: got output %q, want %q", got, want)
		}
	})
}

// checkBlockBefore checks that block is the last block of the node's chain
// before t.
func checkBlockBefore(t *testing.T, node *e2e.Anvil, chain string, block uint64, at time.Time) {
	t.Helper()
	if !node.Header(block).Time().Before(at) || node.Header(block+1).Time().Before(at) {
		t.Errorf("proposal: got %s block %d at %s, then block %d at %s, want the last block before %s",
			chain, block, node.Header(block).Time(), block+1, node.Header(block+1).Time(), at)
	}
}

// requestInfo is the part of the JSON output of `arbot info` that the test
// checks.
type requestInfo struct {
	Charter string `json:"charter"`
	State   string `json:"state"`
	Terms   struct {
		Bond        *big.Int `json:"bond"`
		SlashAmount *big.Int `json:"slashAmount"`
		DAOFeeShare uint32   `json:"daoFeeShare"`
	} `json:"terms"`
	Proposal struct {
		Consensus         ethrpc.Address  `json:"consensus"`
		Block             uint64          `json:"block"`
		Time              time.Time       `json:"time"`
		EthereumBlock     uint64          `json:"ethereumBlock"`
		EthereumBlockTime time.Time       `json:"ethereumBlockTime"`
		SafeBlock         uint64          `json:"safeBlock"`
		SafeBlockTime     time.Time       `json:"safeBlockTime"`
		TxHash            ethrpc.Hash     `json:"txHash"`
		Epoch             uint64          `json:"epoch"`
		OracleData        ethrpc.Bytes    `json:"oracleData"`
		SafeTxHash        ethrpc.Hash     `json:"safeTxHash"`
		Transaction       json.RawMessage `json:"transaction"`
	} `json:"proposal"`
	Votes         []vote       `json:"votes"`
	DisputedRules []string     `json:"disputedRules"`
	Arbitration   *arbitration `json:"arbitration"`
}

type vote struct {
	Sentinel ethrpc.Address `json:"sentinel"`
	Vote     string         `json:"vote"`
	Reason   string         `json:"reason"`
	Rules    []string       `json:"rules"`
}

type arbitration struct {
	FrozenBlock uint64  `json:"frozenBlock"`
	Deadline    uint64  `json:"deadline"`
	Outcome     string  `json:"outcome"`
	Context     string  `json:"context"`
	Record      *record `json:"record"`
}

type record struct {
	Block  uint64      `json:"block"`
	TxHash ethrpc.Hash `json:"txHash"`
}

// canonical returns the JSON encoding of v decoded into generic values, with
// numbers kept exact, for comparing JSON documents.
func canonical(t *testing.T, v any) any {
	t.Helper()
	data, ok := v.(json.RawMessage)
	if !ok {
		var err error
		if data, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
	return v
}
