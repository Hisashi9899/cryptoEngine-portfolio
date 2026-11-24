"""FR収集スクリプト（VPS cron用・標準libのみ・DB不使用）。

使い方（5分毎）:
  */5 * * * * /usr/bin/python3 /opt/cryptoengine/ops/scripts/funding_collect.py --exchange binance --symbols BTCUSDT,ETHUSDT,SOLUSDT >> /tmp/funding_collect.log 2>&1

既存の検証・取引ループとは独立。失敗しても exit 0（cron騒音防止）。
保存先は backend/api/funding_store.py の規約に従う。
"""

import argparse
import json
import os
import sys
import time
import urllib.parse as _parse
import urllib.request as _req

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "backend", "api"))

try:
    import funding_store as _store
except ImportError:
    _store = None

BINANCE_FAPI = "https://fapi.binance.com"
BYBIT_API = "https://api.bybit.com"


def _get_json(url: str, timeout: int = 15) -> object:
    req = _req.Request(url, headers={"User-Agent": "cryptoEngine-funding-collect"})
    with _req.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def _collect_binance(symbols: list) -> list:
    rows = []
    for s in symbols:
        try:
            fr = _get_json(f"{BINANCE_FAPI}/fapi/v1/fundingRate?symbol={_parse.quote(s)}&limit=1")
            rate = float((fr[-1] or {}).get("fundingRate", 0)) if isinstance(fr, list) and fr else 0.0
            rows.append({"exchange": "binance", "symbol": s, "funding_rate": rate})
        except Exception as e:  # 1銘柄失敗で全体を止めない
            print(f"warn: binance {s} failed: {e}", flush=True)
        time.sleep(0.3)  # レート制限配慮の順次取得
    return rows


def _collect_bybit(symbols: list) -> list:
    rows = []
    for s in symbols:
        try:
            url = f"{BYBIT_API}/v5/market/tickers?category=linear&symbol={_parse.quote(s)}"
            data = _get_json(url)
            lst = ((data or {}).get("result") or {}).get("list") or []
            rate = float((lst[0] or {}).get("fundingRate", 0)) if lst else 0.0
            rows.append({"exchange": "bybit", "symbol": s, "funding_rate": rate})
        except Exception as e:
            print(f"warn: bybit {s} failed: {e}", flush=True)
        time.sleep(0.3)
    return rows


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--exchange", default="binance", choices=["binance", "bybit"])
    ap.add_argument("--symbols", default="BTCUSDT,ETHUSDT,SOLUSDT,XRPUSDT,DOGEUSDT")
    args = ap.parse_args()
    symbols = [s.strip().upper() for s in args.symbols.split(",") if s.strip()][:20]
    if not symbols:
        print("no symbols", flush=True)
        return 0
    if args.exchange == "bybit":
        rows = _collect_bybit(symbols)
    else:
        rows = _collect_binance(symbols)
    if _store is None:
        print(json.dumps({"ts": int(time.time() * 1000), "exchange": args.exchange, "rates": rows}))
        return 0
    path = _store.append_snapshot(args.exchange, rows)
    print(f"ok: {len(rows)} rates -> {path}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
