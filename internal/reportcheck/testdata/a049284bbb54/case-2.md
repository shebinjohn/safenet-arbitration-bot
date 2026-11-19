# Case (run 2): request 0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27

## Verdict

`insecure` under `R-4.3` (value sent to a recipient outside the expected target set, § 2.4), resting on § 3.7 and, as a fallback, § 3.8.

## Request

- Safe `0xc4994fe4f501a02434ca7f25b14fd803f5d7a388` on Ethereum Mainnet (chain ID 1), nonce 17, proposed 2026-11-10T01:26:25Z, `safeBlock` 26381211.
- Request ID `0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27`, `FROZEN`.
- Charter `charter.safenet-gov.eth`, CID `bafkreigg6ajqdb52kb644wcxegurt7r6jlclru4mjy3lwxkbh6avm3vgre`.
- Disputed rule: R-4.3, cited by `0x6d081448c04d9ef7cf8ecf54207b1931ecc25312` (`DENIED`, reason `R-4.3`). Two other sentinels voted `APPROVED` with empty reasons. Votes are not evidence (§ 2.14).

## Argument

1. The Safe is a § 2.1 Safe (proxy 1.4.1, official singleton `0x41675c09...`, version 1.4.1), so R-4.0 is not in issue, and only R-4.3 is considered.
2. The transaction is one CALL, value 0, to token `0xdac17f95...1ec7`, with `transfer(0xc01ea83bacda63097627f45d987684569fdceae5, 10000000)`, no refund, no batch. The token's recipient receives the tokens, so it is the target address under § 2.4 ("the address that receives value or tokens"), not the token contract. It is a value transfer to a recipient that R-4.3 tests.
3. R-4.3 asks whether the recipient is inside the expected target set, the set consistent with the Safe's established onchain pattern (§ 2.4): prior history, protocol-recorded purpose, standard user behavior. A new address is not automatically outside it (§ 2.4), so I ask what places this one inside.
4. Nothing does:
   - It is not an owner (owners are `0x0cf77778...`, `0x66cd5fa0...`, `0x0ae40c60...`) and the Safe has no modules.
   - It has no code as of `safeBlock` (code size 0, status `no-code`), so it is not a contract counterparty of an interaction (§ 2.4 Notes).
   - The Safe's complete executed history, nonces 0 to 16 (Safe Transaction Service, each confirmed onchain by `arbot`), has it as no target, recipient or approved spender. In the last 100,000 blocks, the Safe's ERC-20 transfers out, its executions, and its transfers in include no transfer to or from it.
   - `oracleData` is `0x`, so no purpose is recorded (§ 2.11).
   - No admissible evidence shows a link, label or legitimate history for it (the report has no such evidence, and says that it can't collect it).
5. Factors of R-4.3 for a novel recipient: (a) no recorded purpose or onchain history exists to be consistent with; (b) there is no evidence of address poisoning, since none of the Safe's prior counterparties or lookalikes were shown to resemble it, which cuts neither way and does not place it inside the set; (c) no admissible evidence on what standard users would do (§ 2.9, a gap). Nothing favors the recipient.
6. The strongest argument for `secure` is the Safe's own pattern: the Safe has repeatedly paid a new payee a small 10000000 amount first (nonces 0, 1, 3, 5, 6), and then larger sums to the same payee (nonces 13 and 14 follow 5 and 6, 2 follows 1, 4 follows 3). This transfer has that shape. But a Safe's habit of paying new accounts is not a link to this account. The Council rejected the same inference in `0xce3b5448...` and `0x6652ef59...`: "earlier payments to other new accounts don't make it expected". Here the new payee is paid the first small amount, so the pattern would make every new address expected, which would empty the rule. The small amount shows less value at risk, not a recipient in the set. Nothing here shows a prior or later relationship with this address (a later payment cannot be used anyway, § 3.6).
7. The finding is therefore that the recipient is outside the expected target set (§ 3.7, § 3.8 reasonable finding, defensible by admissible evidence and the Charter's rules). If the Council sees the Safe's habit as evidence the other way, the evidence is at best evenly balanced on a material security question (where the funds go), which § 3.8 resolves as `insecure`.

## Material evidence

- One call: `transfer(0xc01ea83b...eae5, 10000000)` on `0xdac17f95...1ec7`, value 0, CALL, no refund (`arbot calls`).
- Recipient: no code as of 26381211, not an owner, not a module; owners 3, threshold 2, no modules (`arbot signatures`, `arbot safe`).
- No transfer to or from the recipient, and no execution involving it, in blocks 26281212 to 26381211 (`arbot transfers`, `arbot transfers -in`, `arbot executions -calls`).
- Executed Safe transactions at nonces 0 to 16 never involve it as target, recipient or spender (`arbot history`, Safe Transaction Service, offchain source observed 2026-11-10T10:08:01Z, each confirmed onchain).
- Earlier new payees were paid 10000000 first in nonces 0, 1, 3, 5, 6, followed by larger transfers to the same payees at nonces 2, 4, 13, 14.
- `oracleData` is `0x`. The execution at block 26381211 is the effect of the transaction itself and was not used (§ 3.6).

## Precedents

- Applied: `0xce3b54488bb44619c7b5d6deee37837b0c0a7d3c3e5a6ce47c1bd7f6bf8fc41e` (ERC-20 transfer to a codeless, non-owner account with no prior interaction and no purpose; insecure) and `0x6652ef59f0d6407810cb3e7b9baead7722e2cfa4daa6f27a2dfc22b5eb58912b` (same, codeless and empty; earlier payments to other new accounts don't make it expected). Both hold that the Safe's history of paying other new accounts does not place an unlinked recipient in the set.
- Applied by analogy: `0x844fe527e82fbf88b70e05db4c487de8143ca5d3b97e2eca24f6348c4d894434` and `0x077791fe52b006b7440aaccb4240ee043a64862aa4dc299632905559541e08be` (codeless, unlinked non-owner recipients, native value; evenly balanced, insecure by § 3.8), and `0x5a5a248f863ab189d8170147aee52b8cb6203f592996ef19793e7c040165b31e` (codeless recipient, no history; insecure).
- Distinguished: `0x298149bb3e95eb950dfb281185f338dcf91f1d3585ad9c6ec22ace33b668c7e3` (recipient was an owner; here it is not), `0x586a2ed7fc5cbbe9043a34fb9d9d81a7f46b66740a84767c8515b782b97cf6eb` (recipient had earlier sent the Safe funds; none here), and `0xd0040a42d9ff1cb18e53ccb27acf7e79022eb36f052ee0d731c05db040c675a9` and `0x5d05c97126068fef0018afdb623304f6501e7e954dea42c0b8ec8aba8faa957d` (recipient was paid before; here it was not, in the complete history of nonces 0 to 16).
- The small first amount, the point on which this request differs from the closest rulings, is not covered by any ruling that I read, so that part is novel (§ 5.1) and may be flagged for SafeDAO.

## Weaknesses

- The amount is small, and the Safe's earlier pattern of a 10000000 first payment to each new payee matches it. A Council that treats that pattern as the "established onchain pattern" (§ 2.4) could find the recipient inside the set. I argue it is not, following `0xce3b5448...` and `0x6652ef59...`, but those transfers were large or whole-balance, so they are not identical.
- The token's symbol and decimals are not reported (gap), so the real size of 10000000 is unknown, and the test-payment reading is an inference from amounts alone.
- Transfers before block 26281212 not made by the Safe's own executions (an inbound payment from the recipient, an allowance movement) could not be established because wide scans failed. An earlier inbound payment from the recipient would place it inside the set, as in `0x586a2ed7...`. The Safe's own executions are fully covered (nonces 0 to 16), and the 100,000-block scans found none.
- No evidence on the recipient's own history, labels, scam flags or address-poisoning resemblance, nor on standard user behavior, is available (gaps). A lookalike match with the Safe's prior payees (`0x799973...`, `0xa73dc7...`, `0x1231de...`, `0x41656c...`, `0xe408d4...`) would support insecurity further. Nothing was shown either way.
- The Transaction Service is an offchain source for nonces 0 to 8, though `arbot` confirms each onchain.
