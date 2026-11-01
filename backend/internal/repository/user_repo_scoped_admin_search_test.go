package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestScopedAdminUserSearchDoesNotMatchHiddenFields(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	user, err := client.User.Create().
		SetEmail("public-address@example.invalid").
		SetUsername("VisibleUser").
		SetPasswordHash("synthetic-password-hash").
		SetNotes("private-review-note").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.APIKey.Create().SetUserID(user.ID).
		SetName("synthetic-key").SetKey("sk-0123456789abcdef").Save(ctx)
	require.NoError(t, err)

	// Run the real repository Count and list queries, not a response projection.
	// In particular, a key-prefix probe must not reveal a matching user or total.
	for _, identity := range []struct {
		name      string
		principal *adminaccess.Principal
	}{
		{name: "full_admin"},
		{name: "scoped_admin", principal: &adminaccess.Principal{
			KeyID:  "synthetic-scoped-key",
			Policy: adminaccess.Policy{GlobalPermissions: []string{"users.read"}},
		}},
	} {
		t.Run(identity.name, func(t *testing.T) {
			queryContext := ctx
			if identity.principal != nil {
				queryContext = adminaccess.WithPrincipal(ctx, identity.principal)
			}
			for _, tc := range []struct {
				name    string
				search  string
				public  bool
				matches bool
			}{
				{"email", "PUBLIC-ADDRESS", true, true},
				{"username", "visibleuser", true, true},
				{"api_key", "sk-0123456789abcdef", false, true},
				{"api_key_prefix", "sk-0123", false, true},
				{"notes", "private-review-note", false, true},
				{"notes_substring", "review-note", false, true},
				{"unmatched", "does-not-exist", true, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					includeSubscriptions := false
					rows, result, err := repo.ListWithFilters(queryContext,
						pagination.PaginationParams{Page: 1, PageSize: 20},
						service.UserListFilters{Search: tc.search, IncludeSubscriptions: &includeSubscriptions})
					require.NoError(t, err)
					require.NotNil(t, result)
					allowedMatch := tc.matches && (identity.principal == nil || tc.public)
					if allowedMatch {
						require.Equal(t, int64(1), result.Total)
						require.Len(t, rows, 1)
						require.Equal(t, user.ID, rows[0].ID)
					} else {
						require.Zero(t, result.Total, "hidden field search must not reveal the matching count")
						require.Empty(t, rows, "hidden field search must not reveal a matching user")
					}
				})
			}
		})
	}
}
