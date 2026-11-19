// Package webfetch fetches web pages from an allowlist of hosts, to keep a
// snapshot of what a page said, and the Ethereum addresses that it names.
//
// A web page is an offchain source (Charter § 3.4) that anyone can write, such
// as one that names a fake "official" address, or text aimed at the agents that
// read it. So the package never returns a page's text: only the facts of the
// fetch, the snapshot's hash, and the addresses that the page names, which are
// leads, never facts, until they are confirmed onchain.
package webfetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/httpretry"
)

// MaxSize is the largest page that Fetch reads. A larger page is refused,
// rather than saved in part.
const MaxSize = 10 << 20

// maxRedirects is the most redirects that Fetch follows.
const maxRedirects = 10

// ErrNotAllowed is the error for a URL that isn't HTTPS on an allowed host.
var ErrNotAllowed = errors.New("not allowed")

// Error is an error of a fetch.
type Error struct {
	message string
	// transient is whether the request failed rather than being answered.
	transient bool
}

func (e *Error) Error() string {
	return e.message
}

// Transient reports whether err is, or wraps, a fetch that failed rather than
// being answered, such as for a rate limit, a server error, or a dropped
// connection.
func Transient(err error) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.transient
}

// Fetcher fetches pages from the hosts of an allowlist.
type Fetcher struct {
	// Allow are the hosts that it fetches from, exactly, such as "github.com",
	// which doesn't allow "www.github.com". A host with a port, such as
	// "localhost:8443", allows that port, and one without only the default port.
	Allow []string
	// Base sends the requests, or http.DefaultTransport if nil, through
	// httpretry.Transport.
	Base http.RoundTripper
	// Delay is the time before the first retry of httpretry.Transport, or its
	// default if zero.
	Delay time.Duration
}

// Page is a page that Fetch fetched.
type Page struct {
	// URL is the URL asked for, and FinalURL the one that answered, after
	// Redirects, the URLs that redirected to the next.
	URL       string   `json:"url"`
	FinalURL  string   `json:"finalUrl"`
	Redirects []string `json:"redirects"`
	// ObservedAt is when the request was sent.
	ObservedAt  time.Time `json:"observedAt"`
	Status      int       `json:"status"`
	ContentType string    `json:"contentType"`
	Size        int       `json:"size"`
	// SHA256 is the SHA-256 of the body, in hex.
	SHA256 string `json:"sha256"`
	Body   []byte `json:"-"`
}

// allowed checks that u is an HTTPS URL on an allowed host, without
// credentials.
func (f *Fetcher) allowed(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("%w: %s isn't an HTTPS URL", ErrNotAllowed, u.Redacted())
	}
	if u.User != nil {
		return fmt.Errorf("%w: %s has credentials", ErrNotAllowed, u.Redacted())
	}
	host := strings.ToLower(u.Host)
	if port := u.Port(); port == "443" {
		host = strings.ToLower(u.Hostname())
	}
	for _, a := range f.Allow {
		if strings.ToLower(a) == host {
			return nil
		}
	}
	return fmt.Errorf("%w: the host %s isn't on the allowlist (sources.allow)", ErrNotAllowed, host)
}

// Fetch fetches the page at rawURL, which must be an HTTPS URL on an allowed
// host, following redirects only to such URLs. It fails for a page larger than
// MaxSize. A status that isn't 200 OK is reported, not an error, unless it is
// one that a server fails with rather than answering, such as 429 Too Many
// Requests or a 5xx, after httpretry's retries.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	if err := f.allowed(u); err != nil {
		return nil, err
	}
	page := &Page{URL: u.String(), Redirects: []string{}}
	// refused is whether a redirect was refused, which is an answer, not a failure
	// to get one.
	refused := false
	client := &http.Client{
		Transport: &httpretry.Transport{Base: f.Base, Delay: f.Delay},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				refused = true
				return fmt.Errorf("more than %d redirects", maxRedirects)
			}
			// Each hop must be allowed, so that an allowed host can't send the fetch to one
			// that isn't.
			if err := f.allowed(req.URL); err != nil {
				refused = true
				return fmt.Errorf("redirect from %s refused: %w", via[len(via)-1].URL.Redacted(), err)
			}
			page.Redirects = append(page.Redirects, via[len(via)-1].URL.String())
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	page.ObservedAt = time.Now().UTC().Truncate(time.Second)
	resp, err := client.Do(req)
	if refused {
		return nil, err
	} else if err != nil {
		return nil, &Error{message: err.Error(), transient: ctx.Err() == nil}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxSize+1))
	if err != nil {
		return nil, &Error{message: fmt.Sprintf("%s: reading the page: %v", resp.Request.URL.Redacted(), err), transient: true}
	}
	if len(body) > MaxSize {
		return nil, fmt.Errorf("%s: the page is larger than %d bytes, so nothing is saved", resp.Request.URL.Redacted(), MaxSize)
	}
	if s := resp.StatusCode; s == http.StatusTooManyRequests || s >= 500 {
		return nil, &Error{message: fmt.Sprintf("%s: HTTP %s", resp.Request.URL.Redacted(), resp.Status), transient: true}
	}
	sum := sha256.Sum256(body)
	page.FinalURL = resp.Request.URL.String()
	page.Status, page.ContentType = resp.StatusCode, resp.Header.Get("Content-Type")
	page.Size, page.SHA256, page.Body = len(body), hex.EncodeToString(sum[:]), body
	return page, nil
}

// Lead is an address that a page names, verbatim: a lead to check onchain,
// never a fact.
type Lead struct {
	Address string `json:"address"`
	// Label is always "lead".
	Label string `json:"label"`
	// Occurrences is the number of times that the page has it, in this case.
	Occurrences int `json:"occurrences"`
}

// Addresses returns the Ethereum addresses that body names, 0x and 40 hex
// digits, not part of a longer word or hex value, such as a hash, in the order
// of their first occurrence, each as it is written, so that addresses that
// differ only in case are listed apart.
func Addresses(body []byte) []Lead {
	var leads []Lead
	for i := 0; i+42 <= len(body); i++ {
		if body[i] != '0' || (body[i+1] != 'x' && body[i+1] != 'X') {
			continue
		}
		if i > 0 && isWord(body[i-1]) {
			continue
		}
		end := i + 2
		for end < len(body) && isHex(body[end]) {
			end++
		}
		if end-i != 42 || (end < len(body) && isWord(body[end])) {
			continue
		}
		address := string(body[i:end])
		if j := slices.IndexFunc(leads, func(l Lead) bool { return l.Address == address }); j >= 0 {
			leads[j].Occurrences++
		} else {
			leads = append(leads, Lead{Address: address, Label: "lead", Occurrences: 1})
		}
		i = end - 1
	}
	return leads
}

func isHex(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'f' || 'A' <= b && b <= 'F'
}

func isWord(b byte) bool {
	return isHex(b) || 'g' <= b && b <= 'z' || 'G' <= b && b <= 'Z' || b == '_'
}
