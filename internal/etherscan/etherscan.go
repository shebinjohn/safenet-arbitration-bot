// Package etherscan is a client for Etherscan's multichain API, which indexes
// the accounts, transactions, token transfers, and verified contracts of many
// networks.
//
// Etherscan is an offchain source (Charter § 3.4): what it returns is its own
// index, which callers check against onchain data where they can, such as the
// block that a contract was created in. The API needs a key, which is a secret:
// the client never puts it in an error or a URL that it returns, so that no
// output of arbot can leak it.
package etherscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/httpretry"
)

// DefaultURL is the URL of Etherscan's V2 multichain API, which takes the chain
// ID as the chainid query parameter.
const DefaultURL = "https://api.etherscan.io/v2/api"

// Source is the name of the source of every result of the client, as the
// commands that report its results label them.
const Source = "etherscan"

// redacted replaces the API key in the URLs that the client returns.
const redacted = "REDACTED"

// maxResponseSize is the largest response that the client accepts: a page of a
// listing, or a contract's sources, which getsourcecode returns whole.
const maxResponseSize = 32 << 20

// Retries of a request that Etherscan rate limits, which it does with HTTP
// status 200 and a result that says so, rather than with 429, so httpretry
// doesn't see it: up to rateLimitRetries times, waiting rateLimitDelay,
// doubling for each retry up to rateLimitMaxDelay, with jitter.
var (
	rateLimitRetries  = 4
	rateLimitDelay    = 1 * time.Second
	rateLimitMaxDelay = 30 * time.Second
)

// minInterval is the least time between two requests of a client. Etherscan's
// free plan allows a few requests a second, and a listing's pages are requested
// one after another, so the client spaces them rather than be rate limited.
var minInterval = 400 * time.Millisecond

// Error is a request to Etherscan that failed. Its message never has the API
// key in it, even if the error that caused it did, such as a *url.Error, which
// has the request's URL, so it doesn't wrap that error.
type Error struct {
	message   string
	transient bool
}

func (e *Error) Error() string {
	return e.message
}

// Transient reports whether err is, or wraps, a request to Etherscan that
// failed rather than being answered, such as for a rate limit, a server error,
// or a dropped connection. Such a failure says nothing about the account, so it
// must never be read as an empty listing or an unverified contract: the command
// fails, and can be run again later.
func Transient(err error) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.transient
}

// Client sends requests to the Etherscan API for one chain, one at a time.
type Client struct {
	http    *http.Client
	baseURL string
	key     string
	chainID uint64

	mu   sync.Mutex
	last time.Time
}

// NewClient returns a client for the API at baseURL, such as DefaultURL, that
// reads the chain with the ID chainID with the API key key.
func NewClient(baseURL, key string, chainID uint64) *Client {
	return &Client{http: httpretry.Client, baseURL: baseURL, key: key, chainID: chainID}
}

// ChainID returns the ID of the chain that the client reads.
func (c *Client) ChainID() uint64 {
	return c.chainID
}

// Observation is where and when a result of the client was observed: the URL
// that it was fetched from, with the API key replaced by REDACTED, and the time
// of the response, in UTC.
type Observation struct {
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
}

// requestURL returns the URL of a request with the query parameters params, and
// the same URL with the key redacted, which is the only one that the client
// returns or puts in an error.
func (c *Client) requestURL(params url.Values) (full, shown string, err error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", "", &Error{message: fmt.Sprintf("invalid Etherscan URL: %s", c.redact(err.Error()))}
	}
	query := u.Query()
	for name, values := range params {
		query[name] = values
	}
	query.Set("chainid", strconv.FormatUint(c.chainID, 10))
	query.Set("apikey", c.key)
	u.RawQuery = query.Encode()
	full = u.String()
	query.Set("apikey", redacted)
	u.RawQuery = query.Encode()
	return full, u.String(), nil
}

// redact returns s with the API key replaced by REDACTED, as it is and as a
// query parameter encodes it, for a message that may quote a URL or a body.
func (c *Client) redact(s string) string {
	if c.key == "" {
		return s
	}
	s = strings.ReplaceAll(s, c.key, redacted)
	return strings.ReplaceAll(s, url.QueryEscape(c.key), redacted)
}

// response is the envelope of every answer of the API: a status of "1" with the
// result, or "0" with a message, and a result that is empty or explains the
// error.
type response struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

