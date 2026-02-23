package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Username      string `json:"username"`
	IsAdmin       bool   `json:"is_admin"`
	WorkspaceSlug string `json:"workspace_slug"`
	WorkspaceRole string `json:"workspace_role"`
	jwt.RegisteredClaims
}

type TokenService struct {
	signingKey []byte
	expiry     time.Duration
}

func NewTokenService(signingKey []byte, expiry time.Duration) *TokenService {
	return &TokenService{
		signingKey: signingKey,
		expiry:     expiry,
	}
}

func (s *TokenService) Generate(username string, isAdmin bool, workspaceSlug, workspaceRole string) (string, error) {
	now := time.Now()
	claims := Claims{
		Username:      username,
		IsAdmin:       isAdmin,
		WorkspaceSlug: workspaceSlug,
		WorkspaceRole: workspaceRole,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiry)),
			Issuer:    "serverless-platform",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.signingKey)
}

func (s *TokenService) Validate(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(_ *jwt.Token) (any, error) {
		return s.signingKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}
