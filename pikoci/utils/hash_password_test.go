package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestHashPassword(t *testing.T) {
	hash, err := utils.HashPassword("mypassword")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, "mypassword", hash)
}

func TestCheckPasswordHash(t *testing.T) {
	hash, err := utils.HashPassword("mypassword")
	require.NoError(t, err)

	assert.True(t, utils.CheckPasswordHash("mypassword", hash))
	assert.False(t, utils.CheckPasswordHash("wrongpassword", hash))
	assert.False(t, utils.CheckPasswordHash("mypassword", "invalidhash"))
}
