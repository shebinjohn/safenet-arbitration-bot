---
name: add-evidence
description: Add an `arbot` command that gathers a kind of evidence for arbitrating a Safenet request, such as a Safe's transfer history or a token's supply. Use when an agent needs evidence that no `arbot` command provides, such as a gap in a `detective` report, instead of reading chains with `cast`, `curl`, or scripts.
---

# Add an Evidence Command

Agents collect every fact for an arbitration with `arbot` commands, so the whole process runs with `arbot` alone and moves to another agent without change. When a fact isn't available, they don't read chains with `cast`, `curl`, or scripts. They record the fact as a gap, and someone adds the command that provides it with this skill. The `detective` sub-agent lists its gaps in the Gaps section of its report, each with the command that would fill it.

## Inputs

Before writing any code, make sure you have:

- **The evidence**: the fact the command reports, such as "the ERC-20 transfers from the Safe to an address", for which accounts, and on which chain.
- **The Charter sections**: the rules and definitions the evidence bears on, such as § 2.4 and R-4.3, from the Charter version that applies. Fetch it with `arbot charter`.
- **Example requests**, if any: IDs of requests that need the evidence, such as those of a `detective` report with the gap.

Ask the user for any input that the request doesn't settle.

## Is the Evidence Admissible?

The command must only report evidence that the Charter admits (§ 3.3), as of the proposal (§ 2.8 and § 3.6):

- **Onchain data** comes from the Safe's chain at the request's `safeBlock`, or from Ethereum Mainnet at its `ethereumBlock`, and never from later blocks. It never includes the effects of the transaction itself.
- **Offchain evidence**, such as a threat-intelligence label, must be public, attributable, and reasonably verifiable (§ 3.4). The command reports its source and when it observed it, so that the Council can state both (§ 2.7 and § 3.6).

Private or after-the-fact claims of what the user intended are excluded (§ 3.5), so no command reports them.

A command reports facts, not verdicts. Deciding a class of calls under a rule is a check (see the `add-check` skill), and weighing evidence is left to the agents and the Council.

## Is the Source Available?

Invariant 4 in `AGENTS.md` says that no command may require configuration, so the command must work with the default public RPCs from `internal/ethrpc` and the public IPFS gateways:

- Prefer `eth_call`, `eth_getCode`, and `eth_getStorageAt`, which public nodes serve at older blocks. Avoid `eth_getProof`, which fewer nodes serve.
- Read logs with `ScanLogs`, which splits `eth_getLogs` into block ranges that public RPCs accept. Scanning a long history is slow, so bound the range with a flag, and report the range that the command scanned.
- Native transfers and internal calls aren't in logs, and public nodes rarely serve traces (`debug_` and `trace_` methods). The same goes for offchain sources, such as block explorers and threat-intelligence feeds.

If the evidence needs a source that isn't available by default, stop and agree with the user on one first. Add it as an optional setting in `internal/config`, with a public default, rather than make it required.

## Steps

1. **Write the logic** in an `internal/` package, such as `internal/safenet` for the protocol or a new package for a new kind of evidence, and keep the command thin. Read chains through `ethrpc.Dialer` and the `Client` methods, and decode ABI with `internal/solabi`. Use only the standard library (invariant 5).

2. **Add the command** in `internal/cli/<name>.go`, and register it in the `commands` map in `internal/cli/cli.go` with a one-line description. Follow the existing commands, such as `internal/cli/charter.go`:
   - Take `-request-file <path>` to read the Safe, its chain, and the blocks from a request that `arbot info -json` wrote, as `arbot charter` and `arbot classify` do. Take any other arguments as flags.
   - Take `-json` for a stable JSON document for agents, and print text for humans otherwise. Quote strings that come from the chain, as `arbot info` does, so that they can't contain terminal escape sequences.
   - Write a usage message that says what the command reports, and as of which block.

3. **Test it** without public RPCs:
   - Unit-test the logic against a fake node, such as `fakeNode` in `internal/checks/safe_test.go`.
   - Add an end-to-end test in `internal/e2e`, which runs `arbot` on local anvil nodes. The Mainnet node keeps the states of its recent blocks, so the command can read the Safe at `safeBlock`.

4. **Try it** on the example requests, with `go run ./cmd/arbot info -json <request-id> > .msgboard/<request-id>.json`, then `go run ./cmd/arbot <name> -request-file .msgboard/<request-id>.json`.

5. **Document it** in the list of implemented subcommands under Components in `AGENTS.md`, with its flags and output. The `detective` sub-agent finds commands with `arbot -h`, so its instructions only change if the command changes how it collects evidence.

6. **Add it to the playbooks** of the `evidence` skill, in `skills/evidence/`, whose case types need the evidence, such as `value-recipient.md` for a recipient's history: the command with its flags, in the order to run it, what its output shows and can't show, and the Charter references that its facts bear on. Remove the known gap that it fills. A test checks that every `arbot` command and flag that the skills and agents mention exists, so name it as `arbot <command> -<flag>`. The vector harness (`just vectors`) runs each playbook's commands, which it reads from the code spans of the numbered steps of its Commands section that run on `-request-file <request.json>`, so write the command there in that form, with placeholders such as `<recipient>` for its other arguments, then run `just vectors` to see that it runs on the Sentinel test vectors.

7. **Run `just precommit`**, which formats the sources and runs the same checks and tests as CI.
