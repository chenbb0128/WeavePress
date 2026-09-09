package authn

import (
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func TestAccessTokenContainsUserAndRole(t *testing.T) {
	service := New(nil, nil, config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-at-least-32-characters", AccessTTL: 15 * time.Minute})
	service.now = func() time.Time { return time.Now().UTC() }
	token, err := service.issueAccess(workspace.User{ID: 42, Role: workspace.RoleEditor})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ParseAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "42" || claims.Role != workspace.RoleEditor || claims.ID == "" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestPermissionsSeparateAdministration(t *testing.T) {
	service := New(nil, nil, config.AuthConfig{})
	editor := service.Permissions(workspace.RoleEditor)
	admin := service.Permissions(workspace.RoleAdmin)
	if contains(editor, "user:view") {
		t.Fatal("editor received user administration permission")
	}
	if !contains(admin, "user:view") {
		t.Fatal("admin misses user administration permission")
	}
}

func TestPermissionsIncludeAIWritingForAdminAndEditor(t *testing.T) {
	service := New(nil, nil, config.AuthConfig{})
	want := []string{
		"ai:analysis:create",
		"ai:analysis:view",
		"ai:generation:create",
		"ai:generation:view",
		"ai:job:retry",
	}
	for _, role := range []workspace.Role{workspace.RoleAdmin, workspace.RoleEditor} {
		permissions := service.Permissions(role)
		for _, code := range want {
			if !contains(permissions, code) {
				t.Errorf("Permissions(%q) missing %q", role, code)
			}
		}
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
