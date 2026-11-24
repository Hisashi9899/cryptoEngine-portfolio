-- CryptoEngine schema
-- 冪等に実行できるよう CREATE TABLE IF NOT EXISTS を使用する。
-- timestamp は domain モデル（Go core / demo_data）に合わせてミリ秒エポック（BIGINT）で保存する。

CREATE TABLE IF NOT EXISTS signals (
    id            BIGSERIAL PRIMARY KEY,
    symbol        TEXT NOT NULL,
    type          TEXT NOT NULL,
    confidence    DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason        TEXT NOT NULL DEFAULT '',
    mid_price     DOUBLE PRECISION NOT NULL DEFAULT 0,
    spread_basis  DOUBLE PRECISION NOT NULL DEFAULT 0,
    timestamp     BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_signals_symbol ON signals (symbol);
CREATE INDEX IF NOT EXISTS idx_signals_timestamp ON signals (timestamp DESC);

CREATE TABLE IF NOT EXISTS trades (
    id          BIGSERIAL PRIMARY KEY,
    symbol      TEXT NOT NULL,
    side        TEXT NOT NULL,
    price       DOUBLE PRECISION NOT NULL DEFAULT 0,
    volume      DOUBLE PRECISION NOT NULL DEFAULT 0,
    pnl         DOUBLE PRECISION,
    timestamp   BIGINT NOT NULL DEFAULT 0,
    order_id    TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'filled',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_trades_symbol ON trades (symbol);
CREATE INDEX IF NOT EXISTS idx_trades_timestamp ON trades (timestamp DESC);

CREATE TABLE IF NOT EXISTS positions (
    id                   BIGSERIAL PRIMARY KEY,
    symbol               TEXT NOT NULL,
    side                 TEXT NOT NULL DEFAULT 'long',
    size                 DOUBLE PRECISION NOT NULL DEFAULT 0,
    volume               DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_price            DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_price           DOUBLE PRECISION NOT NULL DEFAULT 0,
    market_value         DOUBLE PRECISION NOT NULL DEFAULT 0,
    unrealized_pnl       DOUBLE PRECISION NOT NULL DEFAULT 0,
    unrealized_pnl_rate  DOUBLE PRECISION NOT NULL DEFAULT 0,
    timestamp            BIGINT NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_positions_symbol ON positions (symbol);
CREATE INDEX IF NOT EXISTS idx_positions_timestamp ON positions (timestamp DESC);

CREATE TABLE IF NOT EXISTS balance_history (
    id          BIGSERIAL PRIMARY KEY,
    asset       TEXT NOT NULL,
    free        DOUBLE PRECISION NOT NULL DEFAULT 0,
    locked      DOUBLE PRECISION NOT NULL DEFAULT 0,
    timestamp   BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_balance_asset ON balance_history (asset);
CREATE INDEX IF NOT EXISTS idx_balance_timestamp ON balance_history (timestamp DESC);

CREATE TABLE IF NOT EXISTS system_status (
    id          BIGSERIAL PRIMARY KEY,
    service     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT '',
    detail      JSONB NOT NULL DEFAULT '{}'::jsonb,
    timestamp   BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_system_service ON system_status (service);
CREATE INDEX IF NOT EXISTS idx_system_timestamp ON system_status (timestamp DESC);

-- BUY→SELL ペアリング（②エントリー→クローズ）。timestamp は ms エポック（BIGINT）。
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

-- 未クローズエントリー（symbol ごとに 1 件）。BUY 後に SELL が来るまで保持する。
CREATE TABLE IF NOT EXISTS open_entries (
    symbol     TEXT PRIMARY KEY,
    side       TEXT NOT NULL DEFAULT 'BUY',
    price      DOUBLE PRECISION NOT NULL DEFAULT 0,
    volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    timestamp  BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
