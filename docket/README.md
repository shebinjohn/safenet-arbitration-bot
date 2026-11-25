# Docket generator

Builds the "Safenet Arbitration Docket" artifact page.

- `docket.json`: the requests on the docket (newest last), their draft verdict
  (`secure`, `insecure`, `not accepted`), how they were decided, and when the Safe
  executed the transaction (status only). Also the sentinel names, from
  <https://docs.safefoundation.org/safenet-aegis/governance/participant-selection#current-participant-set>.
- `sections.html`: the fact-pattern sections, Council callouts and Notes, with link
  macros (`{{req:…}}`, `{{addr:<chain>:<address>[|text]}}`,
  `{{tx:<chain>:<hash>[|text]}}`, `{{prec:0x<prefix>}}`, `{{sentinel:<address>}}`).
  Precedents resolve to the `record:` of their corpus header.
- `template.html`: the page, with `{{…}}` placeholders.

Inputs from `.msgboard/`: `<short-id>/request.json` (`arbot info -json`),
`<short-id>/report.md`, and `corpus/` (from `git archive upstream/main corpus`).

```bash
python3 docket/build.py -o .msgboard/docket-out
(cd .msgboard/docket-out && python3 -m http.server 8765) &
node docket/preview.js /path/to/screenshots   # 1200px and 390px: overflow, console errors, every report opens
```

Publish `.msgboard/docket-out/index.html` with `reports/<short-id>.md` as files to
the existing artifact URL (it keeps its `downloads` capability and sharing). To
remove a settled request, delete it from `docket.json` and its section from
`sections.html`, and pass `null` for its report path on publish.
