package etherscan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// testKey is the API key of the tests, which no output may contain.
const testKey = "TESTKEY"

var (
	recipient = ethrpc.MustParseAddress("0x316473f1E8bCD1549527d07A9B68B545f0D2976e")
	helper    = ethrpc.MustParseAddress("0xe530c247341e857042FdFf953E73Dd6c580f8D42")
	usdc      = ethrpc.MustParseAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
)

// fast makes the client's waits short for a test.
func fast(t *testing.T) {
	delay, interval := rateLimitDelay, minInterval
	rateLimitDelay, minInterval = time.Millisecond, 0
	t.Cleanup(func() { rateLimitDelay, minInterval = delay, interval })
}

// fixture returns the recorded answer in testdata/name.json.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// api is a fake Etherscan API, which answers each request with answer, and
// records the requests' queries.
type api struct {
	t      *testing.T
	answer func(query map[string]string) (status int, body []byte)

	mu      sync.Mutex
	queries []map[string]string
}

func (a *api) serve() string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := map[string]string{}
		for name, values := range r.URL.Query() {
			query[name] = values[0]
		}
		if query["apikey"] != testKey || query["chainid"] != "1" {
			a.t.Errorf("got query %v, want the test key and chain 1", query)
		}
		a.mu.Lock()
		a.queries = append(a.queries, query)
		a.mu.Unlock()
		status, body := a.answer(query)
		w.WriteHeader(status)
		w.Write(body)
	}))
	a.t.Cleanup(server.Close)
	return server.URL + "/v2/api"
}

// fixed returns a fake API that answers every request with the fixture name.
func fixed(t *testing.T, name string) (*api, *Client) {
	body := fixture(t, name)
	a := &api{t: t, answer: func(map[string]string) (int, []byte) { return http.StatusOK, body }}
	return a, NewClient(a.serve(), testKey, 1)
}

// noKey fails the test if s has the API key in it.
func noKey(t *testing.T, what string, s string) {
	t.Helper()
	if strings.Contains(s, testKey) {
		t.Errorf("%s has the API key: %s", what, s)
	}
}

// checkObservation checks that obs has the redacted URL of the request.
func checkObservation(t *testing.T, obs Observation, action string) {
	t.Helper()
	noKey(t, "the URL", obs.URL)
	if !strings.Contains(obs.URL, "apikey=REDACTED") || !strings.Contains(obs.URL, "action="+action) || !strings.Contains(obs.URL, "chainid=1") {
		t.Errorf("got URL %q, want the request's, with the key redacted", obs.URL)
	}
	if obs.ObservedAt.IsZero() || obs.ObservedAt.Location() != time.UTC {
		t.Errorf("got observation time %v, want one in UTC", obs.ObservedAt)
	}
}

func TestCreation(t *testing.T) {
	fast(t)
	a, c := fixed(t, "creation")
	creation, obs, err := c.Creation(t.Context(), helper)
	if err != nil {
		t.Fatal(err)
	}
	checkObservation(t, obs, "getcontractcreation")
	factory := ethrpc.MustParseAddress("0x9b35af71d77eaf8d7e40252370304687390a1a52")
	want := Creation{
		Creator: ethrpc.MustParseAddress("0x46565eedb0bb948ae787dd105cce300f736da28e"), Factory: &factory,
		TxHash: ethrpc.MustParseHash("0x14fabd2a90182726c6e4127b227fa5126672b44084bfc2228faab933ea69adb1"),
		Block:  26432157, Time: time.Unix(1794887075, 0).UTC(),
	}
	if creation == nil || creation.Creator != want.Creator || creation.Factory == nil || *creation.Factory != factory ||
		creation.TxHash != want.TxHash || creation.Block != want.Block || !creation.Time.Equal(want.Time) {
		t.Errorf("got creation %+v, want %+v", creation, want)
	}
	if q := a.queries[0]; q["module"] != "contract" || q["contractaddresses"] != helper.String() {
		t.Errorf("got query %v", q)
	}

	// An account that Etherscan has no creation of, such as one without code, has
	// none, which isn't an error.
	_, c = fixed(t, "creation-none")
	if creation, _, err := c.Creation(t.Context(), recipient); err != nil || creation != nil {
		t.Errorf("Creation of an account without one: got %+v, %v; want none", creation, err)
	}

	// An answer about another contract says nothing about this one.
	_, c = fixed(t, "creation")
	if _, _, err := c.Creation(t.Context(), recipient); err == nil || Transient(err) {
		t.Errorf("Creation with an answer about another contract: got %v, want an error", err)
	}
}

