package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// TestFill fills in the placeholders of playbook commands from the output of
// arbot calls -json of a batch that approves a spender and transfers a token,
// and of arbot executions -json.
func TestFill(t *testing.T) {
	const (
		token     = "0x000000000000000000000000000000000000000a"
		spender   = "0x000000000000000000000000000000000000000b"
		recipient = "0x000000000000000000000000000000000000000c"
		multiSend = "0x000000000000000000000000000000000000000d"
	)
	safe := ethrpc.MustParseAddress("0x0000000000000000000000000000000000000001")
	calls := `{"calls": [
		{"kind": "batched", "to": {"address": "` + token + `"}, "operation": "CALL", "value": 0, "function": {"name": "approve",
			"args": [{"name": "spender", "type": "address", "value": "` + spender + `"}, {"name": "value", "type": "uint256", "value": 5}]}},
		{"kind": "batched", "to": {"address": "` + token + `"}, "operation": "CALL", "value": 0, "function": {"name": "transfer",
			"args": [{"name": "to", "type": "address", "value": "` + recipient + `"}, {"name": "value", "type": "uint256", "value": 5}]}},
		{"kind": "refund", "to": {"address": "0x0000000000000000000000000000000000000000"}, "operation": "CALL", "value": 0}
	]}`
	o := &outputs{request: &safenet.Request{Proposal: safenet.Proposal{Transaction: safenet.SafeTransaction{Safe: safe}}}}
	o.add("calls", []byte(calls))
	o.add("executions", []byte(`{"executions": [{"block": 5, "txHash": "0xaa"}, {"block": 9, "txHash": "0xbb"}], "history": {"creation": {"block": 3}}}`))

	for _, test := range []struct {
		line   string
		want   []string
		reason string
	}{
		{"account -json -spender <spender> -request-file <request.json> <token>", []string{"account -json -spender " + spender + " -request-file r.json " + token}, ""},
		{"transfers -all -json -to <recipient> -request-file <request.json>", []string{"transfers -all -json -to " + recipient + " -request-file r.json"}, ""},
		// Every account and address argument, without repeats.
		{"account -json -request-file <request.json> <address>", []string{
			"account -json -request-file r.json " + token, "account -json -request-file r.json " + spender, "account -json -request-file r.json " + recipient,
		}, ""},
		{"simulate -json -call <n> -request-file <request.json>", []string{"simulate -json -call 1 -request-file r.json"}, ""},
		{"safe -json -at <block> -compare <safe> -request-file <request.json>", []string{"safe -json -at 3 -compare " + strings.ToLower(safe.String()) + " -request-file r.json"}, ""},
		{"events -json -request-file <request.json> <transaction-hash>", []string{"events -json -request-file r.json 0xbb"}, ""},
		{"signatures -json -request-file <request.json> <target>", nil, "no delegate call"},
	} {
		runs, reason := o.fill(strings.Fields(test.line), "r.json")
		var got []string
		for _, run := range runs {
			got = append(got, strings.Join(run, " "))
		}
		if !slices.Equal(got, test.want) || !strings.Contains(reason, test.reason) {
			t.Errorf("%s: got %q, reason %q; want %q, reason with %q", test.line, got, reason, test.want, test.reason)
		}
	}

	// A delegate call to a contract other than a MultiSend has a target.
	o.add("calls", []byte(`{"calls": [{"kind": "root", "to": {"address": "`+multiSend+`", "contract": {"name": "MultiSend", "version": "1.3.0"}}, "operation": "DELEGATECALL", "value": 0},
		{"kind": "root", "to": {"address": "`+token+`"}, "operation": "DELEGATECALL", "value": 0}]}`))
	if runs, _ := o.fill([]string{"<target>"}, "r.json"); len(runs) != 1 || runs[0][0] != token {
		t.Errorf("<target>: got %q, want only the delegate call that isn't to a MultiSend", runs)
	}
	// Without the output of arbot calls, a placeholder from it can't be filled.
	o.calls = nil
	if _, reason := o.fill([]string{"<token>"}, "r.json"); !strings.Contains(reason, "arbot calls") {
		t.Errorf("<token> without calls: got reason %q", reason)
	}
	if !json.Valid([]byte(calls)) {
		t.Fatal("the calls aren't JSON")
	}
}
