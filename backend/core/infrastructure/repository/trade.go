package repository

import (
	"context"

	"github.com/cryptoEngine/core/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type tradeRepo struct {
	pool *pgxpool.Pool
}

func (r *tradeRepo) Save(ctx context.Context, trade *domain.Trade) error {
	const query = `
		INSERT INTO trades (symbol, side, price, volume, timestamp, order_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
	`
	_, err := r.pool.Exec(ctx, query,
		trade.Symbol,
		string(trade.Side),
		trade.Price,
		trade.Volume,
		trade.Timestamp,
		trade.OrderID,
		trade.Status,
	)
	return err
}

func (r *tradeRepo) List(ctx context.Context, symbol string, limit int) ([]*domain.Trade, error) {
	const query = `
		SELECT symbol, side, price, volume, timestamp, order_id, status
		FROM trades
		WHERE ($1 = '' OR symbol = $1)
		ORDER BY timestamp DESC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trades := []*domain.Trade{}
	for rows.Next() {
		t := &domain.Trade{}
		if err := rows.Scan(&t.Symbol, &t.Side, &t.Price, &t.Volume, &t.Timestamp, &t.OrderID, &t.Status); err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}
