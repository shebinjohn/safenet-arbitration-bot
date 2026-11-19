package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// mention is an arbot command line that a document mentions, such as
// "arbot calls -json -abi".
type mention struct {
	line int
	// global are the flags before the command, command is the command, or "" if
	// there is none, such as for "arbot -h", and flags are its flags, without their
	// dashes and values.
	global  []string
	command string
	flags   []string
}

var (
	codeSpan    = regexp.MustCompile("``(.+?)``|`([^`]+)`")
	flagToken   = regexp.MustCompile(`^--?([a-zA-Z][a-zA-Z0-9-]*)`)
	usageFlag   = regexp.MustCompile(`(?m)^ {2}-([^\s=]+)(?: (\S+))?(?:\t|$)`)
	shellBreaks = []string{"|", ">", ">>", "<", "&&", "||", ";", "2>", "2>&1"}
)

// mentions returns the arbot command lines in the code spans and code blocks of
// a Markdown document. valued reports whether a flag of a command, or of arbot
// itself for the command "", takes a value, which isn't a command.
func mentions(text string, valued func(command, flag string) bool) []mention {
	var found []mention
	fence := false
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fence = !fence
			continue
		}
		var code []string
		if fence {
			code = []string{line}
		} else {
			for _, m := range codeSpan.FindAllStringSubmatch(line, -1) {
				code = append(code, m[1]+m[2])
			}
		}
		for _, c := range code {
			found = append(found, commandLines(i+1, c, valued)...)
		}
	}
	return found
}

// commandLines returns the arbot command lines in a piece of code, such as
// ".msgboard/bin/arbot info -json <request-id> > request.json".
func commandLines(line int, code string, valued func(command, flag string) bool) []mention {
	var found []mention
	words := strings.Fields(code)
	for i, word := range words {
		if word != "arbot" && !strings.HasSuffix(word, "/arbot") {
			continue
		}
		m := mention{line: line}
		rest := words[i+1:]
		for len(rest) > 0 {
			token := strings.Trim(rest[0], "[](),.")
			if slices.Contains(shellBreaks, rest[0]) {
				rest = nil
				break
			}
			name := flagToken.FindStringSubmatch(token)
			if name == nil {
				break
			}
			m.global = append(m.global, name[1])
			rest = rest[1:]
			if valued("", name[1]) && !strings.Contains(token, "=") && len(rest) > 0 {
				rest = rest[1:]
			}
		}
		if len(rest) > 0 {
			command := strings.Trim(rest[0], "[](),.:")
			// A placeholder, such as <command>, or a path, such as ./cmd/arbot in
			// "go build -o arbot ./cmd/arbot", isn't a command.
			if command == "" || strings.ContainsAny(command, "<>{}/.") || slices.Contains(shellBreaks, rest[0]) {
				continue
			}
			m.command = command
			depth := 0
			for _, word := range rest[1:] {
				// In a usage such as "[-blocks N | -all]", | is an alternative, not a pipe.
				if slices.Contains(shellBreaks, word) && !(word == "|" && depth > 0) {
					break
				}
				depth += strings.Count(word, "[") - strings.Count(word, "]")
				// A token such as -in/-out names two flags.
				for _, part := range strings.FieldsFunc(word, func(r rune) bool { return r == '/' || r == '|' }) {
					if name := flagToken.FindStringSubmatch(strings.Trim(part, "[](),.")); name != nil {
						m.flags = append(m.flags, name[1])
					}
				}
			}
		}
		found = append(found, m)
	}
	return found
}

// usageFlags returns the flags that arbot's usage lists, for a command, or for
// arbot itself if command is "", and whether each takes a value.
func usageFlags(t *testing.T, command string) map[string]bool {
	t.Helper()
	args := []string{"arbot", "-h"}
	if command != "" {
		args = []string{"arbot", command, "-h"}
	}
	var stdout, stderr bytes.Buffer
	if code := Run(t.Context(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("%v: got status %d, stderr %q", args, code, stderr.String())
	}
	flags := map[string]bool{"h": false, "help": false}
	for _, m := range usageFlag.FindAllStringSubmatch(stderr.String(), -1) {
		flags[m[1]] = m[2] != ""
	}
	if len(flags) == 2 {
		t.Fatalf("%v: no flags in the usage %q", args, stderr.String())
	}
	return flags
}

func TestMentionedCommandsExist(t *testing.T) {
	flags := map[string]map[string]bool{"": usageFlags(t, "")}
	for name := range commands {
		flags[name] = usageFlags(t, name)
	}
	valued := func(command, flag string) bool { return flags[command][flag] }

	var files []string
	for _, pattern := range []string{"../../skills/*/*.md", "../../agents/*.md", "../../AGENTS.md"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if !slices.Contains(files, "../../skills/evidence/SKILL.md") {
		t.Fatalf("got files %v, want the evidence skill among them", files)
	}
	count := 0
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mentions(string(content), valued) {
			count++
			where := fmt.Sprintf("%s:%d", file, m.line)
			for _, flag := range m.global {
				if _, ok := flags[""][flag]; !ok {
					t.Errorf("%s: arbot has no flag -%s", where, flag)
				}
			}
			if m.command == "" {
				continue
			}
			commandFlags, ok := flags[m.command]
			if !ok {
				t.Errorf("%s: arbot has no command %q", where, m.command)
				continue
			}
			for _, flag := range m.flags {
				if _, ok := commandFlags[flag]; !ok {
					t.Errorf("%s: arbot %s has no flag -%s", where, m.command, flag)
				}
			}
		}
	}
	if count < 50 {
		t.Errorf("found %d arbot command lines, want more: is the parser broken?", count)
	}
}

func TestMentions(t *testing.T) {
	valued := func(command, flag string) bool { return command == "" && flag == "config" }
	text := strings.Join([]string{
		"Run `.msgboard/bin/arbot -config c.json transfers -all -in/-out -json -request-file <path> > out.json`.",
		"Build it with `go build -o .msgboard/bin/arbot ./cmd/arbot`, then run `arbot <command> -h` or `arbot -h`.",
		"```",
		"arbot explorer creation -request-file request.json | jq .",
		"```",
		"The `arbot` CLI, and ``arbot safe [-address <address>] [-blocks N | -all]``.",
	}, "\n")
	got := mentions(text, valued)
	want := []mention{
		{line: 1, global: []string{"config"}, command: "transfers", flags: []string{"all", "in", "out", "json", "request-file"}},
		// The paths and the placeholder aren't commands.
		{line: 2},
		{line: 2, global: []string{"h"}},
		{line: 4, command: "explorer", flags: []string{"request-file"}},
		{line: 6},
		{line: 6, command: "safe", flags: []string{"address", "blocks", "all"}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got mentions\n%v\nwant\n%v", got, want)
	}
}
