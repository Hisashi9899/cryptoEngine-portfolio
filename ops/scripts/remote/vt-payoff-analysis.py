#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""V.T 反実仮想のパターン別ペイオフ分析（読み取り専用・発注経路なし）。

`vt-replay` が出力した JSONL（入口行と出口行）を (symbol, entry_time) で
対にし、手数料込みの純損益を再計算して、入口パターン×方向ごとに
n / 勝率 / PF / 平均利益 / 平均損失 / ペイオフ比 / 期待値 / 純損益 /
平均保有秒 を出す。

前提（live / vt-acceptance と一致させる）:
  - 入口行: ts, symbol, type(buy|sell), price, qty, pattern, ...
  - 出口行: entry_time, entry_pattern, price, qty, ...
  - buy  : (exit-entry)*qty
    sell : (entry-exit)*qty
  - 手数料: (entry+exit)*qty*fee_rate を控除（fee_rate は片道率）。
    taker 0.05%/片道なら 0.0005。maker 入口/利確なら 0.0005 のままでも
    「控除が軽い」側の参考値として --fee-rate で変えられる。

使い方:
  python3 vt-payoff-analysis.py --run-dir /path/to/cf/20261115_043002
  python3 vt-payoff-analysis.py --run-dir DIR --out ops/logs/vt-payoff.tsv
  python3 vt-payoff-analysis.py --root /path/to/cf --k 0 1.0 --out FILE