func TestTransactions(t *testing.T) {
	fast(t)
	a, c := fixed(t, "txlist")
	listing, obs, err := c.Transactions(t.Context(), recipient, 26441835)
	if err != nil {
		t.Fatal(err)
	}
	checkObservation(t, obs, "txlist")
	if q := a.queries[0]; q["endblock"] != "26441835" || q["startblock"] != "0" || q["sort"] != "asc" || q["page"] != "1" || q["offset"] != "1000" {
		t.Errorf("got query %v", q)
	}
	if listing.Capped || len(listing.Results) != 3 {
		t.Fatalf("got %d results, capped %t; want 3, not capped", len(listing.Results), listing.Capped)
	}
	first, second := listing.Results[0], listing.Results[1]
	if first.Block != 15319293 || first.Nonce != 1 || first.Value.String() != "10000000000000000" || first.MethodID != "0x" ||
		first.InputSize != 0 || !first.Success || first.To == nil || *first.To != recipient || first.ContractAddress != nil {
		t.Errorf("got first transaction %+v", first)
	}
	if second.MethodID != "0x6a761202" || !strings.HasPrefix(second.FunctionName, "execTransaction(") || second.InputSize == 0 {
		t.Errorf("got second transaction %+v", second)
	}

	// An account without transactions has an empty listing, not an error.
	_, c = fixed(t, "txlist-empty")
	if listing, _, err := c.Transactions(t.Context(), recipient, 15000000); err != nil || len(listing.Results) != 0 || listing.Capped {
		t.Errorf("Transactions of an account without any: got %+v, %v; want none", listing, err)
	}

	// A result after the end block can't be evidence as of it.
	_, c = fixed(t, "txlist")
	if _, _, err := c.Transactions(t.Context(), recipient, 15319300); err == nil || !strings.Contains(err.Error(), "after the end block") {
		t.Errorf("Transactions with a result after the end block: got %v, want an error", err)
	}
}

// row is a transaction of the listing of a fake API, in the block n.
func row(n int) map[string]string {
	return map[string]string{
		"blockNumber": strconv.Itoa(n), "timeStamp": "1660201310", "hash": fmt.Sprintf("0x%064x", n), "nonce": "0",
		"transactionIndex": "0", "from": recipient.String(), "to": usdc.String(), "value": "0", "input": "0x", "isError": "0",
	}
}

func TestTransactionsPages(t *testing.T) {
	fast(t)
	for _, test := range []struct {
		total  int
		pages  int
		capped bool
	}{
		{total: 2500, pages: 3},
		// A listing that ends at a page's end needs one more request to see that it
		// ends there.
		{total: 2000, pages: 3},
		{total: 50_000, pages: 10, capped: true},
	} {
		a := &api{t: t}
		a.answer = func(q map[string]string) (int, []byte) {
			page, _ := strconv.Atoi(q["page"])
			offset, _ := strconv.Atoi(q["offset"])
			if page*offset > MaxResults {
				return http.StatusOK, fixture(t, "window")
			}
			var rows []map[string]string
			for n := (page - 1) * offset; n < min(page*offset, test.total); n++ {
				rows = append(rows, row(n+1))
			}
			if len(rows) == 0 {
				return http.StatusOK, fixture(t, "txlist-empty")
			}
			body, _ := json.Marshal(map[string]any{"status": "1", "message": "OK", "result": rows})
			return http.StatusOK, body
		}
		listing, _, err := NewClient(a.serve(), testKey, 1).Transactions(t.Context(), recipient, 1_000_000)
		if err != nil {
			t.Fatalf("%d transactions: %v", test.total, err)
		}
		want := min(test.total, MaxResults)
		if len(listing.Results) != want || listing.Capped != test.capped || len(a.queries) != test.pages {
			t.Errorf("%d transactions: got %d results, capped %t, in %d requests; want %d, %t, in %d",
				test.total, len(listing.Results), listing.Capped, len(a.queries), want, test.capped, test.pages)
		}
		if listing.Results[len(listing.Results)-1].Block != uint64(want) {
			t.Errorf("%d transactions: got last block %d, want %d", test.total, listing.Results[len(listing.Results)-1].Block, want)
		}
	}
}

