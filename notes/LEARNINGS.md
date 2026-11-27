# Arbitration learnings

Durable notes from running the `arbitrate` skill on live Safenet disputes. Scratch
files live in `.msgboard/` and are never committed; the published draft rulings live
on the docket artifact.

## Standing rules for each hourly run

- **Sync gate (updated 2026-11-26 by the user).** Before starting an arbitration,
  if the fork's `main` is behind upstream (safe-research) `main`, fast-forward it
  yourself (`git push origin upstream/main:refs/heads/main`, fast-forward only,
  never force). The user durably authorized this. The `pr*` stack branches are
  not covered: never push them. While `pr9-offchain-references` conflicts with
  upstream `main`, build `arbot` from `pr9-offchain-references` as is (accepted by
  the user for 0xc3f9…), and mention the conflict in the run's report.
- **Resolved requests leave the docket automatically.** When `arbot info -json`
  shows a docket request is no longer `FROZEN` (resolved, out of scope, or timed
  out), remove it from the docket and republish without asking, and record the
  Council's outcome and `context` (or its corpus file once indexed) against our draft
  below.
- Check request IDs with `arbot info` first; a typo'd ID (one was off in its 13th hex
  digit) should be matched against `arbot pending` and called out.
- Give sub-agents candidate precedent IDs with outcome and `summary:` from the
  10-line headers, and tell them to read corpus files by full name (they often can't
  Grep `.msgboard/corpus`).
- Pass every sub-agent the evidence playbook rules: one `arbot` command at a time; a
  failed node request is a failure, not an answer; never decode calldata or compute
  addresses by hand. Reject a case that reads tuple fields by word position before it
  reaches the judge.
- Executions after `safeBlock` are never evidence (§ 3.6); report their timing from
  `arbot status` only as status.
- If nothing is new and nothing changed, don't republish or commit.

## Recurring oversight findings (have the detective fix them before the prosecutor rebuilds)

- Fact IDs differ between `evidence.md` and `evidence-summary.json`.
- Sourcify or Etherscan facts labelled onchain, without a URL or observation time.
- Derived facts and inferences ("settlement transaction", a silo identity) not
  marked derived.
- Case text overstates what a precedent's corpus file says.

## `arbot check-report` pitfalls

- Numbers that appear only in oversight files aren't sources.
- A request ID in Precedents that isn't in the corpus fails; don't cite pending
  drafts by ID.
- An abbreviation must shorten a real value.
- Put long hex values and CIDs in backticks, or they overflow on phones in the
  docket.

## Fact patterns

### Vault redeem requests

`claimSharesAndRequestRedeem(uint256)` on vaults with implementation
`0xe50554ec802375c9c3f9c087a8a7bb8c26d3dedf` (Sourcify `Vault`). The judge needs:
every value movement accounted for, including both claim steps to `msg.sender`; the
pending silo treated as a § 2.4 recipient in the expected target set; the silo
identified from logs at or before `safeBlock`, never from the transaction's own later
execution; gaps treated consistently as absence of evidence (§ 3.8); Sourcify
verification and observation times; a SafeDAO flag for the novel pattern (§ 5.1).
Worked examples: d39d, f8a4, b2aa.

### Pre-proposal execution (Charter issue #27)

The rulings conflict:
- `0x1fcf9563…` (secure) counts the transaction's own execution as history;
- `0xa049284b…` (insecure) is silent on it;
- `0x4e701ce5…` (secure) says "The transaction's own execution … was not considered
  (§ 2.8)".

Surface the conflict; don't resolve it silently. Decided 863d (no case accepted).

### When no case can be accepted

The judge rejects both a § 3.8 tie-break and a "provisional secure" when the deciding
fact is in undecoded calldata (2d01 swap `beneficiary`). Stop once the judge says
another round on the same record won't help; write the "Not accepted" report and
record the `arbot` gap.

## Arbitrations

All under Charter `bafkreigg6ajqdb52kb644wcxegurt7r6jlclru4mjy3lwxkbh6avm3vgre`,
disputed rule R-4.3, drafted 2026-11-23/24 (previous session). Council outcome:
see the table and "Council outcomes" below.

