# Case (run 3): request 0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27

## Verdict

`insecure` under `R-4.3` (value sent to a recipient outside the expected target set, § 2.4), resting on § 3.7 and, as a fallback, § 3.8.

## Request

- Safe `0xc4994fe4f501a02434ca7f25b14fd803f5d7a388` on Ethereum Mainnet (chain ID 1), nonce 17, proposed 2026-11-10T01:26:25Z, `safeBlock` 26381211.
- Request ID `0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27`, `FROZEN`.
- Charter `charter.safenet-gov.eth`, CID `bafkreigg6ajqdb52kb644wcxegurt7r6jlclru4mjy3lwxkbh6avm3vgre`.
- Disputed rule: R-4.3, cited by `0x6d081448c04d9ef7cf8ecf54207b1931ecc25312` (`DENIED`, reason `R-4.3`). Two other sentinels voted `APPROVED` with empty reasons. Votes are not evidence (§ 2.14).

## Argument

1. The Safe is a § 2.1 Safe (proxy version 1.4.1, singleton `0x41675c09...`, singleton version 1.4.1, per `arbot calls`), so R-4.0 is not in issue, and only R-4.3 is considered.
2. The transaction is one CALL, value 0, to token `0xdac17f95...1ec7`, with `transfer(0xc01ea83bacda63097627f45d987684569fdceae5, 10000000)`, no refund, no batch. The token's recipient receives the tokens, so it is the target address under § 2.4 ("the address that receives value or tokens"). It is a value transfer to a recipient that R-4.3 tests.
3. R-4.3 asks whether the recipient is inside the expected target set, the set consistent with the Safe's established onchain pattern (§ 2.4): prior history, protocol-recorded purpose, standard user behavior. A new address is not automatically outside it (§ 2.4), so I ask what places this one inside.
4. Nothing does:
   - It is not an owner (owners are `0x0cf77778...`, `0x66cd5fa0...`, `0x0ae40c60...`) and the Safe has no modules.
   - It has no code as of `safeBlock` (code size 0, status `no-code`), so it is not a Safe or a contract of a § 2.1 version.
   - In the Safe's executed Safe transactions at nonces 0 to 16 (Safe Transaction Service, an offchain source, each confirmed onchain by `arbot`; no missing nonces), it is never a target, a recipient, or an approved spender of any decoded call. In the last 100,000 blocks up to `safeBlock`, apart from the transaction's own execution, the Safe's ERC-20 transfers out, its executions, and its transfers in include no transfer to or from it.
   - `oracleData` is `0x`, so no purpose is recorded (§ 2.11).
   - The report has no evidence of a link, label, or legitimate history for it, and lists such evidence as a gap.
5. Factors of R-4.3 for a recipient with no link: (a) no recorded purpose or onchain history exists to be consistent with; (b) there is no evidence either way on address poisoning, which does not place it inside the set; (c) there is no admissible evidence on what standard users would do (§ 2.9, a gap). Nothing favors the recipient.
6. The strongest argument for `secure` is the Safe's own pattern. In the history, the amount 10000000 is paid at nonces 0, 1, 3, 5, and 6, the same amount as this transfer. Nonce 0 is to a Safe owner (`0x66cd5fa0...`), with no larger transfer to it after. For nonces 1, 3, 5, and 6, the recipient (`0x41656ccf...`, `0xe408d476...`, `0x799973cc...`, `0xa73dc770...`) is paid 10000000, and the same recipient is later paid a larger transfer (nonces 2, 4, 13, 14). So the Safe has a history of paying a recipient a small amount, then a larger one. This transfer could be read as the first step of that shape. But a Safe's habit of paying other accounts is not a link to this account. The Council rejected that inference in `0xce3b5448...` and `0x6652ef59...`: earlier payments to other new accounts don't make the recipient expected. Here, the pattern would place every unlinked address inside the set for a first small payment, which would empty the rule. The small amount shows less value at risk, not a recipient inside the set. Nothing in the history shows a prior relationship with this address, and a later payment could not be used anyway (§ 3.6).
7. The finding is therefore that the recipient is outside the expected target set (§ 3.7, a reasonable finding under § 3.8 grounded in the evidence). If the Council sees the Safe's habit as evidence the other way, the admissible evidence is at best evenly balanced on a material security question (where the funds go), which § 3.8 resolves as `insecure`.