func TestTokenTransfers(t *testing.T) {
	fast(t)
	_, c := fixed(t, "tokentx")
	listing, obs, err := c.TokenTransfers(t.Context(), recipient, 26441835)
	if err != nil {
		t.Fatal(err)
	}
	checkObservation(t, obs, "tokentx")
	if len(listing.Results) != 2 {
		t.Fatalf("got %d transfers, want 2", len(listing.Results))
	}
	first := listing.Results[0]
	if first.Block != 15319491 || first.Token != usdc || first.TokenSymbol != "USDC" || first.TokenDecimals == nil || *first.TokenDecimals != 6 ||
		first.To != recipient || first.Value.String() != "100000000" {
		t.Errorf("got first transfer %+v", first)
	}
}

func TestContract(t *testing.T) {
	fast(t)
	_, c := fixed(t, "source-verified")
	contract, obs, err := c.Contract(t.Context(), usdc)
	if err != nil {
		t.Fatal(err)
	}
	checkObservation(t, obs, "getsourcecode")
	implementation := ethrpc.MustParseAddress("0x43506849d7c04f9138d1a2050bbf3a0c054402dd")
	if !contract.Verified || contract.Name != "FiatTokenProxy" || contract.Compiler != "v0.4.24+commit.e67f0147" || contract.Runs != 200 ||
		contract.Optimization || !contract.Proxy || contract.Implementation == nil || *contract.Implementation != implementation {
		t.Errorf("got contract %+v", contract)
	}

	_, c = fixed(t, "source-unverified")
	contract, _, err = c.Contract(t.Context(), recipient)
	if err != nil || contract.Verified || contract.Name != "" || contract.Proxy {
		t.Errorf("Contract of an unverified account: got %+v, %v; want an unverified contract", contract, err)
	}
}

func TestRateLimit(t *testing.T) {
	fast(t)
	limited, ok := fixture(t, "rate-limit"), fixture(t, "txlist-empty")

	// A rate limit is retried, with HTTP status 200 as Etherscan sends it.
	a := &api{t: t}
	a.answer = func(map[string]string) (int, []byte) {
		if len(a.queries) <= 2 {
			return http.StatusOK, limited
		}
		return http.StatusOK, ok
	}
	if listing, _, err := NewClient(a.serve(), testKey, 1).Transactions(t.Context(), recipient, 1); err != nil || len(listing.Results) != 0 || len(a.queries) != 3 {
		t.Errorf("Transactions, rate limited twice: got %+v, %v, in %d requests; want no results in 3", listing, err, len(a.queries))
	}

	// One that lasts is a failure, not an empty listing.
	a, c := fixed(t, "rate-limit")
	listing, _, err := c.Transactions(t.Context(), recipient, 1)
	if listing != nil || !Transient(err) || !strings.Contains(err.Error(), "rate limit") || len(a.queries) != rateLimitRetries+1 {
		t.Errorf("Transactions, always rate limited: got %+v, %v, in %d requests; want a transient error after %d", listing, err, len(a.queries), rateLimitRetries+1)
	}
	if creation, _, err := c.Creation(t.Context(), helper); creation != nil || !Transient(err) {
		t.Errorf("Creation, always rate limited: got %+v, %v; want a transient error", creation, err)
	}
	if contract, _, err := c.Contract(t.Context(), helper); contract != nil || !Transient(err) {
		t.Errorf("Contract, always rate limited: got %+v, %v; want a transient error", contract, err)
	}
}

