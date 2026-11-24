package repository

import (
	"context"
	"fmt"

	"errors"

	"github.com/cryptoEngine/core/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type positionRepo struct {
	pool *pgxpool.Pool
}

func (r *positionRepo) Save(ctx context.Context, position *domain.Position) error {
	const query = `
		INSERT INTO positions
			(symbol, side, size, volume, avg_price, last_price, market_value,
			 unrealized_pnl, unrealized_pnl_rate, timestamp, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now(), now())
	`
	_, err := r.pool.Exec(ctx, query,
		position.Symbol,
		string(position.Side),
		position.Volume,
		position.Volume,
		position.AvgPrice,
		position.LastPrice,
		position.MarketValue,
		position.UnrealizedPnl,
		position.UnrealizedPnlRate,
		position.Timestamp,
	)
	return err
}

// UpsertPosition は symbol 単位でポジションを更新する。存在しない場合は挿入する。
// 自動売買は symbol ごとに 1 つの「現在の保有量」を追跡するため、履歴は残さない。
func (r *positionRepo) UpsertPosition(ctx context.Context, position *domain.Position) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const update = `
		UPDATE positions SET
			side = $2, size = $3, volume = $4, avg_price = $5, last_price = $6,
			market_value = $7, unrealized_pnl = $8, unrealized_pnl_rate = $9,
			timestamp = $10, updated_at = now()
		WHERE symbol = $1
	`
	tag, err := tx.Exec(ctx, update,
		position.Symbol,
		string(position.Side),
		position.Volume,
		position.Volume,
		position.AvgPrice,
		position.LastPrice,
		position.MarketValue,
		position.UnrealizedPnl,
		position.UnrealizedPnlRate,
		position.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("update position: %w", err)
	}

	if tag.RowsAffected() == 0 {
		const insert = `
			INSERT INTO positions
				(symbol, side, size, volume, avg_price, last_price, market_value,
				 unrealized_pnl, unrealized_pnl_rate, timestamp, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now(), now())
		`
		if _, err := tx.Exec(ctx, insert,
			position.Symbol,
			string(position.Side),
			position.Volume,
			position.Volume,
			position.AvgPrice,
			position.LastPrice,
			position.MarketValue,
			position.UnrealizedPnl,
			position.UnrealizedPnlRate,
			position.Timestamp,
		); err != nil {
			return fmt.Errorf("insert position: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r *positionRepo) GetPosition(ctx context.Context, symbol string) (*domain.Position, error) {
	const query = `
		SELECT symbol, side, volume, avg_price, last_price, market_value, unrealized_pnl, unrealized_pnl_rate, timestamp
		FROM positions
		WHERE symbol = $1
		ORDER BY updated_at DESC
		LIMIT 1
	`
	p := &domain.Position{}
	err := r.pool.QueryRow(ctx, query, symbol).
		Scan(&p.Symbol, &p.Side, &p.Volume, &p.AvgPrice, &p.LastPrice,
			&p.MarketValue, &p.UnrealizedPnl, &p.UnrealizedPnlRate, &p.Timestamp)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

func (r *positionRepo) List(ctx context.Context, symbol string, limit int) ([]*domain.Position, error) {
	const query = `
		SELECT symbol, side, volume, avg_price, last_price, market_value, unrealized_pnl, unrealized_pnl_rate, timestamp
		FROM positions
		WHERE ($1 = '' OR symbol = $1)
		ORDER BY timestamp DESC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := []*domain.Position{}
	for rows.Next() {
		p := &domain.Position{}
		if err := rows.Scan(&p.Symbol, &p.Side, &p.Volume, &p.AvgPrice, &p.LastPrice,
			&p.MarketValue, &p.UnrealizedPnl, &p.UnrealizedPnlRate, &p.Timestamp); err != nil {
			return nil, err
		}
		positions = append(positions, p)
	}
	return positions, rows.Err()
}
