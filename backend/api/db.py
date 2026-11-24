"""SQLAlchemy エンジンとセッション管理。

DB に接続できない場合もアプリは停止せず、呼び出し側（repository / main）が
demo データへフォールバックする。エンジン作成自体は遅延接続のため、ここで
例外は発生しない。
"""

import logging
import os
import time
from pathlib import Path

from sqlalchemy import create_engine, text
from sqlalchemy.engine import make_url
from sqlalchemy.orm import sessionmaker

logger = logging.getLogger("crypto-api.db")


def _default_url() -> str:
    user = os.environ.get("DB_USER", "crypto")
    password = os.environ.get("DB_PASSWORD", "demo_local_password")
    host = os.environ.get("DB_HOST", "db")
    port = os.environ.get("DB_PORT", "5432")
    name = os.environ.get("DB_NAME", "crypto_engine")
    return f"postgresql+psycopg2://{user}:{password}@{host}:{port}/{name}"


def _normalize_url(url: str) -> str:
    """postgres:// は SQLAlchemy 2.x で無効なため psycopg2 ドライバを補完する。

    URL.set() の str() はパスワードを *** にマスクするため、
    render_as_string(hide_password=False) で実パスワードを保持する。
    """
    u = make_url(url)
    if u.drivername in ("postgres", "postgresql"):
        u = u.set(drivername="postgresql+psycopg2")
        return u.render_as_string(hide_password=False)
    return url


DATABASE_URL = _normalize_url(os.environ.get("DB_URL") or _default_url())

_WEAK_DB_PASSWORDS = frozenset({
    "", "demo_local_password", "crypto", "password", "postgres", "changeme",
    "admin", "123456", "qwerty",
})


def _effective_db_password() -> str:
    """実効 DB パスワードを返す（DB_URL 優先、未設定時は DB_PASSWORD）。"""
    url = os.environ.get("DB_URL", "")
    if url.strip():
        try:
            return make_url(url).password or ""
        except Exception:
            return ""
    return os.environ.get("DB_PASSWORD", "")


def validate_prod_config() -> None:
    """APP_ENV=prod 時の起動時検証。弱い DB パスワードでの本番起動を fail-fast させる。

    dev（既定）では何もしないため、既存の開発フロー・テストには影響しない。
    """
    if os.environ.get("APP_ENV", "dev").strip().lower() not in ("prod", "production"):
        return
    password = _effective_db_password()
    if len(password) < 12 or password.strip().lower() in _WEAK_DB_PASSWORDS:
        raise RuntimeError(
            "APP_ENV=prod requires a strong DB password (>=12 chars, not a default value). "
            "Set a strong password in both DB_URL and POSTGRES_PASSWORD."
        )

engine = create_engine(DATABASE_URL, pool_pre_ping=True, pool_recycle=600)
SessionLocal = sessionmaker(bind=engine, autoflush=False, expire_on_commit=False)


def get_session():
    """FastAPI の依存関係として使うセッション生成。"""
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()


def _find_schema() -> Path | None:
    candidates = [
        os.environ.get("CORE_DB_SCHEMA_PATH", ""),
        "/app/backend/core/schema.sql",
        "/app/infra/config/schema.sql",
        "backend/core/schema.sql",
        "infra/config/schema.sql",
        str(Path(__file__).resolve().parent.parent / "core" / "schema.sql"),
    ]
    for path in candidates:
        if path and Path(path).exists():
            return Path(path)
    return None


_PAIRING_SCHEMA = """
CREATE TABLE IF NOT EXISTS trade_pairs (
    id               BIGSERIAL PRIMARY KEY,
    symbol           TEXT NOT NULL,
    entry_side       TEXT NOT NULL DEFAULT 'BUY',
    exit_side        TEXT NOT NULL DEFAULT 'SELL',
    entry_price      DOUBLE PRECISION NOT NULL DEFAULT 0,
    exit_price       DOUBLE PRECISION NOT NULL DEFAULT 0,
    entry_time       BIGINT NOT NULL DEFAULT 0,
    exit_time        BIGINT NOT NULL DEFAULT 0,
    entry_volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    exit_volume      DOUBLE PRECISION NOT NULL DEFAULT 0,
    pnl              DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_trade_pairs_symbol ON trade_pairs (symbol);
CREATE INDEX IF NOT EXISTS idx_trade_pairs_exit_time ON trade_pairs (exit_time DESC);

CREATE TABLE IF NOT EXISTS open_entries (
    symbol     TEXT PRIMARY KEY,
    side       TEXT NOT NULL DEFAULT 'BUY',
    price      DOUBLE PRECISION NOT NULL DEFAULT 0,
    volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    timestamp  BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
"""


def init_pairing_tables() -> None:
    """trade_pairs / open_entries を冪等に作成する。

    init_db は signals テーブルが存在すると schema.sql を適用しないため、
    既存 DB に対してペアリング用テーブルだけを追加するための移行処理。
    失敗しても既存機能には影響させない。
    """
    try:
        with engine.begin() as conn:
            conn.exec_driver_sql(_PAIRING_SCHEMA)
        logger.info("ensured pairing tables (trade_pairs / open_entries)")
    except Exception as exc:  # noqa: BLE001
        logger.warning("failed to ensure pairing tables (%s)", exc)


