# Playbook: target-risk

For R-4.6: a transaction is insecure if it interacts with an address or contract where admissible evidence supports a reasonable finding that the target is malicious, compromised, exploited, or otherwise high-risk before the Council ruling. The Council checks onchain data (§ 2.8), public security flags (§ 2.7), source reliability (§ 2.6), and corroborating onchain evidence. The evidence is every account that the transaction reaches, what each is, and what the transaction does with it. Run [`proposal-status`](proposal-status.md) first, and follow the rules of [`SKILL.md`](SKILL.md) for every command.

## Commands

1. **The accounts that the transaction calls**: `arbot calls -json -abi -request-file <request.json>`.
   - Shows: every call, its account's code hash and size and § 2.1 identification, its function, decoded as a well-known function or, with `-abi`, with the ABI that Sourcify verified (offchain, § 3.4), and the derived address of each contract that a CreateCall call deploys (`creates`), which is arithmetic over onchain data, not a deployment that the chain shows.
   - Can't show: the accounts that the called contracts call in turn, which step 4 shows.
2. **Each account**: `arbot account -json -request-file <request.json> <address>`, for each target, each address that a call passes, such as a recipient, spender, or depositor, and each created address.
   - Shows: its code, identification, balance, and nonce as of `safeBlock`, and for a token, what it claims to be. A created address without code at `safeBlock` doesn't exist yet.
   - Can't show: who deployed it or when.
3. **Each contract's code**: `arbot signatures -json -request-file <request.json> <address>`, then `arbot contract-source -json -request-file <request.json> <address>`, or with `-file <path>`, one file of its source.
   - Shows: the functions, fallback and receive, and source files that Sourcify verified for the code at `safeBlock`, with Sourcify's URL, `observedAt`, `verifiedAt`, match, contract name, and compiler (offchain, § 3.4). Read the source of the functions that the transaction calls, such as their access control, and cite the file and function.
   - Can't show: an unverified contract's functions, or a proxy's implementation, which needs a lookup of its own. A contract verified after the proposal is later evidence (§ 3.6).
4. **What the transaction does**: `arbot simulate -json -all -trace -request-file <request.json>`, and `arbot simulate -json -call <n> -request-file <request.json>` for a call that reverts or whose outputs matter, such as the address that a factory returns.
   - Shows: the whole transaction run in order on the state at `safeBlock`, so that each call runs on the effects of those before it; whether it returns or reverts, with the decoded revert error, such as `DeploymentFailed()`; and with `-trace`, every nested call and every `CREATE` and `CREATE2` frame, with the created address, the creator, and the size and hash of the init code.
   - Can't show: the runtime code that a creation deploys, or what it does, the effects of the executed transaction, or a call in a later block. A node that doesn't serve the trace gives `trace unavailable`, which is a gap, not an answer.
5. **The Safe's history with the targets**: `arbot executions -all -calls -json -request-file <request.json>`, and for an earlier transaction whose logs matter, `arbot events -json -request-file <request.json> <transaction-hash>`.
   - Shows: the Safe's earlier executions that call the same contracts or pass the same addresses, with their decoded calls, and the events that an earlier transaction logged, decoded if they are well-known (onchain, § 2.8). Any contract can log a well-known event, so the emitter matters.
   - Can't show: other accounts' use of the targets, and events that aren't well-known, which are printed undecoded.
6. **Token flows**: `arbot transfers -all -json -request-file <request.json>` and `arbot transfers -all -in -json -request-file <request.json>`, when the case turns on what moved between the Safe and a target, as the [`value-recipient`](value-recipient.md) playbook describes them.
7. **Optional, with an Etherscan key: each target's deployer and age**: `arbot explorer creation -json -request-file <request.json> <address>`, and `arbot explorer source -json -request-file <request.json> <address>` for a second record of its verification, next to Sourcify's, for each target that isn't the Safe or a § 2.1 contract.
   - Shows: from Etherscan (offchain, § 3.4, with its `url`, `observedAt`, and `confirmedOnchain`): the target's creator, factory, creation transaction, and block, confirmed onchain if `confirmedOnchain` is `true`, which give its age at `safeBlock`, and whether Etherscan has verified its source, with its name and compiler, and reads it as a proxy, with its implementation.
   - Can't show: labels or security flags (§ 2.7), which Etherscan's free plan doesn't have; that the factory deployed it, which a receipt doesn't say; or when Etherscan verified the source, so a verification after the proposal is later evidence (§ 3.6). A creation with `afterCutoff` isn't evidence. Without an Etherscan key, the commands fail: record the creator and age as a gap, "Etherscan not configured".

## Facts to Record

- Every account that the transaction reaches, by call, nested call, argument, and creation, with its code, identification, and nonce at `safeBlock` (§ 2.8).
- What Sourcify verified for each contract, the functions that the transaction calls, and their access control, with Sourcify's observation and verification times (§ 3.4, § 3.6).
- The simulation's result, its reverts, and the contracts that it creates, with their init code hashes (§ 2.8).
- The Safe's earlier interactions with each target (§ 2.4).

## Known Gaps

Record these whenever they apply:

- **No public security flags checked (§ 2.7): no trusted provider is integrated; the Council checks them by hand.** Record this gap in every R-4.6 case, for every target, as decisive: a reliable flag would change the verdict, and the absence of a checked flag is not evidence that there is none. A summary with it has `humanReview` set to `true`.
- The runtime code of each contract that the transaction creates, and what it does: no `arbot` command decodes init code or shows the code that it deploys. Record it as decisive when the created contract receives value, permissions, or control.
- The creator, creation transaction, and age of each target that isn't the Safe, unless an Etherscan key is configured (step 7): without one, record it as "Etherscan not configured".
- Calls to view functions that the transaction doesn't make, such as a contract's `owner()`.
