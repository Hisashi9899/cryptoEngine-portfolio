#!/usr/bin/env python3
"""V.T 再評価タイミングの判定とメール通知（読み取り専用・発注経路なし）。

判定条件（前回の合意）:
  1) データ準備: 直近 N 日（既定14）の各日が、対象3銘柄すべてで
     5分足200本以上ある（数KBの部分日・欠測日は1日に数えない）
  2) 相場準備:   データ14日が揃っている状態で、いずれかの銘柄の
     ATR(14)/終値 が、その銘柄の実効ゲート閾値
     k*(往復手数料+平均spread) 以上（既定 k=2.5, taker 0.05%/片道）

状態は ops/logs/vt-eval-due.json に保存し、同じ条件で毎日メールしない。
  - data_ready: 条件が崩れる（欠測が出る）と再武装し、再び 14/14 になったら通知
  - atr_ready : 7日クールダウン（局面が続く間の連投を防ぐ）

SMTP は watchdog.py と同じ infra/docker/.env の ALERT_* を使う。
秘密値は表示しない。板CSVは読むだけで、live/paper のファイルは変更しない。

使い方:
  python3 ops/scripts/remote/vt_eval_due.py --dry-run   # 判定だけ（送信・状態更新なし）
  python3 ops/scripts/remote/vt_eval_due.py             # 条件成立時のみ送信
  python3 ops/scripts/remote/vt_eval_due.py --seed      # 現在状態を記録（送信なし）
  python3 ops/scripts/remote/vt_eval_due.py --check-smtp # 接続・ログインのみ（送信なし）
  python3 ops/scripts/remote/vt_eval_due.py --test-mail # テスト送信
"""
import argparse
import gzip
import json
import os
import smtplib
import socket
import sys
from datetime import date, datetime, timedelta, timezone
from email.mime.text import MIMEText

JST = timezone(timedelta(hours=9))
ATR_MIN_BARS = 14
FULL_DAY_BARS = 200  # 5分足の1日=288本。70%未満は部分日として「完全」と扱わない。
ATR_COOLDOWN_SEC = 7 * 24 * 3600
GATE_K = 2.5
TAKER_FEE = 0.0005  # 片道


def now_jst():
    return datetime.now(JST)


def log(msg):
    print("[vt-eval-due] %s %s" % (now_jst().strftime("%Y-%m-%d %H:%M:%S"), msg), flush=True)


def load_dotenv(path):
    out = {}
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                k, v = line.split("=", 1)
                out[k.strip()] = v.strip().strip('"').strip("'")
    except OSError:
        pass
    return out


def send_mail(cfg, subject, body):
    needed = ["ALERT_SMTP_HOST", "ALERT_SMTP_USER", "ALERT_SMTP_PASS", "ALERT_TO"]
    if any(not cfg.get(k) for k in needed):
        log("smtp not configured; not sent")
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
    except Exception as exc:  # noqa: BLE001
        log("smtp send failed: %s" % type(exc).__name__)
        return False


def day_path(log_dir, symbol, day):
    base = os.path.join(log_dir, "orderbook_%s_%s" % (symbol, day))
    for ext in (".csv", ".csv.gz"):
        if os.path.isfile(base + ext):
            return base + ext
    return None


