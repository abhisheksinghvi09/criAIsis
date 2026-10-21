package handler

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"criaisis/internal/domain/entity"
	"criaisis/internal/domain/repository"

	"github.com/rs/zerolog"
)

// workspaceContextKey carries the authenticated tenant down to a handler.
type workspaceContextKey struct{}

// Auth authenticates management requests.
//
// Two distinct credentials exist on purpose. The platform admin key may only
// create workspaces. A workspace admin key administers exactly one tenant and
// can never reach another, which is the multi-tenant authorization boundary.
type Auth struct {
	workspaces  repository.WorkspaceRepository
	platformKey string
	log         *zerolog.Logger
}

// NewAuth wires the authenticator.
func NewAuth(workspaces repository.WorkspaceRepository, platformKey string, log *zerolog.Logger) *Auth {
	return &Auth{workspaces: workspaces, platformKey: platformKey, log: log}
}

// RequirePlatformAdmin gates workspace creation behind the platform credential.
func (a *Auth) RequirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := bearerToken(r)
		if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(a.platformKey)) != 1 {
			a.log.Warn().Str("path", r.URL.Path).Msg("rejected platform admin request")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid platform admin key"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireWorkspaceAdmin resolves the workspace from the path and verifies that the
// presented key administers that specific workspace.
//
// The key is checked against the workspace named in the URL, so a valid key for
// tenant A presented against tenant B's path is rejected: holding any valid
// credential is not sufficient, it must be the right one.
func (a *Auth) RequireWorkspaceAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teamID := workspaceParam(r)
		presented := bearerToken(r)

		ws, err := a.workspaces.GetBySlackTeamID(r.Context(), teamID)
		if err != nil || !ws.VerifyAdminAPIKey(presented) {
			// Unknown workspace and wrong key are reported identically: the
			// difference would let a caller enumerate which tenants exist.
			a.log.Warn().Str("workspace", teamID).Msg("rejected workspace admin request")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid workspace or admin key"})
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), workspaceContextKey{}, ws)))
	})
}

// WorkspaceFrom returns the tenant authenticated for this request.
func WorkspaceFrom(ctx context.Context) (*entity.Workspace, bool) {
	ws, ok := ctx.Value(workspaceContextKey{}).(*entity.Workspace)
	return ws, ok
}

// WithWorkspace attaches an authenticated workspace to context.
func WithWorkspace(ctx context.Context, ws *entity.Workspace) context.Context {
	return context.WithValue(ctx, workspaceContextKey{}, ws)
}

// bearerToken extracts a bearer credential from the Authorization header.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}