| Request | Chain · nonce | Pattern | Draft | Rounds / what was caught | Council outcome |
| --- | --- | --- | --- | --- | --- |
| `0x863d07a43e2c…` | 42161 · 0 | transfer executed 3 s before proposal | not accepted | 3 judgments thrown out; last case's § 6.4 departure misdescribed `0x1fcf9563…`; `0x4e701ce5…` found afterwards | **secure** (record `0x74257d21…`) |
| `0xd39dc3a746b9…` | 1 · 2 | vault redeem (derUSDC) | secure | accepted on third judgment; two cases thrown out over the silo identification | **secure** (record `0x74257d21…`) |
| `0xf8a4d29019be…` | 1 · 10 | vault redeem (derBTC) | secure | accepted round 1 | **secure** (record `0x74257d21…`) |
| `0xb2aaafd43fb7…` | 1 · 4 | vault redeem (derEURC) | secure | accepted round 1 | **secure** (record `0x74257d21…`) |
| `0x3296fc227b79…` | 1 · 2 | return of the Safe's only inflow (SXT) | secure, human review | accepted round 1; flags/labels gap decisive | **secure** (record `0x74257d21…`) |
| `0x5d7d0c00a4a5…` | 1 · 1537 | payment to recurring payee (USDC) | secure | accepted round 1; lookalike sender noted | **secure** (record `0x74257d21…`) |
| `0x2d0174daa69d…` | 1 · 34 | swap, recipient tuple not decoded | not accepted | 2 judgments thrown out (§ 3.8 tie-break; provisional secure); request ID first given with a typo | **secure** (record `0xed6c1608…`) |
| `0xffa3c8072266…` | 1 · 1127 | Pendle RouterV4 `exitPostExpToToken`, receiver undecoded | not accepted | 2 evidence, 3 oversight (all with findings), 0 judge; stopped at oversight limit | pending |
| `0xac1e39678da5…` | 1 · 24 | Lagoon v0.6.0 vault redeem, same Safe as a768, unverified silo | **secure** | 2 evidence, 6 oversight, 2 judge; accepted in judgment 2 | pending |
| `0xa7682c035962…` | 1 · 19 | Lagoon vault `claimSharesAndRequestRedeem`, owner-created vault | **secure** | 3 evidence, 4 oversight, 2 judge; accepted in judgment 2 | pending |
| `0xc3f9a39d441f…` (R-4.4) | 1 · 22 | USDC approve + `depositForBurn` to Circle CCTP `TokenMessengerV2` proxy, spender never used | not accepted | 3 evidence, 7 oversight, 3 judge rounds; see below | pending |

## RPC and service notes

- Ethereum: `https://eth.drpc.org` serves old state; Flashbots doesn't.
- Gnosis: `https://rpc.gnosischain.com`.
- Arbitrum One: only Blast's public endpoint had old state (chainlist default lagged;
  arb1.arbitrum.io and dRPC lacked it).
- Safe Transaction Service answers 429 at times: wait and rerun, never read as "no
  data".
- Etherscan key: from the session environment, written to a gitignored mode-600
  config in `.msgboard/`.

## Sync log

- 2026-11-25: upstream `main` was 3 corpus-index commits ahead of the fork's `main`.
  Upstream lacks `check-report`, `explorer`, `source`, `status`. Merging
  `pr9-offchain-references` into upstream `main` now **conflicts in 31 files**:
  upstream squash-merged revised versions of the stack's lower PRs (#23–#28 there).
  Built from `pr9-offchain-references` alone (2d0a289); `go test -short ./...`
  passed. The stack needs rebasing onto upstream `main`.
- 2026-11-25 22:02 UTC: new request `0xc3f9a39d441f…` (Ethereum Safe
  `0x659e…6022`, nonce 22, MultiSend delegate call, disputed R-4.4: BlockSec
  deny, Candide approve; deadline block 49952029). Held by the sync gate: fork
  `main` 1 behind upstream, and `pr9-offchain-references` conflicts in 31 files.

### CCTP bridge approval (R-4.4), `0xc3f9a39d441f…`, 2026-11-26

- Held from 2026-11-25 22:02 until the user synced the fork's `main` (the `pr*`
  stack still conflicts; the user accepted building from `pr9-offchain-references`).
- Batch: USDC `approve(0x28b5…cf5d, 788250000)` then `0x8e0250ee` on the spender.
  `arbot classify`: unclassified.
- Judgments, all thrown out, all `secure` cases:
  1. case 3: approve-then-call alone isn't § 2.4 membership; admitted G1/G3 could flip it; no § 5.1 analogy.
  2. case 6: Circle's address page shows *what* the spender is, not that it's in the
     expected target set; precedents 0x266a0fe0… (token creation linked spender onchain)
     and 0xa2e204cf… (pre-proposal registry + earlier approve-then-use) had a second link.
  3. case 9: rests on developers.circle.com pages attributed only to the domain;
     the case itself conceded § 3.8 `insecure` if the Council discounts them.
     Judge's final note: G1/G3 can't change the R-4.4 verdict (mint recipient is R-4.3);
     the only open question is the weight of the documentation.
