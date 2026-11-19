package replay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// TestRecordAndReplay records a node's and a service's answers through a
// fixture, from local servers that stand in for live ones, and replays them
// with the servers gone.
func TestRecordAndReplay(t *testing.T) {
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "eth_getBlockByNumber":
			res["result"] = map[string]any{"number": "0x10", "hash": "0x" + strings.Repeat("ab", 32), "timestamp": "0x64", "miner": "0x" + strings.Repeat("00", 20)}
		default:
			res["error"] = map[string]any{"code": -32000, "message": "missing trie node"}
		}
		json.NewEncoder(w).Encode(res)
	}))
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing/" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"results": [{"safe": "0x1"}], "confirmations": [1, 2], "query": "`+r.URL.RawQuery+`"}`)
	}))
	path := filepath.Join(t.TempDir(), "fixture.json")
	trim := func(_, _ string, body json.RawMessage) json.RawMessage { return Fields(body, "results", "query") }

	// exercise reads a block, a state that the node lacks, and the service's
	// replies, as a test would.
	exercise := func(t *testing.T, f *Fixture) {
		t.Helper()
		client := f.Node(ethrpc.Mainnet)
		block, err := client.GetBlockByNumber(t.Context(), 0x10)
		if err != nil || block.Timestamp != 100 {
			t.Errorf("GetBlockByNumber: got %+v, %v", block, err)
		}
		if _, err := client.GetCode(t.Context(), ethrpc.Address{}, 0x10); err == nil || !strings.Contains(err.Error(), "missing trie node") {
			t.Errorf("GetCode: got error %v, want the node's", err)
		}
		base := f.ServiceURL("svc", service.URL)
		for path, want := range map[string]string{"/list/?page=2": `"query":"page=2"`, "/missing/": "404"} {
			resp, err := http.Get(base + path)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if got := resp.Status + string(body); !strings.Contains(strings.ReplaceAll(got, " ", ""), want) {
				t.Errorf("GET %s: got %q, want %s", path, got, want)
			}
		}
	}

	t.Run("record", func(t *testing.T) {
		exercise(t, Open(t, path, Options{Record: true, RPCs: map[uint64]string{ethrpc.Mainnet: node.URL}, TrimReply: trim}))
	})
	node.Close()
	service.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The recording has only the fields that arbot reads, and no URL.
	for _, absent := range []string{"miner", "confirmations", "127.0.0.1"} {
		if strings.Contains(string(data), absent) {
			t.Errorf("the recording has %q:\n%s", absent, data)
		}
	}
	t.Run("replay", func(t *testing.T) {
		exercise(t, Open(t, path, Options{}))
	})
}
