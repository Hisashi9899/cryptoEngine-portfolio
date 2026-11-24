#!/usr/bin/env bash
# ダッシュボード実データ連携のVPS検証（VPS上で実行）
#   bash ops/deploy/verify-dashboard.sh
# チェック: /health, login, balance, trades, trade-pairs, positions, signals, frontend
set -uo pipefail

APP_DIR="/opt/cryptoengine"
ENV_PROD="$APP_DIR/infra/config/.env.prod"
BASE="http://localhost:8000"
FAIL=0

echo "==> [0] API health待ち (最大60秒)"
for i in $(seq 1 12); do
  if curl -sf "$BASE/health" >/dev/null 2>&1; then
    echo "API healthy"
    break
  fi
  if [ "$i" = "12" ]; then
    echo "FAIL: APIが起動しません" >&2
    exit 1
  fi
  sleep 5
done
curl -s "$BASE/health"; echo

ADMIN_USER="$(grep -E '^AUTH_ADMIN_USERNAME=' "$ENV_PROD" | cut -d= -f2-)"
ADMIN_PASS="$(grep -E '^AUTH_ADMIN_PASSWORD=' "$ENV_PROD" | cut -d= -f2-)"
: "${ADMIN_USER:=admin}"
if [ -z "$ADMIN_PASS" ]; then
  echo "FAIL: AUTH_ADMIN_PASSWORDが空です" >&2
  exit 1
fi

echo "==> [1] login"
LOGIN_JSON="$(curl -s -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")"
TOKEN="$(echo "$LOGIN_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))' 2>/dev/null)"
if [ -z "$TOKEN" ]; then
  echo "FAIL: login失敗: $LOGIN_JSON" >&2
  exit 1
fi
echo "login OK (user=$ADMIN_USER)"

check() {
  local name="$1" path="$2" py="$3"
  echo "==> $name: $path"
  BODY="$(curl -s -H "Authorization: Bearer $TOKEN" "$BASE$path")"
  echo "$BODY" | head -c 600; echo
  if echo "$BODY" | python3 -c "$py" 2>&1; then
    echo "PASS: $name"
  else
    echo "FAIL: $name" >&2
    FAIL=1
  fi
}

check "balance" "/api/v1/balance" "
import json,sys
d=json.load(sys.stdin)
assert d['initial_capital']==1000000.0, d
assert d['total_assets']>0, d
print('total=%s avail=%s invested=%s pnl=%s hist=%s' % (d['total_assets'], d['available'], d['invested'], d['profit_loss'], len(d['history'])))
"

check "positions" "/api/v1/positions" "
import json,sys
d=json.load(sys.stdin)
ps=d.get('positions',[])
ok=all(float(p.get('size',0) or p.get('volume',0) or 0)>0 for p in ps)
assert ok, 'zero position leaked'
print('positions=%d unrealized=%s' % (len(ps), d.get('total_unrealized_pnl')))
"

check "trades" "/api/v1/trades?limit=50" "
import json,sys
d=json.load(sys.stdin)
assert 'trades' in d, d
print('trades=%d' % len(d['trades']))
"

check "trade-pairs" "/api/v1/trade-pairs?limit=5" "
import json,sys
d=json.load(sys.stdin)
assert 'trade_pairs' in d, d
print('trade_pairs=%d' % len(d['trade_pairs']))
"

check "signals" "/api/v1/signals?limit=5" "
import json,sys
d=json.load(sys.stdin)
assert 'signals' in d and len(d['signals'])>0, 'signals empty'
print('signals=%d' % len(d['signals']))
"

check "gmo-balance" "/api/v1/gmo/balance" "
import json,sys
d=json.load(sys.stdin)
assert 'status' in d, d
print('gmo=%s' % str(d)[:300])
"

check "gmo-affordability" "/api/v1/auto-trade/affordability" "
import json,sys
d=json.load(sys.stdin)
assert 'status' in d, d
print('afford=%s' % str(d)[:300])
"

check "orderbook-binance" "/api/v1/orderbook?symbol=BTCUSDT&limit=5" "
import json,sys
d=json.load(sys.stdin)
ob=d.get('order_book',{})
assert len(ob.get('bids',[]))>0 and len(ob.get('asks',[]))>0, d
print('binance mid=%s spread=%s' % (ob.get('mid_price'), ob.get('spread')))
"

check "orderbook-gmo" "/api/v1/orderbook?symbol=BTC&limit=5" "
import json,sys
d=json.load(sys.stdin)
ob=d.get('order_book',{})
assert len(ob.get('bids',[]))>0 and len(ob.get('asks',[]))>0, d
print('gmo BTC mid=%s spread=%s' % (ob.get('mid_price'), ob.get('spread')))
"

check "ticker-gmo" "/api/v1/ticker?symbol=BTC" "
import json,sys
d=json.load(sys.stdin)
tk=d.get('ticker',{})
assert tk.get('last',0)>0, d
print('ticker BTC last=%s' % tk.get('last'))
"

check "market-weather" "/api/v1/market-weather" "
import json,sys
d=json.load(sys.stdin)
# 天気は退役 trader(v2) の産物のため stale が正常値。vtlive の判定には
# 使わない表示専用データ。stale でも FAIL にせず警告のみ。
if d.get('status')!='running':
    print('WARN: weather stale (retired producer): %s' % str(d)[:120])
else:
    assert d.get('count',0)>=10, d
    print('weather count=%d approaching=%s' % (d.get('count'), d.get('approaching')))
"

check "system" "/api/v1/system" "
import json,sys
d=json.load(sys.stdin)
n=len(d.get('docker',{}).get('containers',[]))
assert n>=5, 'containers should be 5, got %d' % n
assert d.get('core',{}).get('status')=='running', d
print('containers=%d core=running (trader retired 2026-09-26; vtlive checked below)' % (n))
"

echo "==> vtlive heartbeat (trader retired; live execution health)"
python3 -c "
import json,sys,time
p='$APP_DIR/backend/core/trade_logs/vt_live_heartbeat.json'
try:
    hb=json.load(open(p))
except Exception as e:
    print('FAIL: vtlive heartbeat unreadable: %s' % e); sys.exit(1)
age=time.time()*1000-hb.get('ts',0)
assert 0<=age<=300000, 'heartbeat stale: age %.0fs last=%s' % (age/1000, hb)
print('vtlive mode=%s opens=%d daily=%s' % (hb.get('mode'), len(hb.get('opens',{}) or {}), hb.get('daily_pnl')))
"

check "auto-trade" "/api/v1/auto-trade" "
import json,sys
d=json.load(sys.stdin)
assert 'enabled' in d and 'risk' in d and 'trade_log' in d, d
print('auto enabled=%s log=%d' % (d.get('enabled'), len(d.get('trade_log',[]))))
"

echo "==> [6] frontend"
FCODE="$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/login || true)"
echo "frontend /login http_code=$FCODE"
if [ "$FCODE" != "200" ]; then
  echo "WARN: frontendが200以外 (=$FCODE)" >&2
fi

echo "==> [7] containers"
docker ps --format "{{.Names}} {{.Status}}" | cat

if [ "$FAIL" != "0" ]; then
  echo "VERIFY FAILED" >&2
  exit 1
fi
echo "VERIFY OK: 残高0円バグ解消・実データ連携を確認"
