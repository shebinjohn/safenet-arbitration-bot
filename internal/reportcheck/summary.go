package reportcheck

import (
	"fmt"
	"slices"
)

// Summary is the evidence summary that the detective writes next to its report,
// as evidence-summary.json, which skills/evidence/SKILL.md defines.
type Summary struct {
	Request       string   `json:"request"`
	DisputedRules []string `json:"disputedRules"`
	// Playbooks are the evidence playbooks that the detective followed.
	Playbooks []string   `json:"playbooks"`
	Facts     []Fact     `json:"facts"`
	Gaps      []Gap      `json:"gaps"`
	Conflicts []Conflict `json:"conflicts"`
	// Uncertainty is "low", "medium", or "high", with the reason for it.
	Uncertainty       string           `json:"uncertainty"`
	UncertaintyReason string           `json:"uncertaintyReason"`
	SuggestedRuling   *SuggestedRuling `json:"suggestedRuling"`
	// HumanReview is whether the Council must review the evidence by hand before
	// relying on a draft, with the reasons: it is required when the uncertainty is
	// high, or a decisive gap is open.
	HumanReview        bool     `json:"humanReview"`
	HumanReviewReasons []string `json:"humanReviewReasons"`
}

// Fact is a fact of the evidence summary, with its source.
type Fact struct {
	ID   string `json:"id"`
	Fact string `json:"fact"`
	// Rules are the rules and sections that the fact bears on, such as R-4.3 and §
	// 2.4.
	Rules  []string `json:"rules"`
	Source Source   `json:"source"`
	// ChainID, Block, and BlockTime are the chain and the block that the fact is as
	// of, and its time, which an offchain fact may not have.
	ChainID   uint64  `json:"chainId"`
	Block     *uint64 `json:"block"`
	BlockTime string  `json:"blockTime"`
}

// Source is where a fact comes from.
type Source struct {
	// Kind is "onchain", "offchain" (§ 3.4), or "derived", for arithmetic or
	// reasoning over other facts.
	Kind string `json:"kind"`
	// Command is the arbot command that reported the fact, or the field of the
	// request file that it is.
	Command string `json:"command"`
	// URL, ObservedAt, and Reliability are those of an offchain source, which the
	// Council must identify (§ 2.7, § 3.4).
	URL         string `json:"url,omitempty"`
	ObservedAt  string `json:"observedAt,omitempty"`
	Reliability string `json:"reliability,omitempty"`
	// From are the IDs of the facts that a derived fact is derived from.
	From []string `json:"from,omitempty"`
}

// Gap is a fact that the evidence needs, but that arbot couldn't provide.
type Gap struct {
	ID     string   `json:"id"`
	Fact   string   `json:"fact"`
	Rules  []string `json:"rules"`
	Reason string   `json:"reason"`
	// Decisive is whether filling the gap could change the verdict.
	Decisive bool `json:"decisive"`
	// Command is the arbot command that would fill it, if one exists, such as one
	// to run again on another RPC, or else how it could be filled.
	Command string `json:"command"`
}

// Conflict is a disagreement between sources, with the facts that disagree.
type Conflict struct {
	Facts       []string `json:"facts"`
	Description string   `json:"description"`
	Resolution  string   `json:"resolution"`
}

// SuggestedRuling is the ruling that the evidence suggests to the prosecutor
// and the judge, which is not a verdict.
type SuggestedRuling struct {
	// Suggestion must be true: the ruling is a suggestion, not a verdict.
	Suggestion bool `json:"suggestion"`
	// Verdict is "secure", "insecure", or null if the evidence suggests neither.
	Verdict *string  `json:"verdict"`
	Rules   []string `json:"rules"`
	// Charter are the Charter references that the suggestion rests on.
	Charter   []string `json:"charter"`
	Reasoning string   `json:"reasoning"`
}

// Problems returns what the summary lacks or gets wrong, as sentences.
func (s *Summary) Problems() []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if s.Request == "" {
		add("it has no request")
	}
	if len(s.Facts) == 0 {
		add("it has no facts")
	}
	if s.Gaps == nil {
		add("it has no gaps array; write [] if there are none")
	}
	if s.Conflicts == nil {
		add("it has no conflicts array; write [] if there are none")
	}

	ids := make(map[string]bool)
	for i, f := range s.Facts {
		name := fmt.Sprintf("fact %d", i+1)
		if f.ID != "" {
			name = "fact " + f.ID
			if ids[f.ID] {
				add("%s: its ID is used twice", name)
			}
			ids[f.ID] = true
		}
		if f.Fact == "" {
			add("%s: it states no fact", name)
		}
		if len(f.Rules) == 0 {
			add("%s: it names no rule or section that it bears on", name)
		}
		switch f.Source.Kind {
		case "onchain":
			if f.Source.Command == "" {
				add("%s: its onchain source has no command or request field", name)
			}
			if f.Block == nil {
				add("%s: its onchain source has no block", name)
			}
		case "offchain":
			if f.Source.Command == "" || f.Source.URL == "" || f.Source.ObservedAt == "" {
				add("%s: an offchain source needs its command, URL, and observation time (§ 3.4)", name)
			}
		case "derived":
			if len(f.Source.From) == 0 {
				add("%s: a derived fact needs the facts that it is derived from", name)
			}
		default:
			add("%s: its source kind is %q, not onchain, offchain, or derived", name, f.Source.Kind)
		}
	}
	for i, f := range s.Facts {
		for _, from := range f.Source.From {
			if !ids[from] {
				add("fact %d: it is derived from %q, which no fact has as its ID", i+1, from)
			}
		}
	}
	decisive := false
	for i, g := range s.Gaps {
		if g.Fact == "" || len(g.Rules) == 0 {
			add("gap %d: it needs the fact that is missing and the rules that it bears on", i+1)
		}
		decisive = decisive || g.Decisive
	}
	for i, c := range s.Conflicts {
		if c.Description == "" || len(c.Facts) == 0 {
			add("conflict %d: it needs a description and the facts that disagree", i+1)
		}
		for _, id := range c.Facts {
			if !ids[id] {
				add("conflict %d: no fact has the ID %q", i+1, id)
			}
		}
	}

	if !slices.Contains([]string{"low", "medium", "high"}, s.Uncertainty) {
		add("its uncertainty is %q, not low, medium, or high", s.Uncertainty)
	}
	if r := s.SuggestedRuling; r == nil {
		add("it has no suggested ruling")
	} else {
		if !r.Suggestion {
			add("its suggested ruling isn't marked as a suggestion, which it always is")
		}
		if r.Verdict != nil && *r.Verdict != "secure" && *r.Verdict != "insecure" {
			add("its suggested verdict is %q, not secure, insecure, or null", *r.Verdict)
		}
		if len(r.Charter) == 0 {
			add("its suggested ruling cites no Charter reference")
		}
	}
	if (s.Uncertainty == "high" || decisive) && !s.HumanReview {
		add("humanReview must be true, since the uncertainty is high or a decisive gap is open")
	}
	if s.HumanReview && len(s.HumanReviewReasons) == 0 {
		add("humanReview is true, but it gives no reasons")
	}
	return problems
}
