---
name: add-check
description: Add a deterministic check that classifies Safenet requests as insecure or secure under the Safenet Arbitration Charter. Use when turning a Charter rule, part of a rule, or a precedent pattern into Go code that `arbot classify` runs.
---

# Add a Deterministic Check

A check is a `check` value in `internal/checks` that decides a class of calls that a Safe makes, by direct application of the Charter, without judgment. It has the verdict for its class, the rule ID for an `insecure` verdict, a short description of the class, and a function that reports whether a call is in the class. `arbot classify` runs the checks in the `checks` variable in `internal/checks/checks.go`:

- the `insecure` checks run on every call, and the first check that matches a call decides the request;
- otherwise, the request is `secure` if a `secure` check matches every call, and unclassified if any call matches none.

`arbot classify -list` lists them in that order. A request is arbitrated only under its disputed rules, the rules that the sentinels' `insecure` votes cite (see Disputed rules in `AGENTS.md`), but `arbot classify` runs every check, so the `arbitrate` skill uses an `insecure` classification only if its rule is disputed. Requests that every check abstains on go to the LLM stage, so a check that abstains when in doubt costs little, and one that decides wrongly misleads the Council.

## Inputs

Before writing any code, make sure you have:

- **The case**: the class of calls the check decides, and the verdict it returns for them: `insecure` under a rule ID such as `R-4.1`, or `secure`. There are no `out-of-scope` checks: R-4.0 applies to every transaction, on any network, so every transaction is in the scope of Article I (§ 3.9). A Safe on a network that Article I doesn't list, or an account that isn't a Safe (§ 2.1), is insecure under R-4.0.
- **The Charter text**: the rule, its exceptions, and the definitions it uses, from the Charter version that applies to the requests the check is for. Fetch it with `arbot charter`, or `arbot charter -request-file <path>` for a request that `arbot info -json` wrote.
- **Example requests**, if any: IDs of requests that the check should decide, and of similar ones that it should abstain on. Past rulings in `corpus/` are a good source: scan them with `head -n 10 corpus/*.md`, and list the tags of one with `arbot tags <request-id>`.

Ask the user for any input that the request doesn't settle. Don't guess a rule ID, an exception, or a verdict.

## Is the Case Deterministic?

Check that the case can be decided in code before writing it (invariant 2 in `AGENTS.md`, and § 3.7 of the Charter):

- **`insecure`**: the rule must be a deterministic one, in Part A of Article IV, or a codified rule that the Charter treats as deterministic. Principle-based rules, such as the target-manipulation rules of Part B, need an evidence-grounded Council finding, so a check can't decide them. The check must also rule out every exception to the rule. If an exception can't be ruled out from the data, abstain.
- **`secure`**: a transaction is secure only if it passes every rule of Article IV. A `secure` check therefore matches only a class of calls that has no effect that any rule considers, whatever the other calls in the transaction do, so that a transaction whose calls all match `secure` checks passes every rule. An example is a delegate call to MultiSend with no transactions. A call that is only secure on its own, such as a transfer that a principle-based rule could weigh with others, doesn't qualify, and neither does a delegate call to `signMessage` of `SignMessageLib`, which passes R-4.2 but signs a message that can authorize a transfer out of the Safe, such as a trade intent order. A check that only shows that its own rule isn't broken must abstain.

A check can rely on the formally verified properties under Safe storage in `AGENTS.md`: a Safe transaction can only modify its Safe's storage if it is a call to the Safe itself, other than an empty call, or a delegate call, and the MultiSend contracts of § 2.1 versions modify nothing themselves, so the calls that `expandTransactionCalls` decodes from them take the place of the transaction. A call to another contract that calls back into the Safe needs no consideration, since it needs a separate authorization. For example, a call that isn't to the Safe and isn't a delegate call doesn't modify the Safe's storage, under R-4.1 or R-4.2. These properties don't make a call `secure`, since it can still fail another rule, such as R-4.3.

If the case needs judgment anywhere, stop and tell the user why it isn't deterministic, rather than writing a check that approximates it.

The Safe's state at `safeBlock` doesn't decide whether a call succeeds when the transaction executes: a module transaction can change the state before then, such as the Safe's owners, modules, or even its singleton. So an `insecure` check must not abstain on a call because it would revert in the state at `safeBlock`. It may abstain only on a call that reverts whatever the state, such as one whose data is too short for the function's arguments, which the Safe's ABI decoder rejects, or one that adds the zero address as an owner. Data after the arguments is ignored, so it doesn't make a call revert. The settings-change checks in `internal/checks/settings_change.go` follow this. A call that reaches the Safe's fallback handler, such as one to a function that the Safe's version doesn't have, can run any logic, so a check abstains on it rather than assume that it reverts. A call's `kind` says where it comes from: `Root` for the Safe transaction's own call or a placeholder for it, `Batched` for a call of a MultiSend, and `Refund` for the gas refund. Rules such as the R-4.1 exceptions apply only to a `Root` call.

