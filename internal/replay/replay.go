// Package replay records the answers of nodes and public services to the
// requests that a test makes, and serves them again, so that tests of past
// arbitrations run on what the chains and services answered then, without the
// network.
//
// A fixture serves a recording: a JSON-RPC node for each chain, which answers
// each request with the answer recorded for its method and parameters, and an
// HTTP server for each service, such as the Safe Transaction Service or
// Sourcify, which answers each GET with the reply recorded for its path and
// query. A request that wasn't recorded fails the test, so a recording can't
// stand in for an answer that it doesn't have.
//
// To record, a fixture forwards each request to a live node or service, keeps
// its answer, and saves the recording when the test ends, if it passed. It
// records only what the test reads, and of an answer only the fields that arbot
// reads (Trim), so that recordings stay small, and it records no URL, so that a
// recording has no key that an RPC URL may carry.
package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/httpretry"
)

// Recording is what a fixture recorded: the JSON-RPC exchanges with each
// chain's node, by chain ID, and the replies of each service, by its name and
// then by the path and query of the request.
type Recording struct {
	RPC  map[string][]Exchange       `json:"rpc"`
	HTTP map[string]map[string]Reply `json:"http,omitempty"`
}

// Exchange is a JSON-RPC request and its answer: a result or an error, or, for
// an answer with another HTTP status than 200, the status and the body, such as
// for a block range that the node won't scan.
type Exchange struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
	Status int             `json:"status,omitempty"`
	Body   string          `json:"body,omitempty"`
}

// Reply is a service's reply to a GET: its status, if it isn't 200, such as 404
// for a Safe that the service doesn't know, and its body, as JSON if it is
// JSON, or else as a string.
type Reply struct {
	Status int             `json:"status,omitempty"`
	Body   json.RawMessage `json:"body"`
}

// Options are the options of a fixture.
type Options struct {
	// Record is whether to record the fixture from live nodes and services, rather
	// than replay it.
	Record bool
	// RPCs are the URLs of the nodes to record from, by chain ID.
	RPCs map[uint64]string
	// TrimResult returns the part of a JSON-RPC result to record. It is Trim if
	// nil.
	TrimResult func(method string, result json.RawMessage) json.RawMessage
	// TrimReply returns the part of a service's JSON reply to record, or the reply
	// as it is if nil.
	TrimReply func(service, path string, body json.RawMessage) json.RawMessage
}

// Fixture serves a recording, or records one, for a test.
type Fixture struct {
	t    testing.TB
	opts Options

	mu  sync.Mutex
	rec Recording
	// served counts the exchanges served by key, so that a request that was made
	// more than once, such as for the latest block, gets its answers in the order
	// recorded.
	served map[string]int
}

// Open returns a fixture that serves the recording in the file at path, or,
// with opts.Record, records it there when the test ends, if it passed.
func Open(t testing.TB, path string, opts Options) *Fixture {
	t.Helper()
	f := &Fixture{t: t, opts: opts, rec: Recording{RPC: map[string][]Exchange{}, HTTP: map[string]map[string]Reply{}}, served: map[string]int{}}
	if opts.Record {
		t.Cleanup(func() { f.save(path) })
		return f
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f.rec); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return f
}