## Material evidence

- One call: `transfer(0xc01ea83b...eae5, 10000000)` on `0xdac17f95...1ec7`, value 0, CALL, no refund (`arbot calls`).
- Recipient: no code as of 26381211, not an owner, not a module; owners 3, threshold 2, no modules (`arbot signatures`, `arbot safe`).
- Apart from the transaction's own execution, no transfer to or from the recipient, and no execution involving it, in blocks 26281212 to 26381211 (`arbot transfers`, `arbot transfers -in`, `arbot executions -calls`).
- Executed Safe transactions at nonces 0 to 16 never involve it as target, recipient, or spender (`arbot history`, Safe Transaction Service, observed 2026-11-10T10:08:01Z, each confirmed onchain, no missing nonces).
- The amounts in nonces 0, 1, 3, 5, and 6 are each 10000000, the same as the proposed transfer. Nonce 0 is to the owner `0x66cd5fa0...`. The recipients of nonces 1, 3, 5, and 6 are later paid larger amounts at nonces 2, 4, 13, and 14 (`arbot history`, `arbot executions`).
- `oracleData` is `0x`. The execution at block 26381211 is the effect of the transaction itself and was not used (§ 3.6).

## Precedents

- Applied: `0xce3b54488bb44619c7b5d6deee37837b0c0a7d3c3e5a6ce47c1bd7f6bf8fc41e` (ERC-20 transfer to a codeless, non-owner account with no prior interaction and no purpose; insecure) and `0x6652ef59f0d6407810cb3e7b9baead7722e2cfa4daa6f27a2dfc22b5eb58912b` (same; earlier payments to other new accounts don't make it expected).
- Applied by analogy: `0x844fe527e82fbf88b70e05db4c487de8143ca5d3b97e2eca24f6348c4d894434` and `0x077791fe52b006b7440aaccb4240ee043a64862aa4dc299632905559541e08be` (native value, codeless non-owner recipient; evenly balanced, insecure by § 3.8), and `0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31e` (codeless recipient, no history; insecure).
- Distinguished: `0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3` (recipient was an owner; here it is not), `0x586a2ed7fc5cbbe9043a34fb9d9d81a7f46b66740a84767c8515b782b97cf6eb` (recipient had earlier sent the Safe funds; none shown here), and `0xd0040a42d9ff1cb18e53ccb27acf7e79022eb36f052ee0d731c05db040c675a9` and `0x5d05c97126068fef0018afdb623304f6501e7e954dea42c0b8ec8aba8faa957d` (recipient was paid earlier; here it was not, in nonces 0 to 16).
- The small amount that matches the Safe's earlier first payments is not covered by any ruling that I read, so that part is novel (§ 5.1) and may be flagged for SafeDAO.

## Weaknesses

- The amount matches the 10000000 that the Safe paid at nonces 0, 1, 3, 5, and 6, and four of those recipients were later paid larger sums. A Council that treats that as the "established onchain pattern" (§ 2.4) could find the recipient inside the set. I argue it is not, following `0xce3b5448...` and `0x6652ef59...`, but those transfers were not identical in amount.
- The token's symbol and decimals are not reported (gap), so the real size of 10000000 is unknown.
- Transfers before block 26281212 not made by the Safe's own executions (such as an inbound payment from the recipient, or an allowance movement) were not established, because the wide scans failed or were stopped (gap). An earlier inbound payment from the recipient would place it inside the set, as in `0x586a2ed7...`. The Safe's own executions are covered by nonces 0 to 16, and the 100,000-block scans found none.
- No evidence on the recipient's own history, labels, scam flags, or address-poisoning resemblance, nor on standard user behavior (gaps).
- The Transaction Service is an offchain source for the history, though `arbot` confirms each transaction onchain.
