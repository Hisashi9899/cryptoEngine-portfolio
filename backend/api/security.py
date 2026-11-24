"""認証・セキュリティユーティリティ。

- bcrypt によるパスワードハッシュ化
- JWT（HS256）の access / refresh token 発行・検証（jti 付き）
- refresh token のサーバー側管理: rotation・再利用検知・失効（logout）
- FastAPI 依存（require_auth）によるエンドポイント保護

シークレットは環境変数 AUTH_SECRET から読み込む。未設定の場合は起動時ではなく
初回利用時にエラーとする（開発時のみ AUTH_ALLOW_DEV_SECRET=1 で既定値を許可）。
"""

import logging
import os
import re
import uuid
from datetime import datetime, timedelta, timezone

import bcrypt
import jwt
from fastapi import Depends, HTTPException, Request, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

logger = logging.getLogger("crypto-api.security")

try:
    import db
except ImportError:  # パッケージとして（backend.api.*）実行される場合
    from . import db

_DEV_SECRET = "cryptoengine-dev-secret-change-me"

JWT_ALGORITHM = "HS256"
ACCESS_TOKEN_TTL = timedelta(hours=float(os.environ.get("JWT_ACCESS_TTL_HOURS", "1")))
REFRESH_TOKEN_TTL = timedelta(days=float(os.environ.get("JWT_REFRESH_TTL_DAYS", "30")))

_bearer_scheme = HTTPBearer(auto_error=False)

# 直接の接続元がプライベート / ループバック（＝nginx 等のリバースプロキシ配下）
# の場合のみ X-Forwarded-For を信頼する。
_PRIVATE_PEER_RE = re.compile(
    r"^(127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|::1$|fc00:|fd[0-9a-f]{2}:)"
)


def client_ip(request: Request) -> str:
    """レート制限用のクライアント IP。プロキシ配下では XFF の先頭を採用する。"""
    host = request.client.host if request.client else "unknown"
    if _PRIVATE_PEER_RE.match(host or ""):
        xff = request.headers.get("x-forwarded-for", "")
        first = xff.split(",")[0].strip()
        if first:
            return first
    return host


def _secret() -> str:
    secret = os.environ.get("AUTH_SECRET", "").strip()
    if not secret:
        # Production must never fall back to a built-in secret, even if the
        # development flag was left in an environment file by mistake.
        if os.environ.get("APP_ENV", "").strip().lower() in ("prod", "production"):
            raise RuntimeError(
                "AUTH_SECRET is not set. Generate one (e.g. `openssl rand -hex 32`) "
                "and set it via env_file / --env-file."
            )
        if os.environ.get("AUTH_ALLOW_DEV_SECRET", "").strip() == "1":
            logger.warning("AUTH_SECRET is not set; using development default secret")
            return _DEV_SECRET
        raise RuntimeError(
            "AUTH_SECRET is not set. Generate one (e.g. `openssl rand -hex 32`) "
            "and set it via env_file / --env-file. Local dev only: set "
            "AUTH_ALLOW_DEV_SECRET=1 to allow the built-in development secret."
        )
    return secret


def hash_password(password: str) -> str:
    return bcrypt.hashpw(password.encode("utf-8"), bcrypt.gensalt()).decode("utf-8")


def verify_password(password: str, password_hash: str) -> bool:
    try:
        return bcrypt.checkpw(password.encode("utf-8"), password_hash.encode("utf-8"))
    except (ValueError, TypeError):
        return False


def _encode(sub: str, token_type: str, ttl: timedelta) -> tuple[str, str]:
    now = datetime.now(timezone.utc)
    jti = uuid.uuid4().hex
    payload = {
        "sub": sub,
        "type": token_type,
        "jti": jti,
        "iat": now,
        "exp": now + ttl,
        "iss": "cryptoengine",
    }
    return jwt.encode(payload, _secret(), algorithm=JWT_ALGORITHM), jti


def create_tokens(subject: str) -> dict:
    """access token（既定 1h）と refresh token（既定 30d）を発行する。

    refresh token は DB に記録し、rotation / 失効管理の対象とする。
    DB 書き込みに失敗した場合は発行を中断する（fail closed）。
    """
    access_token, _ = _encode(subject, "access", ACCESS_TOKEN_TTL)
    refresh_token, refresh_jti = _encode(subject, "refresh", REFRESH_TOKEN_TTL)
    expires_at = datetime.now(timezone.utc) + REFRESH_TOKEN_TTL
    with db.SessionLocal() as session:
        session.execute(
            db.text(
                "INSERT INTO refresh_tokens (jti, username, expires_at) "
                "VALUES (:jti, :u, :e)"
            ),
            {"jti": refresh_jti, "u": subject, "e": expires_at},
        )
        session.commit()
    logger.info("issued tokens: username=%s refresh_jti=%s", subject, refresh_jti[:8])
    return {
        "access_token": access_token,
        "refresh_token": refresh_token,
        "token_type": "bearer",
        "expires_in": int(ACCESS_TOKEN_TTL.total_seconds()),
    }


