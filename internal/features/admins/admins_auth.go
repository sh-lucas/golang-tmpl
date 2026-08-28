package admins

import (
	"context"
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
	hash := sha256.Sum256([]byte(token))
	return h.queries.GetAdminBySession(ctx, queries.GetAdminBySessionParams{
		TokenHash: hash[:],
		ExpiresAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func newSession() (string, []byte) {
	token := rand.Text()
	hash := sha256.Sum256([]byte(token))
	return token, hash[:]
}
