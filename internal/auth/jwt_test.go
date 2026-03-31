package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"serverless-platform/internal/auth"
)

var testSigningKey = []byte("test-signing-key-32-bytes-long!!")

func TestTokenService_GenerateAndValidate(t *testing.T) {
	svc := auth.NewTokenService(testSigningKey, time.Hour)

	tests := []struct {
		name          string
		username      string
		isAdmin       bool
		workspaceSlug string
		workspaceRole string
	}{
		{
			name:          "admin user without workspace",
			username:      "admin",
			isAdmin:       true,
			workspaceSlug: "",
			workspaceRole: "",
		},
		{
			name:          "regular user with workspace",
			username:      "alice",
			isAdmin:       false,
			workspaceSlug: "my-workspace",
			workspaceRole: "owner",
		},
		{
			name:          "member role",
			username:      "bob",
			isAdmin:       false,
			workspaceSlug: "team-ws",
			workspaceRole: "member",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := svc.Generate(tt.username, tt.isAdmin, tt.workspaceSlug, tt.workspaceRole)
			require.NoError(t, err)
			assert.NotEmpty(t, token)

			claims, err := svc.Validate(token)
			require.NoError(t, err)
			assert.Equal(t, tt.username, claims.Username)
			assert.Equal(t, tt.isAdmin, claims.IsAdmin)
			assert.Equal(t, tt.workspaceSlug, claims.WorkspaceSlug)
			assert.Equal(t, tt.workspaceRole, claims.WorkspaceRole)
			assert.Equal(t, "serverless-platform", claims.Issuer)
		})
	}
}

func TestTokenService_ExpiredToken(t *testing.T) {
	svc := auth.NewTokenService(testSigningKey, -time.Hour)

	token, err := svc.Generate("alice", false, "ws", "member")
	require.NoError(t, err)

	_, err = svc.Validate(token)
	assert.Error(t, err)
}

func TestTokenService_WrongSigningKey(t *testing.T) {
	svc1 := auth.NewTokenService(testSigningKey, time.Hour)
	svc2 := auth.NewTokenService([]byte("different-key-32-bytes-long!!!!!"), time.Hour)

	token, err := svc1.Generate("alice", false, "ws", "member")
	require.NoError(t, err)

	_, err = svc2.Validate(token)
	assert.Error(t, err)
}

func TestTokenService_InvalidTokenString(t *testing.T) {
	svc := auth.NewTokenService(testSigningKey, time.Hour)

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty string", token: ""},
		{name: "garbage", token: "not-a-jwt"},
		{name: "truncated", token: "eyJhbGciOiJIUzI1NiJ9.eyJ1c2Vy"}, //nolint:gosec // intentionally invalid test token
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Validate(tt.token)
			assert.Error(t, err)
		})
	}
}
