# Judgment (run 6): request 0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27

## Decision

`thrown out`. The case argues for `insecure` under `R-4.3`, on a § 3.7 finding that the recipient `0xc01ea83bacda63097627f45d987684569fdceae5` is outside the expected target set (§ 2.4). It no longer has a § 3.8 fallback.

## Reasons

### The defects of judgment 3

- **Defect 2, the § 3.8 fallback: cured.**
  - The case drops the fallback, and says that it doesn't claim the evidence is evenly balanced (Verdict, step 8).
  - It now cites § 3.8 only for what that section says: when a principle-based finding is reasonable, and how absence of evidence is weighed. That fits `0x5d05c971…`, which held that absences don't make the evidence evenly balanced.
- **Defect 3, facts used outside Material evidence (§ 3.3): cured.**
  - Material evidence now lists the Safe's proxy and singleton versions.
  - It lists that the owner paid at nonce 0 got no other transfer from the Safe in the range.
  - It lists the token of each earlier 10000000 payment: three on `0xdac17f95…1ec7`, the token of this transfer, and two on `0xa0b86991…eb48`.
  - What remains is minor. Step 4 names the three owners' addresses, but Material evidence gives only their count and the fact that the recipient isn't one. This doesn't affect the verdict.
- **Defect 1, the incomplete search for the decisive fact: narrowed but not cured.** See Defect 1 below.

### What holds

- **Disputed rules (R-4.3).** The case rules under R-4.3 alone, which is the only rule that the `DENIED` vote cites.
- **Target (§ 2.4, R-4.3).** The token contract is an intermediate contract. The account that `transfer` names receives the tokens, so it is the target address.
- **Admissibility (§ 2.8, § 3.4, § 3.6).**
  - The Transaction Service history comes with its source, its observation time, and a reliability basis: `arbot` confirmed each transaction onchain.
  - The scans are onchain data as of `safeBlock`.
  - The case correctly leaves out the transaction's own execution, and doesn't use later payments.
- **Prior history (§ 2.4).** The Safe's executions at nonces 0 to 16 are complete, with no missing nonces. None of them targets the recipient, pays it, or approves it as a spender. On the "prior transaction history" factor of § 2.4, this is positive evidence about the Safe's outbound pattern, not only an absence of evidence. With no recorded purpose (§ 2.11), the R-4.3 factors give the recipient nothing (step 6).
- **The pattern rebuttal (step 7; § 2.4, § 5.1).**
  - The case now states that three of the five earlier 10000000 payments were in the same token, and that four of the five recipients were later paid more.
  - It follows `0xce3b5448…` and `0x6652ef59…`, whose files hold that a Safe's earlier payments to other new accounts don't make a new one expected. In `0x6652ef59…`, those earlier payments were the same kind as the disputed one: whole-balance payments to new accounts. So the analogy to a matching-amount pattern is sound.
  - The case also says the amount match is novel, reasons about it by analogy, and flags it, as § 5.1 requires.
  - This is a reasonable application of precedent. Whether to narrow it is for the Council (§ 6.4).
- **Ruling explanation (§ 5.2).** The case names the Safe and its network, the Charter CID, the rule, the precedents, the material evidence, and the reason.

### Defects

1. **Steps 4 and 5, and the first Weaknesses point (R-4.3, § 2.4, § 3.1, § 3.3, § 5.1). The case still concedes a gap that would reverse its verdict.**
   - **What improved.**
     - The inbound scan from the recipient now covers 1,000,000 blocks, the same depth as `0xce3b5448…`.
     - It reaches 87,853 blocks before the Safe's first execution.
     - The outbound history is complete.
     - This cures the point in judgment 3 that the case's facts were thinner than those of the precedents it follows.
   - **What remains.**
     - The case's own Weaknesses call this a "Gap that could change the verdict". They say that a payment from the recipient before block 25381212 "is not excluded", and that the window "is not shown to cover the Safe's lifetime".
     - They say that the creation block would make "the finding in step 4 … complete". So, by the case's own account, step 4 is not complete on the one fact that its governing test makes decisive.
     - Under that test (`0xce3b5448…`: a new recipient is admitted on proof such as "having sent the Safe funds earlier") and under `0x586a2ed7…`, that fact isn't a circumstantial factor to weigh under § 3.8. If present, it places the recipient inside the set.
     - A Safe can be created and funded long before its first execution. The case states neither its creation block nor when it was first funded.
   - **Step 5's answer doesn't hold.**
     - *Admissibility.* § 3.3 to § 3.5 say what evidence is admissible. They don't make unsearched evidence unavailable. Transfers before block 25381212 are onchain data observable at proposal time (§ 2.8), so they are admissible and "available to Safenet" (§ 3.1). An unsearched period is a gap in the search, not an absence of evidence.
     - *Search depth.* Matching the depth of `0xce3b5448…` doesn't settle the gap either. Its file states the window it searched. It records no gap before that window, and no holding that a window of that size is enough whatever the Safe's age. So under § 5.1 it is not a search-depth rule that lets a case set aside a gap that the case itself calls decisive.
     - *The other applied precedents.* `0x6652ef59…` records a search of "the Safe's history". `0x5a5a248f…` established that the recipient never sent the Safe funds from the Safe's complete balance history.
   - **Judgment 3 asked for an argument, not a concession.** Without the fact, it asked the case to explain why an earlier inbound payment could not change the verdict. The case still concedes that it would.
   - **To cure it**, the `prosecutor` should do one of these:
     - Ask the `detective` for the Safe's creation block as of `safeBlock`, the first block at which its address has code.
       - If `arbot` can't provide it, have the `detective` report the gap so that `add-evidence` adds the command.
       - If the block is at or after 25381212, list it in Material evidence. By the case's own account, the search then covers the Safe's life, and the Weaknesses no longer name a gap that could reverse the verdict.
       - If the block is earlier, ask for `arbot transfers -in -from 0xc01ea83bacda63097627f45d987684569fdceae5 -blocks N`, with `N` reaching back to it.
     - Or argue, rather than concede, why a payment from the recipient before block 25381212 would not place it inside the set. That means distinguishing `0x586a2ed7…` and the test of `0xce3b5448…`, or departing from them with reasons under § 6.4.

2. **Precedents, "Applied by analogy" (§ 5.1, § 6.4, § 3.8). Minor.**
   - **What the files record.** `0x844fe527…` and `0x077791fe…` both held that nothing placed the recipient inside *or outside* the set ("no evidence places it outside"). Both ruled insecure only under § 3.8, on evenly balanced evidence.
   - **How the case uses them.** It describes them only as "native value, codeless non-owner recipient; insecure". It applies them to a § 3.7 finding that the recipient *is* outside the set, while disclaiming § 3.8. On that point, their reasoning is the opposite of the case's.
   - **To cure it**, the new case should state their ground as their files record it. It should then either cite them only for the outcome on a like fact pattern, explaining why it follows `0xce3b5448…` and `0x5a5a248f…` rather than their § 3.8 reasoning, or drop them.
   - This alone would not change the verdict.

3. **Request section (§ 2.14). Minor.** "Votes are not evidence (§ 2.14)" overstates § 2.14. That section says that the protocol doesn't check whether a reason applies the Charter correctly, and that a reason doesn't affect vote counting or create another verdict. It doesn't make votes inadmissible. The argument doesn't use this statement, so it should be reworded or the citation dropped.
