"""認証 API。

- POST /api/v1/auth/login     username + password → access / refresh token
- POST /api/v1/auth/refresh   refresh token → 新しい token ペア（rotation + 再利用検知）
- POST /api/v1/auth/logout    ログアウト（refresh token をサーバー側で失効）
- GET  /api/v1/auth/me        現在のログインユーザー

パスワードは bcrypt でハッシュ化して users テーブルに保存する。管理者は
起動時に AUTH_ADMIN_USERNAME / AUTH_ADMIN_PASSWORD から登録・同期される。

login / refresh には IP 単位のレート制限（ratelimit.py）を適用し、総当たりを
抑止する。閾値は環境変数 AUTH_LOGIN_MAX_ATTEMPTS 等で調整可能。
"""

import logging

from fastapi import APIRouter, Body, Depends, HTTPException, Request
from pydantic import BaseModel

logger = logging.getLogger("crypto-api.auth")
router = APIRouter()

try:
    import db
    import ratelimit
    import security
except ImportError:
    from .. import db
    from .. import ratelimit
    from .. import security


class LoginRequest(BaseModel):
    username: str
    password: str


class RefreshRequest(BaseModel):
    refresh_token: str


class LogoutRequest(BaseModel):
    refresh_token: str | None = None


class LoginResponse(BaseModel):
    access_token: str
    refresh_token: str
    token_type: str
    expires_in: int


def _find_user(identifier: str):
    with db.SessionLocal() as session:
        row = session.execute(
            db.text(
                "SELECT id, username, email, password_hash FROM users "
                "WHERE lower(username) = :u OR lower(email) = :u LIMIT 1"
            ),
            {"u": identifier.strip().lower()},
        ).fetchone()
        if row is None:
            return None
        return {"username": row[1], "email": row[2], "password_hash": row[3]}


# ユーザー列挙（タイミング差）を防ぐためのダミーハッシュ。
# ユーザーが存在しない場合も bcrypt 検証を実行して応答時間を揃える。
_DUMMY_HASH = security.hash_password("dummy-password-for-timing")


@router.post("/api/v1/auth/login", response_model=LoginResponse)
def login(
    request: Request,
    req: LoginRequest = Body(default=LoginRequest(username="", password="")),
):
    ip = security.client_ip(request) if request else "unknown"
    username = req.username.strip().lower()
    if not username or not req.password:
        raise HTTPException(status_code=400, detail="username and password are required")

    allowed, retry_after = ratelimit.check(
        f"login:{ip}", ratelimit.LOGIN_MAX_ATTEMPTS, ratelimit.LOGIN_WINDOW_SEC
    )
    if not allowed:
        raise HTTPException(
            status_code=429,
            detail="too many login attempts; try again later",
            headers={"Retry-After": str(retry_after)},
        )
    # アカウント単位の制限（分散攻撃対策。ユーザー名ごとに時速で制限）
    allowed, retry_after = ratelimit.check(
        f"login:account:{username}",
        ratelimit.LOGIN_MAX_ATTEMPTS_PER_ACCOUNT,
        ratelimit.LOGIN_ACCOUNT_WINDOW_SEC,
    )
    if not allowed:
        raise HTTPException(
            status_code=429,
            detail="too many login attempts; try again later",
            headers={"Retry-After": str(retry_after)},
        )

    user = _find_user(username)
    password_ok = security.verify_password(req.password, (user or {"password_hash": _DUMMY_HASH})["password_hash"])
    if user is None or not password_ok:
        logger.warning("login failed for username=%s ip=%s", username, ip)
        raise HTTPException(status_code=401, detail="invalid username or password")

    tokens = security.create_tokens(user["username"])
    ratelimit.reset_key(f"login:{ip}")
    ratelimit.reset_key(f"login:account:{username}")
    logger.info("login success: username=%s ip=%s", user["username"], ip)
    return tokens


@router.post("/api/v1/auth/refresh", response_model=LoginResponse)
def refresh(
    request: Request,
    req: RefreshRequest = Body(default=RefreshRequest(refresh_token="")),
):
    """refresh token から新しい access / refresh token を発行する（rotation）。

    使用済み refresh token の再提示は再利用攻撃とみなし、該当ユーザーの全
    セッションを失効させる。IP 単位のレート制限を適用。
    """
    if not req.refresh_token:
        raise HTTPException(status_code=400, detail="refresh_token is required")
    ip = security.client_ip(request) if request else "unknown"
    allowed, retry_after = ratelimit.check(
        f"refresh:{ip}", ratelimit.REFRESH_MAX_ATTEMPTS, ratelimit.REFRESH_WINDOW_SEC
    )
    if not allowed:
        raise HTTPException(
            status_code=429,
            detail="too many requests; try again later",
            headers={"Retry-After": str(retry_after)},
        )
    return security.rotate_refresh(req.refresh_token)


@router.post("/api/v1/auth/logout")
def logout(
    req: LogoutRequest = Body(default=LogoutRequest()),
    _: str = Depends(security.require_auth),
):
    """ログアウト。提示された refresh token をサーバー側で失効する。

    access token はステートレスなため、TTL（既定 1h）経過で自然失効する。
    """
    revoked = security.revoke_refresh(req.refresh_token)
    return {"status": "ok", "revoked": revoked}


@router.get("/api/v1/auth/me")
def me(username: str = Depends(security.require_auth)):
    return {"username": username}
