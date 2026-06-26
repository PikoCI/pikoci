package apitoken

import "context"

//go:generate go tool mockgen -destination=../mock/apitoken_repository.go -mock_names=Repository=ApiTokenRepository -package mock github.com/pikoci/pikoci/pikoci/apitoken Repository

// Repository defines the persistence operations for API tokens.
type Repository interface {
	Create(ctx context.Context, t Token, tokenHash string) (uint32, error)
	FindByHash(ctx context.Context, tokenHash string) (*AuthResult, error)
	Filter(ctx context.Context, username string) ([]*Token, error)
	Delete(ctx context.Context, username string, tokenID uint32) error
	DeleteByTeamMember(ctx context.Context, username, teamCanonical string) error
	UpdateLastUsed(ctx context.Context, tokenID uint32) error
}
