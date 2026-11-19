# Playbook: settings-and-delegatecall

For the deterministic rules of Article IV, Part A: R-4.0, a transaction is insecure if its chain isn't one that Article I lists, or its Safe address isn't a Safe (§ 2.1) at proposal time; R-4.1, if it modifies a Safe setting (§ 2.10), unless it falls within an allowed exception; and R-4.2, if it performs a delegate call that changes a storage slot of the Safe, other than those of the `signedMessages` mapping. These rules apply directly (§ 3.7), so the evidence is the calls and the code they reach. Run [`proposal-status`](proposal-status.md) first, and follow the rules of [`SKILL.md`](SKILL.md) for every command.

Two properties of the Safe are formally verified, and need no evidence (see Safe storage in `AGENTS.md`): only a call to the Safe itself, other than an empty one, and a delegate call can modify the Safe's storage, and the MultiSend and MultiSendCallOnly contracts of § 2.1 versions modify nothing themselves, so the calls that `arbot calls` decodes from them take the place of the transaction. Don't record them as gaps.

## Commands

1. **The classification**: read `classification.json`, which the orchestrator wrote with `arbot classify -json -request-file <request.json>`. Don't run `arbot classify` again.
   - Shows: the deterministic checks' verdict, such as `insecure` under R-4.1 for a call of `addOwnerWithThreshold` on the Safe, or `null`. The orchestrator only asks for evidence when the checks didn't decide the disputed rules.
   - Can't show: anything about a call that no check matches, which is why the rest of this playbook runs.
2. **The Safe and its calls**: `arbot calls -json -abi -request-file <request.json>`.
   - Shows: the chain ID (Article I, R-4.0); the Safe's code hash, `proxyVersion`, `singleton`, and `singletonVersion`, which make it a Safe under § 2.1 only if both versions are set (R-4.0); every call, with its operation, whether its account is the Safe itself (`self`, R-4.1), and the § 2.1 contract that its account is, such as MultiSend, MultiSendCallOnly, or CreateCall (R-4.2); each call's function, such as a settings function on the Safe; and the derived address of each contract that a CreateCall call deploys (`creates`).
   - Can't show: what a delegate call to a contract other than a § 2.1 MultiSend does to the Safe's storage.
3. **The Safe's settings**: `arbot safe -json -request-file <request.json>`, and for a setting that a call changes, `arbot safe -json -at <block> -compare <safe> -request-file <request.json>`, with an earlier block of the Safe's history, to compare its settings then and at `safeBlock`.
   - Shows: the Safe's settings at `safeBlock` (§ 2.10), and how they changed between the two blocks, such as a module that an earlier transaction enabled.
   - Can't show: the settings after the transaction, which would be its effects (§ 3.6).
4. **Each delegate call's target**, other than a § 2.1 MultiSend: `arbot account -json -request-file <request.json> <target>`, `arbot signatures -json -request-file <request.json> <target>`, and `arbot contract-source -json -request-file <request.json> <target>`.
   - Shows: the target's code and identification, and the functions and source that Sourcify verified for it, with Sourcify's URL and `observedAt` (offchain, § 3.4). The source shows what the delegate called function writes, such as a storage slot of the Safe, read from the verified code (R-4.2, § 2.2).
   - Can't show: the storage writes of an unverified contract. A contract verified after the proposal is later evidence (§ 3.6).
5. **What the transaction does**: `arbot simulate -json -all -trace -request-file <request.json>`.
   - Shows: the whole transaction run in the Safe's own context on the state at `safeBlock`, delegate calls included, with `simulateAndRevert`, whether it returns or reverts, and with `-trace`, every nested call, such as a `DELEGATECALL` that a delegate called contract makes, and every contract that it creates.
   - Can't show: the storage slots that a delegate call writes, which the trace doesn't list, the effects of the executed transaction, or the signatures, nonce, guard, and gas checks, which a simulation skips.

## Facts to Record

- The chain ID, and whether Article I lists it (R-4.0).
- The Safe's proxy and singleton versions, or why it has none (§ 2.1, R-4.0).
- Each call to the Safe itself, with its function and arguments, and each settings function that it calls, with the exceptions that it does or doesn't meet: non-batched, `execTransaction`, `to` the Safe, operation `0`, value `0`, and the function and its arguments (R-4.1).
- Each delegate call, its target, the target's identification, and what its verified source writes to the Safe's storage (R-4.2).
- The Safe's settings at `safeBlock`, and any change of them in its history that bears on the calls (§ 2.10).

## Known Gaps

- The storage slots that a delegate call to an unverified contract writes: no `arbot` command reports a simulation's storage writes. Record it as decisive when R-4.2 is disputed and the target isn't a § 2.1 MultiSend.
- The settings after the transaction, which aren't evidence anyway (§ 3.6).
