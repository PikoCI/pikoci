package pikoci

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// GenerateTeamWorkerToken creates (or regenerates) a team-scoped worker token.
// It generates a new UUID salt, stores it on the team, and returns a signed JWT.
func (q *PikoCI) GenerateTeamWorkerToken(ctx context.Context, tc string) (string, error) {
	salt := uuid.New().String()

	if err := q.Teams.UpdateWorkerTokenSalt(ctx, tc, salt); err != nil {
		return "", fmt.Errorf("failed to store worker token salt: %w", err)
	}

	tokenStr := signTeamWorkerJWT(q.JWTSecret, tc, salt)

	q.audit(ctx, tc, "worker_token.regenerated", "team", tc, nil)

	return tokenStr, nil
}

// GetTeamWorkerToken returns the current team worker token, or empty string
// if no token has been generated yet.
func (q *PikoCI) GetTeamWorkerToken(ctx context.Context, tc string) (string, error) {
	salt, err := q.Teams.FindWorkerTokenSalt(ctx, tc)
	if err != nil {
		return "", fmt.Errorf("failed to find worker token salt: %w", err)
	}
	if salt == "" {
		return "", nil
	}
	return signTeamWorkerJWT(q.JWTSecret, tc, salt), nil
}

func signTeamWorkerJWT(jwtSecret []byte, tc, salt string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": tc,
		"salt":           salt,
	})
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		panic(err)
	}
	return tokenString
}
