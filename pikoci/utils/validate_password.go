package utils

import "fmt"

// MinPasswordLength is the minimum number of characters required for a password.
const MinPasswordLength = 8

// ValidatePassword checks that a password meets minimum strength requirements.
func ValidatePassword(pass string) error {
	if len(pass) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	return nil
}
