#!/usr/bin/env python3
"""V.T dry-run の観測点到達を判定し、条件成立時だけメール通知する。

目的:
  2026-11-23 の live 設定変更（LINK+LTC / k=1.0 / denyなし / balance 10万）で
  dry-run のデータが溜まったとき、あらかじめ決めた観測点で1回だけ通知する。
  「いつ見ればよいか」を人が覚えなくて済むようにする。

観測点（各1回だけ）:
  - day7     : 設定変更から7日経過し、1件以上（中間確認）
  - pairs20  : 20件到達
  - pairs50  : 50件到達
  - pairs100 : 100件到達
  - pairs200 : 200件到達（受入の件数基準）
  - accept   : pairs>=200 かつ PF>1.2 かつ CI下限>1.0（受入基準の達成）
  - stall    : 3日経過して0件（収集停止の疑い）

安全設計:
  - 読み取り専用。dry-run の JSONL を読むだけで、live/paper の状態や
    発注経路には触れない。
  - 各観測点は1回だけ送る（ops/logs/vt-observe-due.json に記録）。
  - SMTP は既存の vt_eval_due.py / watchdog と同じ infra/docker/.env の
    ALERT_* を使う（宛先 alerts@example.com）。
  - 採点は本体と同じ vt-acceptance を一時ディレクトリに対して実行する。
    手数料は taker 0.05%/片道。docker が使えない場合のみ Python 近似。

使い方:
  python3 ops/scripts/remote/vt_observe_due.py --dry-run   # 判定と本文だけ
  python3 ops/scripts/remote/vt_observe_due.py             # 条件成立時のみ送信
  python3 ops/scripts/remote/vt_observe_due.py --seed      # 現在状態を記録
  python3 ops/scripts/remote/vt_observe_due.py --check-smtp
  python3 ops/scripts/remote/vt_observe_due.py --test-mail
  python3 ops/scripts/remote/vt_observe_due.py --dry-run --force-milestone day7
"""
import argparse
import glob
import json
import os
import random
import shutil
import smtplib
import socket
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from email.mime.text import MIMEText

JST = timezone(timedelta(hours=9))

# 2026-11-23 09:56:05 UTC（18:56 JST）に新設定で vtlive を再作成した時刻。
DEFAULT_SINCE = "2026-11-23T09:56:05Z"
DEFAULT_SYMBOLS = "LINK,LTC"
DEFAULT_PAIR_MILESTONES = (20, 50, 100, 200)
DEFAULT_DAY_MILESTONE = 7
DEFAULT_DAY_MIN_PAIRS = 1
DEFAULT_STALL_DAYS = 3
FEE_TAKER = 0.0005
ACCEPT_MIN_PAIRS = 200
ACCEPT_MIN_PF = 1.2
BOOTSTRAP_N = 2000
BASELINE_TEXT = ("CFリプレイ 35日 (2026-10-17..11-20, balance 10万, k=1.0, denyなし)\n"
                 "  40件  PF 1.316  CI [0.482, 3.401]  純益 +296.1 円")


def now_jst():
    return datetime.now(JST)


def log(msg):
    print("[vt-observe] %s %s" % (now_jst().strftime("%Y-%m-%d %H:%M:%S"), msg), flush=True)


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
        with smtplib.SMTP(cfg["ALERT_SMTP_HOST"],
                          int(cfg.get("ALERT_SMTP_PORT", "587")), timeout=30) as s:
            s.starttls()
            s.login(cfg["ALERT_SMTP_USER"], cfg["ALERT_SMTP_PASS"])
            s.send_message(msg)
        return True
    except Exception as exc:  # noqa: BLE001
        log("smtp send failed: %s" % type(exc).__name__)
        return False


def load_state(path):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def save_state(path, state):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(state, f, ensure_ascii=False, indent=2)
    os.replace(tmp, path)


def parse_since(s):
    s = str(s).strip()
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    try:
        return datetime.fromisoformat(s).astimezone(timezone.utc)
    except ValueError:
        return datetime.fromtimestamp(float(s), timezone.utc)


def fmt_dt(dt):
    return dt.astimezone(JST).strftime("%Y-%m-%d %H:%M JST")


