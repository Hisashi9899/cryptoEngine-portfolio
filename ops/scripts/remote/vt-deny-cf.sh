#!/usr/bin/env bash
# CF専用denyによるパターン除外の検証（読み取り専用・発注経路なし）。
#
#   bash ops/scripts/remote/vt-deny-cf.sh
#
# 目的: 直近3窓（10/31・11/01・11/08 のCF）で「一貫して負けている」
#   入口パターンだけを CF 専用の deny ファイルに書き、その次の窓
#   （11/08..11/20）を out-of-sample として再リプレイする。
#   live が読む vt_static_deny.json には一切触れない。
#
# 安全設計:
#   - vt-replay は発注経路を持たない。独立一時コンテナ（1CPU/192MB）。
#   - 出力は <trade_logs>/cf-deny/ と <trade_logs>/cf-deny-test/ のみ。
#   - deny ファイルは <trade_logs>/vt_cf_deny.json（live とは別ファイル）。
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LOG_DIR="${VT_DENY_LOG_DIR:-$REPO_ROOT/backend/core/trade_logs}"
DENY_JSON="${VT_DENY_JSON:-$LOG_DIR/vt_cf_deny.json}"
STATIC_DENY="${VT_DENY_STATIC:-$LOG_DIR/vt_static_deny.json}"
OOS_FROM="${VT_DENY_OOS_FROM:-20261108}"
OOS_TO="${VT_DENY_OOS_TO:-20261120}"
INS_RUNS="${VT_DENY_INS_RUNS:-20261031_093346 20261101_043002 20261108_043002}"
PAYOFF_TSV="${VT_DENY_PAYOFF_TSV:-$REPO_ROOT/ops/logs/vt-payoff.tsv}"

log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

[ -s "$PAYOFF_TSV" ] || { log "ERROR: payoff tsv not found: $PAYOFF_TSV"; exit 1; }

# 1) 3窓すべてで負けている (direction, pattern) を抽出して deny JSON を作る。
#    live が使う static deny は必ず union する（検証は「static + 追加deny」の
#    差分だけを見る。static を落とすと比較が汚染される）。
python3 - "$PAYOFF_TSV" "$DENY_JSON" "$STATIC_DENY" $INS_RUNS <<'PY'
import csv, json, os, sys
tsv, out, static_path = sys.argv[1], sys.argv[2], sys.argv[3]
runs = sys.argv[4:]
rows = list(csv.DictReader(open(tsv), delimiter="\t"))
d = {}
for r in rows:
    if r["run_id"] not in runs or r["k"] not in ("0", "1.0") or r["pattern"] == "TOTAL":
        continue
    key = (r["direction"], r["pattern"])
    e = d.setdefault(key, {"n": 0, "net": 0.0, "per": {}})
    e["n"] += int(r["n"]); e["net"] += float(r["net"])
    p = e["per"].setdefault(r["run_id"], [0, 0.0])
    p[0] += int(r["n"]); p[1] += float(r["net"])
blocked = {"long": [], "short": []}
for (dirn, pat), e in sorted(d.items()):
    if e["n"] < 3:
        continue
    nets = [e["per"].get(rid, [0, None])[1] for rid in runs]
    if all(v is not None and v < 0 for v in nets):
        blocked[dirn].append(pat)

# static deny（live が常用）を必ず先頭に union する。重複は除く。
static = {"long": [], "short": []}
if static_path and os.path.exists(static_path):
    try:
        sdoc = json.load(open(static_path))
    except Exception as exc:
        print("[deny] ERROR: cannot parse static deny %s: %s" % (static_path, exc))
        sys.exit(2)
    static["long"] = [str(x).strip() for x in (sdoc.get("blocked_long") or []) if str(x).strip()]
    static["short"] = [str(x).strip() for x in (sdoc.get("blocked_short") or []) if str(x).strip()]
else:
    print("[deny] WARN: static deny not found (%s); result is not comparable" % static_path)

merged = {}
for side in ("long", "short"):
    seen = set()
    merged[side] = []
    for pat in static[side] + blocked[side]:
        if pat not in seen:
            seen.add(pat)
            merged[side].append(pat)

doc = {
    "date": "20261120",
    "days": 14,
    "bars_sec": 300,
    "min_pairs": 5,
    "blocked_long": merged["long"],
    "blocked_short": merged["short"],
    "allowed": [],
}
json.dump(doc, open(out, "w"), indent=2)
print("[deny] wrote %s" % out)
print("[deny] static_long : %s" % ", ".join(static["long"]))
print("[deny] static_short: %s" % ", ".join(static["short"]))
print("[deny] added_long  : %s" % ", ".join(blocked["long"]))
print("[deny] added_short : %s" % ", ".join(blocked["short"]))
print("[deny] merged_long : %s" % ", ".join(merged["long"]))
print("[deny] merged_short: %s" % ", ".join(merged["short"]))
PY

if [ ! -s "$DENY_JSON" ]; then
  log "ERROR: deny json not created"
  exit 1
fi

# 2) 対照（static deny のみ）と 3) 検証（CF deny 追加）を同じ窓で回す。
run_cf() {
  local outroot="$1" summary="$2" deny="$3" ks="$4"
  env VT_CF_FROM="$OOS_FROM" VT_CF_TO="$OOS_TO" \
      VT_CF_OUT_ROOT="$LOG_DIR/$outroot" \
      VT_CF_SUMMARY="$REPO_ROOT/ops/logs/$summary" \
      VT_CF_DENY_FILE="$deny" \
      VT_CF_KS="$ks" \
      VT_CF_SYMBOLS="BCH LINK LTC" \
    bash "$REPO_ROOT/ops/scripts/remote/vt-counterfactual.sh"
}

RUN_CONTROL="${VT_DENY_RUN_CONTROL:-1}"

if [ "$RUN_CONTROL" = "1" ]; then
  log "control start (static deny only) $OOS_FROM..$OOS_TO"
  run_cf cf-deny vt-counterfactual-deny.tsv /trade_logs/vt_static_deny.json "0 1.0" \
    || log "WARN: control run failed"
else
  log "control skipped (VT_DENY_RUN_CONTROL=0); reusing existing cf-deny run"
fi

log "test start (cf deny) $OOS_FROM..$OOS_TO"
run_cf cf-deny-test vt-counterfactual-deny-test.tsv /trade_logs/vt_cf_deny.json "0 1.0" \
  || log "WARN: test run failed"

echo
echo "=== deny test $OOS_FROM..$OOS_TO ==="
for f in vt-counterfactual-deny.tsv vt-counterfactual-deny-test.tsv; do
  echo "-- $f --"
  awk -F'\t' -v from="$OOS_FROM" -v to="$OOS_TO" \
    'NR>1 && $2==from && $3==to {printf "  %-4s k%-4s pairs=%-3s wins=%-3s pf=%-9.3f net=%s\n",$5,$6,$7,$8,$9,$12}' \
    "$REPO_ROOT/ops/logs/$f"
done
log "deny-cf end"
