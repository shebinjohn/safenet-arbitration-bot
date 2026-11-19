# Playbook: value-recipient

For R-4.3: a transaction is insecure if it sends value to a recipient outside the expected target set (§ 2.4). The evidence is who receives value, and the Safe's onchain history with each recipient. Run [`proposal-status`](proposal-status.md) first, and follow the rules of [`SKILL.md`](SKILL.md) for every command.

## Commands

1. **The recipients**: `arbot calls -json -abi -request-file <request.json>`.
   - Shows: each call's `value` in wei, the recipient and amount of each well-known token call, such as ERC-20 `transfer(to, amount)` and `transferFrom(from, to, amount)`, and with `-abi`, the arguments of other functions, decoded with the ABI that Sourcify verified (an offchain source, § 3.4, with its URL and `observedAt`). The recipient of an ERC-20 transfer is the target address, and the token is an intermediate contract (§ 2.4).
   - Can't show: a recipient that the called contract picks itself, such as a router's, or one in an argument that isn't decoded, which is a gap. Simulate the transaction for what it sends (step 7).
2. **Each token**: `arbot account -json -request-file <request.json> <token>`.
   - Shows: the token's code, the name, symbol, decimals, and total supply that it returns, and the Safe's balance of it, as of `safeBlock`. These are what the contract claims, not what it is: name the token by its address, with them.
   - Can't show: whether the token is the one that its name suggests. A token that doesn't answer as an ERC-20 has `erc20` null.
3. **Each recipient**: `arbot account -json -request-file <request.json> <recipient>`.
   - Shows: its code hash and size, whether it is the Safe or a § 2.1 contract, its native balance, and its nonce, as of `safeBlock`. An account without code and with nonce 0 has never sent a transaction on this chain.
   - Can't show: who controls it, its history with accounts other than the Safe, or labels.
4. **A recipient that is a Safe**: `arbot safe -json -address <recipient> -request-file <request.json>`, then `arbot safe -json -compare <recipient> -request-file <request.json>`.
   - Shows: its settings, or why it has none, such as `not-a-safe-proxy`, and the owners and settings that it shares with the Safe.
   - Can't show: that it belongs in the expected target set. Shared owners are a fact about who controls two Safes, and the ruling on `0x8b83aa12d81c160fc8f1b2dd5af6e4f8032e61a6b648ff9e2cdb80f6570c3b2d` gave a recipient's owners, which matched the Safe's, no weight (§ 2.4).
5. **The Safe's executions**: `arbot executions -all -calls -json -request-file <request.json>`.
   - Shows: every Safe transaction that the Safe executed up to `safeBlock`, from the Safe Transaction Service (offchain, § 3.4, with its URL and `observedAt`), each confirmed onchain, with its calls decoded as `arbot calls` decodes them, or with `-abi` too, also with the ABIs that Sourcify verified, which asks Sourcify once for each account; whether the history is `complete`, and the `missingNonces`; the Safe's `creation`; and the request's own execution, if it was executed by `safeBlock`, apart from the others (§ 3.6). Look in the decoded calls for each recipient as a target, a token recipient, or an argument.
   - Can't show: executions by modules, which use no nonce. A history that isn't `complete` leaves its missing nonces as a gap.
6. **The Safe's transfers**: `arbot transfers -all -json -to <recipient> -request-file <request.json>`, for the ERC-20 transfers out of the Safe to each recipient, and `arbot transfers -all -in -json -from <recipient> -request-file <request.json>`, for the ERC-20 and native transfers from it into the Safe. Run both without the filter for the Safe's whole pattern of transfers, if the case needs it.
   - Shows: the `Transfer` events of any token, and the Safe's `SafeReceived` events for native currency, in the blocks of the Safe's history that the Safe Transaction Service names, read onchain, with the warnings about transfers that the logs don't have. An ERC-20 transfer can be made by an account with an allowance, without the Safe making a transaction. Any contract can log a `Transfer` event, so the token matters, such as a fake token in an address poisoning attack.
   - Can't show: native currency sent out of the Safe, ERC-721 transfers, native currency that reached the Safe without a `SafeReceived` event, such as before the Safe's address had code, and transfers in blocks that the service doesn't name.
7. **What the transaction sends**, if a call's recipient isn't in its data: `arbot simulate -json -all -trace -request-file <request.json>`.
   - Shows: the whole transaction run on the state at `safeBlock`, with every nested call of the trace and its value, which is onchain data (§ 2.8).
   - Can't show: the effects of the executed transaction, which can differ in a later block. It needs a node that serves `debug_traceCall`, or it says that the trace is unavailable.

## Facts to Record

- Each recipient and what it receives, with the token's address, decimals, and claimed symbol (§ 2.4, R-4.3).
- Each recipient's code, nonce, and balance, and its settings if it is a Safe.
- Each earlier execution or transfer that involves a recipient, by nonce, block, and transaction, or that none does, over the Safe's whole history, with the source and whether the history is complete (§ 2.4).
- The Safe's pattern of comparable payments, such as earlier first payments to new recipients (§ 2.4, § 2.9).
- Recipients that resemble an address of the Safe's history, such as sharing its first and last characters, as derived facts from the addresses that `arbot` printed (R-4.3, address poisoning).
- The protocol-recorded purpose, or that there is none (§ 2.11).

## Known Gaps

- Native currency sent out of the Safe, and ERC-721 transfers, which `arbot transfers` doesn't list.
- Native currency sent to the Safe's address before it had code, which logged no `SafeReceived` event.
- A recipient's own history with accounts other than the Safe, its creator, and its creation.
- Standard user behavior for comparable transactions (§ 2.9), and public labels or flags on a recipient (§ 2.7, § 3.4), which no `arbot` command reports.
