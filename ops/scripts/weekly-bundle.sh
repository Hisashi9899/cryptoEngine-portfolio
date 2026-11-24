#!/usr/bin/env bash
# 週次改善バンドル生成（WSLで実行・日曜定例用）
#
#   ops/scripts/weekly-bundle.sh
#
# やること:
#   1) ops/deploy/sync-master.sh pull で成果物を回収
#      （trade_log.csv / calibration_latest.json / trader_heartbeat.json / funding.jsonl）
#   2) 前回束ね以降の差分だけ集計（状態は ops/logs/.weekly_bundle_state）
#   3) ops/logs/remote/<DATE>/BUNDLE.md を生成する
#      先頭に定型プロンプト＋データ。本文をそのままチャットに貼るだけ。
#
# 冪等: 同日内に再実行しても状態は前回束ね時点のまま進めない
# （BUNDLE.md を作り直すだけ。--advance で状態を前進させる）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OPS_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$OPS_ROOT/.." && pwd)"

ADVANCE=0
if [ "${1:-}" = "--advance" ]; then
  ADVANCE=1
fi

echo "==> [1/3] 成果物を回収（sync-master.sh pull）"
bash "$REPO_ROOT/ops/deploy/sync-master.sh" pull

DATE="$(date +%Y%m%d)"
DEST="$REPO_ROOT/ops/logs/remote/$DATE"
STATE_FILE="$REPO_ROOT/ops/logs/.weekly_bundle_state"
mkdir -p "$DEST"

echo "==> [2/3] 差分を集計 → $DEST/BUNDLE.md"
FUNDING_DEFAULT_SYMBOLS="BTCUSDT,ETHUSDT,SOLUSDT,XRPUSDT,DOGEUSDT" \
STATE_FILE="$STATE_FILE" \
DEST_DIR="$DEST" \
ADVANCE="$ADVANCE" \
python3 - "$DEST" <<'PYEOF'
import csv, hashlib, json, os, sys
from datetime import datetime, timezone

dest = sys.argv[1]
state_path = os.environ["STATE_FILE"]
advance = os.environ.get("ADVANCE", "0") == "1"

def load_state():
    try:
        with open(state_path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}

def read_trades(path):
    rows = []
    try:
        with open(path, encoding="utf-8", newline="") as f:
            for r in csv.DictReader(f):
                rows.append(r)
    except (OSError, ValueError):
        pass
    return rows

def fnum(v):
    try:
        x = float(v or 0)
        return x if x == x and abs(x) != float("inf") else 0.0
    except (TypeError, ValueError):
        return 0.0

state = load_state()
last_trade_ts = state.get("last_trade_ts", "")
last_funding_ts = int(state.get("last_funding_ts", 0) or 0)

# --- 約定 ---
trades = read_trades(os.path.join(dest, "trade_log.csv"))
new_trades = [r for r in trades if (r.get("timestamp", "") or "") > last_trade_ts]
sells = [r for r in new_trades if (r.get("action", "") or "").upper() == "SELL" and (r.get("pnl", "") or "") != ""]
pnls = [fnum(r.get("pnl")) for r in sells]
realized = round(sum(pnls), 4)
wins = sum(1 for p in pnls if p > 0)
losses = sum(1 for p in pnls if p <= 0)
errs = [r for r in new_trades if (r.get("status", "") or "").lower() not in ("", "ok", "filled", "success")]
from collections import Counter as _Counter
rej = _Counter(f"{r.get('status', '')}/{r.get('reason', '')}" for r in new_trades if (r.get("status", "") or "").lower() not in ("", "ok", "filled", "success"))
by_sym = {}
for r in sells:
    s = (r.get("symbol", "") or "?").upper()
    by_sym[s] = round(by_sym.get(s, 0.0) + fnum(r.get("pnl")), 4)
top = sorted(sells, key=lambda r: fnum(r.get("pnl")), reverse=True)[:3]
worst = sorted(sells, key=lambda r: fnum(r.get("pnl")))[:3]

