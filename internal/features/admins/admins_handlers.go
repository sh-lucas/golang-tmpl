package admins

import (
	"net/http"
	"strings"
	"time"

	"github.com/rox-projects/golang-tmpl/internal/helpers"
	"github.com/rox-projects/golang-tmpl/queries"
	"golang.org/x/crypto/bcrypt"
)

type handler struct {
	*helpers.HTTP
	queries *queries.Queries
}

type credentials struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=12"`
}

func RegisterRoutes(mux *http.ServeMux, q *queries.Queries) {
	h := handler{HTTP: helpers.NewHTTP(), queries: q}
	mux.HandleFunc("POST /admins", h.create)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("GET /admins/me", h.me)
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
func (h handler) create(w http.ResponseWriter, r *http.Request) {
	request, ok := helpers.HandleRequest[credentials](h.HTTP, r, w)
	if !ok {
		return
	}
	request.Body.Email = strings.ToLower(request.Body.Email)

	count, err := h.queries.CountAdmins(r.Context())
	if err != nil {
		request.Error("internal server error", http.StatusInternalServerError)
		return
	}
	if count > 0 {
		if _, err := h.authenticate(r.Context(), request.Authorization); err != nil {
			request.Error("authentication required", http.StatusUnauthorized)
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(request.Body.Password), bcrypt.DefaultCost)
	if err != nil {
		request.Error("internal server error", http.StatusInternalServerError)
		return
	}
	admin, err := h.queries.CreateAdmin(r.Context(), queries.CreateAdminParams{Email: request.Body.Email, PasswordHash: string(hash)})
	if err != nil {
		request.Error("email already registered", http.StatusConflict)
		return
	}
	request.Send(admin, http.StatusCreated)
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
func (h handler) login(w http.ResponseWriter, r *http.Request) {
	request, ok := helpers.HandleRequest[credentials](h.HTTP, r, w)
	if !ok {
		return
	}
	request.Body.Email = strings.ToLower(request.Body.Email)
	admin, err := h.queries.GetAdminCredentialsByEmail(r.Context(), request.Body.Email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(request.Body.Password)) != nil {
		request.Error("invalid email or password", http.StatusUnauthorized)
		return
	}
	token, tokenHash := newSession()
	if err := h.queries.CreateAdminSession(r.Context(), queries.CreateAdminSessionParams{
		TokenHash: tokenHash,
		AdminID:   admin.ID,
		ExpiresAt: time.Now().UTC().Add(sessionDuration).Format(time.RFC3339),
	}); err != nil {
		request.Error("internal server error", http.StatusInternalServerError)
		return
	}
	request.Send(struct {
		Token string `json:"token"`
	}{Token: token}, http.StatusOK)
}

// me godoc
// @Summary Get the authenticated admin
// @Tags admins
// @Produce json
// @Security BearerAuth
// @Success 200 {object} queries.GetAdminBySessionRow
// @Failure 401 {object} map[string]any
// @Router /admins/me [get]
func (h handler) me(w http.ResponseWriter, r *http.Request) {
	request, ok := helpers.HandleRequest[struct{}](h.HTTP, r, w)
	if !ok {
		return
	}
	admin, err := h.authenticate(r.Context(), request.Authorization)
	if err != nil {
		request.Error("authentication required", http.StatusUnauthorized)
		return
	}
	request.Send(admin, http.StatusOK)
}