"""
import argparse
import glob
import json
import os
import re
import sys
from collections import defaultdict

DIR_RE = re.compile(r"^k(?P<k>[0-9.]+)$")


def fnum(x, default=0.0):
    try:
        return float(x)
    except (TypeError, ValueError):
        return default


def load_pairs(path):
    """JSONL を読み、(entry, exit) のペアと未対消を返す。"""
    entries = {}
    order = []
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            if "entry_time" in r:
                order.append(r)
            else:
                ts = r.get("ts")
                if ts is None:
                    continue
                entries[int(ts)] = r

    pairs = []
    unpaired = 0
    for r in order:
        try:
            et = int(r.get("entry_time"))
        except (TypeError, ValueError):
            unpaired += 1
            continue
        e = entries.get(et)
        if e is None:
            unpaired += 1
            continue
        ep = fnum(e.get("price"))
        xp = fnum(r.get("price"))
        qty = fnum(r.get("qty")) or fnum(e.get("qty"))
        if ep <= 0 or xp <= 0 or qty <= 0:
            unpaired += 1
            continue
        side = (e.get("type") or "").lower()
        gross = (xp - ep) * qty if side == "buy" else (ep - xp) * qty
        hold = (fnum(r.get("ts")) - et) / 1000.0
        pairs.append({
            "symbol": e.get("symbol") or r.get("symbol") or "",
            "pattern": e.get("pattern") or r.get("entry_pattern") or "unknown",
            "direction": "long" if side == "buy" else "short",
            "entry": ep,
            "exit": xp,
            "qty": qty,
            "gross": gross,
            "hold": hold,
            "mfe": fnum(r.get("mfe")),
            "mae": fnum(r.get("mae")),
            "reason": r.get("reason") or "",
        })
    return pairs, unpaired


def pf(nets):
    win = sum(v for v in nets if v > 0)
    loss = -sum(v for v in nets if v < 0)
    if not nets:
        return 0.0
    if loss <= 0:
        return 1e9 if win > 0 else 0.0
    return win / loss


def summarize(rows, fee_rate):
    """rows（ペア辞書）を fee_rate で採点して集計行を返す。"""
    nets = []
    wins = []
    losses = []
    holds = []
    for r in rows:
        fee = (r["entry"] + r["exit"]) * r["qty"] * fee_rate
        net = r["gross"] - fee
        nets.append(net)
        holds.append(r["hold"])
        if net > 0:
            wins.append(net)
        elif net < 0:
            losses.append(-net)
    n = len(nets)
    if n == 0:
        return None
    avg_win = sum(wins) / len(wins) if wins else 0.0
    avg_loss = sum(losses) / len(losses) if losses else 0.0
    payoff = (avg_win / avg_loss) if avg_loss > 0 else 0.0
    holds_sorted = sorted(holds)
    median_hold = holds_sorted[n // 2] if n else 0.0
    return {
        "n": n,
        "wins": len(wins),
        "winrate": len(wins) / n,
        "pf": pf(nets),
        "avg_win": avg_win,
        "avg_loss": avg_loss,
        "payoff": payoff,
        "expectancy": sum(nets) / n,
        "net": sum(nets),
        "avg_hold": sum(holds) / n,
        "median_hold": median_hold,
        "fees": sum((r["entry"] + r["exit"]) * r["qty"] * fee_rate for r in rows),
    }


HEADER = [
    "run_id", "symbol", "k", "direction", "pattern",
    "n", "wins", "winrate", "pf", "avg_win", "avg_loss", "payoff",
    "expectancy", "net", "fees", "avg_hold_sec", "median_hold_sec",
]


def fmt_row(run_id, symbol, k, direction, pattern, s):
    return [
        run_id, symbol, k, direction, pattern,
        str(s["n"]), str(s["wins"]), "%.4f" % s["winrate"], "%.4f" % s["pf"],
        "%.4f" % s["avg_win"], "%.4f" % s["avg_loss"], "%.4f" % s["payoff"],
        "%.4f" % s["expectancy"], "%.4f" % s["net"], "%.4f" % s["fees"],
        "%.1f" % s["avg_hold"], "%.1f" % s["median_hold"],
    ]


def iter_run_dirs(root, runs):
    if runs:
        return [os.path.abspath(r) for r in runs]
    return sorted(p for p in glob.glob(os.path.join(root, "*")) if os.path.isdir(p))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--run-dir", action="append", default=[],
                    help="反実仮想の run ディレクトリ（複数可）")
    ap.add_argument("--root", default="",
                    help="run ディレクトリの親（既定の run を全部対象）")
    ap.add_argument("--k", nargs="*", default=None,
                    help="対象の k（例: 0 1.0）。省略時は全 k")
    ap.add_argument("--symbols", default="",
                    help="対象銘柄（空白区切り）。省略時は全銘柄")
    ap.add_argument("--fee-rate", type=float, default=0.0005,
                    help="片道手数料率（既定 0.0005 = taker 0.05%%）")
    ap.add_argument("--out", default="",
                    help="TSV 出力先（省略時は標準出力）")
    ap.add_argument("--table", action="store_true",
                    help="人間向けの表も標準エラーへ出す")
    args = ap.parse_args()

    if not args.run_dir and not args.root:
        ap.error("--run-dir か --root のどちらかが必要です")
    kset = set(args.k) if args.k else None
    symset = set(args.symbols.split()) if args.symbols.strip() else None

    out_lines = ["\t".join(HEADER)]
    grand = defaultdict(list)  # (symbol,k) -> nets rows
    any_rows = False

    for run in iter_run_dirs(args.root, args.run_dir):
        run_id = os.path.basename(run.rstrip(os.sep))
        for sym_dir in sorted(glob.glob(os.path.join(run, "*"))):
            if not os.path.isdir(sym_dir):
                continue
            symbol = os.path.basename(sym_dir)
            if symset and symbol not in symset:
                continue
            for k_dir in sorted(glob.glob(os.path.join(sym_dir, "k*"))):
                if not os.path.isdir(k_dir):
                    continue
                m = DIR_RE.match(os.path.basename(k_dir))
                if not m:
                    continue
                k = m.group("k")
                if kset and k not in kset:
                    continue
                files = sorted(glob.glob(os.path.join(k_dir, "*.jsonl")))
                if not files:
                    continue
                rows = []
                unpaired = 0
                for p in files:
                    pr, up = load_pairs(p)
                    rows.extend(pr)
                    unpaired += up
                if not rows:
                    continue
                any_rows = True
                grand[(symbol, k)].extend(rows)

                groups = defaultdict(list)
                for r in rows:
                    groups[(r["direction"], r["pattern"])].append(r)
                for (direction, pattern) in sorted(groups, key=lambda t: (t[0], t[1])):
                    s = summarize(groups[(direction, pattern)], args.fee_rate)
                    if not s:
                        continue
                    out_lines.append("\t".join(
                        fmt_row(run_id, symbol, k, direction, pattern, s)))

                tot = summarize(rows, args.fee_rate)
                if tot:
                    out_lines.append("\t".join(
                        fmt_row(run_id, symbol, k, "all", "TOTAL", tot)))
                if unpaired:
                    print("[payoff] %s %s k%s: unpaired exits=%d"
                          % (run_id, symbol, k, unpaired), file=sys.stderr)

    # 複数 run をまとめた銘柄×k の総計（run をまたいだ比較用）。
    for (symbol, k) in sorted(grand):
        rows = grand[(symbol, k)]
        groups = defaultdict(list)
        for r in rows:
            groups[(r["direction"], r["pattern"])].append(r)
        for (direction, pattern) in sorted(groups, key=lambda t: (t[0], t[1])):
            s = summarize(groups[(direction, pattern)], args.fee_rate)
            if s:
                out_lines.append("\t".join(
                    fmt_row("ALL", symbol, k, direction, pattern, s)))
        tot = summarize(rows, args.fee_rate)
        if tot:
            out_lines.append("\t".join(
                fmt_row("ALL", symbol, k, "all", "TOTAL", tot)))

    if not any_rows:
        print("[payoff] no trades found (run dir / k / symbols を確認)", file=sys.stderr)

    text = "\n".join(out_lines) + "\n"
    if args.out:
        os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
        with open(args.out, "w", encoding="utf-8") as f:
            f.write(text)
        print("[payoff] wrote %s (%d rows)" % (args.out, len(out_lines) - 1),
              file=sys.stderr)
    else:
        sys.stdout.write(text)

    if args.table:
        cols = ["symbol", "k", "direction", "pattern", "n", "winrate",
                "pf", "payoff", "expectancy", "net"]
        idx = [HEADER.index(c) for c in cols]
        lines = [" ".join("%-10s" % c for c in cols)]
        for i in range(1, len(out_lines)):
            cells = out_lines[i].split("\t")
            lines.append(" ".join("%-10s" % cells[j] for j in idx))
        print("\n".join(lines), file=sys.stderr)


if __name__ == "__main__":
    main()
