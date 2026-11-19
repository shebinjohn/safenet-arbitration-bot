# Judgment (run 7): request 0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27

## Decision

`thrown out`. The case argues for `insecure` under `R-4.3`, on a § 3.7 finding that the recipient `0xc01ea83bacda63097627f45d987684569fdceae5` is outside the expected target set (§ 2.4). It doesn't rest on the § 3.8 tie rule.

## Reasons

### The defects of judgments 3 and 6

- **Judgment 6 Defect 1 and judgment 3 Defect 1, the search for the decisive inbound fact: the part judgment 6 named is cured, but the case now concedes a new gap.**
  - The case did what judgment 6 asked first. It lists the creation block, 25468989, which is after 25381212. It gives the offchain source, the observation time, and a reliability basis: `arbot` confirmed the creation onchain (§ 3.4).
  - So the outbound history (nonces 0 to 16) and the inbound scan from the recipient now cover the Safe's whole life as a Safe (§ 2.4). Step 5 correctly answers judgment 6 on that point.
  - But the Weaknesses still name a "Gap that could change the verdict", now for transfers to the address before it had code. See Defect 1 below.
- **Judgment 6 Defect 2, `0x844fe527…` and `0x077791fe…`: cured.**
  - The case states their ground as their files record it: nothing placed the recipient inside or outside the set, and the insecure ruling came from § 3.8.
  - It cites them for the outcome only, and follows `0xce3b5448…` and `0x5a5a248f…` for the finding.
  - It explains through `0x5d05c971…` why it doesn't use the tie rule (§ 5.1, § 6.4).
  - A minor remainder is Defect 2 below.
- **Judgment 6 Defect 3, § 2.14: cured.** The Request section now says what § 2.14 says: the protocol doesn't validate reasons, and a reason doesn't create a verdict. The votes serve only as the source of the disputed rule.
- **Judgment 6's minor point on the owners: cured.** Material evidence now lists the three owners' addresses and the threshold (§ 3.3).
- **Judgment 3 Defects 2 and 3: still cured.** There is no § 3.8 fallback. Material evidence lists every fact the argument uses, including the token of each earlier 10000000 payment.

### What holds

- **Disputed rules (R-4.3).** The case rules under R-4.3 alone, the only rule that the `DENIED` vote cites.
- **Target (§ 2.4, R-4.3).** The token contract is an intermediate contract. The account that `transfer` names receives the tokens, so it is the target address.
- **Admissibility (§ 2.8, § 3.3 to § 3.6).**
  - The Transaction Service history and the creation block each come with a source, an observation time, and onchain confirmation by `arbot`.
  - The scans are onchain data as of `safeBlock`.
  - The case leaves out the transaction's own execution and doesn't use later payments.
- **The Safe's life as a Safe (§ 2.4, § 2.11, R-4.3).**
  - The executions at nonces 0 to 16 are complete. None of them targets, pays, or approves the recipient.
  - No transfer from the recipient into the Safe appears between the creation and `safeBlock`.
  - No purpose is recorded.
  - Under the reading of § 2.4 in `0xce3b5448…`, nothing in that period places the recipient inside the set.
- **The pattern rebuttal (step 8; § 2.4, § 5.1).** This is unchanged from case 6, and judgment 6 accepted it as a reasonable application of `0xce3b5448…` and `0x6652ef59…`. The case flags the matching amount as novel and reasons about it by analogy.
- **Precedent (§ 5.1, § 6.4).** Every ruling the case cites is in `corpus/`, and the case describes each as its file records it.
- **Ruling explanation (§ 5.2).** The case names the Safe and its network, the Charter CID, the rule, the precedents, the material evidence, and the reason.

### Defects

