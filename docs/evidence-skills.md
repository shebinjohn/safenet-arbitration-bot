# Evidence Skills

The evidence skills let an arbitration collect its evidence with `arbot` alone, so that common cases need less manual prompting, and every fact carries its source, its block and time, the Charter references it bears on, its uncertainty, and a suggested ruling. They are:

- the `arbot` commands that gather and check evidence, listed in [Commands](#commands);
- the playbooks of the [`evidence` skill](../skills/evidence/SKILL.md), which say which commands to run for each kind of case, listed in [Playbooks](#playbooks);
- the tests and the vector harness that validate both, in [Validation](#validation).

[`AGENTS.md`](../AGENTS.md) is the reference for each command's flags and exact JSON. This document says what each one is for, what it can't show, and how much manual work the whole saves, in [Manual steps, before and after](#manual-steps-before-and-after).

## Commands

Every command reads the Safe's chain as of the request's `safeBlock`, the last block before the proposal, since evidence must not include the transaction's own effects or anything later (§ 2.8, § 3.6). Each labels its facts as onchain, with their block, or offchain, with the service's URL and observation time (§ 3.4).

### Node failures

- **What:** a request to a node that fails rather than being answered, such as for a rate limit (HTTP 429, or the JSON-RPC codes `-32005`, `-32016`, `-32029`), a server error, or a dropped connection, is a failure, not an answer (`ethrpc.Transient`). Every command fails on it, and says to run it again later or with another RPC URL.
- **Why:** a rate limit or a pruned node was read as "no receipt" or "no code", which rejected executions that had happened (`2a00c4f75bcf`, `edc790eff7b7`, `5f5ff7ee811e`).
- **Limits:** a node that answers wrongly, rather than failing, isn't detected. A pruned node's "missing trie node" is an error, not a transient failure, so the command fails without the hint.

### `arbot executions -all [-calls [-abi]]`

- **Inputs:** the request file.
- **Output:** every Safe transaction that the Safe executed up to `safeBlock`, from the Safe Transaction Service, each confirmed onchain by its hash and the Safe's execution log, with its block time; whether the history is `complete` (every nonce below the Safe's nonce accounted for), the `missingNonces`, the scans of the block gaps, the executions that the chain `rejected`, the Safe's `creation`, and the request's own execution, if it ran by `safeBlock`, apart from the others (§ 3.6). With `-calls`, each transaction's calls, decoded as `arbot calls` decodes them, and with `-abi`, with the ABIs that Sourcify verified.
- **Limits:** executions by modules use no nonce, so a complete history says nothing about them. It needs the service, an offchain source, and fails without one.
- **Charter:** § 2.4 (the Safe's established pattern), § 2.9, § 3.4, § 3.6.

### `arbot transfers -all [-in] [-to | -from <address>]`

- **Inputs:** the request file, and a counterparty to filter on.
- **Output:** the ERC-20 transfers out of the Safe, or with `-in`, the ERC-20 and native transfers into it, over its whole life, read from the logs of the blocks that the service names, with the transfers that the service lists but the logs don't have, as `unmatched`.
- **Limits:** native transfers out of the Safe and ERC-721 transfers aren't listed, nor native currency received without a `SafeReceived` event, such as before the Safe's address had code; the list is only as complete as the service's index. Any contract can log a `Transfer` event, so the token matters.
- **Charter:** § 2.4, R-4.3, R-4.4.

### `arbot account [-block N] [-spender <address>] <address>`

- **Inputs:** the request file and any address, such as a recipient, a token, or a created contract.
- **Output:** its code hash and size, its § 2.1 identification, its native balance and nonce, and for a contract that answers as an ERC-20 token, the name, symbol, decimals, and total supply that it claims, and the Safe's balance and allowance to the spender.
- **Limits:** a token's name is what the contract claims, not what it is. It doesn't show who controls an account or its history with others.
- **Charter:** § 2.4, § 2.5, R-4.3 to R-4.6.

### `arbot safe -address <address> | -at N | -compare <address>`

- **Inputs:** the request file, and another account, or an earlier block.
- **Output:** the settings of any Safe of a § 2.1 version, read from storage: singleton, owners, threshold, nonce, modules, fallback handler, guard, and module guard, or `null` with the reason (`no-code`, `not-a-safe-proxy`, `unknown-singleton`); with `-compare`, the settings of two Safes, or of one Safe at two blocks, field by field, with the owners and modules that they share.
- **Limits:** shared owners are a fact about who controls two Safes, not evidence that a recipient is inside the expected target set (the ruling on `0x8b83aa12…`). A non-standard singleton's storage isn't decoded.
- **Charter:** § 2.1, § 2.4, § 2.10, R-4.0, R-4.1, R-4.3.

### `arbot calls -abi`, and the contracts that calls create

- **Inputs:** the request file.
- **Output:** each call's function and arguments, decoded with the ABI that Sourcify verified when they aren't a well-known function (offchain, with its URL and observation time), and for each call to a § 2.1 CreateCall, the address of the contract that it deploys, derived from its data (`creates`, labelled `derived`, with its inputs).
- **Limits:** an unverified account isn't decoded. A derived CREATE address assumes the transaction runs on the state at `safeBlock`, and a deployment by another contract isn't derived.
- **Charter:** R-4.2, R-4.6 (created targets, [Charter issue #28](https://github.com/safe-research/safenet-charter/issues/28)).

### `arbot simulate -all [-trace]`

- **Inputs:** the request file.
- **Output:** the whole Safe transaction run in order in the Safe's context at `safeBlock`, with `simulateAndRevert`, so that each call of a batch runs on the effects of those before it; its result, and its revert error, decoded as `Error`, `Panic`, or an error of the verified ABI, such as `DeploymentFailed()`; and with `-trace`, every nested call and every `CREATE` and `CREATE2` frame, with the created address, creator, and init code hash.
- **Limits:** it runs on the state at `safeBlock`, not that of a later execution; the trace needs a node that serves `debug_traceCall`, or it says `trace unavailable`; the code that a creation deploys is reported, not judged. Reverting transactions ([#22](https://github.com/safe-research/safenet-charter/issues/22)) and malformed batches ([#26](https://github.com/safe-research/safenet-charter/issues/26)) are reported as facts, not classified.
- **Charter:** § 2.8, R-4.2, R-4.6.

### Block times and `arbot status`

- **Inputs:** the request file.
- **Output:** `info`, `executions`, `transfers`, and `events` report each block's time. `status` reports the transaction's nonce against the Safe's, with `stale` for a nonce that the Safe had used by the proposal ([#20](https://github.com/safe-research/safenet-charter/issues/20)); whether and when the transaction was executed, confirmed onchain; the execution's timing against the proposal, the deadlines, the freeze, and the ruling, such as `before-proposal` ([#27](https://github.com/safe-research/safenet-charter/issues/27)) or `during-voting`; and the other proposals of the Safe's queue ([#19](https://github.com/safe-research/safenet-charter/issues/19)).
- **Limits:** these are facts about the proposal, not evidence of its effects (§ 3.6), and it never classifies a request by them. It needs the oracle request, so it doesn't run on a test vector.
- **Charter:** § 2.12, § 3.6.

### The evidence summary and `arbot check-report`

- **Inputs:** an arbitration directory: `report.md`, `request.json`, and the files that the draft was made from, such as `evidence.md`, `evidence-summary.json`, and the cases.
- **Output:** every address, hash, hex value, number, time, abbreviation, quotation of the Charter, and cited precedent of the draft that isn't in a source, as an error; and of `evidence-summary.json`, the fields that the `evidence` skill defines that it lacks, a fact without its source kind, block, or offchain URL and observation time, and a summary whose uncertainty is high or whose decisive gap is open without `humanReview`, or whose draft doesn't say "Human review: required".
- **Limits:** it checks that a value is in a source, not that it is the right value in its place.
- **Charter:** § 2.7, § 3.4, § 5.2.

### `arbot vector`

- **Inputs:** a spec file of the [Sentinel test vectors](https://github.com/safe-research/sentinel-test-vectors).
- **Output:** a request file in the shape of `arbot info -json`, with the vector's transaction, its block as `safeBlock`, and its rule as the disputed rule, marked with `"vector": {"path", "verdict", "rule", "eoa"}`, so that `classify` and every `-request-file` command run on the vector.
- **Limits:** a vector has no oracle request, so `status` and `charter -request-file` don't apply. An EOA vector has no Safe at its Safe address, so `classify` finds it insecure under R-4.0.

### `arbot explorer creation | txs | tokentx | source` (optional, with an Etherscan key)

- **Inputs:** the request file, an address, and with `-chain`, another network that the Charter covers, for the same address there; and an Etherscan API key, the `etherscan` setting of the configuration file. Without a key, every subcommand fails with a message that says to configure it, and the playbooks record the evidence as a gap, "Etherscan not configured". Etherscan doesn't serve every network on every plan: its free plan doesn't serve Gnosis Chain.
- **Output:** from Etherscan's index, cut at `safeBlock`, or on another chain at the last block before the proposal's time: `creation`, a contract's creator, factory, creation transaction, and block, confirmed onchain from the transaction's receipt and the account's code before and at the block; `txs` and `tokentx`, an account's transactions and ERC-20 transfers with any account, not only the Safe, up to Etherscan's cap of 10,000; and `source`, Etherscan's verification of a contract, as a second record next to Sourcify's. Every result has its source, the URL with the key redacted, `observedAt`, and `confirmedOnchain` (`true`, `false`, or `"n/a"`).
- **Limits:** Etherscan is offchain (§ 3.4), and only a creation is confirmed onchain; listings aren't, and a `capped` one is incomplete. A creation after `safeBlock` is reported, not used as evidence (§ 3.6). Etherscan's address labels need a paid plan, so there are none, and its verification has no time, so a contract verified after the proposal is later evidence. A rate limit, which Etherscan sends with HTTP status 200, is retried, then fails the command rather than reading as an empty listing.
- **Charter:** § 2.4, § 2.6, § 3.4, § 3.6, R-4.3 to R-4.6.

## Playbooks

The playbooks are in [`skills/evidence`](../skills/evidence/SKILL.md). Each lists its commands in order, what each output shows and can't show, the facts to record with their Charter references, and the known gaps. Every case runs `proposal-status`, then the playbook of each disputed rule, and ends with the evidence summary.

| Playbook | Rules | Commands, in order | Known gaps |
| --- | --- | --- | --- |
| `proposal-status` | every case | the request file, `status`, `calls`, `safe` | module executions; an `unknown` execution status |
| `value-recipient` | R-4.3 | `calls -abi`, `account` of each token and recipient, `safe -address` and `-compare` of a recipient Safe, `executions -all -calls`, `transfers -all -to` and `-in -from`, `simulate -all -trace`, and optionally `explorer creation`, `txs`, and `tokentx` of each recipient | native and ERC-721 transfers out; native currency before the Safe had code; a recipient's own history and creator, without an Etherscan key; standard user behavior (§ 2.9); public labels (§ 2.7) |
| `approval-spender` | R-4.4, R-4.5 | `calls -abi`, `account -spender`, `account`, `signatures` and `contract-source` of each spender, `executions -all -calls`, `transfers -all`, `simulate -call` or `-all -trace`, and optionally `explorer creation` and `txs` of each spender | operator approvals; permits; the nature and scale of the interaction (§ 2.5, § 2.9); public flags on a spender; its creator and history, without an Etherscan key |
| `settings-and-delegatecall` | R-4.0 to R-4.2 | `classify`, `calls -abi`, `safe`, `safe -at -compare`, `account`, `signatures`, and `contract-source` of each delegate call target, `simulate -all -trace`, and optionally `explorer creation` and `source` of each target | the storage slots that a delegate call to an unverified contract writes |
| `target-risk` | R-4.6 | `calls -abi`, `account` of each target, argument, and created address, `signatures`, `contract-source`, `simulate -all -trace` and `-call`, `executions -all -calls`, `events`, `transfers -all`, and optionally `explorer creation` and `source` of each target | public security flags (§ 2.7), always decisive, so `humanReview` is required; the runtime code of created contracts; the creator and age of each target, without an Etherscan key; view functions that the transaction doesn't call |

The evidence summary, `evidence-summary.json` and `.md`, has each fact with its source (`onchain`, `offchain` with its URL and observation time, or `derived` from other facts), its block and time, and its rules; the gaps, each `decisive` or not; the conflicts between sources; the uncertainty (`low`, `medium`, or `high`); a suggested ruling, which is a suggestion for the `prosecutor` and the `judge`, not a verdict; and `humanReview`, which is `true` when the uncertainty is high or a decisive gap is open.

## Validation

Validation is deterministic: no agent, and no model, runs in it. The fixture tests run in CI on every change, without the network, and the vector harness runs the same commands on demand. Both check `arbot` and the playbooks, not an agent's reading of their output, so that a change to a command or a playbook that breaks the evidence of a past case fails a test.

### Fixture tests of past arbitrations (CI)

`internal/replay` records the JSON-RPC exchanges and the replies of the Safe Transaction Service and Sourcify that a test makes, and serves them again, so that the tests run `arbot`'s commands on what the chains and services answered once. A request that wasn't recorded fails the test. Fixtures keep only the fields that `arbot` reads, and no URL, so they have no RPC key.

| Request | Ruling | Test | What it pins |
| --- | --- | --- | --- |
| `998b9630ee77` | R-4.3, secure | `internal/cli` `TestCase998b9630ee77` | `transfers -all -to` finds the 8 transfers to `0x123593c1b9b8872fafec7e67baec842258344306`, in USDT and USDC, which the service lists too; `executions -all` finds the Safe's 122 executions, complete up to nonce 122, and those that pay the recipient are in the blocks of those transfers. The fixture keeps the service's transfers to and from the recipient, of the 484 that it lists, since those are the ones that `transfers -all -to` reads the blocks of, and every execution. |
| `6814ad32bff9` | R-4.3, insecure | `internal/cli` `TestCase6814ad32bff9` | No transfer between the Safe and `0x316473f1E8bCD1549527d07A9B68B545f0D2976e`, in either direction, over the Safe's whole life, and no execution that involves it, in a complete history; `safe -address` reads the recipient's settings: 3 owners and a threshold of 2. |
| `cbfadf6b9595` | R-4.6, no accepted case | `internal/cli` `TestCasecbfadf6b9595HumanReview` | The three contracts that the transaction creates, from the trace of its simulation (`internal/simulate` `TestParseTrace`), and its execution during the voting, from its status (`internal/status` `TestFixtures`, whose recording it replays), in an evidence summary with the decisive gap of the public security flags. `internal/reportcheck`'s summary validation rejects it without `humanReview`, and `arbot check-report` rejects a draft of it that doesn't say "Human review: required": this is the check that ambiguous cases go to human review. |
| `a049284bbb54` | R-4.3, insecure | `internal/status` `TestFixtures`, `internal/cli` `TestCheckReport` | Executed 2 seconds before its proposal, with a nonce that the Safe had used; its saved report passes `arbot check-report`, and one with a planted hash fails. |

Other tests pin the evidence of these cases too: `internal/create` and `internal/checks` the CREATE and CREATE2 addresses of `cbfadf6b9595`'s nonce 24, `internal/simulate` its `simulateAndRevert` result and `DeploymentFailed()` error, and `internal/cli` `TestMentionedCommandsExist` that every command and flag that the skills mention exists. `internal/playbooks` `TestLoad` checks that the vector harness can read every playbook.

To record a fixture again, run its test with `-record -record-config <config>`, with a configuration file whose Ethereum Mainnet RPC URL is an archive node, such as `go test ./internal/cli -run TestCase -record -record-config arbot.config.json`.

### Sentinel test vectors (`just vectors`)

`just vectors [dir] [flags]` runs `internal/cmd/vectors` on the vectors cloned in `dir`, by default `../sentinel-test-vectors`:

```sh
git clone https://github.com/safe-research/sentinel-test-vectors ../sentinel-test-vectors
just vectors ../sentinel-test-vectors -config arbot.config.json
```

For each vector, it runs `arbot vector`, `arbot classify -json -request-file`, and the commands of the playbooks that match the vector's rule, or of every playbook for a secure vector, since a secure verdict holds under every rule. It reads the playbooks' commands from their Markdown, so it runs what the agents run, and fills in their placeholders, such as `<recipient>`, from the outputs of the commands before them. It skips a command whose placeholder no output fills, which the playbooks call moot, the commands that need the oracle request, and, unless the configuration has an Etherscan API key, the optional `explorer` commands, as "Etherscan not configured", so that a run without a key doesn't fail on them. The results below are of a run from before the `explorer` commands, which they would count as skipped. It runs one command at a time, and runs a command that a node or a service failed again after a wait. With `-run <regexp> -keep`, it runs only the matching vectors again, such as those that a rate limit failed, and keeps the other vectors' results.

It compares each deterministic result with the vector:

- `correct`: the checks decided the vector's verdict, under its rule for an insecure vector;
- `wrong`: the checks decided the other verdict, which no vector may have;
- `unclassified`, `other-rule`, and `eoa-ignored`: left to the agents, because the checks didn't decide, decided under a rule that the vector doesn't cite, which the `arbitrate` skill ignores since it isn't disputed, or found an EOA vector insecure under R-4.0;
- `off-network`: the vector is on a network that Article I doesn't list, where R-4.0 finds every transaction insecure;
- `failed`: `arbot classify` failed.

It writes `vectors.md` and `vectors.json` to `-out` (default `.msgboard/vectors`), with each vector's expected verdict and rule, the deterministic result, the outcome, and each command that failed or was skipped, with its error, and the outputs of the commands next to them.

It needs an archive node, so it isn't part of CI. Whoever changes the checks or the skills runs it:

- after adding a deterministic check, to see that it decides no vector wrongly;
- after changing a playbook, to see that its commands still run;
- when vectors are added upstream.

### Results

The harness ran on 2026-11-19 on the 29 vectors of [sentinel-test-vectors](https://github.com/safe-research/sentinel-test-vectors) at commit `1e4b594`, with an Ethereum Mainnet archive node, one command at a time:

- 29 vectors, with 398 commands run (353 succeeded, 45 failed) and 187 skipped.
- Decided correctly by the deterministic checks: 0.
- Decided wrongly: 3, all R-4.0 findings on vectors whose Safe address has no Safe (see below).
- Left to the agents: 24: 11 unclassified, 9 EOA vectors with their R-4.0 finding ignored, and 4 insecure under R-4.0 rather than their rule.
- On a network that Article I doesn't list: 2.
- Not classified because `arbot classify` failed: 0.
- With a command that hit a node or service error: 3: the 2 BNB Chain vectors, whose default node doesn't keep the state of their blocks, and 1 rate limit that outlasted the retries.

| Vector | Expected | Deterministic result | Outcome |
| --- | --- | --- | --- |
| `address-poisoning/florence-finance-usdc-transfer` | insecure R-4.3 | unclassified | unclassified |
| `address-poisoning/request-finance-usdc-transfer` | insecure R-4.3 | unclassified | unclassified |
| `address-poisoning/superfortune-legitimate-transfer` | secure | insecure R-4.0 | off-network |
| `address-poisoning/superfortune-poisoned-multisend` | insecure R-4.3 | insecure R-4.0 | off-network |
| `address-poisoning/usdc-81k-contaminated-history` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `address-poisoning/usdt-100k-near-match` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `address-poisoning/usdt-157k-scammer-lookalike` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `address-poisoning/usdt-19k-cex-deposit-lookalike` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `address-poisoning/usdt-41k-cex-deposit-lookalike` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `address-poisoning/usdt-50m-contaminated-history` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `address-poisoning/wbtc-266k-lookalike-transfer` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `address-poisoning/wbtc-68m-lookalike-transfer` | insecure R-4.3 (EOA) | insecure R-4.0 | eoa-ignored |
| `cow/limit-order-partially-fillable-with-approval` | secure | unclassified | unclassified |
| `cow/twap-order-with-approval` | secure | unclassified | unclassified |
| `module-execution/gnosis-pay-delay-module-fail-open` | insecure R-4.3 | insecure R-4.0 | other-rule |
| `safe/nested-unrelayed-insecure-inner` | secure | insecure R-4.0 | wrong |
| `safenet/announcement-with-relaying` | insecure R-4.4 | insecure R-4.0 | other-rule |
| `safenet/announcement-without-relaying` | secure | insecure R-4.0 | wrong |
| `safenet/cancel-with-relaying` | insecure R-4.4 | insecure R-4.0 | other-rule |
| `safenet/cancel-without-relaying` | secure | insecure R-4.0 | wrong |
| `safenet/safenet_approve_stake` | secure | unclassified | unclassified |
| `safenet/safenet_approve_stake_excess` | insecure R-4.5 | unclassified | unclassified |
| `safenet/safenet_claim` | secure | unclassified | unclassified |
| `safenet/safenet_claim_approve_restake` | secure | unclassified | unclassified |
| `safenet/safenet_stake` | secure | unclassified | unclassified |
| `settings-change/bybit-mastercopy-delegatecall` | insecure R-4.2 | insecure R-4.0 | other-rule |
| `settings-change/wazirx-rogue-implementation` | insecure R-4.2 | unclassified | unclassified |
| `token-approval/wbtc-unlimited-increase-approval` | insecure R-4.5 (EOA) | insecure R-4.0 | eoa-ignored |
| `transfers/0xAb876791757b47D202B7d6DeF038940cE7A64fe4_35` | secure | unclassified | unclassified |

What it shows:

- **The checks decide none of the vectors under the vectors' own rules, and leave 24 to the agents.** The vectors are R-4.3 to R-4.5 cases, which are principle-based (§ 3.8) and have no deterministic check, and R-4.2 delegate calls to contracts that no check identifies. Each of them gets the evidence of its playbooks, with the commands that failed or were skipped listed in the report, with the reason.
- **The 3 vectors decided wrongly are R-4.0 findings on Safe addresses that have no Safe of a § 2.1 version at the vector's block, which the Charter makes insecure whatever the transaction does.** `safenet/announcement-without-relaying` and `safenet/cancel-without-relaying` name `0x9641d764fc13c8B624c04430C7356C1C7C8102e2` as their Safe, which `arbot calls` identifies as MultiSendCallOnly 1.4.1, the contract that `safenet/safenet_approve_stake_excess` delegate calls. `safe/nested-unrelayed-insecure-inner` is synthetic, and its parent Safe `0x9c4A4204B79DD291D6b6571C5bE8BBCd0672B3cd` has no code at block 25889841. They are faults of the vectors, not of the checks, and the vectors' repository should fix them, but the harness reports them as `wrong`, since only `arbot`'s identification says so, and a bug in it would look the same.
- **4 vectors are found insecure under R-4.0 rather than their own rule.** `safenet/announcement-with-relaying` and `safenet/cancel-with-relaying` name the same MultiSendCallOnly as their Safe; `settings-change/bybit-mastercopy-delegatecall`'s Safe has the singleton `0x34CfAC646f301356fAa8B21e94227e3583Fe3F5F`, which isn't a Safe singleton of a § 2.1 version; and `module-execution/gnosis-pay-delay-module-fail-open`'s account has code that isn't a § 2.1 SafeProxy. The `arbitrate` skill ignores an insecure classification whose rule isn't disputed, so the agents judge their rules.
- **The 9 EOA vectors are insecure under R-4.0**, since no Safe exists at their Safe addresses, as the vectors' README says. The harness ignores that finding, and `arbot safe` and `arbot executions -all` fail on them, as they must.
- **The 2 BNB Chain vectors are `off-network`.** Article I doesn't list BNB Chain, where R-4.0 finds every transaction insecure; `superfortune-legitimate-transfer` expects `secure` under R-4.3, which the Charter doesn't apply there. Their commands also hit node errors: the default BNB Chain node doesn't keep the state of their blocks, and the chain has no Safe Transaction Service.
- **No command failure exposed a bug in `arbot`'s facts.** The failures are the expected ones: an account that isn't a Safe, a chain without a service, a node without old state, and a delegate call of a Safe whose singleton isn't of a § 2.1 version, which `simulate -all` can't run. One exposed a poor error: `arbot executions -all` on an account without code failed with a decoding error, which it now explains. A rate limit of the Safe Transaction Service, which hit the vectors of the Safe `0x888614448Eb7c766864faFb1Dd20ff0b47988a87` in one run, fails `executions -all` and `transfers -all` as it should, but without the hint to run them again that a node's failure has, since `ethrpc.Transient` only covers nodes; the harness runs them again itself.

## Manual steps, before and after

### Where the manual work came from

These are the manual steps of the 14 arbitrations of 2026-11-12 to 2026-11-19, and what the evidence skills do about each.

| Manual step or gap | Requests | Now |
| --- | --- | --- |
| Whole-life Safe history needed; log scans time out on public RPCs, so the Safe Transaction Service was used by hand. | all R-4.3 cases, `a049284bbb54`, the `0x1235…` cluster | Done: `executions -all` and `transfers -all`, confirmed onchain. |
| A node's rate limit or a pruned node was read as "no receipt" or "no code", so executions were wrongly rejected; agents had to be told to retry with another RPC configuration. | `2a00c4f75bcf`, `edc790eff7b7`, `5f5ff7ee811e` | Done for the misreading: commands fail instead. Running a failed command again, or with another RPC URL, is still needed, but the `evidence` skill says when. |
| Token name, decimals, balance, and allowance unknown; agents named tokens from memory and had to be told to stop. | every transfer case | Done: `account`, with the values that the token claims. |
| No reads of another address: a recipient's owners, history, deployer, or age. | `6814ad32bff9`, the `0x1235…` cluster | Partly: `account` and `safe -address` read its code, identification, balance, nonce, and settings. With an optional Etherscan key, `explorer creation`, `txs`, and `tokentx` give its deployer and age, confirmed onchain, and its own history, offchain; without one, they are still manual. |
| Call arguments decoded by hand; created addresses computed with `cast`. | `a1d41dbda94d`, `cbfadf6b9595`, `8581d6d57749` | Done: `calls -abi`, `executions -calls -abi`, `calls`' derived `creates`, and `simulate -all -trace`. |
| A batch couldn't be simulated in sequence, so a call reverted alone (`DeploymentFailed()`) with no explanation. | `cbfadf6b9595` | Done: `simulate -all`, with the revert error decoded. |
| No public security flags; both R-4.6 cases ended with no accepted case. | `cbfadf6b9595`, `8581d6d57749` | Still manual: no trusted provider is integrated. The `target-risk` playbook records it as a decisive gap, which requires human review. |
| The Safe transaction was executed before the proposal or during voting, and nothing reported it. | `a049284bbb54`, `cbfadf6b9595` | Done: `status`. |
| Report hashes were checked by an ad-hoc script, and wrong values had slipped into drafts twice. | `3d837c9edcb8`, `a1d41dbda94d` | Done: `check-report`, which also checks numbers, quotations, precedents, and the evidence summary. |
| Orchestration nudges were repeated in every prompt: run commands one at a time, use the RPC override, name tokens by address, never decode by hand, sub-agents can't write `report.md`. | all | Done: the `evidence` and `arbitrate` skills have them, so the orchestrator passes the skill instead of the nudges. |

Of these 10, 7 are done, 2 are done in part, and 1, the public security flags, is still manual.

### Two past cases, step by step

The interventions below are those that the arbitrations' files in `.msgboard/<id>/` record (the evidence reports, the oversight findings, the evidence requests, and the cases and judgments), with the plan's account of the session. Each is marked with what does it now.

#### `998b9630ee77`: R-4.3, secure, accepted after 3 cases and 3 oversight rounds

| # | Intervention then | Now |
| --- | --- | --- |
| 1 | The commands were run with a hand-picked RPC configuration, after the default node rate limited them. | Partly: a rate limit fails the command, which the skill says to run again or with another configuration. Picking the configuration is still manual. |
| 2 | The whole history came from a command on an unmerged branch (`arbot history`), because `executions -blocks` scans time out on public RPCs. | Done: `executions -all` (pinned by `TestCase998b9630ee77`). |
| 3 | The transfer scans failed: one over the Safe's life was killed after 9 minutes, and one of 200,000 blocks hit a rate limit, so there was no log evidence of transfers to or from the recipient. | Done: `transfers -all -to` and `-in -from` (pinned: the 8 transfers). |
| 4 | The detective named the tokens "USDT" and "USDC" from memory; oversight caught it, and the case went round again. | Done: `account` reports what each token claims, and the skill forbids names from memory. |
| 5 | The tokens' decimals were a gap. | Done: `account`. |
| 6 | The recipient's code hash was the same as the Safe's proxy, but `arbot` didn't identify it as a Safe or read its owners. | Done: `account` identifies it, and `safe -address` reads its settings. |
| 7 | The detective and oversight each grouped the Safe's 158 transfers into 23 recipients and counted them from the JSON. | Still manual: `arbot` lists the transfers and `transfers -all -to` counts one recipient's, but tallying all recipients is the agent's derived fact. |
| 8 | Whether any recipient looked like the disputed one was checked by eye. | Still manual: the `value-recipient` playbook records it as a derived fact. |
| 9 | Lookalikes among the accounts that sent to the Safe were a gap. | Partly: `transfers -all -in` lists the senders; comparing them is still by eye. |
| 10 | The draft's values were checked with an ad-hoc script. | Done: `check-report`. |
| 11 | The prompt nudges: one command at a time, the RPC override, tokens by address, no hand decoding, the orchestrator writes `report.md`. | Done: the skills. |
| 12 | A claim in the case that the report didn't have ("no lookalike") was caught by oversight, and the case went round again. | Still manual: `check-report -file` checks a case's values, not its claims; oversight still reads it. |

Of 12 interventions, 7 are done by the commands and playbooks, 2 in part, and 3 are still manual.

#### `cbfadf6b9595`: R-4.6, no accepted case after 7 cases, 6 oversight rounds, and 3 judgments

| # | Intervention then | Now |
| --- | --- | --- |
| 1 | Commands were run on a hand-picked RPC configuration, and run again after rate limits. | Partly, as for `998b9630ee77`. |
| 2 | The arguments of call 1 (`setApprovedDepositors`) and calls 2 to 4 (`deploy(bytes,bool)`) weren't decoded, so the detective simulated each call alone to read its inputs, after an evidence request of 9 items. | Done: `calls -abi`. |
| 3 | The calls of the Safe's earlier executions at nonces 15, 16, and 24 weren't decoded (`function: null`). | Done: `executions -all -calls -abi`. |
| 4 | Each call was simulated alone, so call 3 reverted with `0x30116425`, undecoded, and calls 2 and 4 both returned `0x3Da548EE3C9d7fb614C869e53ffdaa1a07b8A0a0`. | Done: `simulate -all` runs the batch in order, and decodes `DeploymentFailed()`. |
| 5 | The three created addresses were worked out by hand from traces, and checked with an ad-hoc verifier of CREATE and CREATE2 arithmetic. | Done: `simulate -all -trace` lists the three `CREATE` frames, and `calls` derives CreateCall deployments (pinned by `TestParseTrace` and `TestCasecbfadf6b9595HumanReview`). |
| 6 | The claim that the transaction was "executed while disputed" was corrected by hand. | Done: `status` reports `during-voting`, before the freeze (pinned by `internal/status` `TestFixtures`). |
| 7 | The `owner()` of `0x40f7085841034f752F17592875703EB5032f2B42` and the `safe()` of the helper `0xe530c247341e857042FdFf953E73Dd6c580f8D42` couldn't be read. | Still manual: no command calls a view function that the transaction doesn't make. |
| 8 | The creator, creation, and age of the targets and of the depositor that call 1 approves were gaps. | Partly: with an Etherscan key, `explorer creation` reports them, confirmed onchain: the helper `0xe530c247341e857042FdFf953E73Dd6c580f8D42` was created by `0x46565eeDB0bb948aE787dD105cCE300f736dA28e` through the factory `0x9b35Af71d77eaf8d7e40252370304687390A1A52` in block 26432157, 21 blocks before `safeBlock`. Without a key, still manual. |
| 9 | Public security flags weren't checked (§ 2.7). | Still manual, and decisive: the summary requires human review (pinned by `TestCasecbfadf6b9595HumanReview`). |
| 10 | The code that the transaction creates wasn't examined. | Still manual: `simulate -trace` reports the init code hash and size, not what the code does. |
| 11 | Events that aren't well-known were printed undecoded, and read by hand. | Still manual. |
| 12 | The draft's values were checked with an ad-hoc script. | Done: `check-report`. |
| 13 | The prompt nudges. | Done: the skills. |

Of 13 interventions, 7 are done by the commands and playbooks, 2 in part, and 4 are still manual.

### What is still manual

- **Public security flags (§ 2.7).** No trusted, free provider is integrated, so the Council checks flags by hand. Every R-4.6 summary records the gap as decisive, and so requires human review.
- **Judging created code and unverified contracts.** `arbot` reports what a transaction creates and calls, and the verified source, but not what unverified or created code does.
- **An account's own history, creator, and age**, other than a Safe's, without an Etherscan key, which `arbot explorer` needs, and isn't available for every network or user; and view functions that the transaction doesn't call.
- **Derived comparisons**, such as tallying a Safe's recipients or spotting lookalike addresses, which the playbooks ask the agents to record as derived facts.
- **Picking another RPC configuration** after a node fails, which the skill prescribes but doesn't do.
- **Claims in a case** that aren't values, which oversight still checks by reading.