// save writes the recording to path, formatted as jq formats it, unless the
// test failed.
func (f *Fixture) save(path string) {
	if f.t.Failed() {
		return
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(f.rec); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// key identifies a request by its chain, method, and parameters.
func key(chain, method string, params json.RawMessage) string {
	var compact bytes.Buffer
	json.Compact(&compact, params)
	return chain + " " + method + " " + compact.String()
}

// NodeURL returns the URL of a node of the chain with the ID chainID that
// serves the recording, or records it from the node of opts.RPCs.
func (f *Fixture) NodeURL(chainID uint64) string {
	f.t.Helper()
	chain := strconv.FormatUint(chainID, 10)
	if f.opts.Record && f.opts.RPCs[chainID] == "" {
		f.t.Fatalf("recording needs the RPC URL of chain %d", chainID)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		res := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if req.Method == "eth_chainId" {
			res["result"] = ethrpc.Quantity(chainID)
			json.NewEncoder(w).Encode(res)
			return
		}
		x, err := f.exchange(chain, req.Method, req.Params, body)
		if err != nil {
			f.t.Errorf("chain %s: %s %s: %v", chain, req.Method, req.Params, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch {
		case x.Status != 0:
			w.WriteHeader(x.Status)
			io.WriteString(w, x.Body)
			return
		case x.Error != nil:
			res["error"] = x.Error
		default:
			res["result"] = x.Result
		}
		json.NewEncoder(w).Encode(res)
	}))
	f.t.Cleanup(server.Close)
	return server.URL
}

// Node returns a client for a node of the chain with the ID chainID that serves
// the recording, as NodeURL does.
func (f *Fixture) Node(chainID uint64) *ethrpc.Client {
	f.t.Helper()
	client, err := ethrpc.NewClient(f.t.Context(), chainID, f.NodeURL(chainID))
	if err != nil {
		f.t.Fatal(err)
	}
	return client
}

// recordTries is how many times a fixture sends a request to a live node or
// service while recording, when it is rate limited or fails, since such a
// failure says nothing about the chain, and isn't recorded.
const recordTries = 6

// exchange returns the answer to a JSON-RPC request: the next one recorded, or,
// when recording, the live node's.
func (f *Fixture) exchange(chain, method string, params json.RawMessage, body []byte) (Exchange, error) {
	k := key(chain, method, params)
	if !f.opts.Record {
		f.mu.Lock()
		defer f.mu.Unlock()
		var matches []Exchange
		for _, x := range f.rec.RPC[chain] {
			if key(chain, x.Method, x.Params) == k {
				matches = append(matches, x)
			}
		}
		if len(matches) == 0 {
			return Exchange{}, fmt.Errorf("not recorded")
		}
		i := min(f.served[k], len(matches)-1)
		f.served[k]++
		return matches[i], nil
	}

	id, _ := strconv.ParseUint(chain, 10, 64)
	x := Exchange{Method: method, Params: params}
	err := retry(func() (bool, error) {
		resp, err := httpretry.Client.Post(f.opts.RPCs[id], "application/json", bytes.NewReader(body))
		if err != nil {
			return true, err
		}
		defer resp.Body.Close()
		answerBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return true, err
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			return true, fmt.Errorf("HTTP %s", resp.Status)
		case resp.StatusCode != http.StatusOK:
			x.Status, x.Body = resp.StatusCode, string(answerBody)
			return false, nil
		}
		var answer struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(answerBody, &answer); err != nil {
			return true, err
		}
		if answer.Error != nil {
			if slices.Contains(rateLimitCodes, answer.Error.Code) {
				return true, fmt.Errorf("rate limited with JSON-RPC error %d", answer.Error.Code)
			}
			var raw struct {
				Error json.RawMessage `json:"error"`
			}
			json.Unmarshal(answerBody, &raw)
			x.Error = raw.Error
			return false, nil
		}
		trim := f.opts.TrimResult
		if trim == nil {
			trim = Trim
		}
		x.Result = trim(method, answer.Result)
		return false, nil
	})
	if err != nil {
		return Exchange{}, err
	}
	f.mu.Lock()
	f.rec.RPC[chain] = append(f.rec.RPC[chain], x)
	f.mu.Unlock()
	return x, nil
}

// rateLimitCodes are the JSON-RPC error codes that nodes rate limit with, as
// ethrpc retries them.
var rateLimitCodes = []int{-32005, -32016, -32029}

// retry runs try until it succeeds, fails for good, or has run recordTries
// times, waiting longer between each run.
func retry(try func() (again bool, err error)) error {
	wait := 2 * time.Second
	for n := 1; ; n++ {
		again, err := try()
		if !again || n == recordTries {
			return err
		}
		time.Sleep(wait)
		wait *= 2
	}
}

// ServiceURL returns the base URL of a service, by its name, such as
// "txservice", that serves the recording, or records it from the service at the
// base URL upstream.
func (f *Fixture) ServiceURL(name, upstream string) string {
	f.t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.RequestURI()
		reply, err := f.reply(name, upstream, path)
		if err != nil {
			f.t.Errorf("%s: GET %s: %v", name, path, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		body := []byte(reply.Body)
		var text string
		if json.Unmarshal(reply.Body, &text) == nil {
			body = []byte(text)
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(cmpStatus(reply.Status))
		w.Write(body)
	}))
	f.t.Cleanup(server.Close)
	return server.URL
}

func cmpStatus(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}

// reply returns a service's reply to a GET of path: the one recorded, or, when
// recording, the live service's.
func (f *Fixture) reply(name, upstream, path string) (Reply, error) {
	if !f.opts.Record {
		f.mu.Lock()
		defer f.mu.Unlock()
		reply, ok := f.rec.HTTP[name][path]
		if !ok {
			return Reply{}, fmt.Errorf("not recorded")
		}
		return reply, nil
	}
	var reply Reply
	err := retry(func() (bool, error) {
		req, err := http.NewRequestWithContext(f.t.Context(), http.MethodGet, upstream+path, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := httpretry.Client.Do(req)
		if err != nil {
			return true, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return true, err
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return true, fmt.Errorf("HTTP %s", resp.Status)
		}
		reply = Reply{Body: body}
		if resp.StatusCode != http.StatusOK {
			reply.Status = resp.StatusCode
		}
		if !json.Valid(body) {
			reply.Body, _ = json.Marshal(string(body))
		} else if f.opts.TrimReply != nil && reply.Status == 0 {
			reply.Body = f.opts.TrimReply(name, path, body)
		}
		return false, nil
	})
	if err != nil {
		return Reply{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rec.HTTP[name] == nil {
		f.rec.HTTP[name] = map[string]Reply{}
	}
	f.rec.HTTP[name][path] = reply
	return reply, nil
}

// Trim returns the result of a JSON-RPC method with only the fields that arbot
// reads, which keeps the recordings small: the number, hash, and timestamp of a
// block, the hash, sender, recipient, and input of a transaction, and the
// fields of a receipt and of logs that ethrpc decodes.
func Trim(method string, result json.RawMessage) json.RawMessage {
	switch method {
	case "eth_getBlockByNumber":
		return Fields(result, "number", "hash", "timestamp")
	case "eth_getTransactionByHash":
		return Fields(result, "hash", "from", "to", "input")
	case "eth_getTransactionReceipt":
		receipt := Fields(result, "transactionHash", "blockNumber", "status", "from", "to", "logs")
		var r map[string]json.RawMessage
		if json.Unmarshal(receipt, &r) != nil || r == nil {
			return receipt
		}
		r["logs"] = trimLogs(r["logs"])
		trimmed, _ := json.Marshal(r)
		return trimmed
	case "eth_getLogs":
		return trimLogs(result)
	}
	return result
}

// logFields are the fields of a log that ethrpc decodes.
var logFields = []string{"address", "topics", "data", "blockNumber", "blockHash", "transactionHash", "logIndex", "removed"}

// trimLogs returns the logs of an array with only logFields.
func trimLogs(logs json.RawMessage) json.RawMessage {
	var list []json.RawMessage
	if json.Unmarshal(logs, &list) != nil || list == nil {
		return logs
	}
	for i, l := range list {
		list[i] = Fields(l, logFields...)
	}
	trimmed, _ := json.Marshal(list)
	return trimmed
}

// Fields returns a JSON object with only the fields of object that fields
// names, or object as it is if it isn't an object.
func Fields(object json.RawMessage, fields ...string) json.RawMessage {
	var full map[string]json.RawMessage
	if json.Unmarshal(object, &full) != nil || full == nil {
		return object
	}
	kept := make(map[string]json.RawMessage)
	for _, field := range fields {
		if v, ok := full[field]; ok {
			kept[field] = v
		}
	}
	trimmed, _ := json.Marshal(kept)
	return trimmed
}
