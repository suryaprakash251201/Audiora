package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the access-token payload. The token is deliberately minimal:
// role and identity only, so that revoking a session is a matter of
// dropping the refresh token rather than waiting for expiry.
type Claims struct {
	UserID int64  `json:"uid"`
	Email  string `json:"email"`
	Admin  bool   `json:"adm"`
	jwt.RegisteredClaims
}

// TokenIssuer signs and verifies access tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenIssuer(secret []byte, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: secret, ttl: ttl}
}

// Issue returns a signed access token for a user.
func (t *TokenIssuer) Issue(userID int64, email string, admin bool) (string, time.Time, error) {
	expiry := time.Now().Add(t.ttl)
	claims := Claims{
		UserID: userID,
		Email:  email,
		Admin:  admin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiry),
			Issuer:    "audiora",
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiry, nil
}

// Parse validates an access token and returns its claims.
func (t *TokenIssuer) Parse(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(tok *jwt.Token) (any, error) {
		// Pin the algorithm. Without this check a token signed with "none"
		// or an asymmetric algorithm would be accepted.
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", tok.Header["alg"])
		}
		return t.secret, nil
	}, jwt.WithIssuer("audiora"), jwt.WithValidMethods([]string{"HS256"}))

	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// NewRefreshToken returns an opaque token and the hash to store. Only the
// hash is persisted, so a database leak cannot be replayed as a session.
func NewRefreshToken() (token string, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken is the one-way function applied before storage.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
