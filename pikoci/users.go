package pikoci

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// defaultAdminUsername and defaultAdmin123Hash identify the migration-seeded
// default user (admin / admin123). Both must match to trigger force-password-change.
const defaultAdminUsername = "admin"
const defaultAdmin123Hash = "$2a$14$FoV/2Z0CRgQyiDJLMcErd.cC/DtWCKMWtxZEaL6HQd/rrtU2DZpAu"

// UserLogin authenticates a user by username and password. On success, it returns
// the user with team memberships and a signed JWT token. If the user is the
// migration-seeded admin with the default password, the MustChangePassword flag
// is set.
func (q *PikoCI) UserLogin(ctx context.Context, un, pass string) (*user.WithMemberships, string, error) {
	// Check if local auth is enabled
	if q.OAuthProviders != nil {
		settings, err := q.OAuthProviders.GetAuthSettings(ctx)
		if err == nil && !settings.LocalAuthEnabled {
			return nil, "", fmt.Errorf("local authentication is disabled")
		}
	}

	if !utils.ValidateCanonical(un) {
		return nil, "", fmt.Errorf("invalid Username format %q", un)
	}
	um, err := q.Users.FindWithMemberships(ctx, un)
	if err != nil {
		return nil, "", fmt.Errorf("failed to Find User: %w", err)
	}

	ok := utils.CheckPasswordHash(pass, um.Password)
	if !ok {
		return nil, "", fmt.Errorf("username or password is wrong")
	}

	// Flag if user is the migration-seeded admin with the default password
	if um.Username == defaultAdminUsername && um.Password == defaultAdmin123Hash {
		um.MustChangePassword = true
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": um,
	})
	tokenString, err := token.SignedString(q.JWTSecret)
	if err != nil {
		return nil, "", fmt.Errorf("failed to Find User: %w", err)
	}

	return um, tokenString, nil
}

// RefreshToken generates a new JWT token for the given username without
// requiring the password. It returns the updated user data and the new token.
func (q *PikoCI) RefreshToken(ctx context.Context, un string) (*user.WithMemberships, string, error) {
	if !utils.ValidateCanonical(un) {
		return nil, "", fmt.Errorf("invalid Username format %q", un)
	}
	um, err := q.Users.FindWithMemberships(ctx, un)
	if err != nil {
		return nil, "", fmt.Errorf("failed to Find User: %w", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": um,
	})
	tokenString, err := token.SignedString(q.JWTSecret)
	if err != nil {
		return nil, "", fmt.Errorf("failed to sign token: %w", err)
	}

	return um, tokenString, nil
}

// GetUser retrieves a user and their team memberships by username.
func (q *PikoCI) GetUser(ctx context.Context, un string) (*user.WithMemberships, error) {
	if !utils.ValidateCanonical(un) {
		return nil, fmt.Errorf("invalid Username format %q", un)
	}

	um, err := q.Users.FindWithMemberships(ctx, un)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	return um, nil
}

// CreateUser creates a new user. If isHash is true, the password is stored
// directly; otherwise it is hashed with bcrypt before storage.
func (q *PikoCI) CreateUser(ctx context.Context, u user.User, isHash bool) (*user.User, error) {
	if !utils.ValidateCanonical(u.Username) {
		return nil, fmt.Errorf("invalid Username format %q", u.Username)
	} else if u.Password == "" {
		return nil, fmt.Errorf("invalid empty Password")
	}

	if !isHash {
		hash, err := utils.HashPassword(u.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to hash Passowrd: %w", err)
		}
		u.Password = hash
	}

	id, err := q.Users.Create(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("failed to Create User: %w", err)
	}
	u.ID = id

	return &u, nil
}

// CreateOrUpdateUser creates a user if they don't exist, or updates their password
// only if they still have the migration-seeded default password (admin123).
// This is only intended for startup user seeding (--users flag), not for the HTTP API.
// Users who have already changed their password via the UI/CLI are not modified.
func (q *PikoCI) CreateOrUpdateUser(ctx context.Context, u user.User, isHash bool) (*user.User, error) {
	if !utils.ValidateCanonical(u.Username) {
		return nil, fmt.Errorf("invalid Username format %q", u.Username)
	} else if u.Password == "" {
		return nil, fmt.Errorf("invalid empty Password")
	}

	if !isHash {
		hash, err := utils.HashPassword(u.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to hash Passowrd: %w", err)
		}
		u.Password = hash
	}

	existing, err := q.Users.Find(ctx, u.Username)
	if err == nil && existing != nil {
		// Only update if this is the migration-seeded admin with the default password.
		// This allows --users to set the admin password on first setup but
		// won't overwrite passwords changed via the UI/CLI.
		if !(existing.Username == defaultAdminUsername && existing.Password == defaultAdmin123Hash) {
			return existing, nil
		}
		existing.Password = u.Password
		if u.FullName != "" {
			existing.FullName = u.FullName
		}
		err = q.Users.Update(ctx, u.Username, *existing)
		if err != nil {
			return nil, fmt.Errorf("failed to Update User: %w", err)
		}
		return existing, nil
	}

	id, err := q.Users.Create(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("failed to Create User: %w", err)
	}
	u.ID = id

	return &u, nil
}

