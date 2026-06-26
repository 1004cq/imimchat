package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Message 持久化
type Message struct {
	ID          int64
	DialogID    int64
	SenderID    int64
	MsgType     int16
	Content     []byte
	ContentText string
	ClientMsgID string
	Seq         int64
	TTLSeconds  int32
	CreatedAt   time.Time
}

func (s *Store) InsertMessage(ctx context.Context, msg *Message) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO messages (dialog_id, sender_id, msg_type, content, content_text, client_msg_id, seq, ttl_seconds, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		RETURNING id
	`, msg.DialogID, msg.SenderID, msg.MsgType, msg.Content, msg.ContentText, msg.ClientMsgID, msg.Seq, msg.TTLSeconds)
	return err
}

func (s *Store) NextSeq(ctx context.Context, dialogID int64) (int64, error) {
	var seq int64
	err := s.pool.QueryRow(ctx, `
		UPDATE dialogs SET last_msg_id = last_msg_id + 1, updated_at = NOW()
		WHERE id = $1 RETURNING last_msg_id
	`, dialogID).Scan(&seq)
	return seq, err
}

func (s *Store) UpdateReadCursor(ctx context.Context, dialogID, userID, lastReadID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE dialog_members SET last_read_id = GREATEST(last_read_id, $3)
		WHERE dialog_id = $1 AND user_id = $2
	`, dialogID, userID, lastReadID)
	return err
}

// User 查询
type User struct {
	ID       int64
	Username string
	Nickname string
	IsBot    bool
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, nickname, is_bot FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Username, &u.Nickname, &u.IsBot)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) IncrementPts(ctx context.Context, userID int64) (int64, error) {
	var pts int64
	err := s.pool.QueryRow(ctx, `
		UPDATE users SET pts = pts + 1 WHERE id = $1 RETURNING pts
	`, userID).Scan(&pts)
	return pts, err
}
