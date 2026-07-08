package pikoci_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

	u, err := s.S.CreateUser(ctx, user.User{Username: "admin", Password: "secretpw"}, false)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, uint32(1), u.ID)
	assert.Equal(t, "admin", u.Username)
	// Password should be hashed
	assert.NotEqual(t, "secretpw", u.Password)
	assert.True(t, utils.CheckPasswordHash("secretpw", u.Password))
}

func TestCreateUser_Hashed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash := testHashPassword("secretpw")

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

	u, err := s.P.CreateOrUpdateUser(ctx, user.User{Username: "newuser", Password: "secretpw"}, false)
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, uint32(1), u.ID)
	assert.Equal(t, "newuser", u.Username)
	assert.True(t, utils.CheckPasswordHash("secretpw", u.Password))
}

func TestCreateOrUpdateUser_UpdatesExistingWithDefaultPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// User still has the default admin123 hash — should be updated
	existing := &user.User{ID: 1, Username: "admin", Password: "$2a$14$FoV/2Z0CRgQyiDJLMcErd.cC/DtWCKMWtxZEaL6HQd/rrtU2DZpAu", FullName: "Admin", Admin: true}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).Return(nil)

	newHash := testHashPassword("newsecret")
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
	customHash := testHashPassword("custom-password")
	existing := &user.User{ID: 1, Username: "admin", Password: customHash, FullName: "Admin", Admin: true}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)

	newHash := testHashPassword("newsecret")
	u, err := s.P.CreateOrUpdateUser(ctx, user.User{Username: "admin", Password: newHash}, true)
	require.NoError(t, err)
	// Password should NOT be changed
	assert.Equal(t, customHash, u.Password)
}

func TestCreateUser_InvalidUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateUser(ctx, user.User{Username: "INVALID USER", Password: "secretpw"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Username format")
}

func TestCreateUser_EmptyPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateUser(ctx, user.User{Username: "admin", Password: ""}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password must be at least 8 characters")
}

func TestCreateUser_ShortPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateUser(ctx, user.User{Username: "admin", Password: "short"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password must be at least 8 characters")
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

	hash := testHashPassword("secretpw")
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: hash},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	u, jwt, err := s.S.UserLogin(ctx, "admin", "secretpw")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.NotEmpty(t, jwt)
	assert.Equal(t, "admin", u.Username)
}

func TestUserLogin_WrongPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash := testHashPassword("secretpw")
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: hash},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	_, _, err := s.S.UserLogin(ctx, "admin", "wrongpwd")
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

func TestUpdateUser_ShortPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	existing := &user.User{ID: 1, Username: "admin", FullName: "Admin", Admin: true, Password: "old-hash"}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)

	_, err := s.S.UpdateUser(ctx, "admin", user.User{
		Password: "short",
		Admin:    true,
	}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password must be at least 8 characters")
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

	hash := testHashPassword("oldpasswd")
	existing := &user.User{ID: 1, Username: "admin", Password: hash}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).Return(nil)

	err := s.S.ChangePassword(ctx, "admin", "oldpasswd", "newpasswd")
	require.NoError(t, err)
}

func TestChangePassword_WrongOld(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash := testHashPassword("oldpasswd")
	existing := &user.User{ID: 1, Username: "admin", Password: hash}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)

	err := s.S.ChangePassword(ctx, "admin", "wrongpass1", "newpasswd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "current password is incorrect")
}

func TestChangePassword_ShortNewPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash := testHashPassword("oldpasswd")
	existing := &user.User{ID: 1, Username: "admin", Password: hash}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)

	err := s.S.ChangePassword(ctx, "admin", "oldpasswd", "short")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password must be at least 8 characters")
}

func TestRefreshToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin"},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	u, jwt, err := s.S.RefreshToken(ctx, "admin")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.NotEmpty(t, jwt)
	assert.Equal(t, "admin", u.Username)
}

func TestRefreshToken_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, _, err := s.S.RefreshToken(ctx, "INVALID USER")
	require.Error(t, err)
}

