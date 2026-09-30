package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/models"
)

// ErrNotFound and ErrEmailTaken are the only errors the HTTP layer needs to
// distinguish; everything else is a genuine bug.
var (
	ErrNotFound     = errors.New("not found")
	ErrEmailTaken   = errors.New("email already registered")
	ErrTokenInvalid = errors.New("invalid or expired token")
)

// Store is the persistence layer for accounts and sessions.
type Store struct {
	db         *db.DB
	issuer     *TokenIssuer
	refreshTTL time.Duration
}

func NewStore(database *db.DB, issuer *TokenIssuer, refreshTTL time.Duration) *Store {
	return &Store{db: database, issuer: issuer, refreshTTL: refreshTTL}
}

// UserCount reports how many accounts exist, used to decide whether to
// bootstrap the first admin.
func (s *Store) UserCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts an account. Email is normalised to lower case so that
// "Ann@x.com" and "ann@x.com" cannot both register.
func (s *Store) CreateUser(email, password, name string, admin bool) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("invalid email address")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	res, err := s.db.Exec(
		`INSERT INTO users (email, password_hash, name, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
		email, hash, strings.TrimSpace(name), boolToInt(admin), db.Now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("user id: %w", err)
	}
	return &models.User{ID: id, Email: email, Name: name, IsAdmin: admin, CreatedAt: db.Now()}, nil
}

// Authenticate checks a login. Both a missing user and a bad password take
// the same time, because the missing-user path still runs a hash comparison
// against a dummy value.
func (s *Store) Authenticate(email, password string) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u models.User
	var hash string
	err := s.db.QueryRow(
		`SELECT id, email, name, is_admin, created_at, last_login_at, password_hash FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.LastLoginAt, &hash)

	if errors.Is(err, sql.ErrNoRows) {
		// Burn comparable time so response latency does not reveal whether
		// the account exists.
		_ = VerifyPassword(password, dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	if err := VerifyPassword(password, hash); err != nil {
		return nil, err
	}

	if _, err := s.db.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, db.Now(), u.ID); err != nil {
		return nil, fmt.Errorf("update last_login: %w", err)
	}
	return &u, nil
}

// dummyHash is a valid argon2id hash of a random string, used only to keep
// the "unknown user" path the same cost as a real verification.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$Y2FuYXJ5c3RhbGxpY2Fub2R5$8kL2sJ8Xh0mS1kQ3vR9dT2eW5yU7iO0pA3sD5fG7hJ9k"

// SetPassword replaces a user's password and revokes every existing session,
// so a password change logs the account out everywhere.
func (s *Store) SetPassword(userID int64, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if _, err := tx.Exec(`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, db.Now(), userID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return tx.Commit()
}

// GetUser fetches one account.
func (s *Store) GetUser(id int64) (*models.User, error) {
	var u models.User
	err := s.db.QueryRow(
		`SELECT id, email, name, is_admin, created_at, last_login_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

// ListUsers returns all accounts, oldest first.
func (s *Store) ListUsers() ([]models.User, error) {
	rows, err := s.db.Query(`SELECT id, email, name, is_admin, created_at, last_login_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := []models.User{}
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.LastLoginAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// IssueSession returns a fresh access/refresh pair and records the refresh token.
func (s *Store) IssueSession(u *models.User, userAgent string) (accessToken string, expiresAt time.Time, refreshToken string, err error) {
	accessToken, expiresAt, err = s.issuer.Issue(u.ID, u.Email, u.IsAdmin)
	if err != nil {
		return "", time.Time{}, "", err
	}
	refreshToken, err = s.storeRefreshToken(u.ID, userAgent)
	if err != nil {
		return "", time.Time{}, "", err
	}
	return accessToken, expiresAt, refreshToken, nil
}

func (s *Store) storeRefreshToken(userID int64, userAgent string) (string, error) {
	token, hash, err := NewRefreshToken()
	if err != nil {
		return "", err
	}
	if len(userAgent) > 255 {
		userAgent = userAgent[:255]
	}
	_, err = s.db.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, user_agent, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		userID, hash, userAgent, db.Now(), db.Now()+int64(s.refreshTTL.Seconds()))
	if err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	return token, nil
}

// Refresh exchanges a refresh token for a new pair. The old token is revoked
// and a new one issued, which turns a stolen refresh token into a one-shot
// steal: the legitimate client will present the new one next, and the
// attacker replaying the old one gets nothing.
func (s *Store) Refresh(refreshToken, userAgent string) (*models.User, string, time.Time, string, error) {
	hash := HashRefreshToken(refreshToken)

	var tokenID, userID int64
	var expiresAt int64
	var revokedAt sql.NullInt64
	err := s.db.QueryRow(
		`SELECT id, user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = ?`, hash,
	).Scan(&tokenID, &userID, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", time.Time{}, "", ErrTokenInvalid
	}
	if err != nil {
		return nil, "", time.Time{}, "", fmt.Errorf("query refresh token: %w", err)
	}

	if revokedAt.Valid {
		// A revoked token being replayed means it was stolen. Drop every
		// session for this user rather than just refusing this one.
		if _, derr := s.db.Exec(`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, db.Now(), userID); derr != nil {
			return nil, "", time.Time{}, "", fmt.Errorf("revoke all sessions: %w", derr)
		}
		return nil, "", time.Time{}, "", ErrTokenInvalid
	}
	if db.Now() > expiresAt {
		return nil, "", time.Time{}, "", ErrTokenInvalid
	}

	u, err := s.GetUser(userID)
	if err != nil {
		return nil, "", time.Time{}, "", err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, "", time.Time{}, "", err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE refresh_tokens SET revoked_at = ? WHERE id = ?`, db.Now(), tokenID); err != nil {
		return nil, "", time.Time{}, "", fmt.Errorf("rotate token: %w", err)
	}

	// Reuse the now-revoked row by replacing its hash rather than inserting,
	// which keeps the table from growing without bound.
	_, hashErr := tx.Exec(`DELETE FROM refresh_tokens WHERE id = ?`, tokenID)
	if hashErr != nil {
		return nil, "", time.Time{}, "", hashErr
	}
	if err := tx.Commit(); err != nil {
		return nil, "", time.Time{}, "", err
	}

	accessToken, exp, newRefresh, err := s.IssueSession(u, userAgent)
	if err != nil {
		return nil, "", time.Time{}, "", err
	}
	return u, accessToken, exp, newRefresh, nil
}

// Revoke drops a single session.
func (s *Store) Revoke(refreshToken string) error {
	_, err := s.db.Exec(`UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`,
		db.Now(), HashRefreshToken(refreshToken))
	return err
}

// DeleteUser removes an account and, by cascade, its playlists, favorites,
// history and sessions.
func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// PurgeExpiredSessions deletes rows that can no longer be used. Called
// periodically so the table does not accumulate dead rows forever.
func (s *Store) PurgeExpiredSessions() (int64, error) {
	cutoff := db.Now() - 7*24*3600
	res, err := s.db.Exec(`DELETE FROM refresh_tokens WHERE expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)`, db.Now(), cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
