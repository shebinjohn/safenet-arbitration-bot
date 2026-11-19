package ethrpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// logNode starts a JSON-RPC server that serves eth_getLogs for ranges of at
// most limit blocks, with one log in each of the given blocks, and rejects
// wider ranges with an HTTP error status and a JSON-RPC error. It records the
// block ranges it is asked for.
func logNode(t *testing.T, limit uint64, blocks ...BlockNumber) (*Client, *[][2]BlockNumber) {
	t.Helper()
	var ranges [][2]BlockNumber
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64      `json:"id"`
			Method string      `json:"method"`
			Params []LogFilter `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Method == "eth_chainId" {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": "0x1"})
			return
		}
		filter := req.Params[0]
		ranges = append(ranges, [2]BlockNumber{filter.FromBlock, filter.ToBlock})
		if uint64(filter.ToBlock-filter.FromBlock+1) > limit {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32602, "message": fmt.Sprintf("range exceeds %d blocks", limit)},
			})
			return
		}
		logs := []Log{}
		for _, block := range blocks {
			if filter.FromBlock <= block && block <= filter.ToBlock {
				logs = append(logs, Log{BlockNumber: block})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": logs})
	}))
	t.Cleanup(server.Close)
	return connect(t, server.URL), &ranges
}

func scan(t *testing.T, client *Client, filter LogFilter) []BlockNumber {
	t.Helper()
	var blocks []BlockNumber
	for log, err := range client.ScanLogs(t.Context(), filter) {
		if err != nil {
			t.Fatalf("ScanLogs: %v", err)
		}
		blocks = append(blocks, log.BlockNumber)
	}
	return blocks
}

func TestScanLogsChunksRange(t *testing.T) {
	want := []BlockNumber{0, 9_999, 10_000, 24_999, 25_000}
	client, ranges := logNode(t, 10_000, want...)

	if got := scan(t, client, LogFilter{FromBlock: 0, ToBlock: 25_000}); !slices.Equal(got, want) {
		t.Errorf("blocks: got %v, want %v", got, want)
	}
	wantRanges := [][2]BlockNumber{{0, 9_999}, {10_000, 19_999}, {20_000, 25_000}}
	if !slices.Equal(*ranges, wantRanges) {
		t.Errorf("ranges: got %v, want %v", *ranges, wantRanges)
	}
}

func TestScanLogsHalvesRangeOnError(t *testing.T) {
	want := []BlockNumber{100, 2_499, 2_500, 6_000}
	client, ranges := logNode(t, 3_000, want...)

	if got := scan(t, client, LogFilter{FromBlock: 100, ToBlock: 6_000}); !slices.Equal(got, want) {
		t.Errorf("blocks: got %v, want %v", got, want)
	}
	wantRanges := [][2]BlockNumber{
		{100, 6_000}, // 10,000 blocks, capped to the filter
		{100, 5_099}, // 5,000 blocks
		{100, 2_599}, // 2,500 blocks
		{2_600, 5_099},
		{5_100, 6_000},
	}
	if !slices.Equal(*ranges, wantRanges) {
		t.Errorf("ranges: got %v, want %v", *ranges, wantRanges)
	}
}

func TestScanLogsFailsBelowMinimumRange(t *testing.T) {
	client, _ := logNode(t, minLogRange-1)
	for _, err := range client.ScanLogs(t.Context(), LogFilter{FromBlock: 0, ToBlock: 100_000}) {
		if err == nil {
			t.Fatal("ScanLogs: expected an error")
		}
		return
	}
	t.Fatal("ScanLogs: yielded nothing, expected an error")
}

func TestScanLogsStopsEarly(t *testing.T) {
	client, ranges := logNode(t, 10_000, 5, 15_000)
	for log, err := range client.ScanLogs(t.Context(), LogFilter{FromBlock: 0, ToBlock: 25_000}) {
		if err != nil {
			t.Fatalf("ScanLogs: %v", err)
		}
		if log.BlockNumber != 5 {
			t.Errorf("block: got %d, want 5", log.BlockNumber)
		}
		break
	}
	if len(*ranges) != 1 {
		t.Errorf("ranges: got %v, want only the first chunk", *ranges)
	}
}

func TestScanLogsKeepsRangeOnRateLimit(t *testing.T) {
	fastRateLimitRetries(t)
	// A rate limit is not about the range, so the scan fails with it instead of
	// halving the range down to the minimum, which only sends more requests.
	var ranges int
	url, _ := node(t, "0x1", func(w http.ResponseWriter, id uint64) {
		ranges++
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32005, "message": "Rate limit exceeded"}})
	})
	client := connect(t, url)
	client.http = http.DefaultClient // don't let httpretry wait on the 429s
	var err error
	for _, err = range client.ScanLogs(t.Context(), LogFilter{FromBlock: 0, ToBlock: 100_000}) {
		break
	}
	if !Transient(err) || ranges != rateLimitRetries+1 {
		t.Errorf("ScanLogs: got %v after %d requests, want a transient error after %d requests for one range", err, ranges, rateLimitRetries+1)
	}
}