def fmt_num(v, nd=3):
    """PF/CI の表示。負けゼロで 1e9 になる場合は ∞ とする。"""
    try:
        v = float(v)
    except (TypeError, ValueError):
        return "?"
    if v >= 1e8:
        return "∞"
    return ("%." + str(nd) + "f") % v


def load_trades(log_dir, since_ms):
    """vt_live_dryrun_*.jsonl から open→close を往復に復元する（Go 実装と同順）。

    close の ts が since_ms 以降のものだけを採用する。設定変更をまたぐ
    建玉は close 時点で新設定の観測に含める。
    """
    files = sorted(glob.glob(os.path.join(log_dir, "vt_live_dryrun_*.jsonl")))
    opened = {}
    trades = []
    for path in files:
        try:
            f = open(path, encoding="utf-8", errors="replace")
        except OSError:
            continue
        with f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    r = json.loads(line)
                except ValueError:
                    continue
                sym = str(r.get("symbol", "")).upper().strip()
                action = r.get("action")
                if action == "open":
                    opened[sym] = {"entry": float(r.get("price") or 0),
                                   "qty": float(r.get("qty") or 0),
                                   "maker": bool(r.get("maker")),
                                   "open_ts": int(r.get("ts") or 0)}
                elif action == "close":
                    p = opened.pop(sym, None)
                    if not p:
                        continue
                    ts = int(r.get("ts") or 0)
                    if ts < since_ms:
                        continue
                    trades.append({
                        "symbol": sym, "entry": p["entry"], "qty": p["qty"],
                        "exit": float(r.get("price") or 0),
                        "pnl": float(r.get("pnl") or 0),
                        "maker": p["maker"], "open_ts": p["open_ts"],
                        "close_ts": ts,
                    })
    trades.sort(key=lambda t: t["close_ts"])
    return trades


def write_normalized(dir_path, trades):
    path = os.path.join(dir_path, "vt_live_dryrun_observe.jsonl")
    with open(path, "w", encoding="utf-8") as f:
        for t in trades:
            f.write(json.dumps({
                "action": "open", "symbol": t["symbol"], "price": t["entry"],
                "qty": t["qty"], "maker": t["maker"], "ts": t["open_ts"],
            }) + "\n")
            f.write(json.dumps({
                "action": "close", "symbol": t["symbol"], "price": t["exit"],
                "qty": t["qty"], "pnl": t["pnl"], "ts": t["close_ts"],
            }) + "\n")
    os.chmod(path, 0o644)
    return path


