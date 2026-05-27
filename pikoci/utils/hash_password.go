package utils

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes the given plaintext password using bcrypt with a cost
// factor of 14 and returns the resulting hash string.
func HashPassword(pass string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pass), 14)
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
