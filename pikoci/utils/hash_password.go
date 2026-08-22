package utils

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost is the bcrypt work factor used by HashPassword.
// Override to bcrypt.MinCost in tests for fast hashing.
var BcryptCost = 14

// HashPassword hashes the given plaintext password using bcrypt with a cost
// factor of BcryptCost and returns the resulting hash string.
func HashPassword(pass string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pass), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to GenerateFromPassword: %w", err)
	}

	return string(bytes), nil
}

// CheckPasswordHash reports whether the plaintext password matches the bcrypt
// hash.
func CheckPasswordHash(pass, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass))
	return err == nil
}
