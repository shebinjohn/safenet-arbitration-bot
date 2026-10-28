package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountErrors(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, []byte(`{"proposal": {"transaction": {"chainId": 1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	address := "0x000000000000000000000000000000000000dEaD"

	tests := []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{address}, 2, "-request-file is required"},
		{[]string{"-request-file", request}, 2, "Usage: arbot account"},
		{[]string{"-request-file", request, address, "extra"}, 2, "Usage: arbot account"},
		{[]string{"-request-file", request, "0x12"}, 2, `address "0x12"`},
		{[]string{"-request-file", request, address}, 1, "no Safe chain ID or block"},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		args := append([]string{"arbot", "-config", config, "account"}, test.args...)
		code := Run(t.Context(), args, &stdout, &stderr)
		if code != test.code || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.stderr) {
			t.Errorf("account %v: got status %d, stdout %q, stderr %q; want %d, no output, and stderr containing %q",
				test.args, code, stdout.String(), stderr.String(), test.code, test.stderr)
		}
	}
}
