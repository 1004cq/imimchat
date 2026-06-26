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
	IsSecret    bool
}

func (s *Store) InsertMessage(ctx context.Context, msg *Message) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO messages (dialog_id, sender_id, msg_type, content, content_text, client_msg_id, seq, ttl_seconds, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		RETURNING id, created_at
	`, msg.DialogID, msg.SenderID, msg.MsgType, msg.Content, msg.ContentText, msg.ClientMsgID, msg.Seq, msg.TTLSeconds).
		Scan(&msg.ID, &msg.CreatedAt)
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

func (s *Store) GetPeerUserID(ctx context.Context, dialogID, userID int64) (int64, error) {
	var peerID int64
	err := s.pool.QueryRow(ctx, `
		SELECT user_id FROM dialog_members
		WHERE dialog_id = $1 AND user_id <> $2
		LIMIT 1
	`, dialogID, userID).Scan(&peerID)
	return peerID, err
}

func (s *Store) GetMessageBySeq(ctx context.Context, dialogID, seq int64) (*Message, error) {
	m := &Message{}
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.dialog_id, m.sender_id, m.msg_type, m.content, m.content_text, m.seq, m.created_at, d.is_secret
		FROM messages m
		JOIN dialogs d ON d.id = m.dialog_id
		WHERE m.dialog_id = $1 AND m.seq = $2
		LIMIT 1
	`, dialogID, seq).Scan(&m.ID, &m.DialogID, &m.SenderID, &m.MsgType, &m.Content, &m.ContentText, &m.Seq, &m.CreatedAt, &m.IsSecret)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Store) ListMessagesForSync(ctx context.Context, userID, chatID, lastSeq int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var (
		rows interface {
			Next() bool
			Scan(dest ...any) error
			Close()
			Err() error
		}
		err error
	)

	if chatID == 0 {
		rows, err = s.pool.Query(ctx, `
			SELECT m.id, m.dialog_id, m.sender_id, m.msg_type, m.content, m.content_text, m.seq, m.created_at, d.is_secret
			FROM messages m
			JOIN dialog_members dm ON dm.dialog_id = m.dialog_id AND dm.user_id = $1
			JOIN dialogs d ON d.id = m.dialog_id
			WHERE m.seq > $2
			ORDER BY m.seq ASC
			LIMIT $3
		`, userID, lastSeq, limit)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT m.id, m.dialog_id, m.sender_id, m.msg_type, m.content, m.content_text, m.seq, m.created_at, d.is_secret
			FROM messages m
			JOIN dialogs d ON d.id = m.dialog_id
			WHERE m.dialog_id = $1 AND m.seq > $2
			ORDER BY m.seq ASC
			LIMIT $3
		`, chatID, lastSeq, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.DialogID, &m.SenderID, &m.MsgType, &m.Content, &m.ContentText, &m.Seq, &m.CreatedAt, &m.IsSecret); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
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