1. **Step 6 and the first Weaknesses point (R-4.3, § 2.4, § 3.1, § 3.7, § 5.1, § 6.4). The case still names an unsearched fact that, by its own account, could reverse its verdict.**
   - **The concession.**
     - Step 6 says: "If a transfer from the recipient before block 25381212 is shown, the Council may reasonably read it as placing the recipient inside the set, and the case would change to `secure`".
     - The Weaknesses call this a "Gap that could change the verdict".
     - The case also says these transfers "are admissible and could be searched" (§ 2.8, § 3.1). So the missing piece is a search, not evidence that doesn't exist.
   - **Why the gap matters.** The case applies the test of `0xce3b5448…`, under which a new recipient is admitted on proof such as "having sent the Safe funds earlier". `0x586a2ed7…` ruled `secure` on that fact. A period that could hold that fact, and that the case admits the Council may count, leaves doubt on the decisive question (§ 3.7).
   - **Step 6's argument doesn't remove the doubt.**
     - *First bullet.* It reads § 2.4's "prior transaction history on the relevant network" as leaving out the address's history from before it had code. § 2.4's text has no such limit, and the case cites nothing for one.
     - *Second bullet.* It says the proof in `0xce3b5448…` and `0x586a2ed7…` is "a trust relation with an operating Safe". Neither file records that limit: `0xce3b5448…` says "having sent the Safe funds earlier", and `0x586a2ed7…` says the recipient "sent the Safe" funds. Adding the limit narrows those rulings. § 6.4 allows that only with a stated ground, yet the case says it "follow[s]" them. The claim that such a transfer "shows no relation of the recipient with this Safe, only with an address that someone later deployed a Safe to" has no fact in Material evidence behind it.
     - *Third bullet.* It is an absence that the case admits it "cannot exclude" (§ 3.8).
     - The case itself calls this "the weakest step of the case".
   - **For the native currency, the gap starts at the creation block, not at 25381212.**
     - `arbot transfers -in` finds native transfers through the Safe's `SafeReceived` events, which the Safe's code logs.
     - The address had no code before block 25468989. So native value sent to it in blocks 25381212 to 25468988 logged no event, and the scan couldn't list it.
     - The 87,777 blocks that the scan covers before the creation therefore cover ERC-20 transfers only. Steps 5 and 6 treat 25381212 as the edge of the gap for both kinds of transfer, but it is the edge only for ERC-20.
   - **To cure it**, the `prosecutor` should do one of these:
     - Ask the `detective` for facts that close the address's inbound history from the recipient before the scanned range:
       - *ERC-20.* `arbot transfers -in -from 0xc01ea83bacda63097627f45d987684569fdceae5 -blocks N`, with `N` reaching back before block 25381212 to a bound the case can justify, or as far back as the chain goes. Filtering on the recipient as sender keeps the scan narrow.
       - *Native.* A fact that bounds the native value the recipient could have sent the address before block 25468989, with the reason it bounds it. Examples are the recipient's own history, such as its nonce as of `safeBlock`, or the address's native balance at block 25468988.
       - If `arbot` can't provide these, the `detective` should report the gap so that `add-evidence` adds the command.
       - If the facts show no such transfer, the case should list them in Material evidence and drop the concession.
     - Or, without those facts, argue the point rather than concede it, under § 5.1 and § 6.4:
       - Either ground in § 2.4's text, or in the files of `0xce3b5448…` and `0x586a2ed7…`, why a transfer to the address before it had code is not "prior transaction history" and not having "sent the Safe funds";
       - or narrow those rulings under § 6.4 and state the ground.
       - Either way, the case can't also say that the Council may reasonably read such a transfer as placing the recipient inside the set.

2. **Precedents and the third Weaknesses point (§ 3.8, § 5.1). Minor.**
   - The case says the Council "may prefer the § 3.8 ground of the first two instead, which gives the same outcome", and could "rule insecure only under the § 3.8 tie rule".
   - It doesn't show that the evidence here is genuinely evenly balanced. Judgment 3 (Defect 2) found that kind of unshown claim to be a defect.
   - The claim also conflicts with the case's own reading of `0x5d05c971…`: absences don't make the evidence evenly balanced against positive evidence for the recipient. Step 8 treats the amount pattern as that kind of evidence.
   - The case doesn't rest on this, so it alone wouldn't throw the case out. The new case should drop "which gives the same outcome", or say that it takes no position on whether § 3.8 would apply.