// ListUsers returns all registered users.
func (q *PikoCI) ListUsers(ctx context.Context) ([]*user.User, error) {
	us, err := q.Users.Filter(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to Find User: %w", err)
	}

	return us, nil
}

// UpdateUser updates an existing user identified by username. It merges the
// provided fields into the existing record and prevents demoting the last admin.
func (q *PikoCI) UpdateUser(ctx context.Context, un string, u user.User, isHash bool) (*user.User, error) {
	existing, err := q.Users.Find(ctx, un)
	if err != nil {
		return nil, fmt.Errorf("failed to Find User: %w", err)
	}

	if u.FullName != "" {
		existing.FullName = u.FullName
	}
	if u.Username != "" && u.Username != un {
		if !utils.ValidateCanonical(u.Username) {
			return nil, fmt.Errorf("invalid Username format %q", u.Username)
		}
		existing.Username = u.Username
	}
	if u.Password != "" {
		if !isHash {
			hash, err := utils.HashPassword(u.Password)
			if err != nil {
				return nil, fmt.Errorf("failed to hash Password: %w", err)
			}
			existing.Password = hash
		} else {
			existing.Password = u.Password
		}
	}

	// Prevent demoting the last admin
	if existing.Admin && !u.Admin {
		all, err := q.Users.Filter(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list users: %w", err)
		}
		adminCount := 0
		for _, a := range all {
			if a.Admin {
				adminCount++
			}
		}
		if adminCount <= 1 {
			return nil, fmt.Errorf("cannot demote the last admin user")
		}
	}
	existing.Admin = u.Admin

	err = q.Users.Update(ctx, un, *existing)
	if err != nil {
		return nil, fmt.Errorf("failed to Update User: %w", err)
	}

	return existing, nil
}

// DeleteUser removes a user by username. It prevents deleting the last admin user.
func (q *PikoCI) DeleteUser(ctx context.Context, un string) error {
	if !utils.ValidateCanonical(un) {
		return fmt.Errorf("invalid Username format %q", un)
	}

	existing, err := q.Users.Find(ctx, un)
	if err != nil {
		return fmt.Errorf("failed to Find User: %w", err)
	}

	if existing.Admin {
		all, err := q.Users.Filter(ctx)
		if err != nil {
			return fmt.Errorf("failed to list users: %w", err)
		}
		adminCount := 0
		for _, a := range all {
			if a.Admin {
				adminCount++
			}
		}
		if adminCount <= 1 {
			return fmt.Errorf("cannot delete the last admin user")
		}
	}

	err = q.Users.Delete(ctx, un)
	if err != nil {
		return fmt.Errorf("failed to Delete User: %w", err)
	}

	return nil
}

// ChangePassword updates the password for the given user after verifying the
// old password matches the stored hash.
func (q *PikoCI) ChangePassword(ctx context.Context, un, oldPassword, newPassword string) error {
	if !utils.ValidateCanonical(un) {
		return fmt.Errorf("invalid Username format %q", un)
	}

	existing, err := q.Users.Find(ctx, un)
	if err != nil {
		return fmt.Errorf("failed to Find User: %w", err)
	}

	// If old password is empty, allow it only for OAuth-only users (they have
	// a random password they don't know). Check if the user has OAuth links.
	if oldPassword == "" {
		if q.OAuthProviders != nil {
			links, err := q.OAuthProviders.FindUserLinksByUser(ctx, existing.ID)
			if err != nil || len(links) == 0 {
				return fmt.Errorf("current password is required")
			}
			// OAuth user setting their first password — allowed
		} else {
			return fmt.Errorf("current password is required")
		}
	} else if !utils.CheckPasswordHash(oldPassword, existing.Password) {
		return fmt.Errorf("current password is incorrect")
	}

	if newPassword == "" {
		return fmt.Errorf("new password cannot be empty")
	}

	hash, err := utils.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash Password: %w", err)
	}
	existing.Password = hash

	err = q.Users.Update(ctx, un, *existing)
	if err != nil {
		return fmt.Errorf("failed to Update User: %w", err)
	}

	return nil
}

// UpdateProfile updates a user's display name and optionally their username.
// Empty values for fullName or newUsername are ignored.
func (q *PikoCI) UpdateProfile(ctx context.Context, un string, fullName, newUsername string) (*user.User, error) {
	if !utils.ValidateCanonical(un) {
		return nil, fmt.Errorf("invalid Username format %q", un)
	}

	existing, err := q.Users.Find(ctx, un)
	if err != nil {
		return nil, fmt.Errorf("failed to Find User: %w", err)
	}

	if fullName != "" {
		existing.FullName = fullName
	}
	if newUsername != "" && newUsername != un {
		if !utils.ValidateCanonical(newUsername) {
			return nil, fmt.Errorf("invalid Username format %q", newUsername)
		}
		existing.Username = newUsername
	}

	err = q.Users.Update(ctx, un, *existing)
	if err != nil {
		return nil, fmt.Errorf("failed to Update User: %w", err)
	}

	return existing, nil
}
