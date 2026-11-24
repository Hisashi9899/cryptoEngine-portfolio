#!/usr/bin/env python3
"""cryptoEngine 監視（VPS 上で cron 実行）。
秘密値は表示しない。アラートは SMTP 設定がある場合のみ送信する。

  python3 ops/deploy/watchdog.py [--compose-dir DIR] [--log-dir DIR]

監視項目（v1/v2/v3 退役後は V.T live が対象）:
  1) vtlive ハートビート鮮度（vt_live_heartbeat.json、しきい値 300 秒）
  2) vtlive コンテナ稼働（cryptoengine-vtlive-1）
  3) 本日確定損益（trade_log.csv の SELL 行、UTC 日付）
  4) api /health 応答
  5) GMO 定期メンテ時間帯（土曜 09:00-11:00 JST。取引停止の旨を通知）
  6) 注文エラー多発（直近60分の error 行が5件以上。ロジック異常の検出）

約定通知は送らない。約定は GMO 側のメールに一本化し、二重通知と
重要メール（申込等）の見逃しを防ぐ。約定の検出自体はログに残す
（監査用）。複数 VPS 運用時は件名・本文にホスト名が入る。

SMTP 設定（infra/docker/.env に記載、未設定ならログのみ）:
  ALERT_SMTP_HOST / ALERT_SMTP_PORT(587) / ALERT_SMTP_USER /
  ALERT_SMTP_PASS / ALERT_SMTP_FROM / ALERT_TO
"""
import csv
import json
import os
import smtplib
import socket
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timezone
from email.mime.text import MIMEText

HEARTBEAT_MAX_AGE_SEC = 300
ALERT_MIN_INTERVAL_SEC = 3600


def log(msg):
    print(f"[watchdog] {datetime.now(timezone.utc).isoformat()} {msg}", flush=True)


def load_dotenv(path):
    """KEY=VALUE を読む（値は返さない。呼び出し側も表示禁止）。"""
    out = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                k, v = line.split("=", 1)
                out[k.strip()] = v.strip().strip('"').strip("'")
    except OSError:
        pass
    return out


def load_state(path):
    try:
        with open(path) as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def save_state(path, state):
    try:
        with open(path, "w") as f:
            json.dump(state, f)
    except OSError as e:
        log(f"state save failed: {e}")


def check_heartbeat(log_dir):
    """(ok, detail) を返す。V.T live のハートビートを見る。"""
    p = os.path.join(log_dir, "vt_live_heartbeat.json")
    try:
        with open(p) as f:
            hb = json.load(f)
    except (OSError, ValueError) as e:
        return False, f"vtlive heartbeat unreadable: {e}"
    age = time.time() * 1000 - hb.get("ts", 0)
    if age < 0 or age > HEARTBEAT_MAX_AGE_SEC * 1000:
        return False, f"vtlive heartbeat stale: age {age/1000:.0f}s, last={hb}"
    opens = hb.get("opens", {}) or {}
    return True, f"mode={hb.get('mode')} opens={len(opens)} daily={hb.get('daily_pnl')}"


def check_container():
    try:
        out = subprocess.run(
            ["docker", "inspect", "-f", "{{.State.Running}}", "cryptoengine-vtlive-1"],
            capture_output=True, text=True, timeout=20,
        )
    except (OSError, subprocess.SubprocessError) as e:
        return False, f"docker inspect failed: {e}"
    running = out.stdout.strip() == "true"
    return running, "running" if running else "NOT running"


def daily_pnl(log_dir):
    """今日（UTC）の SELL 確定損益合計。読めなければ None。"""
    p = os.path.join(log_dir, "trade_log.csv")
    today = datetime.now(timezone.utc).date().isoformat()
    total = 0.0
    found = False
    try:
        with open(p, newline="") as f:
            for row in csv.DictReader(f):
                if row.get("action") != "SELL" or not row.get("pnl"):
                    continue
                try:
                    ts = datetime.fromisoformat(row.get("timestamp", ""))
                    if ts.tzinfo is None:
                        ts = ts.replace(tzinfo=timezone.utc)
                    if ts.astimezone(timezone.utc).date().isoformat() == today:
                        total += float(row["pnl"])
                        found = True
                except (ValueError, TypeError):
                    continue
    except OSError:
        return None
    return round(total, 2) if found else 0.0


