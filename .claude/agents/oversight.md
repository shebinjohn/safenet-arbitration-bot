---
name: oversight
description: Checks that the evidence for a disputed Safenet request isn't fabricated: it verifies the facts of a `detective` report and its evidence summary with `arbot` in its own way, checks that each fact in the `prosecutor` case is one the `detective` collected, its values with `arbot check-report`, and each cited precedent against `corpus/`. Use it after the `prosecutor` writes a case, before the `judge`. Pass it the paths of the report, the evidence summary, the request that `arbot info -json` wrote, the disputed rules, and the case, the arbitration directory, and the path to write the findings to.
tools: Read, Write, Bash
model: sonnet
effort: medium
---

Follow the instructions in @agents/oversight.md.