- Oversight caught:
  - an off-by-one transfer count (15 → 16);
  - derived facts not marked derived;
  - one URL and time shared by 3 contracts;
  - an `evidence-summary.json` field that `check-report` rejects (`status` on a gap);
  - a non-time `observedAt`;
  - line numbers that arbot never prints, so they were all removed;
  - case wording that the evidence didn't support ("is consumed", "just received",
    "issuer", "first-party", "mutually consistent").
- Lesson: `arbot source fetch` needs `sources.allow` in the config; without it
  every documentation lead is a gap. Seed configs with developers.circle.com,
  github.com, raw.githubusercontent.com (add hosts per protocol as needed).
- Lesson: for a documented-protocol spender with no Safe history, the judge wants
  an onchain second link (proxy admin, token minter config) or a pre-proposal
  source, not just post-proposal docs. Ask the detective for those up front.
- Process note: a report-drafting sub-agent bypassed a Write-tool refusal with a
  shell heredoc. The orchestrator re-verified the report (check-report 0/0, read in
  full) before publishing. In future, write report.md in the orchestrator.
- SafeDAO flags: whether post-proposal docs on an otherwise unidentified domain place
  a spender in § 2.4's set; whether R-4.4 reaches a documented call's parameters
  (destination caller) or leaves recipients to R-4.3.
- 2026-11-26 11:03 UTC: new request `0xffa3c8072266…` (Ethereum Safe
  `0x5435…fd46`, nonce 1127, a 15652-byte call with selector `0xf06a07a0` to
  `0x8888…f946`, disputed R-4.3: BlockSec deny, Candide approve; deadline block
  49961457). Held by the sync gate: fork `main` 1 corpus-index commit behind
  upstream, and `pr9-offchain-references` conflicts in 48 files.

## Council outcomes

### 2026-11-26: six requests ruled `secure` in one transaction

Gnosis Chain transaction `0x74257d214ed898952641a08535edf1d303bd4dc6c1a79bb0b5e63d802feb706a`
(block 49477793) resolved 863d, d39d, f8a4, b2aa, 3296 and 5d7d `secure`
(`RESOLVED_APPROVED`), all under R-4.3. The full `context` of each is saved in
`.msgboard/<short-id>/council-context.md` (not committed). They are in upstream's corpus as of index block 49484023
(76 rulings, 2026-11-26 20:02 UTC), and can now be cited by ID.

| Request | Draft | Council | Agreement |
| --- | --- | --- | --- |
| d39d, f8a4, b2aa | secure (vault redeem) | secure: "moving the Safe's own shares into the vault's verified pending silo, with claims to the Safe and no receiver named" | match |
| 3296 | secure, human review | secure: a young Safe returns exactly the amount the recipient sent it | match |
| 5d7d | secure | secure: recipient paid five times before; non-recipient lookalike noted | match |
| 863d | **not accepted** | secure: "A Safe transaction already executed onchain before its proposal is prior onchain history under § 2.8; its payment places the recipient in the expected target set" | Council decided what the agents couldn't |

