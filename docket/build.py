#!/usr/bin/env python3
"""Builds the Safenet Arbitration Docket page.

Usage: python3 docket/build.py [-m MSGBOARD] [-o OUT]

Reads docket/docket.json (the curated list of requests, their draft verdicts and
how they were decided), docket/sections.html (the fact-pattern sections and
notes, with link macros), docket/template.html, each request's
MSGBOARD/<short-id>/request.json (`arbot info -json`) and report.md, and the
10-line headers of MSGBOARD/corpus/*.md. Writes OUT/index.html and
OUT/reports/<short-id>.md, ready to publish to the docket artifact.

Macros in sections.html:
  {{req:<12 hex>}}              link to a request's draft on this page
  {{addr:<chain>:<address>[|text]}}  explorer link on that chain
  {{tx:<chain>:<hash>[|text]}}       explorer link on that chain
  {{prec:0x<8+ hex>}}           corpus precedent, linked to its ruling record
  {{sentinel:<address>}}        sentinel name (if known) and short address
"""
import argparse, glob, html, json, os, re, shutil, sys

HERE = os.path.dirname(os.path.abspath(__file__))
EXPLORERS = {1: ("https://etherscan.io", "Etherscan"), 42161: ("https://arbiscan.io", "Arbiscan"),
             100: ("https://gnosisscan.io", "Gnosisscan")}
FEE_TOKEN = "0x2a22f9c3b484c3629090feed35f17ff8f88f76f0"  # always on Gnosis Chain
NUMBERS = "zero one two three four five six seven eight nine ten eleven twelve".split()
EXT = 'target="_blank" rel="noopener"'


def explorer(chain, address=None):
    if address and address.lower() == FEE_TOKEN:
        chain = 100
    if chain not in EXPLORERS:
        sys.exit(f"no explorer for chain {chain}")
    return EXPLORERS[chain][0]


def short_addr(a):
    return a[:6] + "…" + a[-4:]


def a_addr(chain, a, text=None, cls="mono"):
    a = a.lower()
    return f'<a class="{cls}" href="{explorer(chain, a)}/address/{a}" {EXT}>{html.escape(text or short_addr(a))}</a>'


def a_tx(chain, h, text=None):
    return f'<a class="mono" href="{explorer(chain)}/tx/{h}" {EXT}>{html.escape(text or h[:8] + "…" + h[-6:])}</a>'


def a_req(short):
    return f'<a class="mono" href="#r-{short}">0x{short}…</a>'


def load_corpus(d):
    out = {}
    for f in sorted(glob.glob(os.path.join(d, "0x*.md"))):
        with open(f) as fh:
            head = [next(fh, "") for _ in range(10)]
        tags = dict(l.strip().split(": ", 1) for l in head[1:9] if ": " in l)
        out[tags["request"].lower()] = tags
    return out


