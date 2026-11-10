package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/safenet"
)

// TxServiceExecution is an execution that the Safe Transaction Service of
// ServeTxService lists, with the hashes and block that it reports, which can be
// wrong.
type TxServiceExecution struct {
	Transaction *safenet.SafeTransaction
	SafeTxHash  ethrpc.Hash
	TxHash      ethrpc.Hash
	Block       uint64
}

// txService is a Safe Transaction Service for Ethereum Mainnet that lists the
// executions of a test.
type txService struct {
	*httptest.Server
	mu         sync.Mutex
	executions []TxServiceExecution
}

// ServeTxService lists the executions, in the order given, from the Safe
// Transaction Service for Ethereum Mainnet that Arbot configures arbot to use,
// starting it if needed. Without it, arbot uses no service.
func (n *Testnet) ServeTxService(executions ...TxServiceExecution) {
	n.tb.Helper()
	if n.txService == nil {
		s := &txService{}
		s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path, found := strings.CutPrefix(r.URL.Path, "/api/v1/safes/")
			address, ok := strings.CutSuffix(path, "/multisig-transactions/")
			safe, err := ethrpc.ParseAddress(address)
			// The service requires addresses with their checksum.
			if !found || !ok || err != nil || safe.String() != address {
				http.NotFound(w, r)
				return
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			s.mu.Lock()
			var listed []TxServiceExecution
			for _, e := range s.executions {
				if e.Transaction.Safe == safe {
					listed = append(listed, e)
				}
			}
			s.mu.Unlock()
			results := []map[string]any{}
			for _, e := range listed[min(offset, len(listed)):min(offset+limit, len(listed))] {
				tx := e.Transaction
				results = append(results, map[string]any{
					"safe": tx.Safe, "to": tx.To, "value": tx.Value.String(), "data": tx.Data,
					"operation": uint8(tx.Operation), "safeTxGas": tx.SafeTxGas.Uint64(), "baseGas": tx.BaseGas.Uint64(),
					"gasPrice": tx.GasPrice.String(), "gasToken": tx.GasToken, "refundReceiver": tx.RefundReceiver,
					"nonce": tx.Nonce.Uint64(), "safeTxHash": e.SafeTxHash, "transactionHash": e.TxHash,
					"blockNumber": e.Block, "isExecuted": true, "isSuccessful": true,
				})
			}
			json.NewEncoder(w).Encode(map[string]any{"count": len(listed), "results": results})
		}))
		n.tb.Cleanup(s.Close)
		n.txService = s
	}
	n.txService.mu.Lock()
	defer n.txService.mu.Unlock()
	n.txService.executions = append(n.txService.executions, executions...)
}