# --- FR ---
funding_path = os.path.join(dest, "funding.jsonl")
snaps = []
try:
    with open(funding_path, encoding="utf-8") as f:
        for line in f:
            try:
                snaps.append(json.loads(line))
            except ValueError:
                continue
except OSError:
    pass
fresh = [s for s in snaps if int(s.get("ts", 0) or 0) > last_funding_ts]
series = {}
for s in snaps:
    for r in s.get("rates", []) or []:
        key = f"{r.get('exchange', '?')}:{(r.get('symbol', '') or '').upper()}"
        series.setdefault(key, []).append((int(s.get("ts", 0) or 0), fnum(r.get("funding_rate"))))
fr_lines = []
for key in sorted(series):
    pts = sorted(series[key])[-8:]
    if not pts:
        continue
    vals = [v for _, v in pts]
    flips = sum(1 for a, b in zip(vals, vals[1:]) if a * b < 0)
    fresh_n = sum(1 for ts, _ in pts if ts > last_funding_ts)
    fr_lines.append(f"- {key}: last={vals[-1]:.6f} flips(直近8件)={flips} 新規={fresh_n}件")

# --- 稼働 ---
hb = {}
try:
    with open(os.path.join(dest, "trader_heartbeat.json"), encoding="utf-8") as f:
        hb = json.load(f)
except (OSError, ValueError):
    pass
now_ms = int(datetime.now(timezone.utc).timestamp() * 1000)
hb_age = (now_ms - int(hb.get("ts", 0) or 0)) // 1000 if hb.get("ts") else -1

# --- 較正 ---
calib_path = os.path.join(dest, "calibration_latest.json")
calib_hash, calib_keys = "", []
try:
    with open(calib_path, "rb") as f:
        raw = f.read()
    calib_hash = hashlib.sha256(raw).hexdigest()[:12]
    try:
        calib_keys = sorted(json.loads(raw).keys()) if isinstance(json.loads(raw), dict) else []
    except ValueError:
        pass
except OSError:
    pass
calib_changed = (calib_hash != state.get("calib_hash", "")) if calib_hash else None

L = []
L.append("# 週次束ね（cryptoEngine 精度改善ループ用）")
L.append("")
L.append("あなたはcryptoEngine（国産自動トレード基盤）の改善アナリストです。")
L.append("次の順で回答してください。")
L.append("1. 所見（最大3点、数値根拠付き）")
L.append("2. 原因仮説（1点に絞る）")
L.append("3. 修正案（1点のみ。対象ファイル・行・差分案まで具体的に）")
L.append("4. 次週の計測項目（仮説の検証に必要な数値）")
L.append("制約:")
L.append("- 約定10件未満の指標ではパラメータ調整を提案しない")
L.append("- 実弾の挙動を変える修正は、紙検証（backtest/paper）を条件にする")
L.append("- 束ねに無い情報は推測で埋めず、先に要求する")
L.append("")
L.append(f"対象期間: 前回束ね以降（約定>{last_trade_ts or '先頭'} / FR ts>{last_funding_ts})")
L.append("")
L.append("## 約定")
L.append(f"- 新規行: {len(new_trades)}件（全行: {len(trades)}件）")
L.append(f"- 確定Sell: {len(sells)}件 / {wins}勝{losses}敗 / 確定損益 {realized}")
L.append(f"- 異常行(status警告): {len(errs)}件")
L.append("- 内訳: " + (", ".join(f"{k}x{v}" for k, v in rej.most_common()) or "なし"))
if by_sym:
    L.append("- 銘柄別確定損益: " + ", ".join(f"{k} {v}" for k, v in sorted(by_sym.items())))
for r in top:
    L.append(f"- 上位: {r.get('timestamp','')} {r.get('symbol','')} {r.get('action','')} pnl={r.get('pnl','')}")
for r in worst:
    L.append(f"- 下位: {r.get('timestamp','')} {r.get('symbol','')} {r.get('action','')} pnl={r.get('pnl','')}")
for r in errs[:5]:
    L.append(f"- 警告: {r.get('timestamp','')} {r.get('symbol','')} {r.get('action','')} status={r.get('status','')} {r.get('reason','')}")
