package utils_test

import (
	"testing"

	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/stretchr/testify/assert"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pass    string
		wantErr bool
	}{
		{"empty", "", true},
		{"7 chars", "1234567", true},
		{"8 chars", "12345678", false},
		{"long password", "this-is-a-very-long-password-that-should-be-fine", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := utils.ValidatePassword(tt.pass)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "password must be at least 8 characters")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
