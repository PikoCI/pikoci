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
		{role.Viewer, 1},
		{role.Operator, 2},
		{role.Maintainer, 3},
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
	assert.True(t, role.Viewer.AtLeast(role.Viewer))
	assert.True(t, role.Viewer.AtLeast(role.Public))
	assert.False(t, role.Viewer.AtLeast(role.Operator))
	assert.False(t, role.Public.AtLeast(role.Viewer))
	assert.False(t, role.Role("invalid").AtLeast(role.Public))
}

func TestValid(t *testing.T) {
	for _, r := range []role.Role{role.Public, role.Viewer, role.Operator, role.Maintainer, role.Admin} {
		assert.True(t, r.Valid(), "role=%q", r)
	}
	assert.False(t, role.Role("invalid").Valid())
	assert.False(t, role.Role("").Valid())
	assert.False(t, role.Role("owner").Valid())
}

func TestAssignable(t *testing.T) {
	assert.False(t, role.Public.Assignable())
	assert.True(t, role.Viewer.Assignable())
	assert.True(t, role.Operator.Assignable())
	assert.True(t, role.Maintainer.Assignable())
	assert.True(t, role.Admin.Assignable())
	assert.False(t, role.Role("invalid").Assignable())
	assert.False(t, role.Role("owner").Assignable())
}
