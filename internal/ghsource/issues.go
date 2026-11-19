package ghsource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// CharterOwner and CharterRepo are the repository of the Safenet Arbitration
// Charter, whose issues discuss changes to it.
const (
	CharterOwner = "safe-research"
	CharterRepo  = "safenet-charter"
)

// IssueLabel is what an issue of the Charter's repository is: a discussion of
// the Charter, which may change how it is read, but isn't part of it.
const IssueLabel = "interpretive context: pending discussions, never evidence and never rules"

// maxIssuePages is the most pages of issues that Issues reads, of 100 issues
// each, so that a server whose pages never end can't keep it reading.
const maxIssuePages = 50

// Issue is the metadata of an issue: never its body or comments, which anyone
// can write, and which may carry instructions aimed at the agents.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	// State is "open" or "closed".
	State  string   `json:"state"`
	Labels []string `json:"labels"`
	// URL is the issue's page on GitHub.
	URL string `json:"url"`
}

// Issues is the list of a repository's issues.
type Issues struct {
	// URL is the URL of the first page, and ObservedAt when it was fetched.
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
	// Pages is the number of pages read.
	Pages  int     `json:"pages"`
	Issues []Issue `json:"issues"`
}

// nextLink matches the URL of the next page in a Link header.
var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// Issues lists the issues of a repository in state, "open", "closed", or "all",
// in the order that GitHub lists them, newest first, without its pull requests,
// which the API lists as issues too. It follows the pages that the Link header
// names, only on the API's own URL. It works without a token, at a lower rate
// limit.
func (c *Client) Issues(ctx context.Context, owner, repo, state string) (*Issues, error) {
	if err := checkRepo(owner, repo); err != nil {
		return nil, err
	}
	u := c.apiURL("/repos/"+owner+"/"+repo+"/issues", url.Values{"state": {state}, "per_page": {"100"}})
	list := &Issues{URL: u, Issues: []Issue{}}
	for next := u; next != ""; {
		if list.Pages == maxIssuePages {
			return nil, fmt.Errorf("GitHub: %s: more than %d pages of issues", u, maxIssuePages)
		}
		r, err := c.get(ctx, next, "application/vnd.github+json", true)
		if err != nil {
			return nil, err
		}
		if list.Pages == 0 {
			list.ObservedAt = r.observedAt
		}
		list.Pages++
		var page []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
			HTMLURL     string          `json:"html_url"`
			PullRequest json.RawMessage `json:"pull_request"`
		}
		if err := json.Unmarshal(r.body, &page); err != nil {
			return nil, fmt.Errorf("GitHub: %s: decoding the issues: %w", next, err)
		}
		for _, item := range page {
			if item.PullRequest != nil && string(item.PullRequest) != "null" {
				continue
			}
			issue := Issue{Number: item.Number, Title: item.Title, State: item.State, Labels: []string{}, URL: item.HTMLURL}
			for _, l := range item.Labels {
				issue.Labels = append(issue.Labels, l.Name)
			}
			list.Issues = append(list.Issues, issue)
		}

		next = ""
		if m := nextLink.FindStringSubmatch(r.header.Get("Link")); m != nil {
			// The token is only sent to the API, so a next page elsewhere is refused.
			if !strings.HasPrefix(m[1], c.APIURL+"/") {
				return nil, fmt.Errorf("GitHub: %s: the next page %q isn't on %s", u, m[1], c.APIURL)
			}
			next = m[1]
		}
	}
	return list, nil
}
