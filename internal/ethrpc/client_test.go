package ethrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// testChainID is the chain that test nodes started by serve report.
const testChainID = 1

// serve starts a JSON-RPC server for testChainID that checks each request
// against want and answers with the JSON response object reply, with the
// request's ID filled in. It answers the client's eth_chainId check itself.
func serve(t *testing.T, want, reply string) *Client {
	t.Helper()
	wantRequest := canonical(t, want)
	response := decode(t, reply)

	// The handler runs on the server's goroutine, where t.Fatal must not be called,
	// so it only reports errors.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type: got %q, want application/json", ct)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id := req["id"]
		if req["method"] == "eth_chainId" {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": "0x1"})
			return
		}
		delete(req, "id")
		if got, _ := json.Marshal(req); string(got) != wantRequest {
			t.Errorf("request:\n got %s\nwant %s", got, wantRequest)
		}

		response["jsonrpc"], response["id"] = "2.0", id
		json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(t.Context(), testChainID, server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// canonical re-encodes the JSON object s as compact JSON with sorted keys, the
// same form that encoding a decoded request produces.
func canonical(t *testing.T, s string) string {
	t.Helper()
	data, err := json.Marshal(decode(t, s))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCall(t *testing.T) {
	client := serve(t,
		`{
			"jsonrpc": "2.0",
			"method": "eth_call",
			"params": [
				{"to": "0x00000000000000000000000000000000000000aa", "data": "0x12345678"},
				"0x10"
			]
		}`,
		`{"result": "0xcafe"}`,
	)

	result, err := client.Call(t.Context(), CallRequest{
		To:   Address{19: 0xaa},
		Data: Bytes{0x12, 0x34, 0x56, 0x78},
	}, 16)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !bytes.Equal(result, []byte{0xca, 0xfe}) {
		t.Errorf("result: got %s, want 0xcafe", result)
	}
}

func TestCallIncludesFrom(t *testing.T) {
	client := serve(t,
		`{
			"jsonrpc": "2.0",
			"method": "eth_call",
			"params": [
				{
					"from": "0x00000000000000000000000000000000000000bb",
					"to": "0x00000000000000000000000000000000000000aa"
				},
				"0x0"
			]
		}`,
		`{"result": "0x"}`,
	)

	result, err := client.Call(t.Context(), CallRequest{From: Address{19: 0xbb}, To: Address{19: 0xaa}}, 0)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("result: got %s, want 0x", result)
	}
}

func TestCallRPCError(t *testing.T) {
	client := serve(t,
		`{
			"jsonrpc": "2.0",
			"method": "eth_call",
			"params": [{"to": "0x0000000000000000000000000000000000000000"}, "0x1"]
		}`,
		`{"error": {"code": 3, "message": "execution reverted", "data": "0x08c379a0"}}`,
	)

	_, err := client.Call(t.Context(), CallRequest{}, 1)
	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("Call: got %v, want an *Error", err)
	}
	if rpcErr.Code != 3 || rpcErr.Message != "execution reverted" || string(rpcErr.Data) != `"0x08c379a0"` {
		t.Errorf("error: got %+v", rpcErr)
	}
}

func TestBlockNumber(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_blockNumber", "params": []}`,
		`{"result": "0x2625a00"}`,
	)

	number, err := client.BlockNumber(t.Context())
	if err != nil {
		t.Fatalf("BlockNumber: %v", err)
	}
	if number != 40_000_000 {
		t.Errorf("BlockNumber: got %d, want 40000000", number)
	}
}

func TestGetLogs(t *testing.T) {
	client := serve(t,
		`{
			"jsonrpc": "2.0",
			"method": "eth_getLogs",
			"params": [{
				"fromBlock": "0x10",
				"toBlock": "0x20",
				"address": ["0x00000000000000000000000000000000000000aa"],
				"topics": [
					["0x0000000000000000000000000000000000000000000000000000000000000001"],
					null,
					[
						"0x0000000000000000000000000000000000000000000000000000000000000002",
						"0x0000000000000000000000000000000000000000000000000000000000000003"
					]
				]
			}]
		}`,
		`{"result": [{
			"address": "0x00000000000000000000000000000000000000aa",
			"topics": ["0x0000000000000000000000000000000000000000000000000000000000000001"],
			"data": "0xcafe",
			"blockNumber": "0x11",
			"blockHash": "0x00000000000000000000000000000000000000000000000000000000000000bb",
			"transactionHash": "0x00000000000000000000000000000000000000000000000000000000000000cc",
			"transactionIndex": "0x0",
			"logIndex": "0x2",
			"removed": false
		}]}`,
	)

	logs, err := client.GetLogs(t.Context(), LogFilter{
		FromBlock: 16,
		ToBlock:   32,
		Addresses: []Address{{19: 0xaa}},
		Topics:    [][]Hash{{{31: 1}}, nil, {{31: 2}, {31: 3}}},
	})
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	want := Log{
		Address:         Address{19: 0xaa},
		Topics:          []Hash{{31: 1}},
		Data:            Bytes{0xca, 0xfe},
		BlockNumber:     17,
		BlockHash:       Hash{31: 0xbb},
		TransactionHash: Hash{31: 0xcc},
		LogIndex:        2,
	}
	if len(logs) != 1 || !reflect.DeepEqual(logs[0], want) {
		t.Errorf("GetLogs: got %+v, want [%+v]", logs, want)
	}
}

