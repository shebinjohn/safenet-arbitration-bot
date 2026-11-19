package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

const (
	txSuccess  = "0x00000000000000000000000000000000000000000000000000000000000000a1"
	txReverted = "0x00000000000000000000000000000000000000000000000000000000000000a2"
	txLater    = "0x00000000000000000000000000000000000000000000000000000000000000a3"
	txLying    = "0x00000000000000000000000000000000000000000000000000000000000000a4"
	txUnknown  = "0x00000000000000000000000000000000000000000000000000000000000000a5"

	transferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	alice         = "0x00000000000000000000000000000000000000a1"
	bob           = "0x00000000000000000000000000000000000000b2"
)

// receipt returns a receipt of the transaction hash in block, from alice, that
// logs an ERC-20 Transfer of 7 and an event that isn't well known, unless it
// reverted.
func receipt(hash string, block int, status string) map[string]any {
	logs := []any{}
	if status == "0x1" {
		word := func(address string) string { return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(address, "0x") }
		logs = []any{
			map[string]any{
				"address": "0x00000000000000000000000000000000000000c3", "logIndex": "0x0", "transactionHash": hash,
				"topics": []string{transferTopic, word(alice), word(bob)},
				"data":   "0x" + strings.Repeat("0", 63) + "7",
			},
			map[string]any{
				"address": "0x00000000000000000000000000000000000000c4", "logIndex": "0x1", "transactionHash": hash,
				"topics": []string{"0x00000000000000000000000000000000000000000000000000000000000000dd"},
				"data":   "0x" + strings.Repeat("ab", 40),
			},
		}
	}
	return map[string]any{
		"transactionHash": hash, "blockNumber": fmt.Sprintf("0x%x", block), "status": status,
		"from": alice, "to": bob, "logs": logs,
	}
}

