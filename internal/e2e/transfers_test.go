package e2e_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// transfersOutput is the JSON output of `arbot transfers`.
type transfersOutput struct {
	Safe      ethrpc.Address  `json:"safe"`
	ChainID   uint64          `json:"chainId"`
	FromBlock uint64          `json:"fromBlock"`
	ToBlock   uint64          `json:"toBlock"`
	Direction string          `json:"direction"`
	From      *ethrpc.Address `json:"from"`
	To        *ethrpc.Address `json:"to"`
	Transfers []transferJSON  `json:"transfers"`
}

type transferJSON struct {
	Type     string          `json:"type"`
	Block    uint64          `json:"block"`
	TxHash   ethrpc.Hash     `json:"txHash"`
	LogIndex uint64          `json:"logIndex"`
	Token    *ethrpc.Address `json:"token"`
	From     ethrpc.Address  `json:"from"`
	To       ethrpc.Address  `json:"to"`
	Value    *big.Int        `json:"value"`
}

// TestTransfers runs `arbot transfers` against the fee token and the native
// currency on the Safe's chain, with transfers out of the Safe and into it,
// before and after a proposal, and between other accounts.
func TestTransfers(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	r := n.Propose(nil)
	if r.Transaction.ChainID.Uint64() != ethrpc.Mainnet {
		t.Fatalf("Safe transaction is for chain %s, want Ethereum Mainnet", r.Transaction.ChainID)
	}
	safe, token := r.Transaction.Safe, n.Artifacts.FeeToken
	alice, bob := ethrpc.Address{0: 0xa1, 19: 1}, ethrpc.Address{0: 0xb0, 19: 2}
	ether := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e18)) }

	// The Safe's transfers before the proposal, and one from another account. The
	// Mainnet node's blocks are earlier than the proposal until it mines up to it.
	n.InstallFeeToken(n.Mainnet)
	first := n.TransferFeeToken(n.Mainnet, safe, alice, ether(1))
	second := n.TransferFeeToken(n.Mainnet, safe, bob, ether(2))
	third := n.TransferFeeToken(n.Mainnet, safe, alice, ether(3))
	n.TransferFeeToken(n.Mainnet, n.Sponsor, alice, ether(5))

	// Transfers into the Safe before the proposal: the fee token from alice, and
	// ether from bob, which the Safe logs as received.
	tokenIn := n.TransferFeeToken(n.Mainnet, alice, safe, ether(6))
	n.Mainnet.SetBalance(bob, ether(8))
	etherIn := n.Mainnet.Receipt(n.Mainnet.Send(bob, safe, nil, ether(7)))

	// Transfers after the proposal, which the evidence must not include.
	n.Mainnet.MineUntil(n.Header(r.Block).Time())
	later := n.TransferFeeToken(n.Mainnet, safe, alice, ether(4))
	n.TransferFeeToken(n.Mainnet, alice, safe, ether(9))

	arbot := n.Arbot()
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, arbot.Run(t, "info", "-json", r.ID.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	safeBlock := decode[requestInfo](t, arbot.Run(t, "info", "-json", r.ID.String())).Proposal.SafeBlock
	transfer := func(receipt *e2e.Receipt, kind string, token *ethrpc.Address, from, to ethrpc.Address, value *big.Int) transferJSON {
		return transferJSON{
			Type: kind, Block: uint64(receipt.BlockNumber), TxHash: receipt.TransactionHash,
			LogIndex: uint64(receipt.Logs[len(receipt.Logs)-1].LogIndex), Token: token, From: from, To: to, Value: value,
		}
	}
	all := []transferJSON{
		transfer(first, "erc20", &token, safe, alice, ether(1)),
		transfer(second, "erc20", &token, safe, bob, ether(2)),
		transfer(third, "erc20", &token, safe, alice, ether(3)),
	}
	in := []transferJSON{
		transfer(tokenIn, "erc20", &token, alice, safe, ether(6)),
		transfer(etherIn, "native", nil, bob, safe, ether(7)),
	}
	if safeBlock < in[1].Block {
		t.Fatalf("Safe block %d is before the last transfer, in block %d", safeBlock, in[1].Block)
	}

	someone := ethrpc.Address{19: 9}
	tests := []struct {
		name      string
		args      []string
		fromBlock uint64
		direction string
		from, to  *ethrpc.Address
		want      []transferJSON
	}{
		{"default range", nil, 0, "out", nil, nil, all},
		{"recipient", []string{"-to", bob.String()}, 0, "out", nil, &bob, all[1:2]},
		{"recipient without transfers", []string{"-to", someone.String()}, 0, "out", nil, &someone, []transferJSON{}},
		{"bounded range", []string{"-blocks", fmt.Sprint(safeBlock - all[2].Block + 1)}, all[2].Block, "out", nil, nil, all[2:]},
		{"only the Safe block", []string{"-blocks", "1"}, safeBlock, "out", nil, nil, []transferJSON{}},
		{"into the Safe", []string{"-in"}, 0, "in", nil, nil, in},
		{"into the Safe from a token sender", []string{"-in", "-from", alice.String()}, 0, "in", &alice, nil, in[:1]},
		{"into the Safe from an ether sender", []string{"-in", "-from", bob.String()}, 0, "in", &bob, nil, in[1:]},
		{"into the Safe from an account without transfers", []string{"-in", "-from", someone.String()}, 0, "in", &someone, nil, []transferJSON{}},
		{"into the Safe in a bounded range", []string{"-in", "-blocks", fmt.Sprint(safeBlock - in[1].Block + 1)}, in[1].Block, "in", nil, nil, in[1:]},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"transfers", "-json", "-request-file", path}, test.args...)
			got := decode[transfersOutput](t, arbot.Run(t, args...))
			want := transfersOutput{
				Safe: safe, ChainID: ethrpc.Mainnet, FromBlock: test.fromBlock, ToBlock: safeBlock, Direction: test.direction,
				From: test.from, To: test.to, Transfers: test.want,
			}
			if !reflect.DeepEqual(got, want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				t.Errorf("transfers %v:\ngot  %s\nwant %s", test.args, g, w)
			}
		})
	}

	t.Run("all", func(t *testing.T) {
		// A service that lists the first two transfers out of the Safe, one that the
		// logs don't have, and one after the proposal, and the transfers into it.
		type listing struct {
			kind     string
			receipt  *e2e.Receipt
			token    *ethrpc.Address
			from, to ethrpc.Address
			value    *big.Int
		}
		listings := []listing{
			{"ERC20_TRANSFER", first, &token, safe, alice, ether(1)},
			{"ERC20_TRANSFER", first, &token, safe, bob, ether(1)},
			{"ERC20_TRANSFER", second, &token, safe, bob, ether(2)},
			{"ERC20_TRANSFER", later, &token, safe, alice, ether(4)},
			{"ERC20_TRANSFER", tokenIn, &token, alice, safe, ether(6)},
			{"ETHER_TRANSFER", etherIn, nil, bob, safe, ether(7)},
		}
		service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/safes/"+safe.String()+"/transfers/" {
				http.NotFound(w, r)
				return
			}
			results := []map[string]any{}
			for _, l := range listings {
				results = append(results, map[string]any{
					"type": l.kind, "blockNumber": uint64(l.receipt.BlockNumber), "transactionHash": l.receipt.TransactionHash,
					"tokenAddress": l.token, "from": l.from, "to": l.to, "value": l.value.String(),
				})
			}
			json.NewEncoder(w).Encode(map[string]any{"count": len(results), "results": results})
		}))
		t.Cleanup(service.Close)
		withService := arbot.WithTxService(t, ethrpc.Mainnet, service.URL)

		type serviceJSON struct {
			Listed    int `json:"listed"`
			Blocks    int `json:"blocks"`
			Unmatched int `json:"unmatched"`
		}
		type allOutput struct {
			transfersOutput
			Service *serviceJSON `json:"service"`
		}
		for _, test := range []struct {
			args    []string
			want    []transferJSON
			service serviceJSON
		}{
			{nil, all[:2], serviceJSON{Listed: 3, Blocks: 2, Unmatched: 1}},
			{[]string{"-to", bob.String()}, all[1:2], serviceJSON{Listed: 2, Blocks: 2, Unmatched: 1}},
			{[]string{"-in"}, in, serviceJSON{Listed: 2, Blocks: 2}},
		} {
			args := append([]string{"transfers", "-json", "-all", "-request-file", path}, test.args...)
			got := decode[allOutput](t, withService.Run(t, args...))
			if got.FromBlock != 0 || got.ToBlock != safeBlock || !reflect.DeepEqual(got.Transfers, test.want) ||
				got.Service == nil || *got.Service != test.service {
				g, _ := json.Marshal(got)
				t.Errorf("transfers %v: got %s, want blocks 0 to %d, the transfers %+v, and %+v", test.args, g, safeBlock, test.want, test.service)
			}
		}
		// The transfer that the logs don't have is a warning.
		stderr := withService.Fail(t, 0, "transfers", "-all", "-request-file", path)
		if want := "lists a transfer of " + ether(1).String() + " from " + safe.String() + " to " + bob.String(); !strings.Contains(string(stderr), want) {
			t.Errorf("transfers -all: got stderr %q, want a warning containing %q", stderr, want)
		}
		text := withService.Run(t, "transfers", "-all", "-request-file", path)
		for _, want := range []string{
			fmt.Sprintf(`(?m)^ERC-20 transfers from %s on chain 1, up to block %d, in the 2 blocks with transfers that the Safe Transaction Service lists$`, safe, safeBlock),
			`(?m)^The service lists 3 such transfers, of which 1 aren't in the logs of their blocks\.$`,
		} {
			if !regexp.MustCompile(want).Match(text) {
				t.Errorf("transfers -all: output has no line matching %q:\n%s", want, text)
			}
		}
		if out := withService.Run(t, "transfers", "-json", "-request-file", path); strings.Contains(string(out), `"service"`) {
			t.Errorf("transfers without -all: output has a service")
		}
		if stderr := arbot.Fail(t, 1, "transfers", "-all", "-request-file", path); !strings.Contains(string(stderr), "-all needs the Safe Transaction Service of chain 1") {
			t.Errorf("transfers -all without a service: got %q, want an error about the service", stderr)
		}
	})

	t.Run("account", func(t *testing.T) {
		type accountOutput struct {
			ChainID   uint64          `json:"chainId"`
			SafeBlock uint64          `json:"safeBlock"`
			Safe      ethrpc.Address  `json:"safe"`
			Address   ethrpc.Address  `json:"address"`
			CodeSize  int             `json:"codeSize"`
			Self      bool            `json:"self"`
			Contract  json.RawMessage `json:"contract"`
			Balance   *big.Int        `json:"balance"`
			Nonce     uint64          `json:"nonce"`
			ERC20     *struct {
				Symbol      string   `json:"symbol"`
				Decimals    uint8    `json:"decimals"`
				SafeBalance *big.Int `json:"safeBalance"`
			} `json:"erc20"`
		}
		// The fee token, of which the Safe has what alice sent it before the proposal,
		// since it sent out all that it wrapped.
		got := decode[accountOutput](t, arbot.Run(t, "account", "-json", "-request-file", path, token.String()))
		if got.ChainID != ethrpc.Mainnet || got.SafeBlock != safeBlock || got.Safe != safe || got.Address != token ||
			got.CodeSize == 0 || got.Self || string(got.Contract) != "null" || got.ERC20 == nil ||
			got.ERC20.Symbol == "" || got.ERC20.Decimals != 18 || got.ERC20.SafeBalance.Cmp(ether(6)) != 0 {
			t.Errorf("account of the fee token: got %+v %+v, want a contract that is an ERC-20 token with 18 decimals, of which the Safe has %s", got, got.ERC20, ether(6))
		}
		// Alice sent two transactions before the proposal, to wrap ether and send it.
		if got := decode[accountOutput](t, arbot.Run(t, "account", "-json", "-request-file", path, alice.String())); got.CodeSize != 0 || got.Nonce != 2 || got.ERC20 != nil {
			t.Errorf("account of alice: got %+v, want no code, nonce 2, and no token", got)
		}
		if got := decode[accountOutput](t, arbot.Run(t, "account", "-json", "-request-file", path, someone.String())); got.CodeSize != 0 || got.Nonce != 0 || got.Balance.Sign() != 0 || got.ERC20 != nil {
			t.Errorf("account of an unused account: got %+v, want nothing", got)
		}
		if got := decode[accountOutput](t, arbot.Run(t, "account", "-json", "-request-file", path, safe.String())); !got.Self || got.CodeSize == 0 {
			t.Errorf("account of the Safe: got %+v, want the Safe itself", got)
		}

		text := arbot.Run(t, "account", "-request-file", path, token.String())
		for _, want := range []string{
			fmt.Sprintf(`(?m)^Account %s on chain 1, as of block %d$`, token, safeBlock),
			`(?m)^  Code +\d+ bytes, hash 0x[0-9a-f]{64}$`,
			`(?m)^  Decimals +18$`,
			`(?m)^  Safe balance +` + ether(6).String() + `$`,
		} {
			if !regexp.MustCompile(want).Match(text) {
				t.Errorf("account: output has no line matching %q:\n%s", want, text)
			}
		}
	})

	t.Run("text", func(t *testing.T) {
		text := arbot.Run(t, "transfers", "-request-file", path)
		header := fmt.Sprintf(`(?m)^ERC-20 transfers from %s on chain 1, in blocks 0 to %d$`, safe, safeBlock)
		row := fmt.Sprintf(`(?m)^%d +%s +%s +%s +%s$`, all[1].Block, all[1].TxHash, token, bob, ether(2))
		for _, want := range []string{header, row} {
			if !regexp.MustCompile(want).Match(text) {
				t.Errorf("transfers: output has no line matching %q:\n%s", want, text)
			}
		}
		if got := arbot.Run(t, "transfers", "-request-file", path, "-blocks", "1"); !regexp.MustCompile(`(?m)^none$`).Match(got) {
			t.Errorf("transfers -blocks 1: got output %q, want one saying none", got)
		}

		text = arbot.Run(t, "transfers", "-in", "-request-file", path)
		header = fmt.Sprintf(`(?m)^Transfers to %s on chain 1, in blocks 0 to %d$`, safe, safeBlock)
		tokenRow := fmt.Sprintf(`(?m)^%d +%s +erc20 +%s +%s +%s$`, in[0].Block, in[0].TxHash, token, alice, ether(6))
		etherRow := fmt.Sprintf(`(?m)^%d +%s +native +- +%s +%s$`, in[1].Block, in[1].TxHash, bob, ether(7))
		for _, want := range []string{header, tokenRow, etherRow} {
			if !regexp.MustCompile(want).Match(text) {
				t.Errorf("transfers -in: output has no line matching %q:\n%s", want, text)
			}
		}
	})
}