def check_api():
    try:
        with urllib.request.urlopen("http://localhost:8000/health", timeout=10) as r:
            body = r.read().decode()
        ok = '"ok"' in body or "'ok'" in body or "ok" in body
        return ok, body[:80]
    except Exception as e:  # noqa: BLE001
        return False, f"api health failed: {e}"


def in_maint_window(now_utc=None):
    """GMO 定期メンテ時間帯（土曜 09:00-11:00 JST）かを返す。"""
    from datetime import timedelta
    now = now_utc or datetime.now(timezone.utc)
    jst = now + timedelta(hours=9)
    return jst.weekday() == 5 and 9 <= jst.hour < 11


def host_label():
    """複数 VPS 識別用のホスト名（取れなければ unknown）。"""
    try:
        return socket.gethostname()
    except OSError:
        return "unknown"


def count_recent_errors(log_dir, now_utc=None, window_sec=3600, limit=5000):
    """直近 window_sec の trade_log.csv の error 行数を数える。

    注文失敗の連続はロジック・API 異常の兆候。ファイル末尾の
    limit 行だけ見て負荷を抑える。読めなければ 0。
    """
    now = now_utc or datetime.now(timezone.utc)
    cutoff = now.timestamp() - window_sec
    try:
        with open(os.path.join(log_dir, "trade_log.csv"), newline="") as f:
            rows = list(csv.DictReader(f))[-limit:]
    except OSError:
        return 0
    n = 0
    for row in rows:
        if (row.get("status") or "").strip().lower() != "error":
            continue
        try:
            ts = datetime.fromisoformat(row.get("timestamp", ""))
            if ts.tzinfo is None:
                ts = ts.replace(tzinfo=timezone.utc)
            if ts.timestamp() >= cutoff:
                n += 1
        except (ValueError, TypeError):
            continue
    return n


def new_fills(log_dir, seen):
    """trade_log.csv の filled 行のうち、seen に無い新規約定を返す。

    seen はキー文字列の集合。キーは timestamp|action|symbol|price|order_id。
    filled 以外（rejected/error 等）は対象外。読めなければ空リスト。
    """
    p = os.path.join(log_dir, "trade_log.csv")
    fresh = []
    try:
        with open(p, newline="") as f:
            for row in csv.DictReader(f):
                if (row.get("status") or "").strip().lower() != "filled":
                    continue
                key = "|".join([
                    (row.get("timestamp") or "").strip(),
                    (row.get("action") or "").strip().upper(),
                    (row.get("symbol") or "").strip().upper(),
                    (row.get("price") or "").strip(),
                    (row.get("order_id") or "").strip(),
                ])
                if not key.strip("|") or key in seen:
                    continue
                fresh.append((key, row))
    except OSError:
        return []
    return fresh


def fmt_fill(row):
    """約定1行の通知用短文。"""
    ts = (row.get("timestamp") or "").strip()
    side = (row.get("action") or "").strip().upper()
    sym = (row.get("symbol") or "").strip().upper()
    price = (row.get("price") or "").strip()
    amt = (row.get("amount") or "").strip()
    pnl = (row.get("pnl") or "").strip()
    oid = (row.get("order_id") or "").strip()
    extra = f" pnl={pnl}" if pnl else ""
    return f"{ts} {side} {sym} price={price} amount={amt}{extra} order={oid or '-'}"


def send_mail(cfg, subject, body):
    """SMTP 送信。設定不足なら False（ログのみ）。"""
    needed = ["ALERT_SMTP_HOST", "ALERT_SMTP_USER", "ALERT_SMTP_PASS", "ALERT_TO"]
    if any(not cfg.get(k) for k in needed):
        log("smtp not configured; alert logged only")
        return False
    msg = MIMEText(body, "plain", "utf-8")
    msg["Subject"] = subject
    msg["From"] = cfg.get("ALERT_SMTP_FROM") or cfg["ALERT_SMTP_USER"]
    msg["To"] = cfg["ALERT_TO"]
    try:
        with smtplib.SMTP(cfg["ALERT_SMTP_HOST"], int(cfg.get("ALERT_SMTP_PORT", "587")), timeout=30) as s:
            s.starttls()
            s.login(cfg["ALERT_SMTP_USER"], cfg["ALERT_SMTP_PASS"])
            s.send_message(msg)
        return True
    except Exception as e:  # noqa: BLE001
        log(f"smtp send failed: {type(e).__name__}")
        return False


