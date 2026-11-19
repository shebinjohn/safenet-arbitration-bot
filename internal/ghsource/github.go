// Package ghsource finds the build artifacts on GitHub that are the source of a
// contract's code, by its bytecode, and lists the issues of a repository.
//
// GitHub is an offchain source (Charter § 3.4), and anyone can commit anything
// to it, such as a README that names a fake "official" address, or text aimed
// at the agents that read it. So the package never reads a repository's text as
// evidence: a repository is a reference for a contract only if it has a build
// artifact, fetched at a commit, whose deployed bytecode is the contract's
// code, which anyone can check against the chain. An address that a repository
// names is never a reference, and the package never searches by one. Issues are
// listed by their metadata only, without their bodies.
package ghsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/httpretry"
)

// DefaultAPIURL is the base URL of GitHub's REST API.
const DefaultAPIURL = "https://api.github.com"

// DefaultRawURL is the base URL that GitHub serves the files of public
// repositories from, by owner, repository, commit, and path.
const DefaultRawURL = "https://raw.githubusercontent.com"

// maxResponseSize is the largest response that the client reads: a build
// artifact has the contract's ABI, code, and source map, and sometimes its
// metadata and sources, which are a few megabytes for a large contract.
const maxResponseSize = 32 << 20

// ErrNotFound is the error for a file or commit that GitHub doesn't have.
var ErrNotFound = errors.New("not found")

// ErrNoToken is the error of a request that needs a token, such as a code
// search, from a client without one.
var ErrNoToken = errors.New("GitHub code search needs authentication, and no github.token is configured")

// Error is an error of a request to GitHub.
type Error struct {
	message string
	// transient is whether the request failed rather than being answered.
	transient bool
}

func (e *Error) Error() string {
	return e.message
}

// Transient reports whether err is, or wraps, a request to GitHub that failed
// rather than being answered, such as for a rate limit, a server error, or a
// dropped connection. It says nothing about GitHub's content, so a caller must
// not read it as "no match" or "not found", and it can be tried again later.
func Transient(err error) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.transient
}

// Client reads GitHub's REST API, and the files of public repositories.
type Client struct {
	// APIURL is the base URL of the REST API, such as DefaultAPIURL.
	APIURL string
	// RawURL is the base URL of the files of repositories, such as DefaultRawURL.
	RawURL string
	// token is the token that authenticates requests to the API, or "" for none. It
	// is a secret: it is only sent to APIURL, in the Authorization header, and
	// never appears in a URL or an error.
	token string
	http  *http.Client
}

// NewClient returns a client of the API at apiURL and the files at rawURL,
// which authenticates its requests to the API with token, unless it is empty.
func NewClient(apiURL, rawURL, token string) *Client {
	return &Client{APIURL: strings.TrimSuffix(apiURL, "/"), RawURL: strings.TrimSuffix(rawURL, "/"), token: token, http: httpretry.Client}
}

// HasToken reports whether the client authenticates its requests to the API.
func (c *Client) HasToken() bool {
	return c.token != ""
}

// response is a response of GitHub that the client read.
type response struct {
	body   []byte
	header http.Header
	// observedAt is when the request was sent.
	observedAt time.Time
}

// get fetches u, with the token if api is true, and returns its body if its
// status is 200 OK. It fails with an *Error, which is transient for a response
// that rate limits the client or fails on GitHub's side, and wraps ErrNotFound
// for 404 Not Found.
func (c *Client) get(ctx context.Context, u, accept string, api bool) (*response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if api {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	}
	r := &response{observedAt: time.Now().UTC().Truncate(time.Second)}
	resp, err := c.http.Do(req)
	if err != nil {
		// A failure to connect, a dropped connection, or a timeout, unless the caller
		// gave up.
		return nil, &Error{message: fmt.Sprintf("GitHub: %s: %v", u, c.redact(err.Error())), transient: ctx.Err() == nil}
	}
	defer resp.Body.Close()
	r.header = resp.Header
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, &Error{message: fmt.Sprintf("GitHub: %s: reading the response: %v", u, c.redact(err.Error())), transient: true}
	}
	switch status := resp.StatusCode; {
	case status == http.StatusOK && len(body) > maxResponseSize:
		return nil, &Error{message: fmt.Sprintf("GitHub: %s: the response exceeds %d bytes", u, maxResponseSize)}
	case status == http.StatusOK:
		r.body = body
		return r, nil
	case status == http.StatusNotFound:
		return nil, fmt.Errorf("GitHub: %s: %w", u, ErrNotFound)
	case rateLimited(resp):
		// GitHub rate limits with 403 Forbidden as well as 429, and httpretry only
		// retries the latter, so a rate limit is a failure to get an answer, never an
		// answer, such as "no match".
		return nil, &Error{message: fmt.Sprintf("GitHub: %s: rate limited (HTTP %s); run the command again later", u, resp.Status), transient: true}
	case status >= 500:
		return nil, &Error{message: fmt.Sprintf("GitHub: %s: HTTP %s", u, resp.Status), transient: true}
	case status == http.StatusUnauthorized:
		return nil, &Error{message: fmt.Sprintf("GitHub: %s: HTTP %s: the configured github.token was rejected", u, resp.Status)}
	}
	return nil, &Error{message: fmt.Sprintf("GitHub: %s: HTTP %s: %q", u, resp.Status, c.redact(snippet(body)))}
}

// rateLimited reports whether resp rate limits the client: a 429 Too Many
// Requests, or a 403 Forbidden with no requests remaining or a Retry-After
// header, as GitHub sends for its primary and secondary rate limits.
func rateLimited(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
	}
	return false
}

