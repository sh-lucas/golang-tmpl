package admins

import (
	"context"
	"net/http"

	"github.com/rox-projects/golang-tmpl/common/mug"
	"github.com/rox-projects/golang-tmpl/queries"
)

type contextKey struct{}

// requireAdmin blocks unauthenticated requests and makes the authenticated
// admin available to the endpoint through its request context.
func (h handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, err := h.authenticate(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			mug.Unauthorized("authentication required").WriteHTTP(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, admin)))
	})
}

// requireAdminAfterBootstrap leaves creation of the first admin public, then
// applies the same explicit authentication middleware to later creations.
func (h handler) requireAdminAfterBootstrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count, err := h.queries.CountAdmins(r.Context())
		if err != nil {
			mug.InternalServerError("internal server error").WriteHTTP(w)
			return
		}
		if count == 0 {
			next.ServeHTTP(w, r)
			return
		}
		h.requireAdmin(next).ServeHTTP(w, r)
	})
}

func adminFromContext(ctx context.Context) (queries.GetAdminBySessionRow, bool) {
	admin, ok := ctx.Value(contextKey{}).(queries.GetAdminBySessionRow)
	return admin, ok
}
