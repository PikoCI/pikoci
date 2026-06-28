package role_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/role"
)

func TestLevel(t *testing.T) {
	tests := []struct {
		role  role.Role
		level int
	}{
		{role.Public, 0},
		{role.Read, 1},
		{role.Write, 2},
		{role.Maintain, 3},
		{role.Admin, 4},
		{role.Role("invalid"), -1},
		{role.Role(""), -1},
		{role.Role("owner"), -1},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.level, tt.role.Level(), "role=%q", tt.role)
	}
}

func TestAtLeast(t *testing.T) {
	assert.True(t, role.Admin.AtLeast(role.Public))
	assert.True(t, role.Admin.AtLeast(role.Admin))
	assert.True(t, role.Read.AtLeast(role.Read))
	assert.True(t, role.Read.AtLeast(role.Public))
	assert.False(t, role.Read.AtLeast(role.Write))
	assert.False(t, role.Public.AtLeast(role.Read))
	assert.False(t, role.Role("invalid").AtLeast(role.Public))
}

func TestValid(t *testing.T) {
	for _, r := range []role.Role{role.Public, role.Read, role.Write, role.Maintain, role.Admin} {
		assert.True(t, r.Valid(), "role=%q", r)
	}
	assert.False(t, role.Role("invalid").Valid())
	assert.False(t, role.Role("").Valid())
	assert.False(t, role.Role("owner").Valid())
}

func TestAssignable(t *testing.T) {
	assert.False(t, role.Public.Assignable())
	assert.True(t, role.Read.Assignable())
	assert.True(t, role.Write.Assignable())
	assert.True(t, role.Maintain.Assignable())
	assert.True(t, role.Admin.Assignable())
	assert.False(t, role.Role("invalid").Assignable())
	assert.False(t, role.Role("owner").Assignable())
}
