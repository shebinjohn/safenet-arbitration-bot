package txservice

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

var hash = ethrpc.Hash{0: 0xaa, 31: 1}

// serve starts a service that answers requests for hash with status and body,
// and returns a client for it.
func serve(t *testing.T, status int, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The path is versioned, and has a trailing slash like the service wants.
		if r.URL.Path != "/tx-service/eth/api/v1/multisig-transactions/"+hash.String()+"/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewClient(server.URL+"/tx-service/eth", ethrpc.Mainnet)
}

func TestTransaction(t *testing.T) {
	// A response of the service, whose numbers are strings or numbers, with fields
	// that the client doesn't use.
	client := serve(t, http.StatusOK, `{
		"safe": "0x5afE000000000000000000000000000000000001",
		"to": "0xa11ce00000000000000000000000000000000003",
		"value": "1000000000000000000000000000000",
		"data": "0x1234",
		"operation": 1,
		"safeTxGas": 3,
		"baseGas": 4,
		"gasPrice": "5",
		"gasToken": "0x0000000000000000000000000000000000000006",
		"refundReceiver": "0x0000000000000000000000000000000000000007",
		"nonce": 8,
		"executionDate": "2026-09-28T09:24:11Z",
		"dataDecoded": {"method": "transfer"}
	}`)
	got, err := client.Transaction(t.Context(), hash)
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	value, _ := new(big.Int).SetString("1000000000000000000000000000000", 10)
	want := &safenet.SafeTransaction{
		ChainID:        big.NewInt(1),
		Safe:           ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001"),
		To:             ethrpc.MustParseAddress("0xa11ce00000000000000000000000000000000003"),
		Value:          value,
		Data:           []byte{0x12, 0x34},
		Operation:      safenet.OperationDelegateCall,
		SafeTxGas:      big.NewInt(3),
		BaseGas:        big.NewInt(4),
		GasPrice:       big.NewInt(5),
		GasToken:       ethrpc.Address{19: 6},
		RefundReceiver: ethrpc.Address{19: 7},
		Nonce:          big.NewInt(8),
	}
	if g, w := got.Hash(), want.Hash(); g != w {
		t.Errorf("Transaction: got %+v with hash %s, want %+v with hash %s", got, g, want, w)
	}
}

func TestTransactionWithoutData(t *testing.T) {
	client := serve(t, http.StatusOK, `{
		"safe": "0x5afE000000000000000000000000000000000001",
		"to": "0xa11ce00000000000000000000000000000000003",
		"value": "0", "data": null, "operation": 0, "safeTxGas": 0, "baseGas": 0, "gasPrice": "0",
		"gasToken": "0x0000000000000000000000000000000000000000",
		"refundReceiver": "0x0000000000000000000000000000000000000000",
		"nonce": 0
	}`)
	got, err := client.Transaction(t.Context(), hash)
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	if len(got.Data) != 0 {
		t.Errorf("Transaction: got data %x, want none", got.Data)
	}
}

func TestTransactionErrors(t *testing.T) {
	if _, err := serve(t, http.StatusNotFound, `{"detail": "Not found."}`).Transaction(t.Context(), hash); !errors.Is(err, ErrNotFound) {
		t.Errorf("Transaction of an unknown hash: got error %v, want ErrNotFound", err)
	}

	// The body of an error is quoted, as it isn't trusted.
	_, err := serve(t, http.StatusInternalServerError, "\x1b[31munavailable").Transaction(t.Context(), hash)
	if err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "\x1b") {
		t.Errorf("Transaction of an unavailable service: got error %q, want one with the status and no escape sequence", err)
	}

	for name, body := range map[string]string{
		"not JSON":        `<html>`,
		"invalid address": `{"safe": "0x12", "value": "0"}`,
		"negative value":  `{"value": "-1"}`,
		"invalid number":  `{"value": "one"}`,
	} {
		if _, err := serve(t, http.StatusOK, body).Transaction(t.Context(), hash); err == nil {
			t.Errorf("Transaction(%s): expected an error", name)
		}
	}

	big := serve(t, http.StatusOK, `{"data": "0x`+strings.Repeat("00", maxResponseSize)+`"}`)
	if _, err := big.Transaction(t.Context(), hash); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("Transaction with a large response: got error %v, want one about its size", err)
	}
}

func TestDefaultURLs(t *testing.T) {
	for _, chainID := range []uint64{ethrpc.Mainnet, ethrpc.ArbitrumOne, ethrpc.Gnosis} {
		if !strings.HasPrefix(DefaultURLs[chainID], "https://") {
			t.Errorf("DefaultURLs[%d]: got %q, want an HTTPS URL", chainID, DefaultURLs[chainID])
		}
	}
}

// serveExecutions starts a service that lists n executed transactions of safe,
// in pages, with the ones in broken having no transaction hash, and returns a
// client for it.
func serveExecutions(t *testing.T, safe ethrpc.Address, n int, broken ...int) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		offset, err := strconv.Atoi(q.Get("offset"))
		if r.URL.Path != "/tx-service/eth/api/v1/safes/"+safe.String()+"/multisig-transactions/" || err != nil ||
			q.Get("executed") != "true" || q.Get("ordering") != "nonce" || q.Get("limit") != "100" {
			http.NotFound(w, r)
			return
		}
		results := []map[string]any{}
		for i := offset; i < min(n, offset+100); i++ {
			result := map[string]any{
				"safe": safe, "to": safe, "value": "0", "data": nil, "operation": 0, "safeTxGas": 0, "baseGas": 0,
				"gasPrice": "0", "gasToken": ethrpc.Address{}, "refundReceiver": ethrpc.Address{}, "nonce": i,
				"safeTxHash": ethrpc.Hash{0: 0x5a, 31: byte(i)}, "transactionHash": ethrpc.Hash{0: 0x7a, 31: byte(i)},
				"blockNumber": 1000 + i, "isExecuted": true,
			}
			if slices.Contains(broken, i) {
				result["transactionHash"] = nil
			}
			results = append(results, result)
		}
		json.NewEncoder(w).Encode(map[string]any{"count": n, "results": results})
	}))
	t.Cleanup(server.Close)
	return NewClient(server.URL+"/tx-service/eth", ethrpc.Mainnet)
}

