package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestVector makes requests of a secure vector, an insecure one, and one of an
// EOA transaction, which the commands that take -request-file read as they read
// the requests that arbot info -json writes.
func TestVector(t *testing.T) {
	const testdata = "../vector/testdata/"
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(t.Context(), append([]string{"arbot", "vector"}, args...), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	for _, test := range []struct {
		file, verdict, rule string
		eoa                 bool
		safeBlock           uint64
	}{
		{"0xAb876791757b47D202B7d6DeF038940cE7A64fe4_35.json", "secure", "", false, 25853030},
		{"safenet_approve_stake_excess.json", "insecure", "R-4.5", false, 25847023},
		{"usdt-100k-near-match.json", "insecure", "R-4.3", true, 24369134},
	} {
		t.Run(test.file, func(t *testing.T) {
			code, stdout, stderr := run("-json", testdata+test.file)
			if code != 0 {
				t.Fatalf("vector -json: got status %d, stderr %q", code, stderr)
			}
			path := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(path, []byte(stdout), 0o644); err != nil {
				t.Fatal(err)
			}
			request, err := readRequest(path)
			if err != nil {
				t.Fatalf("readRequest: %v", err)
			}
			v := request.Vector
			wantRules := []string{}
			if test.rule != "" {
				wantRules = []string{test.rule}
			}
			if v == nil || v.Verdict != test.verdict || v.Rule != test.rule || v.EOA != test.eoa || v.Path != testdata+test.file ||
				request.Proposal.SafeBlock != test.safeBlock || !slices.Equal(request.DisputedRules, wantRules) {
				t.Errorf("got vector %+v, Safe block %d, and disputed rules %q; want %s %s, EOA %t, block %d",
					v, request.Proposal.SafeBlock, request.DisputedRules, test.verdict, test.rule, test.eoa, test.safeBlock)
			}
			// The Charter of a request comes from the oracle, which a vector has none of.
			var out, errOut bytes.Buffer
			if code := Run(t.Context(), []string{"arbot", "charter", "-request-file", path}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "has no Charter name") {
				t.Errorf("charter -request-file: got status %d, stderr %q; want 1 and no Charter name", code, errOut.String())
			}

			code, stdout, stderr = run(testdata + test.file)
			if code != 0 || !strings.Contains(stdout, "Verdict          "+test.verdict+"\n") || strings.Contains(stdout, "R-4.0") != test.eoa {
				t.Errorf("vector: got status %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}

	if code, _, stderr := run(); code != 2 || !strings.Contains(stderr, "Usage: arbot vector") {
		t.Errorf("vector without a spec: got status %d, stderr %q; want 2 and the usage", code, stderr)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"request": {}, "response": {"verdict": "secure"}}`), 0o644)
	if code, _, stderr := run(bad); code != 1 || !strings.Contains(stderr, "reading vector") {
		t.Errorf("vector with an invalid spec: got status %d, stderr %q; want 1", code, stderr)
	}
}