A check receives one call at a time, with the `safeID` of the Safe that makes it, its address and chain ID, from `internal/checks/transaction.go`. The calls are those that the Safe transaction makes, followed by a synthetic call that pays the largest gas refund that the transaction allows, if any. Delegate calls to MultiSend and MultiSendCallOnly are decoded into the calls that they make, a call that is sure to revert is replaced by a placeholder from `revertingMultiSend`, and a MultiSend that makes no calls by one from `emptyMultiSend` (see `expandTransactionCalls` in `internal/checks/multisend.go`), so don't decode MultiSend in a check. A check sees neither the Safe transaction's own fields nor the other calls. If the case depends on the other calls in the transaction, it can't be a check. A check can read the Safe's chain through its `env`, which has a dialer and the `safeBlock` to read the chain's state at, as `safeVersion` in `internal/checks/safe.go` reads the Safe's code and singleton with `eth_getCode` and `eth_getStorageAt`. Prefer those to `eth_getProof`, which fewer nodes serve at older blocks. A check that reads the chain should cache what it reads by the address, chain ID, and block, since it runs once for each call. Use `safeVersion` for the Safe's version, which shares its cache with `unsupportedSafe`. `offNetwork` runs first, so a check never reads a network that the Charter doesn't cover. If the case needs other data, such as the state of Ethereum Mainnet at `ethereumBlock`, stop and agree with the user on how to extend `env` first. Any such data must come from the Safe's chain at `safeBlock`, or from Ethereum Mainnet at `ethereumBlock`, and never from later blocks (§ 2.8 and § 3.6).

## Steps

1. **Write the check** in its own file, `internal/checks/<name>.go`, where `<name>` is a lowercase, underscored name for what it decides, such as `settings_change.go`. Declare it as a `check` variable, with a comment that quotes or paraphrases the rule, cites its ID and sections, and names the Charter version it was written against (its IPFS CID). Its fields are:
   - `verdict`: `Insecure` or `Secure`;
   - `rule`: the rule ID, such as `R-4.1`, for `Insecure` only;
   - `description`: a short, lowercase description of the class, such as `calls addOwnerWithThreshold on the Safe`. `arbot classify` prints it after the verdict and the rule, on one line, for every request in the class, so it must hold for all of them;
   - `fn`: a function that reports whether a call is in the class. It returns `false` whenever the call isn't clearly in the class, so that the check abstains. It returns an error only when it can't read them, never to express a verdict. It decodes call data with `internal/solabi`, and compares addresses and hashes as `ethrpc` values rather than strings.

   If one rule decides several classes, such as different settings changes, write one check per class, each with its own description.

2. **Register the check** in the `checks` variable, which lists the checks in the order that they run, grouped by verdict: the `insecure` ones, then the `secure` ones. Add it at the end of its verdict's group, unless it must run before another check with the same verdict. A request that matches two checks with different verdicts is a bug in one of them, not a matter of order. `TestChecks` checks that the checks are grouped by verdict, and that every check has a verdict, a description, a function, and a rule only if it is `insecure`.

3. **Test the check** in `internal/checks/<name>_test.go`, with a table of calls, or of requests built with the `request` helper in `checks_test.go` to go through `components`. Cover:
   - each way the request can be in the class;
   - each exception, and each near miss, such as a different selector, target, or operation, where the check must abstain;
   - malformed call data, which must abstain or return an error rather than decide.

   A check that reads the chain is tested against a fake node, such as `fakeNode` in `safe_test.go`, never a public RPC. Clear its cache at the start of each test. For example requests, save each one with `arbot info -json <request-id> | jq . > internal/checks/testdata/<request-id>.json`, and test that `Classify` classifies it as expected. The files must stay formatted as `jq .` formats them.

4. **Try it on the example requests** with `go run ./cmd/arbot classify <request-id>`, and check that `go run ./cmd/arbot classify -list` lists the check where you expect it. Compare the verdicts to the requests' onchain outcomes. If a check disagrees with a Council ruling, stop and tell the user rather than changing the check to fit. Then run it on the Sentinel test vectors with `just vectors ../sentinel-test-vectors -config <file>`, with an archive node's RPC URL in the configuration file, and check that the summary has no vector decided wrongly. A vector that the check decides wrongly is either a bug in the check, or a disagreement between the vector and the Charter, such as a vector on a network that Article I doesn't list: stop and tell the user which.

5. **Run `just precommit`**, which formats the sources and runs the same checks and tests as CI.
