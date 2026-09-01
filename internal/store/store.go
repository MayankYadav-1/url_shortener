package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type URL struct {
	ID          int64
	Code        string
	OriginalURL string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	DeletedAt   *time.Time
}

type Stats struct {
	Code        string     `json:"short_code"`
	Clicks      int64      `json:"clicks"`
	UniqueIPs   int64      `json:"unique_ips"`
	LastClicked *time.Time `json:"last_clicked,omitempty"`
}

type Store struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

func (s *Store) CreateURL(ctx context.Context, original string, expires *time.Time, code string) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx,
		`INSERT INTO urls(original_url, short_code, expires_at)
		 VALUES($1, NULLIF($2,''), $3) RETURNING id`,
		original, code, expires,
	).Scan(&id)
	return id, err
}

func (s *Store) SetCode(ctx context.Context, id int64, code string) error {
	_, err := s.db.Exec(ctx, `UPDATE urls SET short_code=$1 WHERE id=$2`, code, id)
	return err
}

func (s *Store) GetByCode(ctx context.Context, code string) (URL, error) {
	var u URL
	err := s.db.QueryRow(ctx,
		`SELECT id, short_code, original_url, created_at, expires_at, deleted_at
		 FROM urls WHERE short_code=$1`,
		code,
	).Scan(&u.ID, &u.Code, &u.OriginalURL, &u.CreatedAt, &u.ExpiresAt, &u.DeletedAt)
	if err != nil {
		return URL{}, ErrNotFoundIfNoRows(err)
	}
	return u, nil
}

func (s *Store) SoftDelete(ctx context.Context, code string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE urls SET deleted_at=NOW() WHERE short_code=$1 AND deleted_at IS NULL`, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecordClick(ctx context.Context, code, ip, ua, referer string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO click_events(short_code, ip, user_agent, referer)
		 VALUES($1,$2,$3,$4)`, code, ip, ua, referer)
	return err
}

func (s *Store) GetAnalytics(ctx context.Context, code string) (Stats, error) {
	var st Stats
	err := s.db.QueryRow(ctx,
		`SELECT
			u.short_code,
			COUNT(c.id),
			COUNT(DISTINCT c.ip),
			MAX(c.created_at)
		 FROM urls u
		 LEFT JOIN click_events c ON c.short_code=u.short_code
		 WHERE u.short_code=$1
		 GROUP BY u.short_code`,
		code,
	).Scan(&st.Code, &st.Clicks, &st.UniqueIPs, &st.LastClicked)
	if err != nil {
		return Stats{}, ErrNotFoundIfNoRows(err)
	}
	return st, nil
}

func ErrNotFoundIfNoRows(err error) error {
	if err == nil {
		return nil
	}
	if err.Error() == "no rows in result set" {
		return ErrNotFound
	}
	return err
}
