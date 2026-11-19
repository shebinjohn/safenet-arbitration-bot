# Draft Ruling: Request a049284bbb54

> **Not accepted.** The `judge` accepted no case within 3 rounds. This report gives the last case and the reasons that it was thrown out, for the Council to rule on without a draft.

## Verdict

**No accepted case.** The last case argues **`insecure`** under `R-4.3`.

- **Decided by**: no accepted case. The `prosecutor`'s case 7 was found `honest` by `oversight` and thrown out by the `judge` in round 3 (`judgment-7.md`).
- **Reason argued by the last case**: R-4.3 makes a transaction insecure if it sends value to a recipient outside the expected target set (§ 2.4). The recipient `0xc01ea83bacda63097627f45d987684569fdceae5` has no code, is not an owner or module, and appears in none of the Safe's executions (nonces 0 to 16, complete). It sent the Safe nothing, and the Safe sent it nothing apart from this transaction, in blocks 25381212 to 26381211, which cover the Safe's whole life since its creation at block 25468989. Under the reading of § 2.4 in `0xce3b5448…`, a new recipient needs positive proof that it belongs, and none is shown.
- **Why it was not accepted**: the case concedes that a transfer from the recipient to the Safe's address before block 25381212, while the address had no code, was not searched. It says the Council may reasonably read such a transfer as placing the recipient inside the set, which would make the verdict `secure`, as in `0x586a2ed7…`. For native value the unsearched period runs up to the creation block 25468989, since a codeless address logs no `SafeReceived` event.
- **Onchain action, if the Council adopts the last case**: `resolveDispute(0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27, false, context)`, by Gnosis Chain block 49683293

## Request

