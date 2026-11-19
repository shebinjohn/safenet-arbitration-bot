package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/safe-research/safenet-arbitration-bot/internal/reportcheck"
)

// checkReport checks the values of the draft ruling of an arbitration directory
// against the files that it was drafted from.
func checkReport(_ context.Context, e *env, args []string) error {
	flags := e.flagSet("check-report")
	asJSON := flags.Bool("json", false, "write the findings as JSON")
	corpusDir := flags.String("corpus", "corpus", "the corpus `directory`, for the precedents")
	file := flags.String("file", reportcheck.ReportFile, "check the file with this `name` in <dir> instead, such as a case, against the sources other than the cases and judgments")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s check-report [flags] <dir>\n\n", e.progname)
		fmt.Fprintf(out, "Checks the draft ruling <dir>/%s of an arbitration against the files that it was drafted from:\n", reportcheck.ReportFile)
		fmt.Fprintf(out, "request.json, classification.json, status.json, disputed-rules.md, evidence.md, %s,\n", reportcheck.SummaryFile)
		fmt.Fprintf(out, "the case-*.md and judgment-*.md files, and %s, whichever <dir> has, and the corpus.\n\n", reportcheck.CharterFile)
		fmt.Fprintf(out, "Every address, hash, and other hex value of 8 or more digits, abbreviation such as 0x1235…4306, number of\n")
		fmt.Fprintf(out, "4 or more digits, and RFC 3339 time must be in a source; a number that the line marks as derived, with\n")
		fmt.Fprintf(out, "\"about\" or \"~\" before it or \"derived\" on the line, is a warning instead. Every blockquote after the first\n")
		fmt.Fprintf(out, "section heading must quote %s verbatim. Every request ID in the Precedents section, in a precedents\n", reportcheck.CharterFile)
		fmt.Fprintf(out, "tag, or followed by an outcome in parentheses, such as \"(insecure)\", must be a ruling of the corpus, with\n")
		fmt.Fprintf(out, "that outcome. An evidence summary must have the fields of the evidence skill, and if it asks for human\n")
		fmt.Fprintf(out, "review, the report must say \"Human review: required\".\n\n")
		fmt.Fprintf(out, "It exits with status 1 if there are errors.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 1); err != nil {
		return err
	}

	if *file == "" || filepath.Base(*file) != *file {
		return usageError(fmt.Sprintf("-file %q must be the name of a file in the directory", *file))
	}
	result, err := reportcheck.CheckFile(flags.Arg(0), *file, *corpusDir)
	if err != nil {
		return err
	}
	if *asJSON {
		if err := writeJSON(e.stdout, result); err != nil {
			return err
		}
	} else {
		for _, f := range result.Errors {
			fmt.Fprintf(e.stdout, "%s:%d: error: %s %s: %s\n", f.File, f.Line, f.Kind, printable(f.Value), printable(f.Message))
		}
		for _, f := range result.Warnings {
			fmt.Fprintf(e.stdout, "%s:%d: warning: %s %s: %s\n", f.File, f.Line, f.Kind, printable(f.Value), printable(f.Message))
		}
		fmt.Fprintf(e.stdout, "%d errors, %d warnings, checked against %s, and the corpus in %s, with the %d rulings that %s names\n",
			len(result.Errors), len(result.Warnings), strings.Join(result.Sources, ", "), *corpusDir, len(result.Rulings), *file)
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("%s has %d errors", *file, len(result.Errors))
	}
	return nil
}
