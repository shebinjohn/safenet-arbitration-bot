# Judgment (run 3): request 0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27

## Decision

`thrown out`. The case argues for `insecure` under `R-4.3`. Its main ground is a § 3.7 finding that the recipient `0xc01ea83bacda63097627f45d987684569fdceae5` is outside the expected target set (§ 2.4), and its fallback is § 3.8.

## Reasons

### What holds

The new case can keep these steps as they are:

- **Disputed rules (R-4.3).** The case rules under R-4.3 alone, which is the only rule that the `DENIED` vote cites. Its remark on R-4.0 doesn't change the verdict, since no sentinel cited R-4.0.
- **Target (§ 2.4, R-4.3).** The token contract is an intermediate contract. The account that `transfer` names receives the tokens, so it is the target address, "the address that receives value or tokens". `0xce3b5448…` and `0x6652ef59…` applied R-4.3 to ERC-20 transfers in the same way.
- **Admissibility (§ 2.8, § 3.4, § 3.6).** The Transaction Service history comes with its source, its observation time, and a reliability basis: `arbot` confirmed each transaction onchain. The case correctly leaves out the transaction's own execution at block 26381211, and correctly declines to use later payments.
- **Precedent (§ 5.1, § 6.4).** All nine rulings cited are in `corpus/`, and the case describes each as its file records it. The distinctions are accurate: `0x298149bb…` had an owner as recipient, `0x586a2ed7…` had a recipient that had earlier sent the Safe funds, and in `0xd0040a42…` and `0x5d05c971…` the same recipient had been paid before. Following `0xce3b5448…` and `0x6652ef59…` is a reasonable application of § 5.1: both rulings hold that a Safe's earlier payments to other new accounts don't put a new, unlinked recipient in the set. The case also flags the matching-amount aspect as novel and reasons about it by analogy.
- **Ruling explanation (§ 5.2).** The case names the Safe and network, the Charter CID, the rule, the precedents, the material evidence, and the reason.

### Defects

1. **Weaknesses / Evidence (step 4 and the third Weaknesses point; R-4.3, § 2.4, § 3.8, § 5.1). The case admits a gap that could reverse its verdict.**
   - **The test the case applies treats the missing fact as decisive.** The case applies the reading of § 2.4 in `0xce3b5448…`: a new recipient is admitted only "on concrete proof that it can be trusted, such as being an owner or having sent the Safe funds earlier". `0x586a2ed7…` put a recipient inside the set because it had earlier sent the Safe funds.
   - **The search for that fact covers only part of the Safe's history.** Inbound transfers were searched only in blocks 26281212 to 26381211. The nonce 0 to 16 history covers only the Safe's own executions, which is its outbound side. The Weaknesses concede that inbound transfers before block 26281212 were not established, because the wide scans failed or were stopped. They also concede that an earlier inbound payment from the recipient "would place it inside the set".
   - **Nothing shows the window covers the Safe's lifetime.** The case doesn't say when the Safe was created, first funded, or executed nonces 0 to 16.
   - **So step 4's "Nothing does" is not established.** It rests on an incomplete search for the very fact that the governing precedent treats as decisive. What is missing is the search, not evidence that would count as an absence of evidence under § 3.8.
   - **The searches in the applied precedents were fuller.** `0xce3b5448…` searched 1,000,000 blocks, and `0x6652ef59…` searched "the Safe's history". On this point the case's facts are thinner than those of the precedents it says it follows.
   - **To cure it**, the `prosecutor` should do one of these:
     - Ask the `detective` for the Safe's inbound transfers from the recipient, ERC-20 and native, over the Safe's whole history up to `safeBlock`. For example, `arbot transfers -in -from 0xc01ea83bacda63097627f45d987684569fdceae5 -blocks N`, with `N` reaching back past the Safe's first activity. Filtering on the recipient as sender narrows the scan.
     - Ask the `detective` for facts that show the scanned window already covers the Safe's whole history, such as its creation and first-funding blocks.
     - If `arbot` can't provide either, have the `detective` report the gap so that `add-evidence` can close it. Without the fact, the case would have to explain why an earlier inbound payment from the recipient could not change the verdict. That means distinguishing `0x586a2ed7…` and the test of `0xce3b5448…`, not conceding them.

2. **§ 3.8 fallback (step 7; § 3.8). The case asserts even balance without showing it.**
   - **What the fallback claims.** If the Council treats the Safe's habit of first paying 10000000 as evidence for the recipient, the case says the evidence is "at best evenly balanced".
   - **What stands against the recipient.** Only absences: no link, no history, no recorded purpose, and no evidence on address poisoning or on standard user behavior.
   - **Why that isn't enough.** § 3.8 says absence of evidence "is not determinative alone". `0x5d05c971…`, which the case cites, held that missing purpose and missing recipient history don't make the evidence evenly balanced against positive evidence for the recipient.
   - **To cure it**, the `prosecutor` should either name admissible evidence that weighs against the recipient, so that the balance is genuinely even on a material question, or drop the fallback and rest on the § 3.7 finding under `0xce3b5448…` and `0x6652ef59…`.
   - The main ground doesn't depend on the fallback, so this defect alone would not throw the case out. It still shouldn't go to the Council in its current form.

3. **Facts used outside Material evidence (steps 1 and 6; § 3.3, § 2.4). Minor.**
   - **Facts the argument uses but Material evidence doesn't list:**
     - the Safe's proxy and singleton versions (step 1);
     - that the owner paid at nonce 0 got "no larger transfer … after" (step 6).
   - **A fact the pattern rebuttal needs but the case doesn't state.** It doesn't say whether the Safe's earlier 10000000 payments at nonces 0, 1, 3, 5, and 6 were in the same token, `0xdac17f95…1ec7`. Step 6 relies on "the same amount as this transfer", and the answer changes how close the Safe's pattern is under § 2.4.
   - None of these facts would change the verdict. The new case should list them in Material evidence, or stop relying on them.
