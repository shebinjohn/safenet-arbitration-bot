package txservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
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

func TestExecuted(t *testing.T) {
	safe := ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001")
	// pageSize+1 executed transactions, so that the client reads two pages, and one
	// that isn't executed.
	var results []map[string]any
	for nonce := range pageSize + 1 {
		results = append(results, map[string]any{
			"safe": safe, "to": "0xa11ce00000000000000000000000000000000003", "value": "0", "data": nil,
			"operation": 0, "safeTxGas": 0, "baseGas": 0, "gasPrice": "0",
			"gasToken": "0x0000000000000000000000000000000000000000", "refundReceiver": "0x0000000000000000000000000000000000000000",
			"nonce": nonce, "safeTxHash": ethrpc.Hash{31: byte(nonce)}, "transactionHash": ethrpc.Hash{0: 0xbb, 31: byte(nonce)},
			"blockNumber": 100 + nonce, "isExecuted": true,
		})
	}
	results = append(results, map[string]any{"safe": safe, "value": "0", "nonce": 99, "isExecuted": false, "transactionHash": nil, "blockNumber": nil})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The address has its checksum, which the service requires.
		if r.URL.Path != "/api/v1/safes/"+safe.String()+"/multisig-transactions/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("executed") != "true" || q.Get("ordering") != "nonce" || q.Get("limit") != "50" {
			http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		var offset int
		fmt.Sscan(q.Get("offset"), &offset)
		page := results[min(offset, len(results)):min(offset+pageSize, len(results))]
		json.NewEncoder(w).Encode(map[string]any{"count": len(results), "results": page})
	}))
	t.Cleanup(server.Close)

	got, err := NewClient(server.URL, ethrpc.Mainnet).Executed(t.Context(), safe)
	if err != nil {
		t.Fatalf("Executed: %v", err)
	}
	if len(got) != pageSize+1 {
		t.Fatalf("Executed: got %d executions, want %d", len(got), pageSize+1)
	}
	for i, e := range got {
		if e.Transaction.Nonce.Int64() != int64(i) || e.SafeTxHash != (ethrpc.Hash{31: byte(i)}) ||
			e.TxHash != (ethrpc.Hash{0: 0xbb, 31: byte(i)}) || e.Block != uint64(100+i) ||
			e.Transaction.Safe != safe || e.Transaction.ChainID.Int64() != 1 {
			t.Errorf("execution %d: got %+v with transaction %+v", i, e, e.Transaction)
		}
	}

	// A Safe that the service doesn't know has no executions.
	if got, err := NewClient(server.URL, ethrpc.Mainnet).Executed(t.Context(), ethrpc.Address{19: 1}); err != nil || len(got) != 0 {
		t.Errorf("Executed of an unknown Safe: got %v, %v, want no executions", got, err)
	}
}

func TestCreation(t *testing.T) {
	safe := ethrpc.MustParseAddress("0x5afE000000000000000000000000000000000001")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/safes/" + safe.String() + "/creation/":
			w.Write([]byte(`{"created": "2026-07-05T21:24:23Z", "transactionHash": "` + hash.String() + `", "factoryAddress": "0x0000000000000000000000000000000000000001"}`))
		case "/api/v1/safes/" + (ethrpc.Address{19: 2}).String() + "/creation/":
			w.Write([]byte(`{"transactionHash": null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, ethrpc.Mainnet)

	if got, err := client.Creation(t.Context(), safe); err != nil || got != hash {
		t.Errorf("Creation: got %s, %v, want %s", got, err, hash)
	}
	for _, unknown := range []ethrpc.Address{{19: 1}, {19: 2}} {
		if _, err := client.Creation(t.Context(), unknown); !errors.Is(err, ErrNotFound) {
			t.Errorf("Creation of %s: got error %v, want ErrNotFound", unknown, err)
		}
	}
}