def approx_stats(trades, fee_mode="taker"):
    """docker が使えない場合の近似採点（CI は Python ブートストラップ）。"""
    nets = []
    fees = 0.0
    for t in trades:
        cost = FEE_TAKER * t["entry"] * t["qty"] + FEE_TAKER * t["exit"] * t["qty"]
        nets.append(t["pnl"] - cost)
        fees += cost
    wins = sum(1 for v in nets if v > 0)
    gw = sum(v for v in nets if v > 0)
    gl = -sum(v for v in nets if v <= 0)
    pf = (gw / gl) if gl > 0 else (1e9 if nets else 0.0)
    lo = hi = 0.0
    if nets:
        rng = random.Random(7)
        pfs = []
        n = len(nets)
        for _ in range(BOOTSTRAP_N):
            w = l = 0.0
            for _ in range(n):
                v = nets[rng.randrange(n)]
                if v > 0:
                    w += v
                else:
                    l -= v
            pfs.append(1e9 if l <= 0 else w / l)
        pfs.sort()
        lo = pfs[BOOTSTRAP_N * 25 // 1000]
        hi = pfs[BOOTSTRAP_N * 975 // 1000 - 1]
    fail = "pairs" if len(nets) < ACCEPT_MIN_PAIRS else (
        "pf" if pf <= ACCEPT_MIN_PF else ("ci" if lo <= 1.0 else ""))
    return {"pairs": len(nets), "wins": wins,
            "winrate": (wins / len(nets)) if nets else 0.0,
            "pf": pf, "pf_ci_low": lo, "pf_ci_high": hi,
            "net": sum(nets), "fees": fees,
            "pass": not fail, "fail_why": fail, "approx": True}


def run_acceptance(repo, tmp_dir, symbols, min_pairs, min_pf, fee_mode):
    """本体と同じ vt-acceptance を一時ディレクトリに対して実行する。"""
    env_file = os.path.join(repo, "infra/config/.env.prod")
    docker_bin = shutil.which("docker") or "/usr/bin/docker"
    cmd = [docker_bin, "run", "--rm", "--cpus=1", "--memory=192m"]
    if os.path.isfile(env_file):
        cmd += ["--env-file", env_file]
    cmd += [
        "-e", "ORDERBOOK_LOG_DIR=/trade_logs",
        "-v", "%s:/trade_logs" % tmp_dir,
        "--entrypoint", "core", "cryptoengine-core:latest",
        "vt-acceptance", "--dir=/trade_logs", "--src=dryrun", "--bars=300", "--days=0",
        "--symbols=%s" % symbols, "--fee-mode=%s" % fee_mode,
        "--min-pairs=%d" % min_pairs, "--min-pf=%s" % min_pf,
    ]
    try:
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=180)
    except Exception as exc:  # noqa: BLE001
        log("vt-acceptance failed (%s); falling back to approx" % type(exc).__name__)
        return None
    for line in reversed((out.stdout or "").strip().splitlines()):
        line = line.strip()
        if line.startswith("{") and line.endswith("}"):
            try:
                return json.loads(line)
            except ValueError:
                continue
    log("vt-acceptance produced no JSON; falling back to approx")
    return None


def heartbeat_summary(log_dir):
    path = os.path.join(log_dir, "vt_live_heartbeat.json")
    try:
        with open(path, encoding="utf-8") as f:
            d = json.load(f)
    except (OSError, ValueError):
        return "heartbeat: 読めません"
    mode = d.get("mode", "?")
    age = d.get("tick_age") or {}
    if isinstance(age, dict) and age:
        worst = max(int(v) for v in age.values())
        return "mode=%s  tick_age最大=%ds" % (mode, worst)
    return "mode=%s" % mode


def milestone_table(elapsed_days, pairs, pair_milestones, day_milestone,
                    day_min_pairs, stall_days, accept):
    rows = []
    d_ok = elapsed_days >= day_milestone and pairs >= day_min_pairs
    rows.append("  day%d (>=%d件): %s (経過 %.1f日, %d件)"
                % (day_milestone, day_min_pairs, "到達" if d_ok else "未達",
                   elapsed_days, pairs))
    for m in pair_milestones:
        rows.append("  pairs%d: %s (%d/%d)" % (m, "到達" if pairs >= m else "未達", pairs, m))
    a_ok = (pairs >= ACCEPT_MIN_PAIRS and accept.get("pf", 0) > ACCEPT_MIN_PF
            and accept.get("pf_ci_low", 0) > 1.0)
    rows.append("  accept (pairs>=%d, PF>%.1f, CI下限>1.0): %s"
                % (ACCEPT_MIN_PAIRS, ACCEPT_MIN_PF, "到達" if a_ok else "未達"))
    rows.append("  stall (%.0f日で0件): %s"
                % (stall_days, "該当" if (elapsed_days >= stall_days and pairs == 0) else "該当なし"))
    return rows


def build_body(host, since, elapsed_days, trades, acc, per_sym, milestones,
               hb, milestone_key, milestone_label, symbols):
    lines = []
    lines.append("V.T dry-run の観測点に到達しました。")
    lines.append("")
    lines.append("ホスト: %s" % host)
    lines.append("判定時刻: %s" % fmt_dt(now_jst()))
    lines.append("観測開始: %s（新設定の適用）" % fmt_dt(since))
    lines.append("経過: %.1f日" % elapsed_days)
    lines.append("到達: %s" % milestone_label)
    lines.append("")
    lines.append("[成績] %s / taker 0.05%%/片道 / dry-run" % "+".join(symbols))
    lines.append("  件数: %d" % acc.get("pairs", 0))
    lines.append("  勝率: %.1f%%" % (float(acc.get("winrate", 0)) * 100))
    lines.append("  PF: %s" % fmt_num(acc.get("pf", 0)))
    lines.append("  PF 95%%CI: [%s, %s]"
                 % (fmt_num(acc.get("pf_ci_low", 0)), fmt_num(acc.get("pf_ci_high", 0))))
    lines.append("  純損益: %+.1f 円" % float(acc.get("net", 0)))
    lines.append("  手数料: %.1f 円" % float(acc.get("fees", 0)))
    verdict = "PASS" if acc.get("pass") else "未達"
    why = acc.get("fail_why") or ""
    lines.append("  受入判定: %s%s" % (verdict, (" (fail_why=%s)" % why) if why else ""))
    if acc.get("approx"):
        lines.append("  ※ 採点は Python 近似（vt-acceptance を実行できませんでした）")
    lines.append("")
    lines.append("[銘柄別]")
    for sym in symbols:
        a = per_sym.get(sym) or {}
        lines.append("  %-4s 件数=%-3d 勝率=%5.1f%% PF=%6s CI下限=%6s 純益=%+8.1f"
                     % (sym, int(a.get("pairs", 0)),
                        float(a.get("winrate", 0)) * 100, fmt_num(a.get("pf", 0)),
                        fmt_num(a.get("pf_ci_low", 0)), float(a.get("net", 0))))
    lines.append("")
    lines.append("[比較]")
    lines.append("  " + BASELINE_TEXT.replace("\n", "\n  "))
    lines.append("")
    lines.append("[観測点の進捗]")
    lines.extend(milestones)
    lines.append("")
    lines.append("[稼働状態]")
    lines.append("  " + hb)
    lines.append("")
    lines.append("[次の操作]")
    lines.append("  - 実弾化の判断は受入基準（pairs>=%d, PF>%.1f, CI下限>1.0）の達成後。"
                 % (ACCEPT_MIN_PAIRS, ACCEPT_MIN_PF))
    lines.append("  - 詳細は VPS で次を実行:")
    lines.append("      cd /opt/cryptoengine")
    lines.append("      python3 ops/scripts/remote/vt_observe_due.py --dry-run")
    lines.append("")
    lines.append("このメールは各観測点で1回だけ送信されます。")
    lines.append("（live は dry-run のままです。実発注はありません。）")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--log-dir", default="")
    ap.add_argument("--env-file", default="")
    ap.add_argument("--state", default="")
    ap.add_argument("--since", default="",
                    help="観測開始（ISO8601 UTC）。省略時は状態ファイル、なければ既定値")
    ap.add_argument("--symbols", default=DEFAULT_SYMBOLS)
    ap.add_argument("--fee-mode", default="taker")
    ap.add_argument("--pair-milestones", default=",".join(str(x) for x in DEFAULT_PAIR_MILESTONES))
    ap.add_argument("--day-milestone", type=int, default=DEFAULT_DAY_MILESTONE)
    ap.add_argument("--day-min-pairs", type=int, default=DEFAULT_DAY_MIN_PAIRS)
    ap.add_argument("--stall-days", type=float, default=DEFAULT_STALL_DAYS)
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--seed", action="store_true")
    ap.add_argument("--test-mail", action="store_true")
    ap.add_argument("--check-smtp", action="store_true")
    ap.add_argument("--force-milestone", default="",
                    help="テスト用: 指定観測点を到達扱いで本文を作る（--dry-run と併用）")
    args = ap.parse_args()

    if args.force_milestone and not args.dry_run:
        print("--force-milestone requires --dry-run "
              "(it would otherwise mark the milestone as sent)")
        return 2

    repo = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../.."))
    log_dir = args.log_dir or os.path.join(repo, "backend/core/trade_logs")
    env_file = args.env_file or os.path.join(repo, "infra/docker/.env")
    state_path = args.state or os.path.join(repo, "ops/logs/vt-observe-due.json")
    pair_milestones = tuple(int(x) for x in args.pair_milestones.split(",") if x.strip())
    symbols = [s for s in args.symbols.replace(",", " ").split() if s]

    cfg = load_dotenv(env_file)

    if args.check_smtp:
        needed = ["ALERT_SMTP_HOST", "ALERT_SMTP_USER", "ALERT_SMTP_PASS", "ALERT_TO"]
        missing = [k for k in needed if not cfg.get(k)]
        if missing:
            print("SMTP_UNCONFIGURED missing=%s" % ",".join(missing))
            return 2
        try:
            with smtplib.SMTP(cfg["ALERT_SMTP_HOST"],
                              int(cfg.get("ALERT_SMTP_PORT", "587")), timeout=30) as s:
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
        ok = send_mail(cfg, "[cryptoEngine] V.T dry-run 観測通知のテスト",
                       "V.T dry-run 観測点通知のテストです。\n"
                       "このメールが届けば、観測点到達時の通知は正常に動作します。\n")
        print("TEST_MAIL_SENT" if ok else "TEST_MAIL_FAILED")
        return 0 if ok else 1

    if not os.path.isdir(log_dir):
        log("ERROR: log dir not found: %s" % log_dir)
        return 2

    state = load_state(state_path)
    if args.since:
        canonical = parse_since(args.since).isoformat().replace("+00:00", "Z")
        if state.get("since") != canonical:
            state = {"since": canonical, "notified": {}}
    elif not state.get("since"):
        state["since"] = DEFAULT_SINCE
    since = parse_since(state["since"])
    notified = dict(state.get("notified") or {})

    now = now_jst()
    elapsed_days = max(0.0, (now - since).total_seconds() / 86400.0)
    since_ms = int(since.timestamp() * 1000)
    sym_set = set(symbols)
    trades = [t for t in load_trades(log_dir, since_ms) if t["symbol"] in sym_set]
    pairs = len(trades)

    acc = {"pairs": 0, "wins": 0, "winrate": 0.0, "pf": 0.0,
           "pf_ci_low": 0.0, "pf_ci_high": 0.0, "net": 0.0, "fees": 0.0,
           "pass": False, "fail_why": "pairs"}
    per_sym = {}
    tmp_dir = None
    if trades:
        tmp_dir = tempfile.mkdtemp(prefix="vt-observe-")
        try:
            os.chmod(tmp_dir, 0o755)
            write_normalized(tmp_dir, trades)
            got = run_acceptance(repo, tmp_dir, ",".join(symbols),
                                 ACCEPT_MIN_PAIRS, ACCEPT_MIN_PF, args.fee_mode)
            if got:
                acc = got
            else:
                acc = approx_stats(trades, args.fee_mode)
            for sym in symbols:
                sub = [t for t in trades if t["symbol"] == sym]
                if sub:
                    g = run_acceptance(repo, tmp_dir, sym, ACCEPT_MIN_PAIRS,
                                       ACCEPT_MIN_PF, args.fee_mode)
                    per_sym[sym] = g if g else approx_stats(sub, args.fee_mode)
                else:
                    per_sym[sym] = {"pairs": 0, "wins": 0, "winrate": 0.0,
                                    "pf": 0.0, "pf_ci_low": 0.0, "pf_ci_high": 0.0,
                                    "net": 0.0, "fees": 0.0}
        finally:
            if tmp_dir:
                shutil.rmtree(tmp_dir, ignore_errors=True)
    else:
        for sym in symbols:
            per_sym[sym] = {"pairs": 0, "wins": 0, "winrate": 0.0, "pf": 0.0,
                            "pf_ci_low": 0.0, "pf_ci_high": 0.0, "net": 0.0, "fees": 0.0}

    # 件数は採点結果（vt-acceptance）を正とする。復元数と食い違う場合は採点側を優先。
    pairs = int(acc.get("pairs", pairs))

    # 到達判定（優先度: accept > stall > 大きい件数 > day7）
    reached = []  # (priority, key, label)
    if (pairs >= ACCEPT_MIN_PAIRS and acc.get("pf", 0) > ACCEPT_MIN_PF
            and acc.get("pf_ci_low", 0) > 1.0):
        reached.append((1000, "accept", "受入基準の達成 (pairs>=%d, PF>%.1f, CI下限>1.0)"
                        % (ACCEPT_MIN_PAIRS, ACCEPT_MIN_PF)))
    if elapsed_days >= args.stall_days and pairs == 0:
        reached.append((900, "stall", "収集停止の疑い（%.0f日で0件）" % args.stall_days))
    for m in pair_milestones:
        if pairs >= m:
            reached.append((50 + m, "pairs%d" % m, "%d件到達" % m))
    if elapsed_days >= args.day_milestone and pairs >= args.day_min_pairs:
        reached.append((10, "day%d" % args.day_milestone,
                        "%d日中間観測（%d件）" % (args.day_milestone, pairs)))

    new = [r for r in reached if r[1] not in notified]
    new.sort(reverse=True)
    forced = None
    if args.force_milestone:
        key = args.force_milestone
        label = next((r[2] for r in reached if r[1] == key), None)
        if label is None:
            label = {"day7": "7日中間観測（テスト）", "pairs20": "20件到達（テスト）",
                     "pairs50": "50件到達（テスト）", "pairs100": "100件到達（テスト）",
                     "pairs200": "200件到達（テスト）", "accept": "受入基準の達成（テスト）",
                     "stall": "収集停止の疑い（テスト）"}.get(key, "%s（テスト）" % key)
        forced = (0, key, label)

    milestones = milestone_table(elapsed_days, pairs, pair_milestones,
                                 args.day_milestone, args.day_min_pairs,
                                 args.stall_days, acc)
    hb = heartbeat_summary(log_dir)

    log("since=%s elapsed=%.2fd pairs=%d pf=%.3f ci=[%.3f,%.3f] net=%+.1f new=%s"
        % (state["since"], elapsed_days, pairs, float(acc.get("pf", 0)),
           float(acc.get("pf_ci_low", 0)), float(acc.get("pf_ci_high", 0)),
           float(acc.get("net", 0)), [r[1] for r in new]))

    if args.seed:
        state["since"] = parse_since(state["since"]).isoformat().replace("+00:00", "Z")
        for _, key, _ in reached:
            notified.setdefault(key, now_jst().isoformat(timespec="seconds"))
        state["notified"] = notified
        state["seeded_at"] = now_jst().isoformat(timespec="seconds")
        state["last_checked"] = now_jst().isoformat(timespec="seconds")
        state["last_pairs"] = pairs
        state["last_net"] = float(acc.get("net", 0))
        save_state(state_path, state)
        log("seeded state=%s notified=%s" % (state_path, sorted(notified)))
        return 0

    chosen = forced if forced else (new[0] if new else None)
    if not chosen:
        if not args.dry_run:
            state["since"] = parse_since(state["since"]).isoformat().replace("+00:00", "Z")
            state["notified"] = notified
            state["last_checked"] = now_jst().isoformat(timespec="seconds")
            state["last_pairs"] = pairs
            state["last_net"] = float(acc.get("net", 0))
            save_state(state_path, state)
        log("no notification")
        return 0

    _, key, label = chosen
    body = build_body(socket.gethostname(), since, elapsed_days, trades, acc,
                      per_sym, milestones, hb, key, label, symbols)
    subject = "[cryptoEngine] V.T dry-run 観測点: %s (PF %s, CI下限 %s)"
    subject = subject % (label, fmt_num(acc.get("pf", 0)), fmt_num(acc.get("pf_ci_low", 0)))

    if args.dry_run:
        print("=== DRY RUN (not sent) ===")
        print("Subject: " + subject)
        print(body)
        return 0

    ok = send_mail(cfg, subject, body)
    if not ok:
        log("send failed; state not updated")
        return 1
    # 到達済みの観測点はまとめて記録し、翌日に過去分を再送しない。
    for _, k, _ in reached:
        notified.setdefault(k, now_jst().isoformat(timespec="seconds"))
    notified.setdefault(key, now_jst().isoformat(timespec="seconds"))
    state["since"] = parse_since(state["since"]).isoformat().replace("+00:00", "Z")
    state["notified"] = notified
    state["last_checked"] = now_jst().isoformat(timespec="seconds")
    state["last_notified_at"] = now_jst().isoformat(timespec="seconds")
    state["last_milestone"] = key
    state["last_pairs"] = pairs
    state["last_net"] = float(acc.get("net", 0))
    save_state(state_path, state)
    log("sent: %s" % subject)
    return 0


if __name__ == "__main__":
    sys.exit(main())