def day_bars(path):
    """板CSVの5分足スロット数を数える（確定・未確定は問わない）。"""
    opener = gzip.open if path.endswith(".gz") else open
    slots = set()
    try:
        with opener(path, "rt", encoding="utf-8", errors="replace") as f:
            for line in f:
                i = line.find(",")
                if i <= 0:
                    continue
                try:
                    ts = int(line[:i])
                except ValueError:
                    continue
                if ts <= 0:
                    continue
                slots.add(ts // 300000)
    except OSError:
        return 0
    return len(slots)


def coverage_exact(log_dir, symbols, days, cache):
    """各日が「5分足 FULL_DAY_BARS 本以上」かを厳密に判定する。

    過去日のファイルは不変なので、サイズが同じなら前回の本数を使う。
    毎日の追加読み込みは新しい1日分（3銘柄）だけで済む。
    """
    today = datetime.now(timezone.utc).date()
    per = {}
    new_cache = {}
    for sym in symbols:
        ok = 0
        for i in range(1, days + 1):
            d = (today - timedelta(days=i)).strftime("%Y%m%d")
            p = day_path(log_dir, sym, d)
            if not p:
                continue
            key = "%s:%s" % (sym, d)
            try:
                size = os.path.getsize(p)
            except OSError:
                continue
            ent = cache.get(key) or {}
            if ent.get("path") == p and ent.get("size") == size:
                bars = int(ent.get("bars", 0))
            else:
                bars = day_bars(p)
            new_cache[key] = {"path": p, "size": size, "bars": bars}
            if bars >= FULL_DAY_BARS:
                ok += 1
        per[sym] = ok
    return per, new_cache


def read_day_metrics(path):
    """板CSVから 5分足 ATR(14)/終値 と平均スプレッド率を出す（近似）。

    engine の ATRRatio と同じ定義（確定5分足の High-Low の単純平均 / 直近終値）。
    最後の1本は未確定の可能性があるため捨てる。
    """
    opener = gzip.open if path.endswith(".gz") else open
    bars = []
    cur = None
    o = h = l = c = None
    spread_sum = 0.0
    spread_n = 0
    try:
        with opener(path, "rt", encoding="utf-8", errors="replace") as f:
            for line in f:
                parts = line.split(",", 3)
                if len(parts) < 3:
                    continue
                try:
                    ts = int(parts[0])
                    mid = float(parts[1])
                    sp = float(parts[2])
                except ValueError:
                    continue
                if mid <= 0:
                    continue
                if mid > 0 and sp >= 0:
                    spread_sum += sp / mid
                    spread_n += 1
                slot = (ts // 300000) * 300000
                if cur is None or slot != cur:
                    if cur is not None:
                        bars.append((o, h, l, c))
                    cur, o, h, l, c = slot, mid, mid, mid, mid
                else:
                    if mid > h:
                        h = mid
                    if mid < l:
                        l = mid
                    c = mid
    except OSError:
        return None
    if cur is not None:
        bars.append((o, h, l, c))
    if len(bars) > 1:
        bars = bars[:-1]  # 未確定足を捨てる
    if len(bars) < ATR_MIN_BARS:
        return None
    last = bars[-1][3]
    if last <= 0:
        return None
    tr = sum(b[1] - b[2] for b in bars[-ATR_MIN_BARS:]) / float(ATR_MIN_BARS)
    avg_spread = spread_sum / spread_n if spread_n else 0.0
    return {"atr_ratio": tr / last, "avg_spread": avg_spread,
            "bars": len(bars), "last": last}


def newest_complete_day(log_dir, symbols):
    """3銘柄すべてにデータがある最新のUTC日を返す（なければ None）。"""
    today = datetime.now(timezone.utc).date()
    for i in range(1, 31):
        d = (today - timedelta(days=i)).strftime("%Y%m%d")
        if all((p := day_path(log_dir, s, d)) and os.path.getsize(p) > 1024 for s in symbols):
            return d
    return None


def load_state(path):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def save_state(path, state):
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(state, f, ensure_ascii=False, indent=2)
    os.replace(tmp, path)


def latest_cf_rows(tsv, limit=12):
    """直近の反実仮想run（最後のrun_idグループ）を返す。"""
    try:
        with open(tsv, encoding="utf-8") as f:
            lines = [ln.rstrip("\n") for ln in f if ln.strip()]
    except OSError:
        return []
    if len(lines) < 2:
        return lines[-limit:]
    last_id = lines[-1].split("\t", 1)[0]
    rows = [ln for ln in lines[1:] if ln.split("\t", 1)[0] == last_id]
    return rows[-limit:]


def build_body(host, cov, days, atr, atr_day, threshold, reasons, cf_rows, need, full_days):
    lines = []
    lines.append("V.T 再評価の判定条件が成立しました。")
    lines.append("")
    lines.append("ホスト: %s" % host)
    lines.append("判定時刻: %s JST" % now_jst().strftime("%Y-%m-%d %H:%M"))
    lines.append("成立: %s" % " / ".join(reasons))
    lines.append("")
    lines.append("[データ被覆] 直近%d日（昨日まで）" % days)
    for sym in sorted(cov):
        mark = "完全" if full_days.get(sym) else "不完全"
        lines.append("  %-5s %d/%d  昨日=%s" % (sym, cov[sym], days, mark))
    lines.append("")
    lines.append("[ボラティリティ] %s の5分足 ATR(14)/終値" % (atr_day or "-"))
    if atr:
        for sym in sorted(atr):
            m = atr[sym]
            if not m:
                lines.append("  %-5s データ不足" % sym)
                continue
            need_sym = need.get(sym, 0.0)
            hit = "到達" if m["atr_ratio"] >= need_sym else "未達"
            note = "" if m["bars"] >= FULL_DAY_BARS else "  ※部分日(%d本)" % m["bars"]
            lines.append("  %-5s ATR=%.2f%%  平均spread=%.3f%%  必要=%.2f%%  %s%s"
                         % (sym, m["atr_ratio"] * 100, m["avg_spread"] * 100,
                            need_sym * 100, hit, note))
    else:
        lines.append("  （板データ不足）")
    if threshold > 0:
        lines.append("  手動しきい値: %.2f%%" % (threshold * 100))
    lines.append("")
    if cf_rows:
        lines.append("[直近の反実仮想]")
        for r in cf_rows:
            lines.append("  " + r)
        lines.append("")
    lines.append("[次の操作]")
    lines.append("  1) bash ops/scripts/remote/vt_data_health.sh")
    lines.append("  2) bash ops/scripts/remote/vt-counterfactual.sh")
    lines.append("  3) 結果を確認し、ゲート・銘柄・サイジングを再評価")
    lines.append("")
    lines.append("このメールは条件成立時に1回だけ送信されます（欠測で再武装）。")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--log-dir", default="")
    ap.add_argument("--days", type=int, default=14)
    ap.add_argument("--symbols", default="BCH LINK LTC")
    ap.add_argument("--atr-threshold", type=float, default=0.0,
                    help="0=銘柄ごとの実効ゲート閾値。>0 で一律上書き（例 0.009）")
    ap.add_argument("--env-file", default="")
    ap.add_argument("--state", default="")
    ap.add_argument("--cf-tsv", default="")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--seed", action="store_true")
    ap.add_argument("--test-mail", action="store_true")
    ap.add_argument("--check-smtp", action="store_true",
                    help="SMTPへ接続・ログインだけ行う（メールは送らない）")
    args = ap.parse_args()

    repo = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../.."))
    log_dir = args.log_dir or os.path.join(repo, "backend/core/trade_logs")
    env_file = args.env_file or os.path.join(repo, "infra/docker/.env")
    state_path = args.state or os.path.join(repo, "ops/logs/vt-eval-due.json")
    cf_tsv = args.cf_tsv or os.path.join(repo, "ops/logs/vt-counterfactual.tsv")
    symbols = [s for s in args.symbols.replace(",", " ").split() if s]

    cfg = load_dotenv(env_file)

    if args.check_smtp:
        # 接続とログインのみ。メールは送らない（到達性の確認）。
        needed = ["ALERT_SMTP_HOST", "ALERT_SMTP_USER", "ALERT_SMTP_PASS", "ALERT_TO"]
        missing = [k for k in needed if not cfg.get(k)]
        if missing:
            print("SMTP_UNCONFIGURED missing=%s" % ",".join(missing))
            return 2
        try:
            with smtplib.SMTP(cfg["ALERT_SMTP_HOST"],
                              int(cfg.get("ALERT_SMTP_PORT", "587")),
                              timeout=30) as s:
                s.ehlo()
                s.starttls()
                s.ehlo()
                s.login(cfg["ALERT_SMTP_USER"], cfg["ALERT_SMTP_PASS"])
            print("SMTP_OK host=%s port=%s to=%s (no mail sent)"
                  % (cfg["ALERT_SMTP_HOST"], cfg.get("ALERT_SMTP_PORT", "587"), cfg["ALERT_TO"]))
            return 0
        except Exception as exc:  # noqa: BLE001
            print("SMTP_FAILED %s" % type(exc).__name__)
            return 1

    if args.test_mail:
        ok = send_mail(cfg, "[cryptoEngine] V.T 再評価通知のテスト",
                       "V.T 再評価タイミング通知のテストです。\n"
                       "このメールが届けば、条件成立時の通知は正常に動作します。")
        print("TEST_MAIL_SENT" if ok else "TEST_MAIL_FAILED")
        return 0 if ok else 1

    if not os.path.isdir(log_dir):
        log("ERROR: log dir not found: %s" % log_dir)
        return 2

    state = load_state(state_path)
    prev_data = bool(state.get("data_ready"))
    last_atr = state.get("atr_notified_at", 0)
    try:
        last_atr = int(last_atr)
    except (TypeError, ValueError):
        last_atr = 0
    now_ts = int(now_jst().timestamp())

    # 被覆は「5分足200本以上の日」だけを数える（数KBの部分日を1日と誤認しない）。
    cov, day_cache = coverage_exact(log_dir, symbols, args.days,
                                    state.get("days") or {})
    data_ready = all(cov.get(s, 0) >= args.days for s in symbols)

    yday = (datetime.now(timezone.utc).date() - timedelta(days=1)).strftime("%Y%m%d")
    atr = {}
    full_days = {}
    for sym in symbols:
        p = day_path(log_dir, sym, yday)
        atr[sym] = read_day_metrics(p) if p else None
        ent = day_cache.get("%s:%s" % (sym, yday)) or {}
        full_days[sym] = int(ent.get("bars", 0)) >= FULL_DAY_BARS

    atr_day = yday
    if not all(atr.values()):
        # ATR は最新の完全日で代替表示する（atr_ready は false のまま）。
        atr_day = newest_complete_day(log_dir, symbols)
        if atr_day and atr_day != yday:
            for sym in symbols:
                p = day_path(log_dir, sym, atr_day)
                atr[sym] = read_day_metrics(p) if p else None

    # 実効ゲート閾値は銘柄ごとに異なる: ATR >= k*(往復手数料 + 平均spread)。
    # 昨日が完全日でない場合のATRは判定に使わない（誤発火防止）。
    need = {}
    for sym in symbols:
        m = atr.get(sym)
        if m:
            need[sym] = GATE_K * (TAKER_FEE * 2 + m["avg_spread"])
    if args.atr_threshold > 0:
        need = {s: args.atr_threshold for s in symbols}
    atr_ready = any(m and full_days.get(sym) and m["atr_ratio"] >= need.get(sym, 1e9)
                    for sym, m in atr.items())

    reasons = []
    if data_ready and not prev_data:
        reasons.append("データ準備 %d/%d" % (args.days, args.days))
    # ATR はデータ14日が揃っているときだけ評価する（復旧直後の早すぎる通知を防ぐ）。
    if data_ready and atr_ready and now_ts - last_atr >= ATR_COOLDOWN_SEC:
        reasons.append("ATR≥ゲート閾値")

    log("coverage=%s data_ready=%s atr_day=%s atr_ready=%s reasons=%s"
        % (cov, data_ready, atr_day, atr_ready, reasons))

    if args.seed:
        state["data_ready"] = data_ready
        if atr_ready and not last_atr:
            state["atr_notified_at"] = now_ts
        state["seeded_at"] = now_jst().isoformat(timespec="seconds")
        state["days"] = day_cache
        save_state(state_path, state)
        log("seeded state=%s" % state_path)
        return 0

    if not reasons:
        if not args.dry_run:
            state["data_ready"] = data_ready
            state["checked_at"] = now_jst().isoformat(timespec="seconds")
            state["days"] = day_cache
            save_state(state_path, state)
        log("no notification")
        return 0

    body = build_body(socket.gethostname(), cov, args.days, atr, atr_day,
                      args.atr_threshold, reasons, latest_cf_rows(cf_tsv),
                      need, full_days)
    subject = "[cryptoEngine] V.T 再評価時期: " + " / ".join(reasons)
    if args.dry_run:
        print("=== DRY RUN (not sent) ===")
        print("Subject: " + subject)
        print(body)
        return 0

    ok = send_mail(cfg, subject, body)
    if ok:
        state["data_ready"] = data_ready
        if any(r.startswith("ATR") for r in reasons):
            state["atr_notified_at"] = now_ts
        state["last_notified_at"] = now_jst().isoformat(timespec="seconds")
        state["last_reasons"] = reasons
        state["days"] = day_cache
        save_state(state_path, state)
        log("sent: %s" % subject)
        return 0
    log("send failed; state not updated")
    return 1


if __name__ == "__main__":
    sys.exit(main())
