package ethrpc

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
)

// Address is a 20-byte Ethereum account address.
type Address [20]byte

// ParseAddress parses a 0x-prefixed hex address. The EIP-55 checksum of
// mixed-case addresses is not verified.
func ParseAddress(s string) (Address, error) {
	var a Address
	err := a.UnmarshalText([]byte(s))
	return a, err
}

// MustParseAddress is like ParseAddress, but panics if s is not an address. It
// is for addresses in the source code.
func MustParseAddress(s string) Address {
	a, err := ParseAddress(s)
	if err != nil {
		panic(err)
	}
	return a
}

// String returns the address as 0x-prefixed hex, with the EIP-55 checksum in
// the case of its letters.
func (a Address) String() string {
	hex := []byte(encodeHex(a[:]))
	hash := keccak256.Hash(hex[2:])
	for i, c := range hex[2:] {
		// A letter is uppercase if the corresponding nibble of the hash of the
		// lowercase hex is 8 or more.
		if nibble := hash[i/2] >> (4 * (1 - i%2)) & 0xf; c >= 'a' && nibble >= 8 {
			hex[2+i] = c - 'a' + 'A'
		}
	}
	return string(hex)
}

// MarshalText encodes the address as lowercase 0x-prefixed hex.
func (a Address) MarshalText() ([]byte, error) {
	return []byte(encodeHex(a[:])), nil
}

func (a *Address) UnmarshalText(text []byte) error {
	b, err := decodeHex(text)
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}
	if len(b) != len(a) {
		return fmt.Errorf("invalid address: got %d bytes, want %d", len(b), len(a))
	}
	copy(a[:], b)
	return nil
}

// Hash is a 32-byte hash, such as a block or transaction hash, or a log topic.
type Hash [32]byte

// ParseHash parses a 0x-prefixed hex hash.
func ParseHash(s string) (Hash, error) {
	var h Hash
	err := h.UnmarshalText([]byte(s))
	return h, err
}

// MustParseHash is like ParseHash, but panics if s is not a hash. It is for
// hashes in the source code.
func MustParseHash(s string) Hash {
	h, err := ParseHash(s)
	if err != nil {
		panic(err)
	}
	return h
}

// String returns the hash as lowercase 0x-prefixed hex.
func (h Hash) String() string {
	return encodeHex(h[:])
}

func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

func (h *Hash) UnmarshalText(text []byte) error {
	b, err := decodeHex(text)
	if err != nil {
		return fmt.Errorf("invalid hash: %w", err)
	}
	if len(b) != len(h) {
		return fmt.Errorf("invalid hash: got %d bytes, want %d", len(b), len(h))
	}
	copy(h[:], b)
	return nil
}

// Bytes is arbitrary binary data, encoded as 0x-prefixed hex.
type Bytes []byte

// String returns the data as lowercase 0x-prefixed hex.
func (b Bytes) String() string {
	return encodeHex(b)
}

func (b Bytes) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

func (b *Bytes) UnmarshalText(text []byte) error {
	decoded, err := decodeHex(text)
	if err != nil {
		return fmt.Errorf("invalid bytes: %w", err)
	}
	*b = decoded
	return nil
}

// Quantity is an unsigned integer, encoded as a hex quantity.
type Quantity uint64

func (q Quantity) MarshalText() ([]byte, error) {
	return []byte("0x" + strconv.FormatUint(uint64(q), 16)), nil
}

func (q *Quantity) UnmarshalText(text []byte) error {
	digits, ok := strings.CutPrefix(string(text), "0x")
	if !ok || digits == "" {
		return fmt.Errorf("invalid quantity %q: not 0x-prefixed hex", text)
	}
	value, err := strconv.ParseUint(digits, 16, 64)
	if err != nil {
		return fmt.Errorf("invalid quantity %q: %w", text, err)
	}
	*q = Quantity(value)
	return nil
}

// BlockNumber is a block number, encoded as a hex quantity.
type BlockNumber uint64

func (n BlockNumber) MarshalText() ([]byte, error) {
	return Quantity(n).MarshalText()
}

func (n *BlockNumber) UnmarshalText(text []byte) error {
	return (*Quantity)(n).UnmarshalText(text)
}

// CallRequest is the message call object passed to eth_call.
type CallRequest struct {
	// From is the sender of the call. It is omitted if zero, which nodes treat as
	// the zero address.
	From Address `json:"from,omitzero"`
	To   Address `json:"to"`
	Data Bytes   `json:"data,omitempty"`
	// Value is the native currency that the call sends, or nil for none.
	Value *BigQuantity `json:"value,omitempty"`
}

// BigQuantity is an integer of any size, encoded as a JSON-RPC quantity, such
// as "0x1a".
type BigQuantity big.Int

func (q *BigQuantity) MarshalText() ([]byte, error) {
	return []byte("0x" + (*big.Int)(q).Text(16)), nil
}

// LogFilter selects the logs that eth_getLogs returns.
type LogFilter struct {
	FromBlock BlockNumber `json:"fromBlock"`
	ToBlock   BlockNumber `json:"toBlock"`
	// Addresses are the contracts whose logs match. If empty, logs from any
	// contract match.
	Addresses []Address `json:"address,omitempty"`
	// Topics[i] lists the values that match the log's i-th topic, the first being
	// the event signature. A nil entry matches any value, and trailing topics that
	// are not listed also match any value.
	Topics [][]Hash `json:"topics,omitempty"`
}

// Log is a log emitted by a contract, as returned by eth_getLogs.
type Log struct {
	Address         Address     `json:"address"`
	Topics          []Hash      `json:"topics"`
	Data            Bytes       `json:"data"`
	BlockNumber     BlockNumber `json:"blockNumber"`
	BlockHash       Hash        `json:"blockHash"`
	TransactionHash Hash        `json:"transactionHash"`
	LogIndex        Quantity    `json:"logIndex"`
	// Removed is set if the log was removed by a chain reorganization.
	Removed bool `json:"removed"`
}

// Transaction is a transaction, as returned by eth_getTransactionByHash. Only
// the fields that the client uses are decoded.
type Transaction struct {
	Hash Hash    `json:"hash"`
	From Address `json:"from"`
	// To is nil for a transaction that creates a contract.
	To    *Address `json:"to"`
	Input Bytes    `json:"input"`
}

// Receipt is a transaction receipt, as returned by eth_getTransactionReceipt.
// Only the fields that the client uses are decoded.
type Receipt struct {
	TransactionHash Hash        `json:"transactionHash"`
	BlockNumber     BlockNumber `json:"blockNumber"`
	// Status is 1 if the transaction succeeded and 0 if it reverted.
	Status Quantity `json:"status"`
	From   Address  `json:"from"`
	// To is nil for a transaction that creates a contract.
	To   *Address `json:"to"`
	Logs []Log    `json:"logs"`
}

// Block is a block header, as returned by eth_getBlockByNumber. Only the fields
// that the client uses are decoded.
type Block struct {
	Number    BlockNumber `json:"number"`
	Hash      Hash        `json:"hash"`
	Timestamp Quantity    `json:"timestamp"`
}

// Time returns the block's timestamp as a time in UTC.
func (b Block) Time() time.Time {
	return time.Unix(int64(b.Timestamp), 0).UTC()
}

// Error is an error returned by the JSON-RPC server.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	// Data holds additional error information, such as the revert data of a failed
	// eth_call.
	Data json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

func encodeHex(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

func decodeHex(text []byte) ([]byte, error) {
	digits, ok := strings.CutPrefix(string(text), "0x")
	if !ok {
		return nil, errors.New("missing 0x prefix")
	}
	return hex.DecodeString(digits)
}
