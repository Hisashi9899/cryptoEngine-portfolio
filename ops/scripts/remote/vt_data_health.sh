#!/usr/bin/env bash
# V.T データ収集の健全性チェック（読み取り専用・発注なし）。
#
#   bash ops/scripts/remote/vt_data_health.sh [--symbols="BCH LINK LTC"] [--bars=300]
#
# 背景: vtlive の heartbeat は「板スナップショットが届いた時刻」ではなく
# 「最後に tick を処理した時刻」を出すため、GMO ws が停止して同じ板を
# 5 秒ごとに再送しているだけでも tick_age は 4-5 秒のままになる。
# つまり heartbeat だけでは板の欠測を検知できない。ここでは板CSVの
# 実データ（行数・最終行の時刻）とシグナル件数を直接見る。
#
# 終了コード: 0=健全 / 1=板データ欠測（本日UTCの行なし） / 2=実行環境エラー
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LOG_DIR="${VT_HEALTH_LOG_DIR:-$REPO_ROOT/backend/core/trade_logs}"
SYMBOLS="${VT_HEALTH_SYMBOLS:-BCH LINK LTC}"
BARS="${VT_HEALTH_BARS:-300}"
# 板CSV・シグナルJSONLのファイル名はUTC日付（コンテナはUTC）。
# ホストはJSTなので、09:00 JSTより前でも正しい日付を見るようUTCで計算する。
TODAY="$(date -u +%Y%m%d)"
YDAY="$(date -u -d "yesterday" +%Y%m%d)"
NOW_MS="$(date +%s%3N)"
UTC_HHMM="$(date -u +%H%M)"

[ -d "$LOG_DIR" ] || { echo "ERROR: log dir not found: $LOG_DIR" >&2; exit 2; }

echo "=== orderbook files (today=$TODAY yesterday=$YDAY) ==="
stale=0
for sym in $SYMBOLS; do
  tf="$LOG_DIR/orderbook_${sym}_${TODAY}.csv"
  yf="$LOG_DIR/orderbook_${sym}_${YDAY}.csv"
  tsz=0
  ysz=0
  [ -f "$tf" ] && tsz="$(stat -c%s "$tf" 2>/dev/null || echo 0)"
  [ -f "$yf" ] && ysz="$(stat -c%s "$yf" 2>/dev/null || echo 0)"

  # 最終行の時刻（あれば）。1KB未満はヘッダのみ＝データなし。
  last=""
  if [ "$tsz" -gt 1024 ]; then
    ts="$(tail -1 "$tf" | cut -d, -f1)"
    if [ -n "$ts" ] && [ "$ts" -gt 0 ] 2>/dev/null; then
      age_min=$(( (NOW_MS - ts) / 60000 ))
      last="last_row_age=${age_min}m"
      if [ "$age_min" -gt 30 ]; then
        last="$last STALE"
        stale=1
      fi
    fi
  else
    # UTC 00:00 直後は当日ファイルがまだ空。最初の3分は前日データで健全とみなす。
    if [ "$((10#$UTC_HHMM))" -lt 3 ]; then
      last="grace(UTC 00:0x)"
    else
      stale=1
    fi
  fi
  printf '  %-5s today=%9sB yesterday=%9sB %s\n' "$sym" "$tsz" "$ysz" "$last"
done

echo
echo "=== signals today (bars=$BARS) ==="
total=0
for sym in $SYMBOLS; do
  f="$LOG_DIR/vt_signals_${sym}_${BARS}_${TODAY}.jsonl"
  n=0
  [ -f "$f" ] && n="$(wc -l <"$f" 2>/dev/null || echo 0)"
  total=$((total + n))
  printf '  %-5s %s\n' "$sym" "$n"
done

echo
echo "=== ws dial errors in vtlive (last 5m) ==="
if command -v docker >/dev/null 2>&1 && docker inspect cryptoengine-vtlive-1 >/dev/null 2>&1; then
  errs="$(docker logs --since 5m cryptoengine-vtlive-1 2>&1 | grep -c 'bad status' || true)"
  echo "  bad_status=$errs"
  echo "  status=$(docker inspect -f '{{.State.Status}}' cryptoengine-vtlive-1) restarts=$(docker inspect -f '{{.RestartCount}}' cryptoengine-vtlive-1)"
  hb="$LOG_DIR/vt_live_heartbeat.json"
  if [ -r "$hb" ]; then
    hbts="$(sed -n 's/.*"ts":\([0-9]*\).*/\1/p' "$hb" | head -1)"
    [ -n "$hbts" ] && echo "  heartbeat_age=$(( (NOW_MS - hbts) / 1000 ))s"
  fi
else
  echo "  (docker not available; skipped)"
fi

echo
if [ "$stale" -ne 0 ]; then
  echo "RESULT: STALE — 本日(UTC)の板データがありません。GMO ws の状態を確認してください。"
  exit 1
fi
echo "RESULT: OK"