|  |  |
| --- | --- |
| Request ID | `0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27` |
| State | `FROZEN`, frozen in Gnosis Chain block [49199453](https://gnosisscan.io/block/49199453) |
| Deadline | Gnosis Chain block [49683293](https://gnosisscan.io/block/49683293), after which the arbitration can time out |
| Safe | [`0xc4994fe4f501a02434ca7f25b14fd803f5d7a388`](https://etherscan.io/address/0xc4994fe4f501a02434ca7f25b14fd803f5d7a388) on Ethereum Mainnet (chain ID 1) |
| Safe tx hash | `0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00f`, nonce 17 |
| Proposed | 2026-11-10T01:26:25Z, in Gnosis Chain transaction [`0x58b5f675b8a12ffa11931416843a139daa181a32a071770db4f05854cb251b60`](https://gnosisscan.io/tx/0x58b5f675b8a12ffa11931416843a139daa181a32a071770db4f05854cb251b60) |
| Evidence as of | Ethereum Mainnet block [26381211](https://etherscan.io/block/26381211), both the Safe's block and the Ethereum Mainnet block |
| Charter version | `bafkreigg6ajqdb52kb644wcxegurt7r6jlclru4mjy3lwxkbh6avm3vgre`, referenced by `charter.safenet-gov.eth` at Ethereum Mainnet block 26381211 (§ 2.12) |
| Sentinel votes | 2 approve, 1 deny, 0 unrevealed |
| Disputed rules | `R-4.3`, the only rule this ruling considers |
| Sentinel fee | 400000, bond 800000000, slash amount 400000 per sentinel, in the smallest unit of the fee token `USDC.e` (`0x2a22f9c3b484c3629090feed35f17ff8f88f76f0`, 6 decimals) |

The Safe transaction was executed onchain in block [26381211](https://etherscan.io/block/26381211), transaction [`0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5`](https://etherscan.io/tx/0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5), the last Mainnet block before the proposal. That is the transaction's own effect, so it isn't evidence (§ 3.6).

## Transaction

| Field | Value |
| --- | --- |
| To | [`0xdac17f958d2ee523a2206206994597c13d831ec7`](https://etherscan.io/address/0xdac17f958d2ee523a2206206994597c13d831ec7) |
| Value | 0 wei |
| Operation | `CALL` |
| Data | `0xa9059cbb000000000000000000000000c01ea83bacda63097627f45d987684569fdceae50000000000000000000000000000000000000000000000000000000000989680` |
| Refund | safeTxGas 0, baseGas 0, gasPrice 0, gasToken `0x0000000000000000000000000000000000000000`, refundReceiver `0x0000000000000000000000000000000000000000` |

| # | To | Value | Operation | Function or data |
| --- | --- | --- | --- | --- |
| 1 | [`0xdac17f958d2ee523a2206206994597c13d831ec7`](https://etherscan.io/address/0xdac17f958d2ee523a2206206994597c13d831ec7) | 0 | `CALL` | `transfer(0xc01ea83bacda63097627f45d987684569fdceae5, 10000000)` (`arbot calls`) |

## Charter Provisions

### `R-4.3`: Value-target manipulation

> - A transaction is insecure if it sends value to a recipient address outside the expected target set.
>
> #### Council applies by checking
>
> - the expected target set under § 2.4;
> - whether the value recipient address is inside or outside that set.
>
> #### For novel recipient addresses, Council weighs whether
>
> - the recipient address has legitimate onchain history consistent with the protocol-recorded purpose (§ 2.11);
> - the recipient address resembles a prior user address in a way consistent with address poisoning;
> - standard users conducting comparable transactions would send value to this recipient address (§ 2.9).

The transaction sends 10000000 units of `0xdac17f95…1ec7` to `0xc01ea83b…eae5`, a novel recipient. The last case finds it outside the set. The `judge` held that the finding is not yet established while a transfer from the recipient before the scanned range, which the case concedes could reverse it, remains unsearched.

### § 2.4: Expected target set

> - The expected target set is the set of economically or permission-relevant target addresses consistent with the user’s established onchain pattern.
>
> #### Target address means
>
> - the address that receives value or tokens, is granted approvals or permissions, or otherwise receives economically relevant effects from the transaction;
> - not merely an intermediate contract address called by the Safe transaction.
>
> #### Council considers
>
> - prior transaction history on the relevant network;
> - protocol-recorded purpose, if any (§ 2.11);
> - standard user behavior for comparable transactions (§ 2.9).
>
> #### Notes
>
> - A new target address is not automatically outside the expected target set.

The recipient named by `transfer` is the target address, and the token contract is an intermediate contract. The open question is whether "prior transaction history" includes transfers to the Safe's address before it had code.

### § 3.7: Rule failure

> - Principle-based rule failure requires a reasonable evidence-grounded Council finding.

R-4.3 is principle-based, so an `insecure` ruling needs a reasonable finding grounded in the evidence.

### § 3.8: Ambiguity

> - If admissible evidence is genuinely evenly balanced on a material security question, the Council rules the transaction insecure.
>
> #### A principle-based insecurity finding is reasonable if defensible by reference to
>
> - admissible evidence;
> - standard user behavior;
> - this charter’s scope and rules.
>
> #### Evidence weighting
>
> - Absence of evidence may be circumstantial evidence, but is not determinative alone.

The last case rests on § 3.8's reasonableness test, not its tie rule. The evidence against the recipient is a set of absences, which § 3.8 says are not determinative alone.

## Reasoning

The argument of the last case, `case-7.md`. It was not accepted.

1. The Safe is a § 2.1 Safe, a proxy of version 1.4.1 with singleton version 1.4.1, and only R-4.3 is disputed (§ 2.1; evidence 1).
2. The transaction is one `transfer` of 10000000 to `0xc01ea83b…eae5`, the target address under § 2.4. The token contract is an intermediate contract (§ 2.4, R-4.3; evidence 2).
3. A new address is not automatically outside the set. Under `0xce3b5448…`, a new recipient is admitted on concrete proof, such as being an owner or having sent the Safe funds earlier (§ 2.4; precedents).
4. No such proof exists. The recipient is not an owner or module, has no code, appears in none of the Safe's executions at nonces 0 to 16, sent the Safe nothing in blocks 25381212 to 26381211, and received nothing from it in that range apart from this transaction. No purpose is recorded (§ 2.4, § 2.11, R-4.3; evidence 3 to 7).
5. The search covers the Safe's life as a Safe. It was created at block 25468989, 87,777 blocks after the scans start, and its executions from nonce 0 are complete (§ 2.4; evidence 4, 5, 6).
6. Transfers to the address before block 25381212, while it had no code, were not searched. The case argues that such a transfer would not be the Safe's "prior transaction history", but it concedes that the Council may reasonably read it otherwise. It calls this its weakest step (§ 2.4, § 5.1).
7. Against `secure`, the case weighs the Safe's habit: it paid exactly 10000000 to new recipients at nonces 0, 1, 3, 5, and 6, and paid four of them more later. Following `0xce3b5448…` and `0x6652ef59…`, the case holds that payments to other new accounts don't make this one expected. Otherwise every unlinked address would be inside the set for a first small payment (§ 2.4, § 5.1; evidence 8).
8. It finds the recipient outside the expected target set, which § 3.7 and § 3.8's reasonableness test require of a principle-based finding (§ 3.7, § 3.8).

The `judge` accepted steps 1 to 5 and 7. It held that step 6 leaves a reasonable doubt on the decisive question, for these reasons (`judgment-7.md`):

- § 2.4's text has no "operating Safe" limit.
- Neither `0xce3b5448…` nor `0x586a2ed7…` records one, and narrowing them needs a stated ground under § 6.4.
- For native value, the unsearched period runs up to the creation block 25468989.

## Material Evidence

| # | Fact | Bears on | Source | As of |
| --- | --- | --- | --- | --- |
| 1 | The Safe is a proxy of version 1.4.1 with singleton `0x41675c099f32341bf84bfc5382af534df5c7461a`, version 1.4.1 | § 2.1 | `arbot calls` | Ethereum Mainnet block 26381211 |
| 2 | One call: `transfer(0xc01ea83bacda63097627f45d987684569fdceae5, 10000000)` on `0xdac17f958d2ee523a2206206994597c13d831ec7`, value 0, `CALL`, no refund | R-4.3, § 2.4 | request `proposal.transaction`, `arbot calls` | Ethereum Mainnet block 26381211 |
| 3 | The recipient has no code (code size 0) | R-4.3, § 2.4 | `arbot signatures` | Ethereum Mainnet block 26381211 |
| 4 | Owners `0x0cf777780ea18a90d52e7c78d3a1c4c26b7274fa`, `0x66cd5fa074c477bd8e730f0fa2ace2a0b998fe98`, `0x0ae40c604a661c09405c35a6f8b88760f28a068f`, threshold 2, no modules: the recipient is neither an owner nor a module | R-4.3, § 2.4 | `arbot safe` | Ethereum Mainnet block 26381211 |
| 5 | The Safe's executions at nonces 0 to 16, with no nonces missing, never target, pay, or approve the recipient | R-4.3, § 2.4 | `arbot history` (Safe Transaction Service `https://api.safe.global/tx-service/eth`, an offchain source observed 2026-11-10T10:08:01Z, each execution confirmed onchain) | Ethereum Mainnet block 26381211 |
| 6 | The Safe's account was created in block 25468989, transaction `0x7529568fd7668adc16c8bc55df229f784f53c74ad7769e6b746b57071c4ba40f`, with `ProxyCreation` logged by `0x4e1dcf7ad4e460cfd30791ccc4f9c8a4f820ec67`. Nonce 0 is at block 25469065 | R-4.3, § 2.4 | `arbot history` (Safe Transaction Service, observed 2026-11-10T12:15:41Z, confirmed onchain: no code at block 25468988, code at block 25468989) | Ethereum Mainnet block 26381211 |
| 7 | In blocks 25381212 to 26381211: no ERC-20 or native transfer from the recipient into the Safe, and no ERC-20 transfer from the Safe to it apart from this transaction | R-4.3, § 2.4 | `arbot transfers -in -from`, `arbot transfers -to`, `-blocks 1000000` | Ethereum Mainnet block 26381211 |
| 8 | 10000000 paid at nonce 0 (on `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48`, to the owner `0x66cd5fa0…`), nonce 1 (`0xdac17f95…1ec7`, to `0x41656ccf…`), nonce 3 (`0xa0b8…eb48`, to `0xe408d476…`), and nonces 5 and 6 (`0xdac1…1ec7`, to `0x799973cc…` and `0xa73dc770…`). Larger payments to the last four follow at nonces 2, 4, 13, and 14 | R-4.3, § 2.4 | `arbot history` | Ethereum Mainnet block 26381211 |
| 9 | `oracleData` is `0x`: no protocol-recorded purpose | § 2.11 | request `proposal.oracleData` | Gnosis Chain block 49199445 |

## Onchain Evidence for Review

| Evidence | Chain | Transaction |
| --- | --- | --- |
| Transaction proposal | Gnosis Chain | [`0x58b5f675b8a12ffa11931416843a139daa181a32a071770db4f05854cb251b60`](https://gnosisscan.io/tx/0x58b5f675b8a12ffa11931416843a139daa181a32a071770db4f05854cb251b60) |
| Creation of the Safe's account (evidence 6) | Ethereum Mainnet | [`0x7529568fd7668adc16c8bc55df229f784f53c74ad7769e6b746b57071c4ba40f`](https://etherscan.io/tx/0x7529568fd7668adc16c8bc55df229f784f53c74ad7769e6b746b57071c4ba40f) |
| Nonce 0, the first execution (evidence 6, 8) | Ethereum Mainnet | [`0xb5ff5ec5cff23389e581c086f53557a844137e4eb7188ece1d6f95f3a24040c8`](https://etherscan.io/tx/0xb5ff5ec5cff23389e581c086f53557a844137e4eb7188ece1d6f95f3a24040c8) |
| The transaction's own execution, excluded under § 3.6 | Ethereum Mainnet | [`0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5`](https://etherscan.io/tx/0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5) |

| Account | Chain | Address |
| --- | --- | --- |
| Safe | Ethereum Mainnet | [`0xc4994fe4f501a02434ca7f25b14fd803f5d7a388`](https://etherscan.io/address/0xc4994fe4f501a02434ca7f25b14fd803f5d7a388) |
| Token, the call's target | Ethereum Mainnet | [`0xdac17f958d2ee523a2206206994597c13d831ec7`](https://etherscan.io/address/0xdac17f958d2ee523a2206206994597c13d831ec7) |
| Recipient | Ethereum Mainnet | [`0xc01ea83bacda63097627f45d987684569fdceae5`](https://etherscan.io/address/0xc01ea83bacda63097627f45d987684569fdceae5) |
| Singleton | Ethereum Mainnet | [`0x41675c099f32341bf84bfc5382af534df5c7461a`](https://etherscan.io/address/0x41675c099f32341bf84bfc5382af534df5c7461a) |
| Proxy factory | Ethereum Mainnet | [`0x4e1dcf7ad4e460cfd30791ccc4f9c8a4f820ec67`](https://etherscan.io/address/0x4e1dcf7ad4e460cfd30791ccc4f9c8a4f820ec67) |

## Precedents

- `0xce3b54488bb44619c7b5d6deee37837b0c0a7d3c3e5a6ce47c1bd7f6bf8fc41e` (insecure): applied, because it is an ERC-20 transfer to a codeless, non-owner account with no interaction and no purpose. It holds that payments to other new accounts don't make a recipient expected, and that a new recipient is admitted on proof such as being an owner or having sent the Safe funds earlier.
- `0x6652ef59f0d6407810cb3e7b9baead7722e2cfa4daa6f27a2dfc22b5eb58912b` (insecure): applied, for the same holding, with earlier payments of the same kind as the disputed one.
- `0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31e` (insecure): applied, because the recipient was codeless with no history.
- `0x844fe527e82fbf88b70e05db4c487de8143ca5d3b97e2eca24f6348c4d894434` and `0x077791fe52b006b7440aaccb4240ee043a64862aa4dc299632905559541e08be` (insecure): applied for the outcome only, because they ruled on native value to a codeless non-owner recipient under the § 3.8 tie rule, which the last case doesn't use.
- `0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3` (secure): distinguished, because its recipient was an owner.
- `0x586a2ed7fc5cbbe9043a34fb9d9d81a7f46b66740a84767c8515b782b97cf6eb` (secure): distinguished, because its recipient had earlier sent the Safe funds. No such payment is shown here since the Safe's creation. The `judge` held that the case hasn't shown this distinction for the period before block 25381212.
- `0xd0040a42d9ff1cb18e53ccb27acf7e79022eb36f052ee0d731c05db040c675a9` and `0x5d05c97126068fef0018afdb623304f6501e7e954dea42c0b8ec8aba8faa957d` (secure): distinguished, because the recipient had been paid before. Here it was not.
- Novel fact patterns: the Safe's repeated 10000000 first payments to new recipients, and a transfer to the address before it became a Safe. The case reasons about both by analogy (§ 5.1), and the first may be flagged for SafeDAO as `0xce3b5448…` did.

## Sentinel Votes

Their reasons decide the disputed rules, but they aren't evidence (§ 2.14).

| Sentinel | Vote | Reason | Cited rules |
| --- | --- | --- | --- |
| [`0x101d59dbb8c5e70f45d52634f23800fca4fbe031`](https://gnosisscan.io/address/0x101d59dbb8c5e70f45d52634f23800fca4fbe031) | APPROVED | `` | none |
| [`0x6d081448c04d9ef7cf8ecf54207b1931ecc25312`](https://gnosisscan.io/address/0x6d081448c04d9ef7cf8ecf54207b1931ecc25312) | DENIED | `R-4.3` | `R-4.3` |
| [`0xc84a0928632382e75c543acced101b9525027362`](https://gnosisscan.io/address/0xc84a0928632382e75c543acced101b9525027362) | APPROVED | `` | none |

## Weaknesses and Gaps

- **The reason no case was accepted.** Transfers from the recipient to the Safe's address before block 25381212 were not searched, and for native value, before the creation block 25468989. The case concedes that such a transfer could make the verdict `secure`, as in `0x586a2ed7…`. To close it:
  - For ERC-20, rerun `arbot transfers -in -from 0xc01ea83bacda63097627f45d987684569fdceae5` with a `-blocks` that reaches further back. With the recipient filter, the scan stays narrow.
  - For native value, `arbot transfers -in` can't see value sent to a codeless address. A new `arbot` command (via `add-evidence`) could bound it, such as one that reports an account's nonce or native balance at a block, for the recipient as of `safeBlock` or the Safe's address at block 25468988.
- **Whether that period counts.** Alternatively, the Council can decide from the Charter that transfers to an address before it was a Safe are not its "prior transaction history" (§ 2.4), or narrow `0xce3b5448…` and `0x586a2ed7…` with a stated ground (§ 6.4). Then the gap doesn't matter.
- **The pattern of small first payments.** The Safe paid exactly this amount to new recipients five times, and later paid four of them more, three with this token. A Council that reads this as the Safe's "established onchain pattern" could find the recipient inside the set. No ruling in the corpus decides the amount match.
- **Absences only.** The evidence against the recipient is absences (no link, history, purpose, or evidence on poisoning or standard behavior), which § 3.8 says are not determinative alone. The verdict depends on reading § 2.4 to require positive proof for a new recipient.
- **Minor point of the `judge`.** The case says the Council could instead rule insecure under the § 3.8 tie rule, "which gives the same outcome", without showing that the evidence is evenly balanced.
- **Other gaps that `arbot` can't fill yet**:
  - the token's symbol and decimals, so the real size of 10000000 is unknown;
  - the recipient's own history, labels, scam flags, and resemblance to other addresses;
  - standard user behavior for comparable transactions (§ 2.9);
  - native and ERC-721 transfers out of the Safe.
- **Offchain source.** The Safe's execution history and creation come from the Safe Transaction Service, an offchain source (§ 3.4). `arbot` confirmed each one onchain.

## Recusals

For the Council to complete (§ 5.2).

## Draft Ruling Context

The context of the last case, if the Council adopts it.

```text
---
precedents: 0xce3b54488bb44619c7b5d6deee37837b0c0a7d3c3e5a6ce47c1bd7f6bf8fc41e, 0x6652ef59f0d6407810cb3e7b9baead7722e2cfa4daa6f27a2dfc22b5eb58912b, 0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31e, 0x844fe527e82fbf88b70e05db4c487de8143ca5d3b97e2eca24f6348c4d894434, 0x077791fe52b006b7440aaccb4240ee043a64862aa4dc299632905559541e08be, 0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3, 0x586a2ed7fc5cbbe9043a34fb9d9d81a7f46b66740a84767c8515b782b97cf6eb, 0xd0040a42d9ff1cb18e53ccb27acf7e79022eb36f052ee0d731c05db040c675a9, 0x5d05c97126068fef0018afdb623304f6501e7e954dea42c0b8ec8aba8faa957d
tags: erc20-transfer, novel-recipient, codeless-recipient, test-payment, no-prior-interaction, value-target
summary: Small ERC-20 test transfer from a Safe to a codeless, non-owner address with no history with the Safe since its creation; recipient outside the expected target set, insecure under R-4.3.
---

Safe 0xc4994fe4f501a02434ca7f25b14fd803f5d7a388 on Ethereum Mainnet (chain ID 1), Safe transaction 0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00f (nonce 17), Charter bafkreigg6ajqdb52kb644wcxegurt7r6jlclru4mjy3lwxkbh6avm3vgre, rule R-4.3. The transaction transfers 10000000 units of token 0xdac17f958d2ee523a2206206994597c13d831ec7 to 0xc01ea83bacda63097627f45d987684569fdceae5. As of block 26381211, the recipient has no code and is not an owner or module. It appears in none of the Safe's executions (nonces 0 to 16, complete). It sent the Safe nothing, and received nothing from it apart from this transaction, in blocks 25381212 to 26381211, which cover the Safe's life since its creation at block 25468989. No purpose is recorded. Following 0xce3b5448… and 0x6652ef59…, the Safe's earlier small payments to other new recipients don't make this one expected, and nothing places it inside the expected target set (§ 2.4). The transaction is insecure under R-4.3 (§ 3.7, § 3.8). Distinguished: 0x298149bb… (owner recipient), 0x586a2ed7… (recipient had funded the Safe), 0xd0040a42… and 0x5d05c971… (recipient paid before).
```

## Arbitration Record

- **Directory**: `.msgboard/a049284bbb54/`
- **Classification**: `{"verdict": null}` (`unclassified`)
- **Rounds**: 4 evidence, 4 oversight (problems found once), 3 judge (all thrown out)
- **Final files**: `case-7.md`, `oversight-7.md`, `judgment-7.md`
- **Environment**: the default public Mainnet RPC was pruned, so `./arbot.config.json` overrides Ethereum Mainnet with a public RPC that serves the proposal block's state and old logs, ending with `https://rpc.mevblocker.io`.
- **Tooling added during this arbitration**: `arbot history`, which reads the Safe's executions and its creation from the Safe Transaction Service and confirms each onchain, on the branch `claude/pensive-lamport-s5dzc8`.
