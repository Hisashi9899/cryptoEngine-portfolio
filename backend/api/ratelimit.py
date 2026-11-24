"""軽量なインメモリ・スライディングウィンドウ レートリミッタ。

- 認証系エンドポイント（login / refresh）の総当たり攻撃を抑止する。
- 単一プロセス（uvicorn ワーカー 1）前提。複数ワーカー / 複数ホスト構成では
  nginx (limit_req) 等の前段制限と併用すること。
- 閾値は環境変数で調整可能（0 以下で無効化）。
"""

import os
import threading
import time
from collections import defaultdict, deque

logger = __import__("logging").getLogger("crypto-api.ratelimit")


def _env_int(name: str, default: int) -> int:
    try:
        return int(os.environ.get(name, "").strip() or default)
    except ValueError:
        return default


LOGIN_MAX_ATTEMPTS = _env_int("AUTH_LOGIN_MAX_ATTEMPTS", 5)
LOGIN_WINDOW_SEC = _env_int("AUTH_LOGIN_WINDOW_SEC", 900)
# アカウント単位の制限（分散攻撃対策。成功時にリセットされる）
LOGIN_MAX_ATTEMPTS_PER_ACCOUNT = _env_int("AUTH_LOGIN_MAX_ATTEMPTS_PER_ACCOUNT", 20)
LOGIN_ACCOUNT_WINDOW_SEC = _env_int("AUTH_LOGIN_ACCOUNT_WINDOW_SEC", 3600)
REFRESH_MAX_ATTEMPTS = _env_int("AUTH_REFRESH_MAX_ATTEMPTS", 60)
REFRESH_WINDOW_SEC = _env_int("AUTH_REFRESH_WINDOW_SEC", 300)

_LOCK = threading.Lock()
_HITS: dict[str, deque[float]] = defaultdict(deque)
_LAST_PRUNE = time.monotonic()


def _prune(now: float, window_sec: int) -> None:
    """60秒に1回、全キーの古い記録を掃除してメモリを解放する。"""
    global _LAST_PRUNE
    if now - _LAST_PRUNE < 60:
        return
    _LAST_PRUNE = now
    cutoff = now - max(window_sec, LOGIN_WINDOW_SEC, REFRESH_WINDOW_SEC)
    stale = [k for k, q in _HITS.items() if not q or q[-1] < cutoff]
    for k in stale:
        del _HITS[k]


def check(key: str, max_attempts: int, window_sec: int) -> tuple[bool, int]:
    """試行を記録し、許可なら (True, 0)、超過なら (False, 再試行までの秒数) を返す。

    許可された場合のみカウントを消費する（失敗後も再試行を数え続けるより、
    シンプルに「試行回数」で絞る方針）。
    """
    if max_attempts <= 0 or window_sec <= 0:
        return True, 0
    now = time.monotonic()
    with _LOCK:
        _prune(now, window_sec)
        q = _HITS[key]
        while q and q[0] <= now - window_sec:
            q.popleft()
        if len(q) >= max_attempts:
            retry_after = int(window_sec - (now - q[0])) + 1
            logger.warning(
                "rate limit exceeded: key=%s attempts=%d window=%ds",
                key.split(":", 1)[0], len(q), window_sec,
            )
            return False, max(retry_after, 1)
        q.append(now)
        return True, 0


def reset_key(key: str) -> None:
    """ログイン成功時などにカウンタを解放する。"""
    with _LOCK:
        _HITS.pop(key, None)
