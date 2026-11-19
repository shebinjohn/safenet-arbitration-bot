package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/events"
)

// eventsReport is the JSON output of `arbot events`.
type eventsReport struct {
	ChainID uint64 `json:"chainId"`
	// SafeBlock is the request's Safe block, which the transaction is not after.
	SafeBlock uint64      `json:"safeBlock"`
	TxHash    ethrpc.Hash `json:"txHash"`
	Block     uint64      `json:"block"`
	// BlockTime is the timestamp of Block.
	BlockTime time.Time `json:"blockTime"`
	// Status is "success", or "reverted" for a transaction that has no events.
	Status string         `json:"status"`
	From   ethrpc.Address `json:"from"`
	// To is null for a transaction that creates a contract.
	To     *ethrpc.Address `json:"to"`
	Events []eventLog      `json:"events"`
}

// eventLog is a log of the transaction, and the well-known event that it is.
type eventLog struct {
	LogIndex uint64         `json:"logIndex"`
	Emitter  ethrpc.Address `json:"emitter"`
	Topics   []ethrpc.Hash  `json:"topics"`
	Data     ethrpc.Bytes   `json:"data"`
	// Event is null if the log isn't a well-known event.
	Event *events.Event `json:"event"`
}

// maxTextBytes is the length of the longest bytes that the text output shows in
// full.
const maxTextBytes = 36

// eventsCommand lists the events that a transaction on the Safe's chain of a
// request logged.
func eventsCommand(ctx context.Context, e *env, args []string) error {
	flags := e.flagSet("events")
	asJSON := flags.Bool("json", false, "write the events as JSON")
	requestFile := flags.String("request-file", "", "read the Safe's chain and block from the request in the JSON file at `path`, as written by arbot info -json")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s events [flags] -request-file <path> <transaction-hash>\n\n", e.progname)
		fmt.Fprintf(out, "Lists the events that a transaction logged, from the Safe's chain of a request, as of the request's\n")
		fmt.Fprintf(out, "Safe block. It fails for a transaction in a later block, whose events aren't evidence of the\n")
		fmt.Fprintf(out, "proposal. It reports each log with its emitter, topics, and data, and, if it is a well-known event of\n")
		fmt.Fprintf(out, "an ERC-20 or ERC-721 token, WETH, or a Safe, its name and arguments. A log is decoded by its\n")
		fmt.Fprintf(out, "signature only, and any contract can emit a log with the signature of a well-known event, so check the\n")
		fmt.Fprintf(out, "emitter. A transaction that reverted has no events. The text output shortens long data, and -json\n")
		fmt.Fprintf(out, "has all of it.\n\n")
		flags.PrintDefaults()
	}
	if err := parse(flags, args, 1); err != nil {
		return err
	}
	hash, err := ethrpc.ParseHash(flags.Arg(0))
	if err != nil {
		return usageError(fmt.Sprintf("transaction hash %q: %v", flags.Arg(0), err))
	}
	safe, err := openSafe(ctx, e, *requestFile)
	if err != nil {
		return err
	}

	receipt, err := safe.eth.GetTransactionReceipt(ctx, hash)
	if err != nil {
		return err
	}
	if receipt.TransactionHash != hash {
		return fmt.Errorf("the node returned the receipt of transaction %s, not %s", receipt.TransactionHash, hash)
	}
	if uint64(receipt.BlockNumber) > safe.SafeBlock {
		return fmt.Errorf("transaction %s is in block %d of chain %d, after the request's Safe block %d", hash, receipt.BlockNumber, safe.ChainID, safe.SafeBlock)
	}

	blockTime, err := safe.eth.BlockTime(ctx, receipt.BlockNumber)
	if err != nil {
		return fmt.Errorf("reading the time of block %d: %w", receipt.BlockNumber, err)
	}
	report := eventsReport{
		ChainID: safe.ChainID, SafeBlock: safe.SafeBlock, TxHash: hash, Block: uint64(receipt.BlockNumber), BlockTime: blockTime,
		Status: "reverted", From: receipt.From, To: receipt.To, Events: []eventLog{},
	}
	if receipt.Status == 1 {
		report.Status = "success"
	}
	for _, log := range receipt.Logs {
		report.Events = append(report.Events, eventLog{
			LogIndex: uint64(log.LogIndex), Emitter: log.Address, Topics: log.Topics, Data: log.Data, Event: events.Decode(log),
		})
	}

	if *asJSON {
		return writeJSON(e.stdout, report)
	}
	fmt.Fprintf(e.stdout, "Transaction %s on chain %d, in block %d at %s (%s)\n", report.TxHash, report.ChainID, report.Block, formatTime(report.BlockTime), report.Status)
	fmt.Fprintf(e.stdout, "  From  %s\n", report.From)
	if report.To != nil {
		fmt.Fprintf(e.stdout, "  To    %s\n", report.To)
	}
	fmt.Fprintf(e.stdout, "\n")
	if len(report.Events) == 0 {
		_, err = fmt.Fprintln(e.stdout, "no events")
		return err
	}
	for _, log := range report.Events {
		fmt.Fprintf(e.stdout, "Log %d, emitted by %s\n", log.LogIndex, log.Emitter)
		if event := log.Event; event != nil {
			fmt.Fprintf(e.stdout, "  Event  %s\n", event.Signature)
			for _, arg := range event.Args {
				fmt.Fprintf(e.stdout, "    %s = %s\n", arg.Name, formatValue(arg.Value))
			}
		} else {
			fmt.Fprintf(e.stdout, "  Event  unknown\n")
		}
		topics := make([]string, len(log.Topics))
		for i, topic := range log.Topics {
			topics[i] = topic.String()
		}
		fmt.Fprintf(e.stdout, "  Topics  %s\n", strings.Join(topics, " "))
		fmt.Fprintf(e.stdout, "  Data    %s\n", formatValue(log.Data))
	}
	return nil
}

// formatValue formats an argument of an event, shortening long bytes.
func formatValue(value any) string {
	if b, ok := value.(ethrpc.Bytes); ok && len(b) > maxTextBytes {
		return fmt.Sprintf("%s... (%d bytes)", b[:maxTextBytes], len(b))
	}
	return fmt.Sprint(value)
}
