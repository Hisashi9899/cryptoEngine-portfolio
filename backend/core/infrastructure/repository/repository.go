// Package repository は PostgreSQL（pgx）への永続化を提供する。
// usecase はこのパッケージが定義するインターフェースのみに依存し、
// 実装は main 側の DI で注入される。
package repository

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/cryptoEngine/core/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SignalRepository は売買シグナルの永続化を担う。
type SignalRepository interface {
	Save(ctx context.Context, signal *domain.Signal) error
}

// TradeRepository はトレード履歴の永続化を担う。
type TradeRepository interface {
	Save(ctx context.Context, trade *domain.Trade) error
	List(ctx context.Context, symbol string, limit int) ([]*domain.Trade, error)
}

// PositionRepository はポジションの永続化を担う。
// UpsertPosition は symbol 単位の現在ポジション更新、GetPosition は取得。
type PositionRepository interface {
	Save(ctx context.Context, position *domain.Position) error
	List(ctx context.Context, symbol string, limit int) ([]*domain.Position, error)
	UpsertPosition(ctx context.Context, position *domain.Position) error
	GetPosition(ctx context.Context, symbol string) (*domain.Position, error)
}

// BalanceRepository は資金残高スナップショットの永続化を担う。
type BalanceRepository interface {
	Save(ctx context.Context, balance *domain.Balance) error
	List(ctx context.Context, asset string, limit int) ([]*domain.Balance, error)
}

// Repository は DB 接続と各リポジトリを束ねる。
type Repository struct {
	pool      *pgxpool.Pool
	Signals   SignalRepository
	Trades    TradeRepository
	Positions PositionRepository
	Balances  BalanceRepository
}

// New は url の PostgreSQL へ接続し、疎通確認済みの Repository を返す。
func New(ctx context.Context, databaseURL string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool new: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}

	repo := &Repository{
		pool:      pool,
		Signals:   &signalRepo{pool: pool},
		Trades:    &tradeRepo{pool: pool},
		Positions: &positionRepo{pool: pool},
		Balances:  &balanceRepo{pool: pool},
	}
	return repo, nil
}

// Close は DB 接続プールを閉じる。
func (r *Repository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// DatabaseURL は DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME の
// 環境変数から接続 URL を組み立てる。
func DatabaseURL() string {
	u := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%s", envOr("DB_HOST", "localhost"), envOr("DB_PORT", "5432")),
		Path:   envOr("DB_NAME", "crypto_engine"),
	}
	user := envOr("DB_USER", "crypto")
	if password := os.Getenv("DB_PASSWORD"); password != "" {
		u.User = url.UserPassword(user, password)
	} else {
		u.User = url.User(user)
	}
	return u.String()
}

// msToTime はミリ秒エポックを time.Time に変換する。0 以下は現在時刻を返す。
func msToTime(ms int64) time.Time {
	if ms > 0 {
		return time.UnixMilli(ms)
	}
	return time.Now()
}
