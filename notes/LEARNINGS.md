# Arbitration learnings

Durable notes from running the `arbitrate` skill on live Safenet disputes. Scratch
files live in `.msgboard/` and are never committed; the published draft rulings live
on the docket artifact.

## Standing rules for each hourly run

- **Sync gate.** Before arbitrating anything new, check that the fork
  (shebinjohn/safenet-arbitration-bot) is in sync: its `main` equals upstream
  (safe-research) `main`, and `pr9-offchain-references` merges cleanly onto upstream
  `main`. If not, ask the user to sync the fork first, and don't start new
  arbitrations until they have. Monitoring (pending, resolved checks) continues.
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
pending for all, as of Gnosis block 49458567 (2026-11-25).

| Request | Chain · nonce | Pattern | Draft | Rounds / what was caught | Council outcome |
| --- | --- | --- | --- | --- | --- |
| `0x863d07a43e2c…` | 42161 · 0 | transfer executed 3 s before proposal | not accepted | 3 judgments thrown out; last case's § 6.4 departure misdescribed `0x1fcf9563…`; `0x4e701ce5…` found afterwards | pending |
| `0xd39dc3a746b9…` | 1 · 2 | vault redeem (derUSDC) | secure | accepted on third judgment; two cases thrown out over the silo identification | pending |
| `0xf8a4d29019be…` | 1 · 10 | vault redeem (derBTC) | secure | accepted round 1 | pending |
| `0xb2aaafd43fb7…` | 1 · 4 | vault redeem (derEURC) | secure | accepted round 1 | pending |
| `0x3296fc227b79…` | 1 · 2 | return of the Safe's only inflow (SXT) | secure, human review | accepted round 1; flags/labels gap decisive | pending |
| `0x5d7d0c00a4a5…` | 1 · 1537 | payment to recurring payee (USDC) | secure | accepted round 1; lookalike sender noted | pending |
| `0x2d0174daa69d…` | 1 · 34 | swap, recipient tuple not decoded | not accepted | 2 judgments thrown out (§ 3.8 tie-break; provisional secure); request ID first given with a typo | pending |
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
