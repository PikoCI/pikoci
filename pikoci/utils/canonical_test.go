package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestCanonicalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Team", "my-team"},
		{"Hello World!", "hello-world"},
		{"already-canonical", "already-canonical"},
		{"UPPER CASE", "upper-case"},
		{"  spaces  ", "spaces"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, utils.Canonicalize(tt.input))
		})
	}
}

func TestValidateCanonical(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"my-team", true},
		{"valid-123", true},
		{"", false},
		{"Has Spaces", false},
		{"UPPER", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, utils.ValidateCanonical(tt.input))
		})
	}
}

func TestValidateResourceCanonical(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"git.my-repo", true},
		{"cron.my-cron", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, utils.ValidateResourceCanonical(tt.input))
		})
	}
}

func TestResourceCanonical(t *testing.T) {
	assert.Equal(t, "git.my-repo", utils.ResourceCanonical("git", "my-repo"))
	assert.Equal(t, "cron.timer", utils.ResourceCanonical("cron", "timer"))
}
