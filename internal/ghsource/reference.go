package ghsource

import (
	"cmp"
	"context"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/solcmeta"
)

// ReferenceLabel is what a matching artifact is: the source of the code, and
// nothing else that its repository says.
const ReferenceLabel = "source of this bytecode"

// The origins of a candidate artifact.
const (
	FromSearch    = "search"
	FromCandidate = "candidate"
)

// MaxHits is the number of files of each search that are compared, the first
// page of GitHub's results by relevance.
const MaxHits = 10

// Candidate is a file of a repository at a commit, to compare as an artifact.
type Candidate struct {
	Owner, Repo string
	// Commit is a full commit SHA, never a branch, whose files can change.
	Commit, Path string
}

// candidatePattern matches <owner>/<repo>@<commit>:<path>.
var candidatePattern = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)@([0-9a-f]{40}):(.+)$`)

// ParseCandidate parses a candidate artifact of the form
// <owner>/<repo>@<commit>:<path>, whose commit is a full commit SHA in
// lowercase.
func ParseCandidate(s string) (Candidate, error) {
	m := candidatePattern.FindStringSubmatch(s)
	if m == nil {
		return Candidate{}, fmt.Errorf("invalid candidate %q: want <owner>/<repo>@<commit>:<path>, with a full commit SHA in lowercase, never a branch", s)
	}
	c := Candidate{Owner: m[1], Repo: m[2], Commit: m[3], Path: m[4]}
	if err := checkRepo(c.Owner, c.Repo); err != nil {
		return Candidate{}, err
	}
	if err := checkPath(c.Path); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func (c Candidate) String() string {
	return c.Owner + "/" + c.Repo + "@" + c.Commit + ":" + c.Path
}

// MetadataInfo is the metadata that the compiler appended to the code.
type MetadataInfo struct {
	// Kind is "ipfs", "bzzr0", or "bzzr1".
	Kind string `json:"kind"`
	// Hash is the hash as the code has it, in hex with 0x, and CID its IPFS CID, or
	// "" for a Swarm hash.
	Hash string `json:"hash"`
	CID  string `json:"cid"`
	// Compiler is the solc version, or "" if the metadata has none.
	Compiler     string `json:"compiler"`
	Experimental bool   `json:"experimental"`
	// Offset is where the metadata starts in the code, and Length its length, up to
	// the end of the code.
	Offset int `json:"offset"`
	Length int `json:"length"`
}

// SearchInfo is what the code search did.
type SearchInfo struct {
	// Available is whether the code was searched for, which needs a token and a
	// metadata hash, and Reason why it wasn't, if it wasn't.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// URL is the API searched.
	URL      string    `json:"url"`
	Searches []*Search `json:"queries"`
}

// Reference is an artifact whose deployed bytecode matches the code.
type Reference struct {
	// Reference is always ReferenceLabel.
	Reference  string    `json:"reference"`
	Owner      string    `json:"owner"`
	Repo       string    `json:"repo"`
	Commit     string    `json:"commit"`
	Path       string    `json:"path"`
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
	// From is FromSearch or FromCandidate.
	From   string `json:"from"`
	Format string `json:"format"`
	*Match
}

// NotReference is a file that was compared, or would have been, and isn't a
// reference, with the reason.
type NotReference struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	// Commit is "" for a search hit that wasn't fetched.
	Commit string `json:"commit"`
	Path   string `json:"path"`
	// URL and ObservedAt are those of the fetch, and empty for a file that wasn't
	// fetched.
	URL        string     `json:"url,omitempty"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
	From       string     `json:"from"`
	Reason     string     `json:"reason"`
}

// Report is the result of a search for the sources of a contract's code.
type Report struct {
	// Metadata is the code's metadata, or nil if it has none, and MetadataReason
	// why not.
	Metadata       *MetadataInfo  `json:"metadata"`
	MetadataReason string         `json:"metadataReason,omitempty"`
	Search         SearchInfo     `json:"search"`
	References     []Reference    `json:"references"`
	NotReferences  []NotReference `json:"notReferences"`
	Notes          []string       `json:"notes"`
}

// Finder finds the artifacts on GitHub that are the source of a contract's
// code.
type Finder struct {
	GitHub *Client
}