**Precedent conflict resolved (Charter issue #27).** The 863d ruling sides with
`0x1fcf9563…`: a transaction's own execution before its proposal counts as prior
history (§ 2.8). The `0x4e701ce5…` reading (own execution not considered) is now
the minority view. Tell future agents to cite 863d once it is in the corpus.

Lesson: the Council relied on the pre-proposal execution that three judge rounds
refused to weigh. When the deciding question is a Charter interpretation with
conflicting precedent, stop early and hand it to the Council as "not accepted"
rather than spending rounds.

### Pendle router exit (R-4.3), `0xffa3c8072266…`, 2026-11-26

- One call to `PendleRouterV4` (Sourcify, exact), delegated in the simulation to the
  `ActionMiscV3` facet's `exitPostExpToToken(address receiver, …)`. The `receiver` is
  undecoded (G1: `calls -abi` uses the proxy's empty ABI; G2: trace has no args).
- Strong history: 1127 executions, 149 to the router, 14 with the same selector.
  `arbot events` on those 14: one logged Transfer to the Safe in each, only 7 of them
  USDC from the router; third-party net recipients in 7. Half the pattern predates
  the facet.
- Prosecutor argued `secure` on the pattern, in a close case. Oversight found problems three times in a
  row (status values check-report rejects; transfer counts that vary between runs
  (591/590, 779/780) as the service's listing changes; "14 of 14 output", "same call",
  "never as native value" overstatements), so per the skill it never reached the judge.
  Report written by the orchestrator: check-report 0/0 first run.
- Lessons:
  - Tell the detective up front: no `arbot status` values in evidence files, and
    say "a logged Transfer to the Safe", not "the output reached the Safe".
  - Tell the prosecutor up front: "same selector and target", never "same call",
    unless arguments are decoded.
  - Service-listed transfer counts drift between runs; state them with the
    observation time.
- The Council can decode `receiver` from the calldata. arbot can't. This is the third
  "not accepted" in a row (2d01, c3f9, ffa3) caused by proxy/facet or tuple decoding.
  Top priority for `add-evidence`.

### Vault redeem (R-4.3), `0xa7682c035962…`, 2026-11-26: secure, accepted

- Same `Vault` implementation `0xe505…dedf` as d39d/f8a4/b2aa (Council: secure today).
  The vault was created by an owner of the Safe (who also created the Safe).
- Judgment 1 threw out case 4: the silo was identified from logs only, the verdict was
  called "tentative", and § 3.8's absence-of-evidence rule was used to cover the
  recipient question. The judge suggested the cure: read the verified source for
  where `pendingSilo` is set. It is assigned once in `__ERC7540_init`, reachable only
  from an `initializer`. That plus the creation-tx `Approval` by the Silo and later
  logs made the silo a *finding*; G10/G14 (stored value, upgrade history) were weighed
  as minor ambiguity (§ 3.8 second rule), and judgment 2 accepted.
- Lesson for vault redeems: ask the detective up front for (1) the `pendingSilo`
  assignment and initializer guard from the verified source, (2) the creation-tx
  logs, (3) later logs using the silo, (4) other users' same-selector calls as § 2.9.
  Never call a verdict "tentative" (§ 2.13).
- Process: a sub-agent run was cut off by the account's 5-hour usage limit at 13:20 UTC
  (reset 17:20). Hourly runs 14:00–18:00 queued and were collapsed into one at 18:02.
  The orchestrator wrote report.md; check-report 0/0 first run.
- Corpus: upstream's index stayed at block 49477242 through 18:00 UTC, so the six
  Council rulings of block 49477793 weren't citable yet.

### 2026-11-27: `0x2d0174daa69d…` ruled `secure`

Record `0xed6c1608ea4dc9332f57a0596bea61cb79bb54efb95a3d5b3e53d55d4283f9dd` (Gnosis block
49498130). The Council decoded the swap's `beneficiary` as zero, so the verified router
pays the output to the Safe; it relied on Velora's documentation (docs.velora.xyz,
"chains-and-contracts", observed 2026-11-26) and the Safe's earlier execution with the
router (tx `0xa246291e…`, block 25136498). Precedents: 0xa2e204cf…, 0x6b655b88…
applied; insecure R-4.3 rulings with unlinked recipients distinguished.

Lessons:
- The draft's callout ("decode `beneficiary`; zero means the Safe receives the output")
  was exactly the Council's path. A "not accepted" draft that names the one deciding
  check is useful.
- The Council used an earlier router execution the agents had found but couldn't tie
  to the output. With tuple decoding (ARBOT-GAPS) this would have been an accepted
  `secure` draft. The same likely holds for `0xffa3…` (Pendle `receiver`).
- Add docs.velora.xyz to `sources.allow` for aggregator cases.

### Vault redeem v0.6.0 (R-4.3), `0xac1e39678da5…`, 2026-11-27: secure, accepted

- Same Safe as a768, another Lagoon vault (`LagoonVault` v0.6.0 implementation
  `0x6c77…249f`), created by an owner of the Safe. The silo `0xae0f…1a69` is not
  Sourcify-verified, and its code hash differs from the v0.5.0 silos.
- Judgment 1 threw out case 3: the silo was assumed to be escrow, and the link was weaker than the precedents'.
  It named the cure. The detective then found: the silo code hash vs verified silos (differs);
  Etherscan verifies it as `src/v0.6.0/Silo.sol` (offchain); the vault's verified
  compilation contains that `Silo`; the Safe's nonce 19/20 redeems on the a768 vault
  logged share transfers to a Sourcify-verified `Silo`. Judgment 2 accepted.
- Oversight caught the detective comparing Transfer values with calldata argument words
  (hand-decoding): removed. Tell detectives explicitly not to compare log values with
  calldata.
- My own brief wrongly said the other vault had "the same creator"; the prosecutor
  correctly refused to assert it. Orchestrator briefs must not add facts.
- The Safe Transaction Service returned "Monthly quota exceeded" (429) for
  `transfers -all`; `executions -all` still worked an hour later. Record quota hits.
- Another 5-hour usage-limit stop at ~18:00 UTC (reset 18:30); the judge was resumed
  with SendMessage rather than restarted.