func TestExecutions(t *testing.T) {
	safe := ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001")
	// More than a page, so that the client asks for the second one.
	got, err := serveExecutions(t, safe, 150).Executions(t.Context(), safe)
	if err != nil {
		t.Fatalf("Executions: %v", err)
	}
	if len(got) != 150 {
		t.Fatalf("Executions: got %d executions, want 150", len(got))
	}
	for i, x := range got {
		want := Execution{SafeTxHash: ethrpc.Hash{0: 0x5a, 31: byte(i)}, TxHash: ethrpc.Hash{0: 0x7a, 31: byte(i)}, Block: uint64(1000 + i)}
		if x.SafeTxHash != want.SafeTxHash || x.TxHash != want.TxHash || x.Block != want.Block ||
			x.Transaction == nil || x.Transaction.Nonce.Int64() != int64(i) || x.Transaction.Safe != safe || x.Transaction.ChainID.Int64() != 1 {
			t.Errorf("Executions[%d]: got %+v, want %+v with a transaction of nonce %d", i, x, want, i)
		}
	}

	// A full last page is followed by an empty one.
	if got, err := serveExecutions(t, safe, 100).Executions(t.Context(), safe); err != nil || len(got) != 100 {
		t.Errorf("Executions of a full page: got %d executions and error %v, want 100", len(got), err)
	}
	if got, err := serveExecutions(t, safe, 0).Executions(t.Context(), safe); err != nil || len(got) != 0 {
		t.Errorf("Executions of none: got %d executions and error %v, want none", len(got), err)
	}
}

func TestExecutionsErrors(t *testing.T) {
	safe := ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001")
	if _, err := serveExecutions(t, safe, 5, 3).Executions(t.Context(), safe); err == nil || !strings.Contains(err.Error(), "no transaction hash") {
		t.Errorf("Executions with one that has no transaction hash: got error %v, want one about it", err)
	}
	// A service for another Safe has no executions of this one.
	other := ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000002")
	if _, err := serveExecutions(t, other, 5).Executions(t.Context(), safe); !errors.Is(err, ErrNotFound) {
		t.Errorf("Executions of an unknown Safe: got error %v, want ErrNotFound", err)
	}
	_, err := serve(t, http.StatusOK, "").Executions(t.Context(), safe)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Executions from a service without the endpoint: got error %v, want ErrNotFound", err)
	}
}

func TestTransfers(t *testing.T) {
	safe := ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001")
	token := ethrpc.MustParseAddress("0x7000000000000000000000000000000000000001")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if r.URL.Path != "/api/v1/safes/"+safe.String()+"/transfers/" || err != nil || r.URL.Query().Get("limit") != "100" {
			http.NotFound(w, r)
			return
		}
		// More than a page of ERC-20 transfers out, then a native transfer in, and an
		// ERC-721 transfer without a value.
		results := []map[string]any{}
		for i := offset; i < min(102, offset+100); i++ {
			result := map[string]any{
				"type": "ERC20_TRANSFER", "blockNumber": 1000 + i, "transactionHash": ethrpc.Hash{31: byte(i)},
				"tokenAddress": token, "from": safe, "to": ethrpc.Address{19: byte(i)}, "value": strconv.Itoa(i), "tokenId": nil,
			}
			switch i {
			case 100:
				result["type"], result["tokenAddress"], result["from"], result["to"] = "ETHER_TRANSFER", nil, ethrpc.Address{19: 1}, safe
			case 101:
				result["type"], result["value"], result["tokenId"] = "ERC721_TRANSFER", nil, "7"
			}
			results = append(results, result)
		}
		json.NewEncoder(w).Encode(map[string]any{"count": 102, "results": results})
	}))
	t.Cleanup(server.Close)

	got, err := NewClient(server.URL, ethrpc.Mainnet).Transfers(t.Context(), safe)
	if err != nil {
		t.Fatalf("Transfers: %v", err)
	}
	if len(got) != 102 {
		t.Fatalf("Transfers: got %d transfers, want 102", len(got))
	}
	if x := got[5]; x.Type != "ERC20_TRANSFER" || x.Block != 1005 || x.TxHash != (ethrpc.Hash{31: 5}) || x.Token == nil || *x.Token != token ||
		x.From != safe || x.To != (ethrpc.Address{19: 5}) || x.Value.Int64() != 5 {
		t.Errorf("Transfers[5]: got %+v, want an ERC-20 transfer of 5 in block 1005", x)
	}
	if x := got[100]; x.Type != "ETHER_TRANSFER" || x.Token != nil || x.To != safe || x.Value.Int64() != 100 {
		t.Errorf("Transfers[100]: got %+v, want a native transfer of 100 to the Safe", x)
	}
	if x := got[101]; x.Type != "ERC721_TRANSFER" || x.Value != nil {
		t.Errorf("Transfers[101]: got %+v, want an ERC-721 transfer without a value", x)
	}

	if _, err := serve(t, http.StatusOK, "").Transfers(t.Context(), safe); !errors.Is(err, ErrNotFound) {
		t.Errorf("Transfers from a service without the endpoint: got error %v, want ErrNotFound", err)
	}
}
