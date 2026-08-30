package admins

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/rox-projects/golang-tmpl/common/mug"
	"github.com/rox-projects/golang-tmpl/queries"
	"golang.org/x/crypto/bcrypt"
)

type handler struct {
	queries   *queries.Queries
	jwtSecret string
}

type credentials struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=12"`
}

func RegisterRoutes(mux *http.ServeMux, q *queries.Queries, jwtSecret string) {
	h := handler{queries: q, jwtSecret: jwtSecret}
	mux.Handle("POST /admins", h.requireAdminAfterBootstrap(mug.Route(h.create)))
	mux.Handle("POST /auth/login", mug.Route(h.login))
	mux.Handle("GET /admins/me", h.requireAdmin(mug.Route(h.me)))
}

// create godoc
// @Summary Create an admin
// @Description The first admin is public bootstrap; subsequent admins require authentication.
// @Tags admins
// @Accept json
// @Produce json
// @Param credentials body credentials true "Admin credentials"
// @Success 201 {object} queries.CreateAdminRow
// @Failure 400 {object} map[string]any
// @Failure 401 {object} map[string]any
// @Failure 409 {object} map[string]any
// @Router /admins [post]
func (h handler) create(ctx context.Context, input credentials) mug.Response {
	input.Email = strings.ToLower(input.Email)
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return mug.InternalServerError("internal server error")
	}
	admin, err := h.queries.CreateAdmin(ctx, queries.CreateAdminParams{Email: input.Email, PasswordHash: string(hash)})
	if err != nil {
		return mug.Conflict("email already registered")
	}
	return mug.Created(admin)
}

// login godoc
// @Summary Authenticate an admin
// @Tags auth
// @Accept json
// @Produce json
// @Param credentials body credentials true "Admin credentials"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]any
// @Failure 401 {object} map[string]any
// @Router /auth/login [post]
func (h handler) login(ctx context.Context, input credentials) mug.Response {
	input.Email = strings.ToLower(input.Email)
	admin, err := h.queries.GetAdminCredentialsByEmail(ctx, input.Email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(input.Password)) != nil {
		return mug.Unauthorized("invalid email or password")
	}
	token, tokenHash := newSession(h.jwtSecret)
	if err := h.queries.CreateAdminSession(ctx, queries.CreateAdminSessionParams{
		TokenHash: tokenHash,
		AdminID:   admin.ID,
		ExpiresAt: time.Now().UTC().Add(sessionDuration).Format(time.RFC3339),
	}); err != nil {
		return mug.InternalServerError("internal server error")
	}
	return mug.OK(struct {
		Token string `json:"token"`
	}{Token: token})
}

// me godoc
// @Summary Get the authenticated admin
// @Tags admins
// @Produce json
// @Security BearerAuth
// @Success 200 {object} queries.GetAdminBySessionRow
// @Failure 401 {object} map[string]any
// @Router /admins/me [get]
func (h handler) me(ctx context.Context, _ struct{}) mug.Response {
	admin, ok := adminFromContext(ctx)
	if !ok {
		return mug.InternalServerError("authenticated admin missing from request context")
	}
	return mug.OK(admin)
}
