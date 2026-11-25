//go:build integration

package repositories

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Everything of the deleted user goes, in one transaction, and nothing of anyone else.
func TestEraseUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	repo := NewEraseRepository(store)
	const gone, kept = "00000000-0000-4000-8000-0000000000e1", "00000000-0000-4000-8000-0000000000e2"
	const count = `SELECT (SELECT count(*) FROM applications WHERE user_id = $1) + (SELECT count(*) FROM emails WHERE user_id = $1) + (SELECT count(*) FROM gmail_connections WHERE user_id = $1) + (SELECT count(*) FROM job_matches WHERE user_id = $1)`
	left := func(id string) int {
		var n int
		require.NoError(t, store.Conn(ctx).QueryRow(ctx, count, id).Scan(&n))
		return n
	}
	for _, id := range []string{gone, kept} {
		for _, q := range []string{
			`INSERT INTO gmail_connections (user_id) VALUES ($1)`,
			`INSERT INTO job_matches (user_id, job_id, resume_id, language, input_hash, score, level) VALUES ($1, gen_random_uuid(), gen_random_uuid(), 'fr', repeat('a', 64), 50, 'partial')`,
			`WITH e AS (INSERT INTO emails (user_id, tracking_id) VALUES ($1, gen_random_uuid()) RETURNING id) INSERT INTO applications (user_id, job_id, email_id) SELECT $1, gen_random_uuid(), id FROM e`,
		} {
			_, err := store.Conn(ctx).Exec(ctx, q, id)
			require.NoError(t, err, q)
		}
	}
	require.Equal(t, 4, left(gone))

	rows, err := repo.EraseUser(ctx, gone)
	require.NoError(t, err)
	assert.Positive(t, rows)
	assert.Zero(t, left(gone))
	assert.Equal(t, 4, left(kept), "another student keeps everything")

	rows, err = repo.EraseUser(ctx, gone)
	require.NoError(t, err, "erasing again is not an error")
	assert.Zero(t, rows)
}
