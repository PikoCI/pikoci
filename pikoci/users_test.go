package pikoci_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
	"go.uber.org/mock/gomock"
)

func TestCreateUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Create(ctx, gomock.Any()).Return(uint32(1), nil)

	u, err := s.S.CreateUser(ctx, user.User{Username: "admin", Password: "secret"}, false)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, uint32(1), u.ID)
	assert.Equal(t, "admin", u.Username)
	// Password should be hashed
	assert.NotEqual(t, "secret", u.Password)
	assert.True(t, utils.CheckPasswordHash("secret", u.Password))
}

func TestCreateUser_Hashed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash, _ := utils.HashPassword("secret")

	s.Users.EXPECT().Create(ctx, gomock.Any()).Return(uint32(2), nil)

	u, err := s.S.CreateUser(ctx, user.User{Username: "admin", Password: hash}, true)
	require.NoError(t, err)
	assert.Equal(t, hash, u.Password)
}

func TestCreateOrUpdateUser_CreatesNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "newuser").Return(nil, fmt.Errorf("not found"))
	s.Users.EXPECT().Create(ctx, gomock.Any()).Return(uint32(1), nil)

	u, err := s.P.CreateOrUpdateUser(ctx, user.User{Username: "newuser", Password: "secret"}, false)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, uint32(1), u.ID)
	assert.Equal(t, "newuser", u.Username)
	assert.True(t, utils.CheckPasswordHash("secret", u.Password))
}

func TestCreateOrUpdateUser_UpdatesExistingWithDefaultPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// User still has the default admin123 hash — should be updated
	existing := &user.User{ID: 1, Username: "admin", Password: "$2a$14$FoV/2Z0CRgQyiDJLMcErd.cC/DtWCKMWtxZEaL6HQd/rrtU2DZpAu", FullName: "Admin", Admin: true}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).Return(nil)

	newHash, _ := utils.HashPassword("newsecret")
	u, err := s.P.CreateOrUpdateUser(ctx, user.User{Username: "admin", Password: newHash}, true)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), u.ID)
	assert.Equal(t, newHash, u.Password)
	assert.True(t, u.Admin)
	assert.Equal(t, "Admin", u.FullName)
}

func TestCreateOrUpdateUser_SkipsExistingWithChangedPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// User already changed their password — should NOT be updated
	customHash, _ := utils.HashPassword("custom-password")
	existing := &user.User{ID: 1, Username: "admin", Password: customHash, FullName: "Admin", Admin: true}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)

	newHash, _ := utils.HashPassword("newsecret")
	u, err := s.P.CreateOrUpdateUser(ctx, user.User{Username: "admin", Password: newHash}, true)
	require.NoError(t, err)
	// Password should NOT be changed
	assert.Equal(t, customHash, u.Password)
}

func TestCreateUser_InvalidUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateUser(ctx, user.User{Username: "INVALID USER", Password: "secret"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Username format")
}

func TestCreateUser_EmptyPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateUser(ctx, user.User{Username: "admin", Password: ""}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid empty Password")
}

func TestGetUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin"},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(expected, nil)

	u, err := s.S.GetUser(ctx, "admin")
	require.NoError(t, err)
	assert.Equal(t, expected, u)
}

func TestGetUser_InvalidUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetUser(ctx, "INVALID USER")
	require.Error(t, err)
}

func TestListUsers(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := []*user.User{{ID: 1, Username: "admin"}, {ID: 2, Username: "pepito"}}
	s.Users.EXPECT().Filter(ctx).Return(expected, nil)

	us, err := s.S.ListUsers(ctx)
	require.NoError(t, err)
	assert.Equal(t, expected, us)
}

func TestUserLogin(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash, _ := utils.HashPassword("secret")
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: hash},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	u, jwt, err := s.S.UserLogin(ctx, "admin", "secret")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.NotEmpty(t, jwt)
	assert.Equal(t, "admin", u.Username)
}

func TestUserLogin_WrongPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash, _ := utils.HashPassword("secret")
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: hash},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	_, _, err := s.S.UserLogin(ctx, "admin", "wrong")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong")
}

func TestUserLogin_DefaultPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Use the actual default admin123 hash from migration
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: "$2a$14$FoV/2Z0CRgQyiDJLMcErd.cC/DtWCKMWtxZEaL6HQd/rrtU2DZpAu"},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	u, jwt, err := s.S.UserLogin(ctx, "admin", "admin123")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.NotEmpty(t, jwt)
	assert.True(t, u.MustChangePassword)
}

func TestUserLogin_DefaultPassword_OtherUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// A non-admin user with the same hash should NOT be flagged
	um := &user.WithMemberships{
		User: user.User{ID: 2, Username: "pepito", Password: "$2a$14$FoV/2Z0CRgQyiDJLMcErd.cC/DtWCKMWtxZEaL6HQd/rrtU2DZpAu"},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "pepito").Return(um, nil)

	u, jwt, err := s.S.UserLogin(ctx, "pepito", "admin123")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.NotEmpty(t, jwt)
	assert.False(t, u.MustChangePassword)
}

func TestUpdateUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	existing := &user.User{ID: 1, Username: "admin", FullName: "Admin", Admin: true, Password: "old-hash"}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).Return(nil)

	u, err := s.S.UpdateUser(ctx, "admin", user.User{
		FullName: "New Name",
		Admin:    true,
	}, false)
	require.NoError(t, err)
	assert.Equal(t, "New Name", u.FullName)
	assert.True(t, u.Admin)
}

func TestUpdateUser_LastAdmin(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	existing := &user.User{ID: 1, Username: "admin", Admin: true, Password: "hash"}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Filter(ctx).Return([]*user.User{
		{ID: 1, Username: "admin", Admin: true},
	}, nil)

	_, err := s.S.UpdateUser(ctx, "admin", user.User{Admin: false}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot demote the last admin")
}

func TestDeleteUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	existing := &user.User{ID: 2, Username: "pepito", Admin: false}
	s.Users.EXPECT().Find(ctx, "pepito").Return(existing, nil)
	s.Users.EXPECT().Delete(ctx, "pepito").Return(nil)

	err := s.S.DeleteUser(ctx, "pepito")
	require.NoError(t, err)
}

func TestDeleteUser_LastAdmin(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	existing := &user.User{ID: 1, Username: "admin", Admin: true}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Filter(ctx).Return([]*user.User{
		{ID: 1, Username: "admin", Admin: true},
	}, nil)

	err := s.S.DeleteUser(ctx, "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete the last admin")
}

func TestChangePassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash, _ := utils.HashPassword("oldpass")
	existing := &user.User{ID: 1, Username: "admin", Password: hash}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).Return(nil)

	err := s.S.ChangePassword(ctx, "admin", "oldpass", "newpass")
	require.NoError(t, err)
}

func TestChangePassword_WrongOld(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash, _ := utils.HashPassword("oldpass")
	existing := &user.User{ID: 1, Username: "admin", Password: hash}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)

	err := s.S.ChangePassword(ctx, "admin", "wrongpass", "newpass")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "current password is incorrect")
}

func TestUpdateProfile(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	existing := &user.User{ID: 1, Username: "admin", FullName: "Admin", Password: "hash"}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).Return(nil)

	u, err := s.S.UpdateProfile(ctx, "admin", "New Name", "admin")
	require.NoError(t, err)
	assert.Equal(t, "New Name", u.FullName)
}
