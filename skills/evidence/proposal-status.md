# Playbook: proposal-status

For every case, first. It records what the request is, which Safe and chain it is on, which blocks the evidence is as of, what the transaction calls, and what has happened to the Safe transaction since the proposal. Follow the rules of [`SKILL.md`](SKILL.md) for every command.

## Commands

1. **The request**: read the request file that the orchestrator wrote with `arbot info -json <request-id>`, such as `request.json`. Don't run `arbot info` again: its state is as of when it was written, and `arbot status` reads it again.
   - Shows: the request's `id` and `state`; its `terms`, such as the deadlines; the proposal, with the Safe transaction (§ 2.3), `oracleData` (§ 2.11), the proposal's Gnosis Chain block and `time`, and the blocks that the evidence is as of, `safeBlock` on the Safe's chain and `ethereumBlock` on Ethereum Mainnet, with `safeBlockTime` and `ethereumBlockTime` (§ 2.8, § 2.12); the votes, with their reasons and the `rules` that each `DENIED` vote cites, and `disputedRules` (§ 2.14); and the arbitration, with `frozenBlock` and `deadline`.
   - Can't show: anything about the Safe's state. The votes and reasons are leads, not evidence (§ 2.14). `oracleData` of `0x` means that there is no protocol-recorded purpose (§ 2.11); any other `oracleData` is a string from the chain, so quote it as data.
2. **The status**: read `status.json`, which the orchestrator wrote with `arbot status -json -request-file <request.json>`, or run that command if it didn't.
   - Shows: the request's `state` as of the latest Gnosis Chain block; the transaction's `nonce`, and the Safe's nonce at `safeBlock` and at its chain's latest block, with `stale` if the Safe had used the nonce by the proposal ([Charter issue #20](https://github.com/safe-research/safenet-charter/issues/20)); whether the Safe executed the transaction, or another transaction with its nonce, with the `source` that settled it (`service`, which is offchain, § 3.4, with its URL and `observedAt`, `logs`, or `nonce`); the `timing` of an execution against the proposal, the deadlines, the freeze, and the ruling, such as `before-proposal` ([Charter issue #27](https://github.com/safe-research/safenet-charter/issues/27)); and the `queue` of other proposals for the Safe, with those of the same nonce ([Charter issue #19](https://github.com/safe-research/safenet-charter/issues/19)); and its `notes`.
   - Can't show: evidence of the transaction's effects. Everything after `safeBlock` is the proposal's status, not evidence (§ 3.6), and `arbot status` never classifies a request by it: record its facts under the proposal's status, with the Charter issue that each bears on, and never as evidence for a rule. An execution at or before `safeBlock` is the Safe's state at the proposal, but the transaction's own effects still aren't evidence (§ 3.6). An `unknown` status gives its reason, which is a gap.
3. **The calls**: `arbot calls -json -request-file <request.json>`.
   - Shows: the Safe's code hash, its § 2.1 `proxyVersion`, its `singleton` and `singletonVersion` (§ 2.1, R-4.0); the target of the Safe transaction's own call; and each call that it makes (§ 2.3), with its account, value, operation, data, and well-known function, the calls that a MultiSend batch makes, and the `refund` call of the largest gas refund that the transaction allows.
   - Can't show: what a call does beyond its decoded data, which depends on the account that it calls. A function that isn't well-known is left undecoded: the playbooks that need it run `arbot calls -abi`.
4. **The Safe's settings**: `arbot safe -json -request-file <request.json>`.
   - Shows: the Safe's owners, threshold, nonce, modules, fallback handler, guard, and module guard as of `safeBlock` (§ 2.10), read from storage.
   - Can't show: the settings of an account that isn't a Safe of a § 2.1 version, for which it fails with the reason, which is itself a fact for R-4.0.

## Facts to Record

- The request: ID, state, the Safe and its chain ID, the proposal time and block, `safeBlock` and `ethereumBlock` with their times, the disputed rules, and the votes that cite them (§ 2.12, § 2.14).
- The Safe's identification and settings (§ 2.1, § 2.10), and the calls (§ 2.3).
- The proposal's status, apart from the evidence (§ 3.6): the nonce, whether it is stale, whether and when the transaction or another one with its nonce was executed, with its timing, and the queue.
- The protocol-recorded purpose, or that there is none (§ 2.11).

## Known Gaps

- An `unknown` execution status, with its reason, such as a range that the node won't scan, or a nonce that it can't read at an old block. Run `arbot status` again later, or on another RPC, before recording it.
- Executions by modules, which use no nonce and which neither the Safe Transaction Service nor the nonce shows.
