package admins

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"strings"
	"time"

	"github.com/rox-projects/golang-tmpl/queries"
)

const sessionDuration = 24 * time.Hour

func (h handler) authenticate(ctx context.Context, authorization string) (queries.GetAdminBySessionRow, error) {
	value := strings.TrimSpace(authorization)
	token, found := strings.CutPrefix(value, "Bearer ")
	if !found || token == "" {
		return queries.GetAdminBySessionRow{}, sql.ErrNoRows
	}
	hash := tokenHash(h.jwtSecret, token)
	return h.queries.GetAdminBySession(ctx, queries.GetAdminBySessionParams{
		TokenHash: hash[:],
		ExpiresAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func newSession(secret string) (string, []byte) {
	token := rand.Text()
	return token, tokenHash(secret, token)
}

func tokenHash(secret, token string) []byte {
	hash := hmac.New(sha256.New, []byte(secret))
	_, _ = hash.Write([]byte(token))
	return hash.Sum(nil)
}