def main():
    p = argparse.ArgumentParser()
    p.add_argument("-m", "--msgboard", default=".msgboard")
    p.add_argument("-o", "--out", default=".msgboard/docket-out")
    p.add_argument("--date", help="date shown on the page (default: docket.json)")
    args = p.parse_args()

    cfg = json.load(open(os.path.join(HERE, "docket.json")))
    sentinels = {k.lower(): v for k, v in cfg["sentinels"].items()}
    corpus = load_corpus(os.path.join(args.msgboard, "corpus"))
    reqs = []
    for e in cfg["requests"]:
        short = e["id"][2:14]
        info = json.load(open(os.path.join(args.msgboard, short, "request.json")))
        if info["id"].lower() != e["id"].lower():
            sys.exit(f"{short}: request.json is for {info['id']}")
        reqs.append((e, short, info))

    def sentinel(addr, cls=""):
        addr = addr.lower()
        name = sentinels.get(addr)
        link = f'<a class="{cls}" href="https://gnosisscan.io/address/{addr}" {EXT}>{html.escape(name or short_addr(addr))}</a>'
        return link

    def prec(prefix):
        hits = [r for r in corpus if r.startswith(prefix.lower())]
        if len(hits) != 1:
            sys.exit(f"precedent {prefix}: {len(hits)} corpus matches")
        t = corpus[hits[0]]
        return f'{a_tx(100, t["record"], prefix + "…")} <span class="muted">({t["outcome"]})</span>'

    def macro(m):
        kind, arg = m.group(1), m.group(2)
        text = None
        if "|" in arg:
            arg, text = arg.split("|", 1)
        if kind == "req":
            return a_req(arg)
        if kind == "prec":
            return prec(arg)
        if kind == "sentinel":
            return f'{sentinel(arg)} <span class="muted mono">{short_addr(arg.lower())}</span>'
        chain, val = arg.split(":", 1)
        return (a_addr if kind == "addr" else a_tx)(int(chain), val, text)

    sections = re.sub(r"\{\{(req|prec|sentinel|addr|tx):([^}]*)\}\}", macro,
                      open(os.path.join(HERE, "sections.html")).read())

    pill = {"secure": "secure", "insecure": "insecure", "not accepted": "open"}
    rows = []
    for e, short, info in reqs:
        pr = info["proposal"]; tx = pr["transaction"]; chain = tx["chainId"]
        votes = []
        for v in info["votes"]:
            if v["vote"] == "DENIED":
                votes.append(f'{sentinel(v["sentinel"])} deny <code>{html.escape(v["reason"]) or "-"}</code>')
            elif v["vote"] == "APPROVED":
                votes.append(f'{sentinel(v["sentinel"])} approve')
        action = {"secure": '<span class="mono">resolveDispute(…, true, …)</span>',
                  "insecure": '<span class="mono">resolveDispute(…, false, …)</span>'}.get(e["draft"], "Council decides")
        when = pr["time"][5:16].replace("T", " ")
        dl = info["arbitration"]["deadline"]
        rows.append(
            f'<tr><td>{a_req(short)}</td><td>{a_addr(chain, tx["safe"])} · {tx["nonce"]}</td>'
            f'<td class="num">{a_tx(100, pr["txHash"], when)}</td><td>{", ".join(info["disputedRules"]) or "-"}</td>'
            f'<td class="votes">{"<br>".join(votes)}</td><td><span class="pill {pill[e["draft"]]}">{e["draft"]}</span></td>'
            f'<td>{html.escape(e["decidedBy"])}</td><td>{action}</td>'
            f'<td class="num"><a class="mono" href="https://gnosisscan.io/block/{dl}" {EXT}>{dl}</a></td>'
            f'<td><button type="button" class="dl" data-report="{short}" hidden>Download</button></td></tr>')

    n = len(reqs)
    counts = {}
    for e, _, _ in reqs:
        counts[e["draft"]] = counts.get(e["draft"], 0) + 1
    label = {"not accepted": "no case accepted"}
    tally = "\n".join(f'    <span class="{pill[k]}">{c} {label.get(k, k)}</span>'
                      for k, c in sorted(counts.items(), key=lambda kv: ["not accepted", "insecure", "secure"].index(kv[0])))
    charters = sorted({e["charter"] for e, _, _ in reqs})
    rules = sorted({r for _, _, i in reqs for r in i["disputedRules"]})
    word = NUMBERS[n].capitalize() if n < len(NUMBERS) else str(n)
    clink = lambda c: f'<a class="mono" href="https://ipfs.io/ipfs/{c}" {EXT}>{c}</a>'
    lede = (f'{word} disputed Safenet request{"s" if n != 1 else ""} awaiting a Council decision, '
            + (f'all under Charter {clink(charters[0])}' if len(charters) == 1 else 'under Charters ' + ", ".join(map(clink, charters)))
            + '. These are drafts from <code>arbot</code> and its agents. Nothing has been submitted onchain, and the Council '
              'rules through the Arbitrator Safe. Select a request ID to read its full draft ruling, or download it as Markdown.')
    executed = "; ".join(f'{a_req(s)} {e["executed"]}' for e, s, _ in reqs if e.get("executed"))
    intro = ('Every request is <code>FROZEN</code>, and none has a ruling in the corpus yet. The proposal time links to the '
             'proposal transaction on Gnosis Chain. The deadline is the Gnosis Chain block after which the arbitration can time out.')
    if executed:
        intro += (f' Each Safe has already executed its transaction: {executed}. Executions after the proposal aren\'t '
                  'evidence (§ 3.6), and no draft relies on them.')

    shorts = [s for _, s, _ in reqs]
    precs = {r: t["record"] for r, t in corpus.items()}
    page = open(os.path.join(HERE, "template.html")).read()
    j = lambda o: json.dumps(o)
    fill = {
        "DATE": args.date or cfg["date"], "LEDE": lede, "TALLY": tally, "DOCKET_INTRO": intro,
        "ROWS": "\n".join(rows), "SECTIONS": sections.rstrip("\n"), "SENTINELS_OBSERVED": cfg["sentinelsObserved"],
        "REPORTS_JSON": j({s: i["id"] for _, s, i in reqs}),
        "VERDICTS_JSON": j({s: [pill[e["draft"]], e["draft"]] for e, s, _ in reqs}),
        "PRECEDENTS_JSON": j(precs),
        "EXPLORERS_JSON": j({s: list(EXPLORERS[i["proposal"]["transaction"]["chainId"]]) for _, s, i in reqs}),
        "SENTINELS_JSON": j(sentinels),
    }
    for k, v in fill.items():
        page = page.replace("{{" + k + "}}", v)
    if "{{" in page:
        sys.exit("unfilled placeholder: " + re.search(r"\{\{[^}]*\}\}", page).group(0))
    os.makedirs(os.path.join(args.out, "reports"), exist_ok=True)
    open(os.path.join(args.out, "index.html"), "w").write(page)
    for s in shorts:
        shutil.copyfile(os.path.join(args.msgboard, s, "report.md"), os.path.join(args.out, "reports", s + ".md"))
    print(f"wrote {args.out}/index.html and {len(shorts)} reports; {len(corpus)} corpus rulings; rules {', '.join(rules)}")


if __name__ == "__main__":
    main()
