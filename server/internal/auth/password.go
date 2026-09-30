package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. Tuned for a login on a small VPS: 64 MiB and 3
// passes costs a brute-force attacker meaningfully while keeping our own
// login well under 100ms.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// ErrInvalidCredentials is returned for any login failure. It deliberately
// does not distinguish "no such user" from "wrong password", so the response
// cannot be used to enumerate accounts.
var ErrInvalidCredentials = errors.New("invalid email or password")

// HashPassword returns a self-describing argon2id hash, safe to store as-is.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks a password against an encoded argon2id hash.
// The comparison is constant time to avoid leaking the hash byte-by-byte.
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("malformed password hash")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return errors.New("malformed password hash version")
	}
	if version != argon2.Version {
		return fmt.Errorf("unsupported argon2 version %d", version)
	}

	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return errors.New("malformed argon2 parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return errors.New("malformed salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return errors.New("malformed key")
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}