func TestRawRequest(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "anvil_mine", "params": ["0x2"]}`,
		`{"result": null}`,
	)
	if err := client.RawRequest(t.Context(), nil, "anvil_mine", Quantity(2)); err != nil {
		t.Fatalf("RawRequest: %v", err)
	}
}

func TestGetCode(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_getCode", "params": ["0x00000000000000000000000000000000000000aa", "0x10"]}`,
		`{"result": "0x6080"}`,
	)

	code, err := client.GetCode(t.Context(), Address{19: 0xaa}, 16)
	if err != nil {
		t.Fatalf("GetCode: %v", err)
	}
	if !bytes.Equal(code, []byte{0x60, 0x80}) {
		t.Errorf("GetCode: got %s, want 0x6080", code)
	}
}

func TestGetStorageAt(t *testing.T) {
	client := serve(t,
		`{
			"jsonrpc": "2.0",
			"method": "eth_getStorageAt",
			"params": [
				"0x00000000000000000000000000000000000000aa",
				"0x0000000000000000000000000000000000000000000000000000000000000001",
				"0x10"
			]
		}`,
		`{"result": "0x0000000000000000000000000000000000000000000000000000000000000abc"}`,
	)

	value, err := client.GetStorageAt(t.Context(), Address{19: 0xaa}, Hash{31: 1}, 16)
	if err != nil {
		t.Fatalf("GetStorageAt: %v", err)
	}
	if want := (Hash{30: 0x0a, 31: 0xbc}); value != want {
		t.Errorf("GetStorageAt: got %s, want %s", value, want)
	}
}

func TestGetBlockByNumber(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_getBlockByNumber", "params": ["0x2625a00", false]}`,
		`{"result": {
			"number": "0x2625a00",
			"hash": "0x00000000000000000000000000000000000000000000000000000000000000bb",
			"timestamp": "0x68d3a000",
			"transactions": []
		}}`,
	)

	block, err := client.GetBlockByNumber(t.Context(), 40_000_000)
	if err != nil {
		t.Fatalf("GetBlockByNumber: %v", err)
	}
	want := Block{Number: 40_000_000, Hash: Hash{31: 0xbb}, Timestamp: 0x68d3a000}
	if block != want {
		t.Errorf("GetBlockByNumber: got %+v, want %+v", block, want)
	}
}

func TestGetBlockByNumberNotFound(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_getBlockByNumber", "params": ["0x2625a00", false]}`,
		`{"result": null}`,
	)
	if _, err := client.GetBlockByNumber(t.Context(), 40_000_000); err == nil {
		t.Fatal("GetBlockByNumber: expected an error")
	}
}

func TestGetTransactionByHash(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_getTransactionByHash", "params": ["0x00000000000000000000000000000000000000000000000000000000000000cc"]}`,
		`{"result": {
			"hash": "0x00000000000000000000000000000000000000000000000000000000000000cc",
			"from": "0x00000000000000000000000000000000000000f1",
			"to": "0x00000000000000000000000000000000000000f2",
			"input": "0x1234",
			"nonce": "0x1"
		}}`,
	)

	hash := Hash{31: 0xcc}
	tx, err := client.GetTransactionByHash(t.Context(), hash)
	if err != nil {
		t.Fatalf("GetTransactionByHash: %v", err)
	}
	to := Address{19: 0xf2}
	want := Transaction{Hash: hash, From: Address{19: 0xf1}, To: &to, Input: Bytes{0x12, 0x34}}
	if !reflect.DeepEqual(tx, want) {
		t.Errorf("GetTransactionByHash: got %+v, want %+v", tx, want)
	}
}

func TestGetTransactionByHashNotFound(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_getTransactionByHash", "params": ["0x00000000000000000000000000000000000000000000000000000000000000cc"]}`,
		`{"result": null}`,
	)
	if _, err := client.GetTransactionByHash(t.Context(), Hash{31: 0xcc}); err == nil {
		t.Fatal("GetTransactionByHash: expected an error")
	}
}