// errEmpty is the answer of a request that has no results, such as a listing of
// an account without transactions, which the API answers with status "0".
var errEmpty = errors.New("no results")

// get sends a request with the query parameters params, and decodes its result
// into v. It returns errEmpty for an answer without results, retries an answer
// that rate limits the client, and returns an *Error for any failure, with the
// redacted URL.
func (c *Client) get(ctx context.Context, params url.Values, v any) (Observation, error) {
	full, shown, err := c.requestURL(params)
	if err != nil {
		return Observation{}, err
	}
	delay := rateLimitDelay
	for retry := 0; ; retry++ {
		r, observed, err := c.send(ctx, full, shown)
		if err != nil {
			return Observation{}, err
		}
		obs := Observation{URL: shown, ObservedAt: observed}
		if r.Status == "1" {
			if err := json.Unmarshal(r.Result, v); err != nil {
				return obs, &Error{message: fmt.Sprintf("Etherscan: %s: decoding the result: %s", shown, c.redact(err.Error()))}
			}
			return obs, nil
		}
		text := c.redact(resultText(r))
		switch {
		case empty(r):
			return obs, errEmpty
		case !rateLimited(text):
			return obs, &Error{message: fmt.Sprintf("Etherscan: %s: %s", shown, text)}
		case retry == rateLimitRetries:
			return obs, &Error{message: fmt.Sprintf("Etherscan: %s: rate limited: %s", shown, text), transient: true}
		}
		wait := delay/2 + rand.N(delay/2+1)
		delay = min(2*delay, rateLimitMaxDelay)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Observation{}, &Error{message: fmt.Sprintf("Etherscan: %s: %v", shown, ctx.Err()), transient: true}
		case <-timer.C:
		}
	}
}

// send sends the request for the URL full once requests are minInterval apart,
// and returns its answer and when it was received. It returns errors with the
// URL shown, without the key.
func (c *Client) send(ctx context.Context, full, shown string) (*response, time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := time.Until(c.last.Add(minInterval)); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: %v", shown, ctx.Err()), transient: true}
		case <-timer.C:
		}
	}
	defer func() { c.last = time.Now() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: %s", shown, c.redact(err.Error()))}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// A *url.Error has the URL with the key, and the error that it wraps may too,
		// so only its redacted message is kept. A failure to connect, a dropped
		// connection, or a timeout says nothing about the account.
		return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: %s", shown, c.redact(cause(err).Error())), transient: true}
	}
	defer resp.Body.Close()
	observed := time.Now().UTC().Truncate(time.Second)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: reading the response: %s", shown, c.redact(cause(err).Error())), transient: true}
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// httpretry has retried it already.
		return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: HTTP %s: %q", shown, resp.Status, c.redact(snippet(body))), transient: true}
	case resp.StatusCode != http.StatusOK:
		return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: HTTP %s: %q", shown, resp.Status, c.redact(snippet(body)))}
	case len(body) > maxResponseSize:
		return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: response exceeds %d bytes", shown, maxResponseSize)}
	}
	var r response
	if err := json.Unmarshal(body, &r); err != nil || (r.Status != "0" && r.Status != "1") {
		return nil, time.Time{}, &Error{message: fmt.Sprintf("Etherscan: %s: not an answer of the API: %q", shown, c.redact(snippet(body)))}
	}
	return &r, observed, nil
}

// cause returns the error that a *url.Error wraps, without its URL.
func cause(err error) error {
	if u, ok := errors.AsType[*url.Error](err); ok {
		return u.Err
	}
	return err
}

// resultText returns the message of an answer with status "0", with its result
// if that is a string, such as "NOTOK: Invalid API Key (#err2)".
func resultText(r *response) string {
	var result string
	if json.Unmarshal(r.Result, &result) == nil && result != "" {
		return r.Message + ": " + result
	}
	return r.Message
}

// empty reports whether an answer with status "0" is one without results, such
// as "No transactions found" or "No data found", rather than an error.
func empty(r *response) bool {
	return strings.HasPrefix(r.Message, "No ") && strings.HasSuffix(r.Message, " found")
}

// rateLimited reports whether the text of an answer with status "0" says that
// the client was rate limited, such as "NOTOK: Max calls per sec rate limit
// reached (3/sec)" or "NOTOK: Max rate limit reached".
func rateLimited(text string) bool {
	return strings.Contains(strings.ToLower(text), "rate limit")
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
