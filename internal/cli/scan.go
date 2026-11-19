package cli

import (
	"context"
	"fmt"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/txservice"
)

// defaultScanBlocks is how many blocks before the Safe block that the commands
// that read a Safe's history scan by default, which is about two weeks on
// Ethereum Mainnet. Public nodes answer a scan of a long history slowly, so it
// is bounded.
const defaultScanBlocks = 100_000

// safeRef is the Safe of a request, and the block to read its chain at, as of
// the proposal: the request's Safe block, without the transaction's own effects
// or anything later.
type safeRef struct {
	Safe    ethrpc.Address
	ChainID uint64
	// SafeBlock is the request's Safe block.
	SafeBlock uint64
	// SafeTxHash is the hash of the request's Safe transaction.
	SafeTxHash ethrpc.Hash
	eth        *ethrpc.Client
}

// openSafe reads the request in the file at requestFile, as written by arbot
// info -json, and returns its Safe, with a client for the Safe's chain. An
// empty requestFile is a usage error.
func openSafe(ctx context.Context, e *env, requestFile string) (*safeRef, error) {
	if requestFile == "" {
		return nil, usageError("-request-file is required")
	}
	request, err := readRequest(requestFile)
	if err != nil {
		return nil, err
	}
	tx := request.Proposal.Transaction
	// A zero block would read the chain's first blocks instead of those before the
	// proposal.
	if tx.ChainID == nil || !tx.ChainID.IsUint64() || request.Proposal.SafeBlock == 0 {
		return nil, fmt.Errorf("request in %s has no Safe chain ID or block", requestFile)
	}
	s := &safeRef{Safe: tx.Safe, ChainID: tx.ChainID.Uint64(), SafeBlock: request.Proposal.SafeBlock, SafeTxHash: request.Proposal.SafeTxHash}
	if s.eth, err = ethrpc.NewDialer(e.cfg.RPCs)(ctx, s.ChainID); err != nil {
		return nil, err
	}
	return s, nil
}

// scan is a range of a Safe's history that a command reads, as of the proposal
// of a request: the blocks of the Safe's chain up to the request's Safe block.
type scan struct {
	*safeRef
	// FromBlock and ToBlock are the first and last blocks of the range, which is
	// blocks long unless it starts at the first block of the chain. ToBlock is the
	// request's Safe block.
	FromBlock, ToBlock uint64
}

// openScan returns a scan of the last blocks up to the Safe block of the
// request in the file at requestFile, with a client for the Safe's chain. An
// empty requestFile or zero blocks is a usage error.
func openScan(ctx context.Context, e *env, requestFile string, blocks uint64) (*scan, error) {
	if requestFile == "" {
		return nil, usageError("-request-file is required")
	}
	if blocks == 0 {
		return nil, usageError("-blocks must be at least 1")
	}
	safe, err := openSafe(ctx, e, requestFile)
	if err != nil {
		return nil, err
	}
	return &scan{safeRef: safe, ToBlock: safe.SafeBlock, FromBlock: safe.SafeBlock - min(blocks-1, safe.SafeBlock)}, nil
}

// txService returns a client for the Safe Transaction Service of the chain with
// the ID chainID, or nil if the configuration leaves it out.
func txService(e *env, chainID uint64) *txservice.Client {
	baseURL, ok := e.cfg.TxServices[chainID]
	if !ok {
		baseURL = txservice.DefaultURLs[chainID]
	}
	if baseURL == "" {
		return nil
	}
	return txservice.NewClient(baseURL, chainID)
}

// errNoTxService is the error of a command that needs the Safe Transaction
// Service of a chain that has none.
func errNoTxService(flag string, chainID uint64) error {
	return fmt.Errorf("%s needs the Safe Transaction Service of chain %d, which has none (see the txServices setting, where an empty URL leaves it out)", flag, chainID)
}