func TestErrors(t *testing.T) {
	fast(t)
	for _, test := range []struct {
		name      string
		status    int
		body      string
		transient bool
		message   string
	}{
		{name: "not supported", status: 200, body: string(fixture(t, "not-supported")), message: "Free API access is not supported for this chain"},
		{name: "invalid key", status: 200, body: string(fixture(t, "invalid-key")), message: "Invalid API Key"},
		{name: "window", status: 200, body: string(fixture(t, "window")), message: "Result window is too large"},
		// An answer that quotes the key has it redacted.
		{name: "key quoted", status: 200, body: `{"status":"0","message":"NOTOK","result":"Invalid API Key TESTKEY"}`, message: "Invalid API Key REDACTED"},
		{name: "server error", status: 500, body: "apikey=TESTKEY failed", transient: true, message: "HTTP 500"},
		{name: "forbidden", status: 403, body: "forbidden", message: "HTTP 403"},
		{name: "not JSON", status: 200, body: "<html>", message: "not an answer"},
	} {
		a := &api{t: t, answer: func(map[string]string) (int, []byte) { return test.status, []byte(test.body) }}
		_, _, err := NewClient(a.serve(), testKey, 1).Transactions(t.Context(), recipient, 1)
		if err == nil || Transient(err) != test.transient || !strings.Contains(err.Error(), test.message) {
			t.Errorf("%s: got %v, transient %t; want an error with %q, transient %t", test.name, err, Transient(err), test.message, test.transient)
		} else {
			noKey(t, test.name, err.Error())
		}
	}
}

func TestKeyNeverInOutput(t *testing.T) {
	fast(t)
	var outputs []string
	record := func(v any, obs Observation, err error) {
		data, _ := json.Marshal(v)
		outputs = append(outputs, string(data), obs.URL)
		if err != nil {
			outputs = append(outputs, err.Error(), fmt.Sprintf("%+v", err))
		}
	}
	for _, name := range []string{"creation", "creation-none", "txlist", "tokentx", "source-verified", "rate-limit", "invalid-key"} {
		_, c := fixed(t, name)
		creation, obs, err := c.Creation(t.Context(), helper)
		record(creation, obs, err)
		txs, obs, err := c.Transactions(t.Context(), recipient, 26441835)
		record(txs, obs, err)
		transfers, obs, err := c.TokenTransfers(t.Context(), recipient, 26441835)
		record(transfers, obs, err)
		contract, obs, err := c.Contract(t.Context(), usdc)
		record(contract, obs, err)
	}

	// A request that fails to connect has an error whose URL has the key, which the
	// client's error doesn't.
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	_, obs, err := NewClient(server.URL, testKey, 1).Contract(t.Context(), usdc)
	if !Transient(err) {
		t.Errorf("Contract of an API that can't be reached: got %v, want a transient error", err)
	}
	record(nil, obs, err)

	// An invalid base URL has its error redacted too.
	_, obs, err = NewClient("http://TESTKEY%zz", testKey, 1).Contract(t.Context(), usdc)
	if err == nil {
		t.Error("Contract with an invalid URL: got no error")
	}
	record(nil, obs, err)

	for _, output := range outputs {
		noKey(t, "an output", output)
	}
}

// TestFixturesHaveNoKey checks that the recorded answers have no API key, nor a
// URL that could carry one.
func TestFixturesHaveNoKey(t *testing.T) {
	files, err := filepath.Glob("testdata/*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if s := strings.ToLower(string(data)); strings.Contains(s, "apikey") || strings.Contains(s, "api.etherscan.io") {
			t.Errorf("%s has an API key or an Etherscan URL", file)
		}
	}
}