// receiptNode starts an Ethereum Mainnet node with the receipts, and returns
// the path of a configuration file for it.
func receiptNode(t *testing.T, receipts map[string]any) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64   `json:"id"`
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "eth_chainId":
			res["result"] = "0x1"
		case "eth_getTransactionReceipt":
			res["result"] = receipts[req.Params[0]]
		case "eth_getBlockByNumber":
			var number ethrpc.BlockNumber
			number.UnmarshalText([]byte(req.Params[0]))
			res["result"] = fakeHeader(number)
		default:
			res["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"rpcs": {"1": %q}}`, server.URL), 0o644); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestEvents(t *testing.T) {
	config := receiptNode(t, map[string]any{
		txSuccess:  receipt(txSuccess, 100, "0x1"),
		txReverted: receipt(txReverted, 99, "0x0"),
		txLater:    receipt(txLater, 101, "0x1"),
		// A node that answers with another transaction's receipt.
		txLying: receipt(txSuccess, 100, "0x1"),
	})
	request := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(request, []byte(`{"proposal": {"safeBlock": 100, "transaction": {"chainId": 1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(t.Context(), append([]string{"arbot", "-config", config, "events"}, args...), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	t.Run("json", func(t *testing.T) {
		code, stdout, stderr := run("-json", "-request-file", request, txSuccess)
		if code != 0 {
			t.Fatalf("events: status %d, stderr %q", code, stderr)
		}
		var got struct {
			ChainID, SafeBlock, Block uint64
			BlockTime                 time.Time
			TxHash, Status            string
			Events                    []struct {
				LogIndex uint64
				Emitter  string
				Data     string
				Event    *struct {
					Name, Signature string
					Args            []struct {
						Name, Type string
						Value      any
					}
				}
			}
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("decoding %s: %v", stdout, err)
		}
		if got.ChainID != 1 || got.SafeBlock != 100 || got.Block != 100 || !got.BlockTime.Equal(fakeBlockTime(100)) || got.Status != "success" ||
			!strings.EqualFold(got.TxHash, txSuccess) || len(got.Events) != 2 {
			t.Fatalf("events: got %+v", got)
		}
		transfer := got.Events[0].Event
		if transfer == nil || transfer.Name != "Transfer" || transfer.Signature != "Transfer(address,address,uint256)" || len(transfer.Args) != 3 ||
			transfer.Args[2].Name != "value" || transfer.Args[2].Value != float64(7) || !strings.EqualFold(transfer.Args[1].Value.(string), bob) {
			t.Errorf("first event: got %+v, want a Transfer of 7 to %s", transfer, bob)
		}
		if got.Events[1].Event != nil || got.Events[1].LogIndex != 1 {
			t.Errorf("second event: got %+v, want log 1 that isn't a well-known event", got.Events[1])
		}
		if !strings.Contains(stdout, `"event": null`) {
			t.Errorf("events: output has no null event:\n%s", stdout)
		}
	})

	t.Run("text", func(t *testing.T) {
		code, stdout, stderr := run("-request-file", request, txSuccess)
		if code != 0 {
			t.Fatalf("events: status %d, stderr %q", code, stderr)
		}
		for _, want := range []string{
			`(?m)^Transaction ` + txSuccess + ` on chain 1, in block 100 at ` + fakeBlockTime(100).Format(time.RFC3339) + ` \(success\)$`,
			`(?m)^Log 0, emitted by 0x[0-9a-fA-F]{38}[cC]3$`,
			`(?m)^  Event  Transfer\(address,address,uint256\)$`,
			`(?m)^    value = 7$`,
			`(?m)^  Event  unknown$`,
			// Long data is shortened.
			`(?m)^  Data    0x(ab){36}\.\.\. \(40 bytes\)$`,
		} {
			if !regexp.MustCompile(want).MatchString(stdout) {
				t.Errorf("events: output has no line matching %q:\n%s", want, stdout)
			}
		}
	})

	t.Run("reverted", func(t *testing.T) {
		code, stdout, stderr := run("-request-file", request, txReverted)
		if code != 0 || !strings.Contains(stdout, "(reverted)") || !strings.Contains(stdout, "no events") {
			t.Errorf("events: got status %d, stdout %q, stderr %q, want a reverted transaction without events", code, stdout, stderr)
		}
	})

	failures := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"after the Safe block", []string{"-request-file", request, txLater}, 1, "after the request's Safe block 100"},
		{"unknown transaction", []string{"-request-file", request, txUnknown}, 1, "not found"},
		{"receipt of another transaction", []string{"-request-file", request, txLying}, 1, "not " + txLying},
		{"no request file", []string{txSuccess}, 2, "-request-file is required"},
		{"no hash", []string{"-request-file", request}, 2, "Usage: arbot events"},
		{"invalid hash", []string{"-request-file", request, "0x1234"}, 2, `transaction hash "0x1234"`},
	}
	for _, test := range failures {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := run(test.args...)
			if code != test.code || stdout != "" || !strings.Contains(stderr, test.stderr) {
				t.Errorf("events %v: got status %d, stdout %q, stderr %q; want %d, no output, and stderr containing %q",
					test.args, code, stdout, stderr, test.code, test.stderr)
			}
		})
	}
}

// TestEventsBlockTimeTransient checks that a block time that the node fails to
// serve fails the command, rather than being left out.
func TestEventsBlockTimeTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64   `json:"id"`
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "eth_chainId":
			res["result"] = "0x1"
		case "eth_getTransactionReceipt":
			res["result"] = receipt(txSuccess, 100, "0x1")
		default:
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(server.Close)
	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"rpcs": {"1": %q}}`, server.URL), 0o644); err != nil {
		t.Fatal(err)
	}
	request := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(request, []byte(`{"proposal": {"safeBlock": 100, "transaction": {"chainId": 1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), []string{"arbot", "-config", config, "events", "-json", "-request-file", request, txSuccess}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "reading the time of block 100") || !strings.Contains(stderr.String(), "run the command again later") {
		t.Errorf("events: got status %d, stderr %q; want 1, and that the block time failed transiently", code, stderr.String())
	}
}
