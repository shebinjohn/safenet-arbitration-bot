// Package corpus formats the rulings corpus: one Markdown file per ruling that
// the arbitrator recorded onchain, named after its request ID, and an index of
// the Gnosis Chain blocks whose rulings the corpus has.
package corpus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/ipfs"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// HeaderLines is the number of lines of the header that every corpus file
// starts with, including the lines that delimit it.
const HeaderLines = 10

// DocumentStart is the line, and the thematic break after it, that start the
// ruling document at the end of a corpus file. Everything after it is the
// document, verbatim.
const DocumentStart = "<!-- DOCUMENT START -->\n---\n\n"

// IndexFile is the name of the index in the corpus directory.
const IndexFile = "index.json"

// Index records how far the corpus has been indexed.
type Index struct {
	// Block is the last Gnosis Chain block whose rulings are in the corpus.
	Block uint64 `json:"block"`
}

// ReadIndex reads the index of the corpus in dir.
func ReadIndex(dir string) (Index, error) {
	path := filepath.Join(dir, IndexFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return Index{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var index Index
	if err := decoder.Decode(&index); err != nil {
		return Index{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return index, nil
}

// Marshal returns the index as indented JSON, as `jq .` formats it.
func (i Index) Marshal() []byte {
	data, _ := json.MarshalIndent(i, "", "  ")
	return append(data, '\n')
}

// Filename returns the name of the corpus file of the ruling on request id.
func Filename(id ethrpc.Hash) string {
	return id.String() + ".md"
}

// ContextCID returns the CID that the context of a ruling references, as
// "ipfs://<cid>" or "<cid>", if it is one rather than the ruling text itself.
func ContextCID(context string) (ipfs.CID, bool) {
	cid, err := ipfs.ParseCIDFromURL(context)
	if err != nil {
		cid, err = ipfs.ParseCID(context)
	}
	return cid, err == nil
}

var rulePattern = regexp.MustCompile(`\bR-[0-9]+(?:\.[0-9]+)*`)

// Entry is the corpus file of a ruling.
type Entry struct {
	// Request is the request that the arbitrator ruled on, or marked out of scope.
	Request *safenet.Request
	// Charter is the CID of the Charter version that applies to the request.
	Charter ipfs.CID
	// Document is the ruling document: the content that the ruling's context
	// references, or the context itself if it is the ruling text.
	Document []byte
}

// Rules returns the IDs of the Charter rules that the ruling document cites,
// such as "R-4.1", in the order they are first cited.
func (e *Entry) Rules() []string {
	var rules []string
	for _, rule := range rulePattern.FindAllString(string(e.Document), -1) {
		if !slices.Contains(rules, rule) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// Markdown returns the corpus file: the header, then the proposal, the Safe
// transaction, the sentinels' votes, and the ruling, then DocumentStart and the
// ruling document, verbatim.
func (e *Entry) Markdown() []byte {
	r := e.Request
	p := r.Proposal
	tx := p.Transaction
	a := r.Arbitration
	var b bytes.Buffer

	rules := "-"
	if len(e.Rules()) > 0 {
		rules = strings.Join(e.Rules(), ", ")
	}
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "request: %s\n", r.ID)
	fmt.Fprintf(&b, "outcome: %s\n", a.Outcome)
	fmt.Fprintf(&b, "rules: %s\n", rules)
	fmt.Fprintf(&b, "chain: %s\n", tx.ChainID)
	fmt.Fprintf(&b, "safe: %s\n", tx.Safe)
	fmt.Fprintf(&b, "charter: %s\n", e.Charter)
	fmt.Fprintf(&b, "proposed: %s\n", p.Time.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "record: %s\n", a.Record.TxHash)
	fmt.Fprintf(&b, "---\n")

	fmt.Fprintf(&b, "\n# Ruling on request %s\n", r.ID)

	fmt.Fprintf(&b, "\n## Proposal\n\n")
	fmt.Fprintf(&b, "- Oracle: %s\n", r.Oracle)
	fmt.Fprintf(&b, "- Consensus: %s\n", p.Consensus)
	fmt.Fprintf(&b, "- Charter name: %q\n", r.Charter)
	fmt.Fprintf(&b, "- Gnosis Chain block: %d (%s)\n", p.Block, p.Time.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Gnosis Chain transaction: %s\n", p.TxHash)
	fmt.Fprintf(&b, "- Ethereum Mainnet block before: %d\n", p.EthereumBlock)
	fmt.Fprintf(&b, "- Safe chain block before: %d\n", p.SafeBlock)
	fmt.Fprintf(&b, "- Epoch: %d\n", p.Epoch)
	fmt.Fprintf(&b, "- Oracle data: %s\n", p.OracleData)
	fmt.Fprintf(&b, "- Safe transaction hash: %s\n", p.SafeTxHash)

	fmt.Fprintf(&b, "\n## Safe transaction\n\n")
	fmt.Fprintf(&b, "- Chain ID: %s\n", tx.ChainID)
	fmt.Fprintf(&b, "- Safe: %s\n", tx.Safe)
	fmt.Fprintf(&b, "- To: %s\n", tx.To)
	fmt.Fprintf(&b, "- Value: %s\n", tx.Value)
	fmt.Fprintf(&b, "- Data: %s\n", tx.Data)
	fmt.Fprintf(&b, "- Operation: %s\n", tx.Operation)
	fmt.Fprintf(&b, "- Safe tx gas: %s\n", tx.SafeTxGas)
	fmt.Fprintf(&b, "- Base gas: %s\n", tx.BaseGas)
	fmt.Fprintf(&b, "- Gas price: %s\n", tx.GasPrice)
	fmt.Fprintf(&b, "- Gas token: %s\n", tx.GasToken)
	fmt.Fprintf(&b, "- Refund receiver: %s\n", tx.RefundReceiver)
	fmt.Fprintf(&b, "- Nonce: %s\n", tx.Nonce)

	// Reasons are quoted, as the oracle doesn't validate them.
	fmt.Fprintf(&b, "\n## Votes\n\n")
	for _, v := range r.Votes {
		if v.Reason == "" {
			fmt.Fprintf(&b, "- %s: %s\n", v.Sentinel, v.Vote)
		} else {
			fmt.Fprintf(&b, "- %s: %s, reason %q\n", v.Sentinel, v.Vote, v.Reason)
		}
	}
	if len(r.Votes) == 0 {
		fmt.Fprintf(&b, "None.\n")
	}

	fmt.Fprintf(&b, "\n## Ruling\n\n")
	fmt.Fprintf(&b, "- Outcome: %s\n", a.Outcome)
	fmt.Fprintf(&b, "- Frozen: block %d\n", a.FrozenBlock)
	fmt.Fprintf(&b, "- Deadline: block %d\n", a.Deadline)
	fmt.Fprintf(&b, "- Record: block %d, transaction %s\n", a.Record.Block, a.Record.TxHash)
	if _, ok := ContextCID(a.Context); ok {
		fmt.Fprintf(&b, "- Context: %s, which references the document below\n", a.Context)
	} else {
		fmt.Fprintf(&b, "- Context: the document below\n")
	}

	fmt.Fprintf(&b, "\n%s", DocumentStart)
	b.Write(e.Document)
	return b.Bytes()
}

// Tag is a line of a header, "key: value".
type Tag struct {
	Key, Value string
}

// Tags returns the tags of the corpus file content: those of its header, and
// those of the header that its ruling document starts with, if it has one. A
// document header is delimited by "---" lines, like the file's header, such as
// one with the precedents, tags, and summary of the ruling.
//
// The file's header is written by arbot index, so a malformed one is an error.
// The document is written by the Council, so a document whose header is
// malformed has no tags instead.
func Tags(content []byte) (header, document []Tag, err error) {
	header, err = parseHeader(string(content))
	if err != nil {
		return nil, nil, fmt.Errorf("header: %w", err)
	}
	_, doc, ok := strings.Cut(string(content), "\n"+DocumentStart)
	if !ok {
		return nil, nil, fmt.Errorf("no %q line", strings.TrimSuffix(DocumentStart, "\n---\n\n"))
	}
	if strings.HasPrefix(doc, "---\n") {
		document, _ = parseHeader(doc)
	}
	return header, document, nil
}

// Header returns the tags of the header of the corpus file content, such as its
// outcome, without reading its ruling document.
func Header(content []byte) ([]Tag, error) {
	return parseHeader(string(content))
}

// parseHeader parses the header that text starts with, and returns its tags.
func parseHeader(text string) ([]Tag, error) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, errors.New("doesn't start with a --- line")
	}
	var tags []Tag
	for n := 2; ; n++ {
		line, after, ok := strings.Cut(rest, "\n")
		if !ok {
			return nil, errors.New("no --- line ends it")
		}
		rest = after
		if line == "---" {
			return tags, nil
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d is not \"key: value\": %q", n, line)
		}
		tags = append(tags, Tag{Key: key, Value: strings.TrimSpace(value)})
	}
}