func TestGetTransactionReceipt(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_getTransactionReceipt", "params": ["0x00000000000000000000000000000000000000000000000000000000000000cc"]}`,
		`{"result": {
			"transactionHash": "0x00000000000000000000000000000000000000000000000000000000000000cc",
			"blockNumber": "0x2625a00",
			"status": "0x1",
			"from": "0x00000000000000000000000000000000000000f1",
			"to": null,
			"logs": [{
				"address": "0x00000000000000000000000000000000000000aa",
				"topics": ["0x00000000000000000000000000000000000000000000000000000000000000dd"],
				"data": "0x01",
				"blockNumber": "0x2625a00",
				"blockHash": "0x00000000000000000000000000000000000000000000000000000000000000bb",
				"transactionHash": "0x00000000000000000000000000000000000000000000000000000000000000cc",
				"logIndex": "0x3"
			}],
			"gasUsed": "0x5208"
		}}`,
	)

	hash := Hash{31: 0xcc}
	got, err := client.GetTransactionReceipt(t.Context(), hash)
	if err != nil {
		t.Fatalf("GetTransactionReceipt: %v", err)
	}
	want := Receipt{
		TransactionHash: hash, BlockNumber: 40_000_000, Status: 1, From: Address{19: 0xf1},
		Logs: []Log{{
			Address: Address{19: 0xaa}, Topics: []Hash{{31: 0xdd}}, Data: Bytes{1}, BlockNumber: 40_000_000,
			BlockHash: Hash{31: 0xbb}, TransactionHash: hash, LogIndex: 3,
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetTransactionReceipt: got %+v, want %+v", got, want)
	}
}

func TestGetTransactionReceiptNotFound(t *testing.T) {
	client := serve(t,
		`{"jsonrpc": "2.0", "method": "eth_getTransactionReceipt", "params": ["0x00000000000000000000000000000000000000000000000000000000000000cc"]}`,
		`{"result": null}`,
	)
	if _, err := client.GetTransactionReceipt(t.Context(), Hash{31: 0xcc}); err == nil {
		t.Fatal("GetTransactionReceipt: expected an error")
	}
}

// node starts a JSON-RPC server that reports chainID to eth_chainId, and passes
// every other request to handle. It returns the server's URL and a count of the
// eth_chainId requests it has served.
func node(t *testing.T, chainID string, handle func(w http.ResponseWriter, id uint64)) (string, *atomic.Int64) {
	t.Helper()
	var chainIDRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Method == "eth_chainId" {
			chainIDRequests.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": chainID})
			return
		}
		handle(w, req.ID)
	}))
	t.Cleanup(server.Close)
	return server.URL, &chainIDRequests
}

// connect returns a client for the node at url, expecting testChainID.
func connect(t *testing.T, url string) *Client {
	t.Helper()
	client, err := NewClient(t.Context(), testChainID, url)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestRequestHTTPError(t *testing.T) {
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	})
	if _, err := connect(t, url).Call(t.Context(), CallRequest{}, 1); err == nil {
		t.Fatal("Call: expected an error")
	}
}

func TestRequestHTTPErrorWithRPCError(t *testing.T) {
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      0,
			"error":   map[string]any{"code": -32602, "message": "block range too large"},
		})
	})
	_, err := connect(t, url).GetLogs(t.Context(), LogFilter{})
	rpcErr, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("GetLogs: got %v, want an *Error", err)
	}
	if rpcErr.Code != -32602 || rpcErr.Message != "block range too large" {
		t.Errorf("error: got %+v", rpcErr)
	}
}

func TestRequestMismatchedID(t *testing.T) {
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id + 1000, "result": "0x"})
	})
	if _, err := connect(t, url).Call(t.Context(), CallRequest{}, 1); err == nil {
		t.Fatal("Call: expected an error")
	}
}

func TestNewClientVerifiesChainID(t *testing.T) {
	var requests atomic.Int64
	url, chainIDRequests := node(t, "0x64", func(w http.ResponseWriter, id uint64) {
		requests.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": "0x2a"})
	})

	client, err := NewClient(t.Context(), 100, url)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.ChainID() != 100 {
		t.Errorf("ChainID: got %d, want 100", client.ChainID())
	}
	for range 3 {
		if _, err := client.BlockNumber(t.Context()); err != nil {
			t.Fatalf("BlockNumber: %v", err)
		}
	}
	if n := chainIDRequests.Load(); n != 1 {
		t.Errorf("eth_chainId requests: got %d, want 1", n)
	}
	if n := requests.Load(); n != 3 {
		t.Errorf("eth_blockNumber requests: got %d, want 3", n)
	}
}

