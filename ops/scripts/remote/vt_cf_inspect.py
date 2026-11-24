#!/usr/bin/env python3
"""replay/paper の JSONL 往復整合を検査する読み取り専用ツール。

vt-replay の出力は「成行入口で qty<=0 でも建玉を開く」ため、vt-acceptance の
往復数とリプレイ summary の往復数が食い違うことがある。このツールは
  - 入口/出口の件数
  - qty<=0 / price<=0 の入口（実取引では成立しない幽霊玉）
  - 逐次対付け（vt-acceptance と同方式）の往復数
  - entry_time 基準の往復数
を出して、差分の原因を特定する。ファイルは変更しない。

使い方:
  python3 vt_cf_inspect.py FILE [FILE...]
"""
import json
import sys
from collections import Counter


def inspect(path):
    entries = []
    exits = []
    bad = []
    with open(path, "r", encoding="utf-8", errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                r = json.loads(line)
            except ValueError:
                continue
            if r.get("pattern") == "exit":
                exits.append(r)
                continue
            if (r.get("type") or "").lower() in ("buy", "sell"):
                entries.append(r)
                if (r.get("qty") or 0) <= 0 or (r.get("price") or 0) <= 0:
                    bad.append(r)

    # vt-acceptance (pairNetPnlFeesAuto) と同じ逐次対付け。
    opens = {}
    paired = 0
    unmatched_exit = 0
    for r in sorted(entries + exits, key=lambda x: x.get("ts", 0)):
        sym = (r.get("symbol") or "").upper()
        if r.get("pattern") == "exit":
            if sym in opens:
                paired += 1
                del opens[sym]
            else:
                unmatched_exit += 1
            continue
        if sym in opens:
            continue
        if (r.get("qty") or 0) <= 0 or (r.get("price") or 0) <= 0:
            continue
        opens[sym] = r

    # entry_time 基準（リプレイ summary に近い数え方）。
    by_entry_time = sum(1 for r in exits if r.get("entry_time"))

    print(json.dumps({
        "file": path,
        "entries": len(entries),
        "exits": len(exits),
        "ghost_entries(qty<=0 or price<=0)": len(bad),
        "ghost_qty0": sum(1 for r in bad if (r.get("qty") or 0) <= 0),
        "ghost_price0": sum(1 for r in bad if (r.get("price") or 0) <= 0),
        "pairs_sequential": paired,
        "pairs_by_entry_time": by_entry_time,
        "unmatched_exits": unmatched_exit,
        "ghost_patterns": dict(Counter((r.get("pattern") or "?") for r in bad)),
        "ghost_types": dict(Counter((r.get("type") or "?") for r in bad)),
        "ghost_sample": [{k: r.get(k) for k in ("ts", "type", "pattern", "price", "qty", "mid_price")}
                         for r in bad[:3]],
    }, ensure_ascii=False))


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("usage: vt_cf_inspect.py FILE [FILE...]", file=sys.stderr)
        raise SystemExit(2)
    for p in sys.argv[1:]:
        inspect(p)