def init_db() -> None:
    """signals テーブルが無ければ schema.sql を適用する。冪等。

    DB 起動直後は認証前の一時サーバーが HEALTHY になることがあるため、
    短いリトライを行ってから失敗時は demo フォールバックに任せる。
    """
    last_error: Exception | None = None
    for attempt in range(1, 4):
        try:
            with engine.begin() as conn:
                exists = conn.execute(
                    text("SELECT to_regclass('public.signals') IS NOT NULL")
                ).scalar()
                if not exists:
                    schema = _find_schema()
                    if schema is None:
                        logger.warning("schema.sql not found; skipping schema apply")
                        return
                    conn.exec_driver_sql(schema.read_text(encoding="utf-8"))
                    logger.info("applied database schema from %s", schema)
                _migrate_positions(conn)
                _migrate_users(conn)
                _migrate_refresh_tokens(conn)
                return
        except Exception as exc:  # noqa: BLE001
            last_error = exc
            logger.warning(
                "schema apply attempt %d/3 failed (%s)", attempt, exc
            )
            time.sleep(2)
    if os.environ.get("APP_ENV", "dev").strip().lower() in ("prod", "production"):
        logger.error(
            "database unavailable in prod; login and trade history will not work. "
            "Check POSTGRES_PASSWORD vs DB_PASSWORD consistency (note: POSTGRES_PASSWORD "
            "applies only at first volume init; use ALTER USER for existing volumes).",
            exc_info=last_error,
        )
    else:
        logger.warning("database unavailable; falling back to demo data", exc_info=last_error)


_POSITIONS_MIGRATION = """
ALTER TABLE positions ADD COLUMN IF NOT EXISTS last_price DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE positions ADD COLUMN IF NOT EXISTS market_value DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE positions ADD COLUMN IF NOT EXISTS unrealized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE positions ADD COLUMN IF NOT EXISTS unrealized_pnl_rate DOUBLE PRECISION NOT NULL DEFAULT 0;
"""


def _migrate_positions(conn) -> None:
    """自動売買対応カラム（positions）を既存 DB に冪等に追加する。"""
    try:
        conn.exec_driver_sql(_POSITIONS_MIGRATION)
        logger.info("ensured positions auto-trade columns")
    except Exception as exc:  # noqa: BLE001
        logger.warning("failed to migrate positions columns (%s)", exc)


_USERS_SCHEMA = """
CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL DEFAULT '',
    username      TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
"""

_USERS_MIGRATION = """
ALTER TABLE users ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_username
    ON users (username) WHERE username <> '';
"""


def _migrate_users(conn) -> None:
    """users テーブルを作成し、AUTH_ADMIN_USERNAME の管理者を登録する。

    AUTH_ADMIN_USERNAME / AUTH_ADMIN_PASSWORD が設定されている場合のみ
    登録する。env を唯一の真実とし、設定された username / email / password を
    常に反映する（冪等な upsert）。
    """
    try:
        conn.exec_driver_sql(_USERS_SCHEMA)
        conn.exec_driver_sql(_USERS_MIGRATION)
        username = os.environ.get("AUTH_ADMIN_USERNAME", "").strip().lower()
        email = os.environ.get("AUTH_ADMIN_EMAIL", "").strip().lower()
        password = os.environ.get("AUTH_ADMIN_PASSWORD", "")
        if not username or not password:
            return
        if not email:
            email = username
        from security import hash_password  # noqa: PLC0415

        row = conn.execute(
            text(
                "SELECT id FROM users WHERE lower(username) = :u OR lower(email) = :e LIMIT 1"
            ),
            {"u": username, "e": email},
        ).scalar()
        hashed = hash_password(password)
        if row is None:
            conn.execute(
                text(
                    "INSERT INTO users (email, username, password_hash) VALUES (:e, :u, :h)"
                ),
                {"e": email, "u": username, "h": hashed},
            )
            logger.info("seeded admin user: %s", username)
        else:
            conn.execute(
                text(
                    "UPDATE users SET email = :e, username = :u, password_hash = :h "
                    "WHERE id = :id"
                ),
                {"e": email, "u": username, "h": hashed, "id": row},
            )
            logger.info("synced admin user: %s", username)
    except Exception as exc:  # noqa: BLE001
        logger.warning("failed to migrate users table (%s)", exc)


_REFRESH_TOKENS_SCHEMA = """
CREATE TABLE IF NOT EXISTS refresh_tokens (
    jti        TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    issued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_username ON refresh_tokens (username);
"""


def _migrate_refresh_tokens(conn) -> None:
    """refresh token 失効管理テーブルを冪等に作成する。"""
    try:
        conn.exec_driver_sql(_REFRESH_TOKENS_SCHEMA)
        logger.info("ensured refresh_tokens table")
    except Exception as exc:  # noqa: BLE001
        logger.warning("failed to migrate refresh_tokens table (%s)", exc)
