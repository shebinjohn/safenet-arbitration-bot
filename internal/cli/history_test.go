package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/executions"
)

func TestHistoryErrors(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, []byte(`{"proposal": {"transaction": {"chainId": 1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		args   []string
		code   int
		stderr string
	}{
		{nil, 2, "-request-file is required"},
		{[]string{"-request-file", request, "extra"}, 2, "Usage: arbot history"},
		{[]string{"-request-file", request}, 1, "no Safe chain ID or block"},
		{[]string{"-request-file", filepath.Join(dir, "missing.json")}, 1, "no such file"},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		args := append([]string{"arbot", "-config", config, "history"}, test.args...)
		code := Run(t.Context(), args, &stdout, &stderr)
		if code != test.code || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.stderr) {
			t.Errorf("history %v: got status %d, stdout %q, stderr %q; want %d, no output, and stderr containing %q",
				test.args, code, stdout.String(), stderr.String(), test.code, test.stderr)
		}
	}
}

func TestNonceRanges(t *testing.T) {
	for _, test := range []struct {
		ranges []executions.NonceRange
		want   string
	}{
		{nil, "none"},
		{[]executions.NonceRange{{From: 0, To: 3}, {From: 7, To: 7}}, "0-3, 7"},
	} {
		if got := nonceRanges(test.ranges); got != test.want {
			t.Errorf("nonceRanges(%v): got %q, want %q", test.ranges, got, test.want)
		}
	}
}