def decode_token(token: str, expected_type: str = "access") -> dict:
    """JWT を検証して payload を返す。無効・期限切れ・型不一致は 401。"""
    try:
        payload = jwt.decode(token, _secret(), algorithms=[JWT_ALGORITHM], issuer="cryptoengine")
    except jwt.ExpiredSignatureError as exc:
        raise HTTPException(status_code=401, detail="token expired") from exc
    except jwt.InvalidTokenError as exc:
        raise HTTPException(status_code=401, detail="invalid token") from exc

    if payload.get("type") != expected_type:
        raise HTTPException(status_code=401, detail="invalid token type")
    if not payload.get("sub"):
        raise HTTPException(status_code=401, detail="token missing subject")
    if expected_type == "refresh" and not payload.get("jti"):
        raise HTTPException(status_code=401, detail="token missing jti")
    return payload


def rotate_refresh(refresh_token: str) -> dict:
    """refresh token を検証し、使用済みにマークして新しいトークンペアを返す。

    - 未記録の jti → 無効（401）
    - 使用済み / 失効済み jti の再提示 → 再利用攻撃とみなし、当該ユーザーの
      全 refresh token を失効させる（401）
    """
    payload = decode_token(refresh_token, expected_type="refresh")
    sub = payload["sub"]
    jti = payload["jti"]
    now = datetime.now(timezone.utc)
    try:
        with db.SessionLocal() as session:
            row = session.execute(
                db.text(
                    "SELECT used_at, revoked_at FROM refresh_tokens WHERE jti = :j"
                ),
                {"j": jti},
            ).fetchone()
            if row is None:
                logger.warning("refresh reuse/unknown jti: user=%s jti=%s", sub, jti[:8])
                raise HTTPException(status_code=401, detail="invalid token")
            if row[0] is not None or row[1] is not None:
                session.execute(
                    db.text(
                        "UPDATE refresh_tokens SET revoked_at = :n "
                        "WHERE username = :u AND revoked_at IS NULL AND used_at IS NULL"
                    ),
                    {"n": now, "u": sub},
                )
                session.commit()
                logger.error(
                    "SECURITY: refresh token replay detected; revoked all sessions "
                    "for user=%s jti=%s",
                    sub, jti[:8],
                )
                raise HTTPException(
                    status_code=401,
                    detail="refresh token reuse detected; all sessions revoked",
                )
            # 期限切れ済みの行を掃除（7日より古いものは削除）。
            session.execute(
                db.text(
                    "DELETE FROM refresh_tokens WHERE expires_at < :cutoff"
                ),
                {"cutoff": now - timedelta(days=7)},
            )
            session.execute(
                db.text("UPDATE refresh_tokens SET used_at = :n WHERE jti = :j"),
                {"n": now, "j": jti},
            )
            session.commit()
    except HTTPException:
        raise
    except Exception as exc:
        logger.warning("refresh validation failed (%s); failing closed", exc)
        raise HTTPException(status_code=401, detail="invalid token") from exc
    return create_tokens(sub)


def revoke_refresh(refresh_token: str | None) -> bool:
    """logout 用。refresh token を失効させる（無効な入力は黙って False）。"""
    if not refresh_token:
        return False
    try:
        payload = decode_token(refresh_token, expected_type="refresh")
    except HTTPException:
        return False
    jti = payload["jti"]
    try:
        with db.SessionLocal() as session:
            result = session.execute(
                db.text(
                    "UPDATE refresh_tokens SET revoked_at = :n "
                    "WHERE jti = :j AND revoked_at IS NULL"
                ),
                {"n": datetime.now(timezone.utc), "j": jti},
            )
            session.commit()
            return (result.rowcount or 0) > 0
    except Exception as exc:
        logger.warning("revoke_refresh failed (%s)", exc)
        return False


def require_auth(
    credentials: HTTPAuthorizationCredentials | None = Depends(_bearer_scheme),
) -> str:
    """Protected エンドポイント用の依存。有効な access token の sub（username）を返す。

    Authorization: Bearer <token> が無い・無効・期限切れの場合は 401 を投げる。
    """
    if credentials is None:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="not authenticated",
            headers={"WWW-Authenticate": "Bearer"},
        )
    payload = decode_token(credentials.credentials, expected_type="access")
    return payload["sub"]


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()