L.append("")
L.append("## ファンディング")
L.append(f"- スナップショット: 全{len(snaps)}件 / 新規{len(fresh)}件")
L.extend(fr_lines if fr_lines else ["- データなし"])
L.append("")
L.append("## 稼働")
L.append(f"- trader heartbeat: age={hb_age}s ticks={hb.get('ticks', '?')} orders={hb.get('orders', '?')} last={hb.get('last_signal', '?')}")
L.append("")
L.append("## 較正")
if calib_hash:
    L.append(f"- calibration_latest: hash={calib_hash} 変更={'あり' if calib_changed else 'なし'} keys={','.join(calib_keys[:10])}")
else:
    L.append("- calibration_latest: 未回収")
L.append("")
L.append("## V.T採点")
vts_path = os.path.join(dest, "vt_score_weekly.json")
try:
    with open(vts_path, encoding="utf-8") as f:
        vts = json.load(f)
    t = vts.get("total", {}) or {}
    L.append(f"- pairs={t.get('pairs', '?')} wins={t.get('wins', '?')} pnl={t.get('pnl', '?')} unpaired={vts.get('unpaired_entries', '?')}")
    for name, section in (("pattern", vts.get("by_pattern", {}) or {}),
                          ("regime", vts.get("by_regime", {}) or {}),
                          ("exit", vts.get("by_exit_reason", {}) or {})):
        items = sorted((section or {}).items(), key=lambda kv: kv[1].get("pnl", 0))
        if items:
            L.append("- " + name + ": " + ", ".join(
                f"{k} {v.get('pairs', 0)}戦{v.get('wins', 0)}勝{v.get('pnl', 0)}" for k, v in items))
except (OSError, ValueError) as e:
    L.append(f"- vt_score_weekly.json 未回収 ({e})")
# V.T記録の直接検査（qty:0 regressions と未対出口の検出）
import glob as _glob
vt_rows, vt_entries, vt_exits, vt_zeroqty = 0, 0, 0, 0
for pat in ("vt_signals_*.jsonl",):
    for path in _glob.glob(os.path.join(dest, pat)):
        try:
            with open(path, encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        r = json.loads(line)
                    except ValueError:
                        continue
                    vt_rows += 1
                    if r.get("pattern") == "exit":
                        vt_exits += 1
                    elif (r.get("type") or "") in ("buy", "sell"):
                        vt_entries += 1
                        try:
                            if float(r.get("qty", 0) or 0) <= 0:
                                vt_zeroqty += 1
                        except (TypeError, ValueError):
                            vt_zeroqty += 1
        except OSError:
            continue
L.append(f"- 記録行: {vt_rows}件（入口{vt_entries}/出口{vt_exits}）qty:0入口={vt_zeroqty}件")
if vt_zeroqty:
    L.append("- 警告: qty:0 の入口あり（sizing 回帰の疑い。要調査）")
L.append("")
L.append(f"注意: 新規約定{len(new_trades)}件。10件未満の週は調整週とせず観測継続が既定。")

with open(os.path.join(dest, "BUNDLE.md"), "w", encoding="utf-8") as f:
    f.write("\n".join(L) + "\n")

if advance:
    max_trade_ts = max([r.get("timestamp", "") or "" for r in trades] + [last_trade_ts])
    max_funding_ts = max([int(s.get("ts", 0) or 0) for s in snaps] + [last_funding_ts])
    with open(state_path, "w", encoding="utf-8") as f:
        json.dump({"last_trade_ts": max_trade_ts, "last_funding_ts": max_funding_ts,
                   "calib_hash": calib_hash}, f, ensure_ascii=False, indent=2)
    print(f"state advanced: trade>{max_trade_ts} funding>{max_funding_ts}")
else:
    print("state kept (advance with --advance after review)")
print(f"wrote {os.path.join(dest, 'BUNDLE.md')}")
PYEOF

echo "==> [3/3] 完了。BUNDLE.md をチャットに貼ってください（初回は --advance なし推奨）"
