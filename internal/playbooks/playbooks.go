// Package playbooks reads the evidence playbooks of skills/evidence: the arbot
// commands that each playbook runs, in order, and the rules that it is for.
//
// The playbooks are written for agents, in Markdown, and the vector harness
// runs their commands. It reads them from the Markdown, rather than from a list
// of its own, so that the commands that it runs are the ones that the agents
// run, and can't drift from them.
package playbooks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Always is the name of the playbook that runs for every case, first.
const Always = "proposal-status"

// Playbook is an evidence playbook.
type Playbook struct {
	// Name is the playbook's name, such as "value-recipient", which its file is
	// named after.
	Name string
	// Rules are the rules that the playbook is for, as the skill's table lists
	// them, or none for the playbook that runs for every case.
	Rules []string
	// Commands are the arbot command lines that its Commands section runs on a
	// request file, in order, such as "arbot calls -json -abi -request-file
	// <request.json>", with their placeholders.
	Commands []string
}

var (
	// tableRow is a row of the skill's table of playbooks, such as "| [`value-
	// recipient`](value-recipient.md) | R-4.3 | R-4.3 is disputed |".
	tableRow = regexp.MustCompile("^\\| \\[`([a-z-]+)`\\]\\(([a-z-]+\\.md)\\) \\| ([^|]+) \\|")
	ruleID   = regexp.MustCompile(`R-\d+\.\d+`)
	codeSpan = regexp.MustCompile("`(arbot [^`]+)`")
	step     = regexp.MustCompile(`^\d+\. `)
)

// RequestFile is the placeholder of the request file in a command.
const RequestFile = "<request.json>"

// Load reads the playbooks that the skill in dir, such as skills/evidence,
// lists in its table, in the table's order.
func Load(dir string) ([]Playbook, error) {
	skill, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	var playbooks []Playbook
	for _, line := range strings.Split(string(skill), "\n") {
		m := tableRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p := Playbook{Name: m[1], Rules: rules(m[3])}
		if m[2] != p.Name+".md" {
			return nil, fmt.Errorf("%s: playbook %s links to %s", dir, p.Name, m[2])
		}
		content, err := os.ReadFile(filepath.Join(dir, m[2]))
		if err != nil {
			return nil, err
		}
		if p.Commands = Commands(string(content)); len(p.Commands) == 0 {
			return nil, fmt.Errorf("%s: playbook %s runs no command on a request file", dir, p.Name)
		}
		playbooks = append(playbooks, p)
	}
	if !slices.ContainsFunc(playbooks, func(p Playbook) bool { return p.Name == Always }) {
		return nil, fmt.Errorf("%s: no %s playbook in the table of SKILL.md", dir, Always)
	}
	return playbooks, nil
}

// rules returns the rules that a cell of the table names, such as "R-4.4,
// R-4.5", or none for "every case".
func rules(cell string) []string {
	var found []string
	for _, id := range ruleID.FindAllString(cell, -1) {
		if !slices.Contains(found, id) {
			found = append(found, id)
		}
	}
	return found
}

// Commands returns the arbot command lines of the numbered steps of a
// playbook's Commands section that run on a request file, in order, without
// repeats. A step's other code spans, such as a mention of "arbot calls", or a
// command on a request ID, such as "arbot info -json <request-id>", don't run
// on a request file, so they aren't commands of the playbook.
func Commands(markdown string) []string {
	var commands []string
	in := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "## ") {
			in = line == "## Commands"
			continue
		}
		if !in || !step.MatchString(line) {
			continue
		}
		for _, m := range codeSpan.FindAllStringSubmatch(line, -1) {
			command := strings.Join(strings.Fields(m[1]), " ")
			if strings.Contains(command, "-request-file "+RequestFile) && !slices.Contains(commands, command) {
				commands = append(commands, command)
			}
		}
	}
	return commands
}

// For returns the playbooks to run for a case whose disputed rules are rules,
// in the order of Load: the playbook for every case, then those of the rules.
// With no rules, it returns every playbook, as for a secure verdict, which
// holds under every rule.
func For(playbooks []Playbook, rules []string) []Playbook {
	var chosen []Playbook
	for _, p := range playbooks {
		if p.Name == Always || len(rules) == 0 || slices.ContainsFunc(p.Rules, func(r string) bool { return slices.Contains(rules, r) }) {
			chosen = append(chosen, p)
		}
	}
	return chosen
}
