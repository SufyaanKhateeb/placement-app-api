package middlewares

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
	"github.com/jackc/pgx/v5"
)

type sessionKey struct{}
type permissionsKey struct{}

type ResolvePermissions func(context.Context, types.TokenInput) ([]string, error)

var (
	errAuthenticationRequired = errors.New("authentication required")
	errPermissionDenied       = errors.New("insufficient permissions")
)

func Session(ctx context.Context) (types.TokenInput, bool) {
	session, ok := ctx.Value(sessionKey{}).(types.TokenInput)
	return session, ok
}

func Permissions(ctx context.Context) []string {
	permissions, _ := ctx.Value(permissionsKey{}).([]string)
	return permissions
}

func HasPermission(ctx context.Context, permission string) bool {
	return slices.Contains(Permissions(ctx), permission)
}

func Authenticate(auth types.AuthService, resolve ResolvePermissions, accountType string, required ...string) func(http.HandlerFunc) http.Handler {
	return func(next http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("ACCESS_TOKEN")
			refresh := errors.Is(err, http.ErrNoCookie)
			if refresh {
				cookie, err = r.Cookie("REFRESH_TOKEN")
			}
			if err != nil {
				utils.WriteJsonError(w, http.StatusUnauthorized, errAuthenticationRequired)
				return
			}

			token, err := auth.VerifyToken(cookie.Value)
			if err != nil || token == nil || !token.Valid {
				utils.WriteJsonError(w, http.StatusUnauthorized, errAuthenticationRequired)
				return
			}
			claims, ok := token.Claims.(*types.CustomClaims)
			if !ok || claims.Id <= 0 {
				utils.WriteJsonError(w, http.StatusUnauthorized, errAuthenticationRequired)
				return
			}
			role := claims.Type
			if role == types.LegacyRoleStudent {
				role = types.RoleUser
			}
			if role != types.RoleUser && role != types.RoleAdmin {
				utils.WriteJsonError(w, http.StatusUnauthorized, errAuthenticationRequired)
				return
			}
			if accountType != "" && role != accountType {
				utils.WriteJsonError(w, http.StatusForbidden, errPermissionDenied)
				return
			}
			session := types.TokenInput{Id: claims.Id, Type: role, TokenData: claims.TokenData}
			permissions, err := resolve(r.Context(), session)
			if errors.Is(err, pgx.ErrNoRows) {
				utils.WriteJsonError(w, http.StatusUnauthorized, errAuthenticationRequired)
				return
			}
			if err != nil {
				utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not check access"))
				return
			}
			for _, permission := range required {
				if !slices.Contains(permissions, permission) {
					utils.WriteJsonError(w, http.StatusForbidden, errPermissionDenied)
					return
				}
			}
			if refresh {
				lifetime := time.Duration(config.Env.JWTExpirationTime) * time.Second
				claims.Type = role
				access, err := auth.SignJwt(lifetime, *claims)
				if err != nil {
					utils.WriteJsonError(w, http.StatusInternalServerError, errors.New("could not refresh session"))
					return
				}
				utils.WriteJwtToCookie(w, "ACCESS_TOKEN", access, lifetime)
			}

			ctx := context.WithValue(r.Context(), sessionKey{}, session)
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, permissionsKey{}, permissions)))
		})
	}
}
