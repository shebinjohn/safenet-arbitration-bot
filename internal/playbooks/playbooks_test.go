package playbooks

import (
	"slices"
	"strings"
	"testing"
)

// TestLoad reads the playbooks of the evidence skill, so that a change to their
// table or their Commands sections that the vector harness can't read fails
// here.
func TestLoad(t *testing.T) {
	playbooks, err := Load("../../skills/evidence")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range playbooks {
		names = append(names, p.Name)
		for _, c := range p.Commands {
			if !strings.HasPrefix(c, "arbot ") || !strings.Contains(c, "-request-file "+RequestFile) {
				t.Errorf("%s: command %q doesn't run arbot on a request file", p.Name, c)
			}
		}
	}
	want := []string{"proposal-status", "value-recipient", "approval-spender", "settings-and-delegatecall", "target-risk"}
	if !slices.Equal(names, want) {
		t.Fatalf("got playbooks %v, want %v", names, want)
	}
	byName := func(name string) Playbook {
		return playbooks[slices.IndexFunc(playbooks, func(p Playbook) bool { return p.Name == name })]
	}
	if r := byName("settings-and-delegatecall").Rules; !slices.Equal(r, []string{"R-4.0", "R-4.1", "R-4.2"}) {
		t.Errorf("settings-and-delegatecall: got rules %v, want R-4.0 to R-4.2", r)
	}
	if r := byName("proposal-status").Rules; len(r) != 0 {
		t.Errorf("proposal-status: got rules %v, want none, since it is for every case", r)
	}
	// The value-recipient playbook starts with the calls, and reads the Safe's
	// whole history.
	vr := byName("value-recipient").Commands
	if vr[0] != "arbot calls -json -abi -request-file <request.json>" ||
		!slices.Contains(vr, "arbot executions -all -calls -json -request-file <request.json>") ||
		!slices.Contains(vr, "arbot transfers -all -json -to <recipient> -request-file <request.json>") {
		t.Errorf("value-recipient: got commands %q", vr)
	}

	var chosen []string
	for _, p := range For(playbooks, []string{"R-4.5"}) {
		chosen = append(chosen, p.Name)
	}
	if !slices.Equal(chosen, []string{"proposal-status", "approval-spender"}) {
		t.Errorf("For R-4.5: got %v, want proposal-status and approval-spender", chosen)
	}
	if got := For(playbooks, nil); len(got) != len(playbooks) {
		t.Errorf("For no rules: got %d playbooks, want all %d", len(got), len(playbooks))
	}
}

func TestCommands(t *testing.T) {
	markdown := strings.Join([]string{
		"# Playbook: example",
		"",
		"Mentions `arbot calls -json -request-file <request.json>` before the commands.",
		"",
		"## Commands",
		"",
		"1. **The request**: read the file that `arbot info -json <request-id>` wrote. Don't run `arbot info` again.",
		"2. **The calls**: `arbot calls -json -request-file <request.json>`, then `arbot account  -json -request-file <request.json> <token>`.",
		"   - Shows: what `arbot simulate -json -all -request-file <request.json>` would show, which isn't a step.",
		"3. **Again**: `arbot calls -json -request-file <request.json>`.",
		"",
		"## Known Gaps",
		"",
		"1. `arbot transfers -json -request-file <request.json>` isn't a command here.",
	}, "\n")
	got := Commands(markdown)
	want := []string{"arbot calls -json -request-file <request.json>", "arbot account -json -request-file <request.json> <token>"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
