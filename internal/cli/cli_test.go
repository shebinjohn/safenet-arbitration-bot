package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTransientError(t *testing.T) {
	// A node that answers its chain ID, then fails every request, as an overloaded
	// node does. The command fails, and says that running it again may help, since
	// the failure says nothing about the chain.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Method != "eth_chainId" {
			http.Error(w, "overloaded", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": "0x1"})
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"rpcs": {"1": %q}}`, server.URL), 0o644); err != nil {
		t.Fatal(err)
	}
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, []byte(`{"proposal": {"safeBlock": 100, "transaction": {"chainId": 1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), []string{"arbot", "-config", config, "events", "-request-file", request, txSuccess}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "500") || !strings.Contains(stderr.String(), "run the command again later") {
		t.Errorf("events: got status %d, stderr %q; want 1 and a hint to run it again", code, stderr.String())
	}

	// An error that isn't about the node has no such hint.
	stderr.Reset()
	code = Run(t.Context(), []string{"arbot", "-config", config, "events", "-request-file", filepath.Join(dir, "missing.json"), txSuccess}, &stdout, &stderr)
	if code != 1 || strings.Contains(stderr.String(), "again later") {
		t.Errorf("events with a missing file: got status %d, stderr %q; want 1 and no hint", code, stderr.String())
	}
}