def main():
    compose_dir = None
    log_dir = None
    args = sys.argv[1:]
    if "--test-alert" in args:
        env = load_dotenv(os.path.join(
            os.path.dirname(os.path.abspath(__file__)), "../../infra/docker/.env"))
        ok = send_mail(env, "[cryptoEngine] テスト通知",
                       "cryptoEngine 監視のテスト通知です。\nこのメールが届けば異常通知は正常に動作します。")
        print("TEST_ALERT_SENT" if ok else "TEST_ALERT_FAILED")
        return 0 if ok else 1
    for i, a in enumerate(args):
        if a == "--compose-dir" and i + 1 < len(args):
            compose_dir = args[i + 1]
        if a == "--log-dir" and i + 1 < len(args):
            log_dir = args[i + 1]
    if compose_dir is None:
        compose_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../infra/docker")
    if log_dir is None:
        log_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../backend/core/trade_logs")
    os.chdir(compose_dir)

    env = load_dotenv(os.path.join(compose_dir, ".env"))
    max_loss = float(env.get("MAX_DAILY_LOSS_USDT", "2000") or 2000)

    problems = []
    ok, detail = check_heartbeat(log_dir)
    log(f"heartbeat: {'OK' if ok else 'NG'} ({detail})")
    if not ok:
        problems.append(f"vtlive ハートビート異常: {detail}")
    ok, detail = check_container()
    log(f"container: {'OK' if ok else 'NG'} ({detail})")
    if not ok:
        problems.append(f"vtlive コンテナ停止中: {detail}")
    pnl = daily_pnl(log_dir)
    log(f"daily pnl (UTC): {pnl}")
    if pnl is not None and pnl <= -max_loss:
        problems.append(f"本日確定損益 {pnl} が日次リミット {-max_loss} に到達")
    ok, detail = check_api()
    log(f"api: {'OK' if ok else 'NG'} ({detail})")
    if not ok:
        problems.append(f"api 応答異常: {detail}")
    if in_maint_window():
        problems.append("GMO定期メンテ時間帯（土曜09:00-11:00 JST）: 発注停止中")
    nerr = count_recent_errors(log_dir)
    log(f"order errors (60min): {nerr}")
    if nerr >= 5:
        problems.append(f"注文エラー多発: 直近60分に {nerr} 件（ロジック・API異常の疑い）")

    state_path = "/tmp/crypto-watchdog.json"
    state = load_state(state_path)
    now = time.time()

    # 約定の検出はログのみ（通知しない）。約定通知は GMO 側メールに一本化し、
    # 二重通知と重要メールの見逃しを防ぐ。初回は既存分を見たことにする。
    seen = set(state.get("fills_seen", []))
    initialized = "fills_seen" in state
    fills = new_fills(log_dir, seen)
    if not initialized:
        for key, _row in fills:
            seen.add(key)
        fills = []
    if fills:
        log(f"new fills (log only): {len(fills)}")
        for _key, row in fills:
            log(f"  fill: {fmt_fill(row)}")
    for key, _row in fills:
        seen.add(key)
    state["fills_seen"] = sorted(seen)[-500:]
    if not initialized or fills:
        save_state(state_path, state)

    if not problems:
        log("all clear")
        return 0

    host = host_label()
    fresh = []
    for p in problems:
        key = p.split(":")[0]
        last = state.get(key, 0)
        if now - last >= ALERT_MIN_INTERVAL_SEC:
            fresh.append(p)
            state[key] = now
    save_state(state_path, state)

    if fresh:
        subject = f"[cryptoEngine][{host}] vtlive 異常検知"
        body = (
            f"cryptoEngine 監視アラート\nホスト: {host}\n時刻: "
            f"{datetime.now(timezone.utc).isoformat()}\n\n"
            + "\n".join(f"- {p}" for p in fresh)
        )
        log("ALERT: " + " / ".join(s.splitlines()[0] for s in fresh))
        send_mail(env, subject, body)
        return 2
    log("alerts suppressed (interval)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
