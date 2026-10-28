package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/cli"
)

// Arbot runs arbot in the test's process, configured to read chains from nodes.
// As the test imports arbot's implementation, changing it invalidates the
// test's cached results.
type Arbot struct {
	config string
}

// NewArbot returns an Arbot configured to read each node's chain from it.
func NewArbot(tb testing.TB, nodes ...*Anvil) *Arbot {
	tb.Helper()
	return newArbot(tb, "", nodes...)
}

// newArbot returns an Arbot configured to read each node's chain from it, and
// to fetch IPFS content from the gateway at the base URL gateway, unless it is
// empty.
func newArbot(tb testing.TB, gateway string, nodes ...*Anvil) *Arbot {
	tb.Helper()
	rpcs := make(map[uint64]string)
	// The tests never use the public Safe Transaction Services, so the Safe
	// Transaction Service is left out.
	services := make(map[uint64]string)
	for _, node := range nodes {
		rpcs[node.ChainID()] = node.URL
		services[node.ChainID()] = ""
	}
	config := map[string]any{"rpcs": rpcs, "txServices": services}
	if gateway != "" {
		config["ipfs"] = gateway
	}
	data, err := json.Marshal(config)
	if err != nil {
		tb.Fatal(err)
	}
	path := filepath.Join(tb.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		tb.Fatal(err)
	}
	return &Arbot{config: path}
}

// WithTxService returns an Arbot configured like a, but with the Safe
// Transaction Service at the base URL url for the chain with the ID chainID.
func (a *Arbot) WithTxService(tb testing.TB, chainID uint64, url string) *Arbot {
	tb.Helper()
	data, err := os.ReadFile(a.config)
	if err != nil {
		tb.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		tb.Fatal(err)
	}
	config["txServices"].(map[string]any)[strconv.FormatUint(chainID, 10)] = url
	if data, err = json.Marshal(config); err != nil {
		tb.Fatal(err)
	}
	path := filepath.Join(tb.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		tb.Fatal(err)
	}
	return &Arbot{config: path}
}

// Run runs arbot with args, and returns its stdout. It fails the test if arbot
// fails.
func (a *Arbot) Run(tb testing.TB, args ...string) []byte {
	tb.Helper()
	code, stdout, stderr := a.run(tb, args)
	if code != 0 {
		tb.Fatalf("arbot %v: exit status %d\n%s", args, code, stderr)
	}
	return stdout
}

// Fail runs arbot with args, and returns its stderr. It fails the test unless
// arbot exits with status code.
func (a *Arbot) Fail(tb testing.TB, code int, args ...string) []byte {
	tb.Helper()
	got, _, stderr := a.run(tb, args)
	if got != code {
		tb.Fatalf("arbot %v: got exit status %d, want %d\n%s", args, got, code, stderr)
	}
	return stderr
}

func (a *Arbot) run(tb testing.TB, args []string) (code int, stdout, stderr []byte) {
	var out, errs bytes.Buffer
	code = cli.Run(tb.Context(), append([]string{"arbot", "-config", a.config}, args...), &out, &errs)
	return code, out.Bytes(), errs.Bytes()
}