// redact removes the token from s, in case an error quotes it.
func (c *Client) redact(s string) string {
	if c.token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.token, "REDACTED")
}

// snippet returns the start of a response body, for an error message, which
// quotes it since it isn't trusted.
func snippet(body []byte) string {
	const limit = 200
	if len(body) > limit {
		body = body[:limit]
	}
	return string(bytes.TrimSpace(body))
}

// apiURL returns the URL of the API's path, with query.
func (c *Client) apiURL(path string, query url.Values) string {
	u := c.APIURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// Hit is a file that a code search found.
type Hit struct {
	Owner, Repo, Path string
	// Ref is the commit that the search indexed the file at, as the search names
	// it, which is usually a commit SHA.
	Ref string
}

// Search is the result of a code search.
type Search struct {
	Query      string    `json:"query"`
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
	// TotalCount is the number of files that GitHub found, and Incomplete is
	// whether it timed out before searching every repository.
	TotalCount int  `json:"totalCount"`
	Incomplete bool `json:"incomplete"`
	// Hits are the files of the first page of results.
	Hits []Hit `json:"-"`
}

// SearchCode searches the code on GitHub for query, and returns the first
// perPage files that it finds, in GitHub's order of relevance. It needs a
// token, since code search does: without one, it fails with ErrNoToken.
func (c *Client) SearchCode(ctx context.Context, query string, perPage int) (*Search, error) {
	if c.token == "" {
		return nil, ErrNoToken
	}
	u := c.apiURL("/search/code", url.Values{"q": {query}, "per_page": {strconv.Itoa(perPage)}})
	r, err := c.get(ctx, u, "application/vnd.github+json", true)
	if err != nil {
		return nil, err
	}
	var result struct {
		TotalCount        int  `json:"total_count"`
		IncompleteResults bool `json:"incomplete_results"`
		Items             []struct {
			Path string `json:"path"`
			// URL is the file's URL in the contents API, whose ref parameter is the commit
			// that the search indexed.
			URL        string `json:"url"`
			Repository struct {
				Name  string `json:"name"`
				Owner struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"repository"`
		} `json:"items"`
	}
	if err := json.Unmarshal(r.body, &result); err != nil {
		return nil, fmt.Errorf("GitHub: %s: decoding the search: %w", u, err)
	}
	s := &Search{Query: query, URL: u, ObservedAt: r.observedAt, TotalCount: result.TotalCount, Incomplete: result.IncompleteResults}
	for _, item := range result.Items {
		hit := Hit{Owner: item.Repository.Owner.Login, Repo: item.Repository.Name, Path: item.Path}
		if contents, err := url.Parse(item.URL); err == nil {
			hit.Ref = contents.Query().Get("ref")
		}
		s.Hits = append(s.Hits, hit)
	}
	return s, nil
}

// commitSHA matches a full commit SHA, which names a commit, and so the files
// at it, for good, unlike a branch or a tag.
var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// IsCommitSHA reports whether ref is a full commit SHA, in lowercase.
func IsCommitSHA(ref string) bool {
	return commitSHA.MatchString(ref)
}

// Commit returns the SHA of the commit that ref names in a repository, such as
// a branch, so that its files are fetched at the commit, which can't change.
func (c *Client) Commit(ctx context.Context, owner, repo, ref string) (string, error) {
	if err := checkRepo(owner, repo); err != nil {
		return "", err
	}
	u := c.apiURL("/repos/"+owner+"/"+repo+"/commits/"+url.PathEscape(ref), nil)
	r, err := c.get(ctx, u, "application/vnd.github+json", true)
	if err != nil {
		return "", err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(r.body, &commit); err != nil {
		return "", fmt.Errorf("GitHub: %s: decoding the commit: %w", u, err)
	}
	if !IsCommitSHA(commit.SHA) {
		return "", fmt.Errorf("GitHub: %s: the commit has no SHA", u)
	}
	return commit.SHA, nil
}

// File is a file of a repository at a commit.
type File struct {
	Owner, Repo, Commit, Path string
	URL                       string
	ObservedAt                time.Time
	Content                   []byte
}

// File fetches the file at path in a repository at a commit, which must be a
// full commit SHA, never a branch, whose files can change.
func (c *Client) File(ctx context.Context, owner, repo, commit, path string) (*File, error) {
	if err := checkRepo(owner, repo); err != nil {
		return nil, err
	}
	if !IsCommitSHA(commit) {
		return nil, fmt.Errorf("commit %q isn't a full commit SHA", commit)
	}
	if err := checkPath(path); err != nil {
		return nil, err
	}
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	u := c.RawURL + "/" + owner + "/" + repo + "/" + commit + "/" + strings.Join(segments, "/")
	// The token is only for the API, so a file of a public repository is fetched
	// without it.
	r, err := c.get(ctx, u, "*/*", false)
	if err != nil {
		return nil, err
	}
	return &File{Owner: owner, Repo: repo, Commit: commit, Path: path, URL: u, ObservedAt: r.observedAt, Content: r.body}, nil
}

// name matches the names of GitHub owners and repositories.
var name = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func checkRepo(owner, repo string) error {
	if !name.MatchString(owner) || !name.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return fmt.Errorf("invalid repository %q", owner+"/"+repo)
	}
	return nil
}

// checkPath checks that path is a relative path of a file in a repository,
// which can't name a file outside it.
func checkPath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") {
		return fmt.Errorf("invalid path %q: want the path of a file in the repository", path)
	}
	for _, s := range strings.Split(path, "/") {
		if s == "" || s == "." || s == ".." {
			return fmt.Errorf("invalid path %q: want the path of a file in the repository", path)
		}
	}
	return nil
}
