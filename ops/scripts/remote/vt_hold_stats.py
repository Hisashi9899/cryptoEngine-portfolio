#!/usr/bin/env python3
"""vt_signals_*.jsonl の往復保有時間を集計する読み取り専用の診断ツール。

live は --max-hold-sec=43200（12時間）で強制決済するが、vt-replay は
MaxHoldSec を指定できない。反実仮想の比較に影響するかを事前に確認するために、
実際の保有時間分布を出す。

使い方:
  python3 vt_hold_stats.py --dir=/opt/cryptoengine/backend/core/trade_logs \
      --bars=300 --limit=5

出力は JSON（機械可読）。ファイルは一切変更しない。
"""
import argparse
import glob
import json
import os
import statistics


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", default=".")
    ap.add_argument("--bars", type=int, default=300)
    ap.add_argument("--limit", type=int, default=5)
    args = ap.parse_args()

    pattern = os.path.join(args.dir, "vt_signals_*_%d_*.jsonl" % args.bars)
    holds = []
    per_symbol = {}
    files = sorted(glob.glob(pattern))
    for path in files:
        open_entry = {}
        try:
            fh = open(path, "r", encoding="utf-8", errors="replace")
        except OSError:
            continue
        with fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    rec = json.loads(line)
                except ValueError:
                    continue
                sym = rec.get("symbol", "")
                typ = (rec.get("type") or "").lower()
                if rec.get("pattern") == "exit" or rec.get("entry_time"):
                    entry_ts = rec.get("entry_time") or open_entry.get(sym)
                    if not entry_ts:
                        continue
                    sec = (rec.get("ts", 0) - entry_ts) / 1000.0
                    if sec < 0:
                        continue
                    holds.append(sec)
                    per_symbol.setdefault(sym, []).append(sec)
                    open_entry.pop(sym, None)
                elif typ in ("buy", "sell"):
                    open_entry[sym] = rec.get("ts", 0)

    def stats(xs):
        if not xs:
            return {"pairs": 0}
        return {
            "pairs": len(xs),
            "max_h": round(max(xs) / 3600.0, 2),
            "median_h": round(statistics.median(xs) / 3600.0, 2),
            "over_12h": sum(1 for x in xs if x > 43200),
            "over_6h": sum(1 for x in xs if x > 21600),
        }

    longest = sorted(holds, reverse=True)[: max(0, args.limit)]
    out = {
        "files": len(files),
        "all": stats(holds),
        "by_symbol": {k: stats(v) for k, v in sorted(per_symbol.items())},
        "longest_h": [round(x / 3600.0, 2) for x in longest],
    }
    print(json.dumps(out, ensure_ascii=False))


if __name__ == "__main__":
    main()
