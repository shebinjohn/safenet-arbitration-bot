# Playbook: approval-spender

For R-4.4, a transaction is insecure if it grants spend authority, operator rights, or equivalent permissions to an address outside the expected target set (§ 2.4), and for R-4.5, if it grants a functionally unlimited approval (§ 2.5). The evidence is each grant, its amount against the token, and the Safe's history with the spender. Run [`proposal-status`](proposal-status.md) first, and follow the rules of [`SKILL.md`](SKILL.md) for every command.

## Commands

1. **The grants**: `arbot calls -json -abi -request-file <request.json>`.
   - Shows: each ERC-20 `approve(spender, amount)`, decoded as a well-known function, with the token that it is called on, and with `-abi`, other functions decoded with the ABI that Sourcify verified (offchain, § 3.4), such as `setApprovalForAll(operator, approved)` of an ERC-721 or ERC-1155 token, or a protocol's own permission function (R-4.4). The spender or operator is the target address (§ 2.4).
   - Can't show: a permission that a function grants without naming it in its arguments, or one of a contract without a verified ABI, which is a gap. A max `uint256` amount is `115792089237316195423570985008687907853269984665640564039457584007913129639935`, which is always functionally unlimited (§ 2.5, R-4.5).
2. **Each token, with its spender**: `arbot account -json -spender <spender> -request-file <request.json> <token>`.
   - Shows: the token's decimals and total supply (§ 2.5), its claimed name and symbol, the Safe's balance of it, and the Safe's allowance to the spender, as of `safeBlock`. Compare the amount with the supply and the balance as derived facts.
   - Can't show: the token type beyond answering as an ERC-20, or an ERC-721 or ERC-1155 operator approval, which `arbot account` doesn't read. A `totalSupply` of null means that the token didn't return one.
3. **Each spender**: `arbot account -json -request-file <request.json> <spender>`, then, if it has code, `arbot signatures -json -request-file <request.json> <spender>`, and `arbot contract-source -json -request-file <request.json> <spender>` for its verified source.
   - Shows: its code, whether it is a § 2.1 contract, its nonce and balance, and the functions and source that Sourcify verified for it, with Sourcify's URL, `observedAt`, and `verifiedAt` (offchain, § 3.4). A contract verified after the proposal is later evidence, which the Council must say was observed later (§ 3.6).
   - Can't show: who controls it, or what a proxy's implementation does: a proxy's ABI has only the proxy's functions.
4. **The Safe's history with each spender**: `arbot executions -all -calls -json -request-file <request.json>`, and `arbot transfers -all -json -to <spender> -request-file <request.json>` and `arbot transfers -all -in -json -from <spender> -request-file <request.json>`.
   - Shows: earlier approvals to the spender and calls to it in the Safe's executions, and the token flows between them, over the Safe's whole history, as the [`value-recipient`](value-recipient.md) playbook describes them (§ 2.4).
   - Can't show: approvals that the Safe signed offchain, such as permits, which aren't executions, and an earlier allowance's use by the spender other than through the transfers that it logged.
5. **What the grant does**: `arbot simulate -json -call <n> -request-file <request.json>`, for the `<n>`th call of `arbot calls`, or `arbot simulate -json -all -trace -request-file <request.json>` for a batch or a delegate call.
   - Shows: whether the call returns or reverts on the state at `safeBlock`, its decoded outputs, and with `-trace`, the calls that it makes.
   - Can't show: the allowance after the call, since a simulation has no effects to read back, or the call's effects in a later block.
6. **Optional, with an Etherscan key: each spender's deployer, age, and own history**: `arbot explorer creation -json -request-file <request.json> <spender>` and `arbot explorer txs -json -request-file <request.json> <spender>`.
   - Shows: from Etherscan (offchain, § 3.4, with its `url`, `observedAt`, and `confirmedOnchain`), up to `safeBlock`: a contract spender's creator, factory, creation transaction, and block, confirmed onchain if `confirmedOnchain` is `true`, and the spender's transactions with accounts other than the Safe, which aren't confirmed onchain.
   - Can't show: labels, internal calls, or more than 10,000 results, so a `capped` listing is incomplete. Without an Etherscan key, the commands fail: record the spender's creator, age, and history as a gap, "Etherscan not configured".

## Facts to Record

- Each grant: the token, the spender or operator, the amount, and the function (R-4.4, R-4.5).
- The token's decimals, total supply, and the Safe's balance, and the amount as a share of each, as derived facts (§ 2.5).
- The current allowance to the spender at `safeBlock`.
- The spender's code, identification, and verified functions, with Sourcify's observation time.
- The Safe's earlier grants to, calls to, and transfers with the spender, or that it has none (§ 2.4).
- The protocol-recorded purpose, or that there is none, which § 2.5 uses for the stated interaction (§ 2.11).

## Known Gaps

- ERC-721 and ERC-1155 operator approvals already in place, which no `arbot` command reads.
- Approvals signed offchain, such as permits.
- The nature and scale of the interaction beyond the purpose and the Safe's history, and standard user behavior (§ 2.5, § 2.9).
- Public labels or flags on a spender (§ 2.7, § 3.4), which no `arbot` command reports.
- A spender's creator, age, and own history, unless an Etherscan key is configured (step 6): without one, record them as "Etherscan not configured".