func TestRefreshToken_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().FindWithMemberships(ctx, "unknown").Return(nil, assert.AnError)

	_, _, err := s.S.RefreshToken(ctx, "unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to Find User")
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

func TestUserLogin_JWTContainsTokenGen(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash := testHashPassword("secretpw")
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: hash, TokenGen: 5},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	_, tokenString, err := s.S.UserLogin(ctx, "admin", "secretpw")
	require.NoError(t, err)

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte("test-secret"), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	require.NoError(t, err)

	claims := token.Claims.(jwt.MapClaims)
	tg, ok := claims["token_gen"]
	require.True(t, ok, "JWT should contain token_gen claim")
	assert.Equal(t, float64(5), tg)
}

func TestChangePassword_IncrementsTokenGen(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash := testHashPassword("oldpasswd")
	existing := &user.User{ID: 1, Username: "admin", Password: hash, TokenGen: 3}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, u user.User) error {
			assert.Equal(t, uint32(4), u.TokenGen)
			return nil
		},
	)

	err := s.S.ChangePassword(ctx, "admin", "oldpasswd", "newpasswd")
	require.NoError(t, err)
}

func TestUpdateUser_PasswordChangeIncrementsTokenGen(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	existing := &user.User{ID: 1, Username: "admin", FullName: "Admin", Admin: true, Password: "old-hash", TokenGen: 2}
	s.Users.EXPECT().Find(ctx, "admin").Return(existing, nil)
	s.Users.EXPECT().Update(ctx, "admin", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, u user.User) error {
			assert.Equal(t, uint32(3), u.TokenGen)
			return nil
		},
	)

	_, err := s.S.UpdateUser(ctx, "admin", user.User{
		Password: "newpassword",
		Admin:    true,
	}, false)
	require.NoError(t, err)
}

func TestUserLogin_SessionLifetimeAddsExp(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.P.SessionLifetime = 1 * time.Hour

	hash := testHashPassword("secretpw")
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: hash},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	_, tokenString, err := s.S.UserLogin(ctx, "admin", "secretpw")
	require.NoError(t, err)

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte("test-secret"), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	require.NoError(t, err)

	claims := token.Claims.(jwt.MapClaims)
	exp, err := claims.GetExpirationTime()
	require.NoError(t, err)
	require.NotNil(t, exp)
	assert.True(t, exp.After(time.Now()))
	assert.True(t, exp.Before(time.Now().Add(2*time.Hour)))
}

func TestUserLogin_NoSessionLifetimeNoExp(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hash := testHashPassword("secretpw")
	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", Password: hash},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	_, tokenString, err := s.S.UserLogin(ctx, "admin", "secretpw")
	require.NoError(t, err)

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte("test-secret"), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	require.NoError(t, err)

	claims := token.Claims.(jwt.MapClaims)
	_, ok := claims["exp"]
	assert.False(t, ok, "JWT should not contain exp claim when SessionLifetime is 0")
}

func TestRefreshToken_JWTContainsTokenGen(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin", TokenGen: 7},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	_, tokenString, err := s.S.RefreshToken(ctx, "admin")
	require.NoError(t, err)

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte("test-secret"), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	require.NoError(t, err)

	claims := token.Claims.(jwt.MapClaims)
	tg, ok := claims["token_gen"]
	require.True(t, ok, "JWT should contain token_gen claim")
	assert.Equal(t, float64(7), tg)
}

func TestRefreshToken_SessionLifetimeAddsExp(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.P.SessionLifetime = 2 * time.Hour

	um := &user.WithMemberships{
		User: user.User{ID: 1, Username: "admin"},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(um, nil)

	_, tokenString, err := s.S.RefreshToken(ctx, "admin")
	require.NoError(t, err)

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		return []byte("test-secret"), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	require.NoError(t, err)

	claims := token.Claims.(jwt.MapClaims)
	exp, err := claims.GetExpirationTime()
	require.NoError(t, err)
	require.NotNil(t, exp)
	assert.True(t, exp.After(time.Now()))
}
