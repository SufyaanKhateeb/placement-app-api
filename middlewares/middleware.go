package middlewares

import (
	"context"
	"net/http"
	"time"

	"github.com/SufyaanKhateeb/college-placement-app-api/config"
	"github.com/SufyaanKhateeb/college-placement-app-api/types"
	"github.com/SufyaanKhateeb/college-placement-app-api/utils"
)

func AuthMiddleware(authService types.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			accessTokenCookie, err := r.Cookie("ACCESS_TOKEN")
			var claims *types.CustomClaims
			if err == nil {
				token, err := authService.VerifyToken(accessTokenCookie.Value)
				if err != nil {
					// utils.WriteJsonError(w, http.StatusTemporaryRedirect, fmt.Errorf("not authorized"))
					http.Redirect(w, r, "/login", http.StatusFound)
					return
				}
				var ok bool
				claims, ok = token.Claims.(*types.CustomClaims)
				if !ok {
					// utils.WriteJsonError(w, http.StatusUnauthorized, fmt.Errorf("not authorized"))
					http.Redirect(w, r, "/login", http.StatusFound)
					return
				}
			} else {
				refreshTokenCookie, err := r.Cookie("REFRESH_TOKEN")
				if err == nil {
					token, err := authService.VerifyToken(refreshTokenCookie.Value)
					if err != nil {
						// utils.WriteJsonError(w, http.StatusUnauthorized, fmt.Errorf("not authorized"))
						http.Redirect(w, r, "/login", http.StatusFound)
						return
					}
					var ok bool
					claims, ok = token.Claims.(*types.CustomClaims)
					if ok {
						expirationTime := time.Second * time.Duration(config.Env.JWTExpirationTime)
						accessToken, err := authService.SignJwt(expirationTime, types.CustomClaims{
							Id:        claims.Id,
							Type:      claims.Type,
							TokenData: claims.TokenData,
						})
						if err != nil {
							utils.WriteJsonError(w, http.StatusInternalServerError, err)
							return
						}

						utils.WriteJwtToCookie(w, "ACCESS_TOKEN", accessToken, expirationTime)
					} else {
						// utils.WriteJsonError(w, http.StatusUnauthorized, fmt.Errorf("not authorized"))
						http.Redirect(w, r, "/login", http.StatusFound)
						return
					}
				} else {
					// utils.WriteJsonError(w, http.StatusUnauthorized, fmt.Errorf("not authorized"))
					http.Redirect(w, r, "/login", http.StatusFound)
					return
				}
			}
			ctx := context.WithValue(r.Context(), "tokenInput", claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

}

// checks if any type(admin/student) details are present and are valid
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenInput := r.Context().Value("tokenInput").(types.TokenInput)
		if tokenInput.Id == 0 || (tokenInput.Type != "student" && tokenInput.Type != "admin") {
			// utils.WriteJsonError(w, http.StatusForbidden, fmt.Errorf("not authorized"))
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireStudentUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenInput := r.Context().Value("tokenInput").(types.TokenInput)
		if tokenInput.Id == 0 || tokenInput.Type != "student" {
			// utils.WriteJsonError(w, http.StatusForbidden, fmt.Errorf("not authorized"))
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAdminUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenInput := r.Context().Value("tokenInput").(types.TokenInput)
		if tokenInput.Id == 0 || tokenInput.Type != "admin" {
			// utils.WriteJsonError(w, http.StatusForbidden, fmt.Errorf("not authorized"))
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
