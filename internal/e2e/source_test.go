package e2e_test

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/e2e"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// sourceGitHubOutput is the part of the JSON output of `arbot source github`
// that the test checks.
type sourceGitHubOutput struct {
	ChainID   uint64         `json:"chainId"`
	SafeBlock uint64         `json:"safeBlock"`
	Address   ethrpc.Address `json:"address"`
	CodeSize  int            `json:"codeSize"`
	Metadata  *struct {
		Kind     string `json:"kind"`
		Compiler string `json:"compiler"`
	} `json:"metadata"`
	Search struct {
		Available bool `json:"available"`
	} `json:"search"`
	References []struct {
		Reference     string `json:"reference"`
		Commit        string `json:"commit"`
		Path          string `json:"path"`
		Match         string `json:"match"`
		MetadataMatch bool   `json:"metadataMatch"`
	} `json:"references"`
	NotReferences []struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"notReferences"`
}

// TestSourceGitHub runs `arbot source github` on the Safe 1.3.0 singleton of
// the Mainnet node, as of the Safe block, with candidate artifacts of a fake
// GitHub: one with the singleton's deployed bytecode, which is a reference, one
// with the SafeL2 singleton's, which isn't, and on an account without code,
// which has no metadata.
func TestSourceGitHub(t *testing.T) {
	t.Parallel()
	n := e2e.NewTestnet(t)
	r := n.Propose(nil)
	a := n.Artifacts
	const commit = "3333333333333333333333333333333333333333"
	artifact := func(code ethrpc.Bytes) []byte {
		data, _ := json.Marshal(map[string]any{"abi": []any{}, "deployedBytecode": "0x" + hex.EncodeToString(code)})
		return data
	}
	files := map[string][]byte{
		"artifacts/Safe.json":   artifact(a.Accounts[a.SafeSingleton].Code),
		"artifacts/SafeL2.json": artifact(a.Accounts[a.SafeL2Singleton].Code),
	}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, ok := files[strings.TrimPrefix(req.URL.Path, "/raw/safe/contracts/"+commit+"/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(github.Close)

	arbot := n.Arbot().WithSetting(t, "github", map[string]string{"url": github.URL + "/api", "rawUrl": github.URL + "/raw"})
	info := arbot.Run(t, "info", "-json", r.ID.String())
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, info, 0o644); err != nil {
		t.Fatal(err)
	}
	safeBlock := decode[requestInfo](t, info).Proposal.SafeBlock

	got := decode[sourceGitHubOutput](t, arbot.Run(t, "source", "github", "-json",
		"-candidate", "safe/contracts@"+commit+":artifacts/Safe.json",
		"-candidate", "safe/contracts@"+commit+":artifacts/SafeL2.json",
		"-request-file", path, a.SafeSingleton.String()))
	if got.ChainID != ethrpc.Mainnet || got.SafeBlock != safeBlock || got.Address != a.SafeSingleton || got.CodeSize != len(a.Accounts[a.SafeSingleton].Code) ||
		got.Metadata == nil || got.Metadata.Kind != "ipfs" || got.Metadata.Compiler != "0.7.6" || got.Search.Available {
		t.Errorf("source github: got %+v", got)
	}
	if len(got.References) != 1 || got.References[0].Reference != "source of this bytecode" || got.References[0].Path != "artifacts/Safe.json" ||
		got.References[0].Commit != commit || got.References[0].Match != "exact" || !got.References[0].MetadataMatch {
		t.Errorf("source github: got references %+v, want the singleton's artifact", got.References)
	}
	if len(got.NotReferences) != 1 || got.NotReferences[0].Path != "artifacts/SafeL2.json" {
		t.Errorf("source github: got not references %+v, want the SafeL2 singleton's artifact", got.NotReferences)
	}

	none := decode[sourceGitHubOutput](t, arbot.Run(t, "source", "github", "-json", "-candidate", "safe/contracts@"+commit+":artifacts/Safe.json",
		"-request-file", path, ethrpc.Address{19: 0xee}.String()))
	if none.CodeSize != 0 || none.Metadata != nil || len(none.References) != 0 || len(none.NotReferences) != 1 {
		t.Errorf("source github on an account without code: got %+v", none)
	}
}