func TestNewClientRejectsWrongChain(t *testing.T) {
	url, _ := node(t, "0x64", func(w http.ResponseWriter, id uint64) {
		t.Error("unexpected request to a node on the wrong chain")
	})
	if _, err := NewClient(t.Context(), 1, url); err == nil {
		t.Fatal("NewClient: expected an error")
	}
}

func TestNewClientConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(t.Context(), testChainID, server.URL); err == nil {
		t.Fatal("NewClient: expected an error")
	}
}

func TestTransient(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", fmt.Errorf("eth_getTransactionReceipt: transaction 0x01: %w", ErrNotFound), false},
		{"JSON-RPC error", fmt.Errorf("eth_call: %w", &Error{Code: -32000, Message: "execution reverted"}), false},
		{"unrelated error", errors.New("reading request file: no such file"), false},
		{"rate limit in an HTTP 429 body", fmt.Errorf("eth_getTransactionReceipt: %w", &HTTPError{Status: "429 Too Many Requests", StatusCode: 429, Err: &Error{Code: -32016, Message: "over rate limit"}}), true},
		{"rate limit with HTTP 200", fmt.Errorf("eth_call: %w", &Error{Code: -32005, Message: "Rate limit exceeded"}), true},
		{"server error", fmt.Errorf("eth_call: %w", &HTTPError{Status: "500 Internal Server Error", StatusCode: 500, Body: "error"}), true},
		{"refused method", fmt.Errorf("eth_call: %w", &HTTPError{Status: "403 Forbidden", StatusCode: 403, Err: &Error{Code: -32601, Message: "rpc method is not whitelisted"}}), false},
		{"connection failure", fmt.Errorf("eth_call: %w", &url.Error{Op: "Post", URL: "https://node", Err: errors.New("connection refused")}), true},
		{"canceled", fmt.Errorf("eth_call: %w", context.Canceled), false},
	} {
		if got := Transient(test.err); got != test.want {
			t.Errorf("%s: Transient(%v) = %v, want %v", test.name, test.err, got, test.want)
		}
	}
}

func TestRequestHTTPErrorType(t *testing.T) {
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	})
	_, err := connect(t, url).Call(t.Context(), CallRequest{}, 1)
	httpErr, ok := errors.AsType[*HTTPError](err)
	if !ok || httpErr.StatusCode != 500 || httpErr.Body != "unavailable" || !Transient(err) {
		t.Errorf("Call: got %v, want a transient *HTTPError with status 500", err)
	}
}

// fastRateLimitRetries makes the retries of rate-limited requests quick for a
// test.
func fastRateLimitRetries(t *testing.T) {
	t.Helper()
	delay, maxDelay := rateLimitDelay, rateLimitMaxDelay
	rateLimitDelay, rateLimitMaxDelay = time.Millisecond, time.Millisecond
	t.Cleanup(func() { rateLimitDelay, rateLimitMaxDelay = delay, maxDelay })
}

func TestRawRequestRetriesRateLimit(t *testing.T) {
	fastRateLimitRetries(t)
	var calls atomic.Int64
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		res := map[string]any{"jsonrpc": "2.0", "id": id}
		if calls.Add(1) <= 2 {
			// A rate limit with HTTP status 200, which httpretry doesn't retry.
			res["error"] = map[string]any{"code": -32005, "message": "Rate limit exceeded"}
		} else {
			res["result"] = "0x2a"
		}
		json.NewEncoder(w).Encode(res)
	})
	got, err := connect(t, url).BlockNumber(t.Context())
	if err != nil || got != 42 || calls.Load() != 3 {
		t.Errorf("BlockNumber: got %d, %v after %d requests, want 42 after 3", got, err, calls.Load())
	}
}

func TestRawRequestRateLimitGivesUp(t *testing.T) {
	fastRateLimitRetries(t)
	var calls atomic.Int64
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32005, "message": "Rate limit exceeded"}})
	})
	_, err := connect(t, url).BlockNumber(t.Context())
	if !Transient(err) || calls.Load() != int64(rateLimitRetries+1) {
		t.Errorf("BlockNumber: got %v after %d requests, want a transient error after %d", err, calls.Load(), rateLimitRetries+1)
	}
}

func TestRawRequestDoesNotRetryOtherErrors(t *testing.T) {
	var calls atomic.Int64
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": "execution reverted"}})
	})
	if _, err := connect(t, url).Call(t.Context(), CallRequest{}, 1); err == nil || calls.Load() != 1 {
		t.Errorf("Call: got %v after %d requests, want an error after 1", err, calls.Load())
	}
}
