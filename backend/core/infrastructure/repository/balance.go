package repository

import (
	"context"

	"github.com/cryptoEngine/core/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type balanceRepo struct {
	pool *pgxpool.Pool
}

func (r *balanceRepo) Save(ctx context.Context, balance *domain.Balance) error {
	const query = `
		INSERT INTO balance_history (asset, free, locked, timestamp, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
	`
	_, err := r.pool.Exec(ctx, query,
		balance.Asset,
		balance.Free,
		balance.Locked,
		balance.Timestamp,
	)
	return err
}

func (r *balanceRepo) List(ctx context.Context, asset string, limit int) ([]*domain.Balance, error) {
	const query = `
		SELECT asset, free, locked, timestamp
		FROM balance_history
		WHERE ($1 = '' OR asset = $1)
		ORDER BY timestamp DESC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, asset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	balances := []*domain.Balance{}
	for rows.Next() {
		b := &domain.Balance{}
		if err := rows.Scan(&b.Asset, &b.Free, &b.Locked, &b.Timestamp); err != nil {
			return nil, err
		}
		balances = append(balances, b)
	}
	return balances, rows.Err()
}