// Find compares the artifacts of the candidates, and of a code search for the
// code's metadata hash, and for name, the contract's name, if it isn't empty,
// with code, the contract's runtime code. It lists only the artifacts whose
// deployed bytecode matches the code as references, and the others apart, with
// the reason. It fails if a request to GitHub fails rather than being answered
// (Transient), which says nothing about the artifacts.
func (f *Finder) Find(ctx context.Context, code []byte, name string, candidates []Candidate) (*Report, error) {
	r := &Report{
		Search:        SearchInfo{URL: f.GitHub.APIURL, Searches: []*Search{}},
		References:    []Reference{},
		NotReferences: []NotReference{},
	}
	meta, err := solcmeta.Read(code)
	switch {
	case len(code) == 0:
		r.MetadataReason = "the account has no code at the block"
		meta = nil
	case err != nil:
		r.MetadataReason = err.Error()
		meta = nil
	default:
		r.Metadata = &MetadataInfo{
			Kind: meta.Kind, Hash: "0x" + hex.EncodeToString(meta.Hash), CID: meta.CID(), Compiler: meta.Compiler,
			Experimental: meta.Experimental, Offset: meta.Offset, Length: meta.Length,
		}
	}

	seen := map[Candidate]bool{}
	compare := func(c Candidate, from string) error {
		if seen[c] {
			return nil
		}
		seen[c] = true
		not := NotReference{Owner: c.Owner, Repo: c.Repo, Commit: c.Commit, Path: c.Path, From: from}
		if meta == nil {
			not.Reason = "the code has no Solidity metadata, so no artifact is a reference for it"
			r.NotReferences = append(r.NotReferences, not)
			return nil
		}
		file, err := f.GitHub.File(ctx, c.Owner, c.Repo, c.Commit, c.Path)
		if Transient(err) {
			return err
		} else if err != nil {
			not.Reason = err.Error()
			r.NotReferences = append(r.NotReferences, not)
			return nil
		}
		not.URL, not.ObservedAt = file.URL, &file.ObservedAt
		artifact, err := ParseArtifact(file.Content)
		if err != nil {
			not.Reason = err.Error()
			r.NotReferences = append(r.NotReferences, not)
			return nil
		}
		match, err := Compare(code, meta, artifact)
		if err != nil {
			not.Reason = err.Error()
			r.NotReferences = append(r.NotReferences, not)
			return nil
		}
		r.References = append(r.References, Reference{
			Reference: ReferenceLabel, Owner: c.Owner, Repo: c.Repo, Commit: c.Commit, Path: c.Path,
			URL: file.URL, ObservedAt: file.ObservedAt, From: from, Format: artifact.Format, Match: match,
		})
		return nil
	}

	for _, c := range candidates {
		if err := compare(c, FromCandidate); err != nil {
			return nil, err
		}
	}
	if err := f.search(ctx, r, meta, name, compare); err != nil {
		return nil, err
	}
	r.Notes = notes(r)
	return r, nil
}

// search searches GitHub's code for the metadata hash, and for name, and
// compares the JSON files that it finds.
func (f *Finder) search(ctx context.Context, r *Report, meta *solcmeta.Metadata, name string, compare func(Candidate, string) error) error {
	switch {
	case meta == nil:
		r.Search.Reason = "the code has no Solidity metadata hash to search for"
		return nil
	case !f.GitHub.HasToken():
		r.Search.Reason = "GitHub code search needs authentication, and no github.token is configured: -candidate names artifacts to compare without it"
		return nil
	}
	r.Search.Available = true
	queries := []string{hex.EncodeToString(meta.Hash)}
	if name != "" {
		queries = append(queries, name+" deployedBytecode extension:json")
	}
	for _, q := range queries {
		s, err := f.GitHub.SearchCode(ctx, q, MaxHits)
		if err != nil {
			return err
		}
		r.Search.Searches = append(r.Search.Searches, s)
		for _, hit := range s.Hits {
			// Only an artifact's deployed bytecode can be a reference, so a file of another
			// kind, such as a README that names an address, isn't fetched.
			if path.Ext(hit.Path) != ".json" {
				r.NotReferences = append(r.NotReferences, NotReference{
					Owner: hit.Owner, Repo: hit.Repo, Path: hit.Path, From: FromSearch,
					Reason: "not a JSON build artifact, so not fetched: only an artifact's deployed bytecode can be a reference, and an address that a file names never is",
				})
				continue
			}
			commit := hit.Ref
			if !IsCommitSHA(commit) {
				resolved, err := f.GitHub.Commit(ctx, hit.Owner, hit.Repo, cmp.Or(hit.Ref, "HEAD"))
				if Transient(err) {
					return err
				} else if err != nil {
					r.NotReferences = append(r.NotReferences, NotReference{
						Owner: hit.Owner, Repo: hit.Repo, Path: hit.Path, From: FromSearch,
						Reason: fmt.Sprintf("its commit %q couldn't be resolved: %v", hit.Ref, err),
					})
					continue
				}
				commit = resolved
			}
			if err := compare(Candidate{Owner: hit.Owner, Repo: hit.Repo, Commit: commit, Path: hit.Path}, FromSearch); err != nil {
				return err
			}
		}
	}
	return nil
}

// notes returns the limits of a report, as sentences.
func notes(r *Report) []string {
	notes := []string{
		"GitHub is an offchain source (§ 3.4): a reference shows only that a repository holds, at a commit, a build artifact whose deployed bytecode is the contract's code, which anyone can check against the chain. Nothing else in the repository, such as an address that a README names, is evidence, and no repository is listed because an address appears in it.",
	}
	if len(r.References) == 0 {
		notes = append(notes, "No reference found is never adverse evidence: a contract's sources may not be on GitHub, may be committed without build artifacts, or may be compiled with other settings, and code search doesn't index every repository.")
	}
	for _, ref := range r.References {
		if !ref.MetadataMatch {
			notes = append(notes, "A reference without metadataMatch has the same code but another metadata hash: the code matches, but not necessarily the sources or settings, such as comments, file paths, or the compiler's settings.")
			break
		}
	}
	for _, ref := range r.References {
		if ref.Kind == ImmutablesMasked {
			notes = append(notes, "A match with immutables-masked compares every byte but the values of the immutables and library addresses, which the deployment sets: the reference shows the code, not those values, which the chain has.")
			break
		}
	}
	for _, s := range r.Search.Searches {
		if s.TotalCount > len(s.Hits) || s.Incomplete {
			notes = append(notes, fmt.Sprintf("A search found more files than it compared, or timed out: only the first %d of each are compared.", MaxHits))
			break
		}
	}
	if r.Search.Available && len(r.Search.Searches) > 1 {
		notes = append(notes, "The search by the contract's name finds artifacts of any contract of that name, which are references only if their bytecode matches.")
	}
	if r.Metadata != nil && strings.HasPrefix(r.Metadata.Kind, "bzzr") {
		notes = append(notes, "The metadata hash is a Swarm hash, which solc appended before 0.6.0.")
	}
	return notes
}
