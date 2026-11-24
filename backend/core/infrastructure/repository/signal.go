package repository

import (
	"context"

	"github.com/cryptoEngine/core/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type signalRepo struct {
	pool *pgxpool.Pool
}

func (r *signalRepo) Save(ctx context.Context, signal *domain.Signal) error {
	const query = `
		INSERT INTO signals (symbol, type, confidence, reason, mid_price, spread_basis, timestamp, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
	`
	_, err := r.pool.Exec(ctx, query,
		signal.Symbol,
		string(signal.Type),
		signal.Confidence,
		signal.Reason,
		signal.MidPrice,
		signal.SpreadBasis,
		signal.Timestamp,
	)
	return err
}
