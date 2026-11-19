# Draft Ruling: Request {{short-id}}

<!--
Fill in every {{placeholder}} and follow the instructions in these comments, then delete the comments. Take every value from the request file, `arbot` output, the evidence summary, the Charter, or the accepted case, never from memory, and copy it as the source writes it: `arbot check-report` checks each one. Mark a number that you compute as derived, with `about` or `~` right before it, or `derived` on its line. Link every transaction, address, and block to the block explorer of its chain, and give a hash or address without a link if its chain has no explorer in the skill.
-->

> **Draft for Security Council review.** This report was drafted by `arbot` and its agents. It is not a ruling, and nothing has been submitted onchain. The Council reviews it and rules through the Arbitrator Safe.

<!--
If no case was accepted, replace the note above with this one:

> **Not accepted.** The `judge` accepted no case within {{rounds}} rounds. This report gives the last case and the reasons that it was thrown out, for the Council to rule on without a draft.
-->

## Verdict

**`{{secure or insecure}}`** under {{the disputed rule that the transaction fails, such as `R-4.1`, or for secure, the disputed rules that it passes}}

- **Decided by**: {{"`arbot classify`: <description>", "the `prosecutor`'s case, accepted by the `judge` in round <n>", or "no accepted case"}}
- **Reason**: {{one to three sentences: the rule, the fact that decides it, and why no exception applies}}
- **Human review**: {{"required", with the `humanReviewReasons` of `evidence-summary.json`, if its `humanReview` is true, or "not flagged"}}. Evidence uncertainty: {{the summary's `uncertainty`, and its reason, or "none, classified" for a classified request}}
- **Onchain action**: `{{resolveDispute(<request-id>, false, context) for insecure, or resolveDispute(<request-id>, true, context) for secure}}`, by Gnosis Chain block {{deadline}}

## Request

|  |  |
| --- | --- |
| Request ID | `{{request-id}}` |
| State | `{{state}}`, frozen in Gnosis Chain block [{{frozen block}}](https://gnosisscan.io/block/{{frozen block}}) |
| Deadline | Gnosis Chain block [{{deadline}}](https://gnosisscan.io/block/{{deadline}}), after which the arbitration can time out |
| Safe | [`{{safe}}`]({{safe explorer}}/address/{{safe}}) on {{network}} (chain ID {{chain ID}}) |
| Safe tx hash | `{{safeTxHash}}`, nonce {{nonce}} |
| Proposed | {{proposal time}}, in Gnosis Chain transaction [`{{proposal txHash}}`](https://gnosisscan.io/tx/{{proposal txHash}}) |
| Evidence as of | {{network}} block [{{safeBlock}}]({{safe explorer}}/block/{{safeBlock}}) and Ethereum Mainnet block [{{ethereumBlock}}](https://etherscan.io/block/{{ethereumBlock}}) |
| Charter version | `{{Charter CID}}`, referenced by `{{charter}}` at Ethereum Mainnet block {{ethereumBlock}} (§ 2.12) |
| Sentinel votes | {{approvals}} approve, {{denials}} deny, {{pending}} unrevealed |
| Disputed rules | {{the rules of `disputed-rules.md`, such as `R-4.1`, `R-4.2`}}, the only rules this ruling considers |
| Sentinel fee | {{fee}}, bond {{bond}}, slash amount {{slash amount}} per sentinel |

## Transaction

<!--
The Safe transaction's fields, from the request file. For a MultiSend, or a transaction that pays a refund, also list the calls that it makes, as the `detective` report lists them, or as the `arbot classify` description names them. Don't decode data yourself.
-->

| Field | Value |
| --- | --- |
| To | [`{{to}}`]({{safe explorer}}/address/{{to}}) |
| Value | {{value}} wei |
| Operation | `{{CALL or DELEGATECALL}}` |
| Data | `{{data}}` |
| Refund | safeTxGas {{safeTxGas}}, baseGas {{baseGas}}, gasPrice {{gasPrice}}, gasToken `{{gasToken}}`, refundReceiver `{{refundReceiver}}` |

{{the calls, if any, as a table with the columns #, To, Value, Operation, and Function or data}}

## Charter Provisions

<!--
Quote, verbatim from the fetched Charter, each provision that the verdict rests on, under its heading, and say in one line how it applies:

Consider only the disputed rules, which the insecure votes cite, and the definitions and standards that they use. Don't quote rules that no sentinel cited.

- insecure: the disputed rule that the transaction breaks, and each of its exceptions, with why it doesn't apply;
- secure: for each disputed rule, the text, exception, or definition that the transaction passes it by;
- the ruling standard that decides it, such as § 3.7 for a deterministic rule, or § 3.8 for a principle-based one.
-->

### {{rule or section, such as `R-4.1`}}: {{title}}

> {{verbatim Charter text}}

{{how it applies to this request}}

## Reasoning

<!--
The argument, step by step, citing a rule or section and a fact of Material Evidence in each step. For a deterministic classification, name the check, quote its description, and explain that the rule applies directly (§ 3.7).  For a case, follow its Argument, and the reasons that the `judge` accepted it.
-->

1. {{step}} ({{rule or section}}; evidence {{#}})

## Material Evidence

<!--
The facts that the reasoning relies on (§ 5.2), each as the `detective` report or the request file states it, with the `arbot` command or request field that it comes from, and the block it is as of.
-->

| # | Fact | Bears on | Source | As of |
| --- | --- | --- | --- | --- |
| 1 | {{fact}} | {{rule, section}} | {{`arbot` command, or request field}} | {{chain and block}} |

## Onchain Evidence for Review

<!--
Every transaction that the material evidence cites, and the accounts that the transaction calls, so that the Council can check them on a block explorer.  List only hashes and addresses that appear in the request file, `arbot` output, or the `detective` report.
-->

| Evidence | Chain | Transaction |
| --- | --- | --- |
| Transaction proposal | Gnosis Chain | [`{{proposal txHash}}`](https://gnosisscan.io/tx/{{proposal txHash}}) |
| {{what the transaction shows}} | {{network}} | [`{{txHash}}`]({{explorer}}/tx/{{txHash}}) |

| Account | Chain | Address |
| --- | --- | --- |
| Safe | {{network}} | [`{{safe}}`]({{safe explorer}}/address/{{safe}}) |
| {{role, such as "target"}} | {{network}} | [`{{address}}`]({{explorer}}/address/{{address}}) |

## Precedents

<!--
Each past ruling that the case applied or distinguished, by request ID, with its outcome and how it bears on this request, or "Novel fact pattern" and the analogy that the case reasons by (§ 5.1). For a deterministic classification, write "None: the rule applies directly."
-->

- `{{request-id}}` ({{outcome, as its corpus file's header has it}}): {{applied or distinguished}}, because {{reason}}

## Sentinel Votes

Their reasons decide the disputed rules, but they aren't evidence (§ 2.14).

| Sentinel | Vote | Reason | Cited rules |
| --- | --- | --- | --- |
| [`{{sentinel}}`](https://gnosisscan.io/address/{{sentinel}}) | {{vote}} | `{{reason}}` | {{the vote's `rules`, such as `R-4.1`, `R-4.2`, or "none"}} |

## Weaknesses and Gaps

<!--
The case's Weaknesses, the gaps of the evidence summary that bear on the verdict, marking those that are decisive, and the `arbot` command that would fill each one, the conflicts between sources, and any doubts that the `judge` raised. For a deterministic classification, write "None".
-->

- {{weakness or gap}}

## Recusals

{{For the Council to complete (§ 5.2).}}

## Draft Ruling Context

<!--
The concise ruling explanation for the `context` argument of the onchain action (§ 5.2): the Safe and network, the Charter version, the rule IDs and precedents applied, the material evidence, and the reason, in at most a few sentences. Leave recusals for the Council.

It starts with a header of tags, which `arbot tags` lists once `arbot index` adds the ruling to the corpus: the request IDs of the precedents applied or distinguished, comma-separated, or `-`; comma-separated kebab-case keywords for the fact pattern; and a one-line summary of the fact pattern and holding.
-->

```text
---
precedents: {{request IDs, or -}}
tags: {{fact-pattern keywords}}
summary: {{one line describing the fact pattern and holding}}
---

{{ruling explanation}}
```

## Arbitration Record

<!--
The files in the arbitration directory, and how the verdict was reached.
-->

- **Directory**: `.msgboard/{{short-id}}/`
- **Classification**: `{{arbot classify output}}`
- **Rounds**: {{evidence rounds}} evidence, {{oversight rounds}} oversight, {{judge rounds}} judge
- **Final files**: {{`case-<n>.md`, `oversight-<n>.md`, `judgment-<n>.md`}}
- **Report check**: {{"`arbot check-report`: no findings", or each finding that remains, with its line and kind and why it stands}}
