package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReport(t *testing.T) {
	const saved = "../reportcheck/testdata/a049284bbb54"
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(t.Context(), append([]string{"arbot", "check-report"}, args...), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	// The saved report has a warning, which doesn't fail it.
	code, stdout, stderr := run("-corpus", "../../corpus", saved)
	if code != 0 || !strings.Contains(stdout, "report.md:94: warning: quotation") || !strings.HasSuffix(stdout, "0 errors, 1 warnings, checked against request.json, evidence.md, case-2.md, case-3.md, case-6.md, case-7.md, judgment-3.md, judgment-6.md, judgment-7.md, charter.md, and the corpus in ../../corpus, with the 9 rulings that report.md names\n") {
		t.Errorf("check-report: got status %d, stdout %q, stderr %q; want 0 and one warning", code, stdout, stderr)
	}

	// A report with a hash that no source has fails, and the JSON has it.
	dir := t.TempDir()
	for _, name := range []string{"report.md", "request.json"} {
		content, err := os.ReadFile(filepath.Join(saved, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "report.md" {
			content = []byte("## Request\n\nThe Safe transaction hash is `0x" + strings.Repeat("ab", 32) + "`.\n")
		}
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, stdout, stderr = run("-json", "-corpus", "../../corpus", dir)
	var result struct {
		Errors []struct {
			File, Kind, Value, Message string
			Line                       int
		}
		Warnings []any
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("check-report -json: %v, in %q", err, stdout)
	}
	if code != 1 || len(result.Errors) != 1 || result.Errors[0].Line != 3 || result.Errors[0].Kind != "hash" || result.Warnings == nil ||
		!strings.Contains(stderr, "report.md has 1 errors") {
		t.Errorf("check-report -json: got status %d, %+v, stderr %q; want 1 and one hash error on line 3", code, result, stderr)
	}

	// The directory is required, and must have a report.
	if code, _, stderr := run(); code != 2 || !strings.Contains(stderr, "Usage: arbot check-report") {
		t.Errorf("check-report without a directory: got status %d, stderr %q; want 2 and the usage", code, stderr)
	}
	if code, _, stderr := run(t.TempDir()); code != 1 || !strings.Contains(stderr, "report.md") {
		t.Errorf("check-report without a report: got status %d, stderr %q; want 1", code, stderr)
	}
}
