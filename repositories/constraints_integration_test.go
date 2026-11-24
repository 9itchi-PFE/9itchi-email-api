//go:build integration

package repositories

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstraintsAcceptValidRows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	c := store.Conn(ctx)

	var emailID string
	require.NoError(t, c.QueryRow(ctx,
		`INSERT INTO emails (user_id, tracking_id) VALUES (gen_random_uuid(), gen_random_uuid()) RETURNING id`).Scan(&emailID))
	var status string
	require.NoError(t, c.QueryRow(ctx, `SELECT status FROM emails WHERE id = $1`, emailID).Scan(&status))
	assert.Equal(t, "draft", status)

	_, err := c.Exec(ctx, `INSERT INTO applications (user_id, job_id, email_id) VALUES (gen_random_uuid(), gen_random_uuid(), $1)`, emailID)
	require.NoError(t, err)

	_, err = c.Exec(ctx, `DELETE FROM emails WHERE id = $1`, emailID)
	require.NoError(t, err)
	var linked *string
	require.NoError(t, c.QueryRow(ctx, `SELECT email_id FROM applications`).Scan(&linked))
	assert.Nil(t, linked, "deleting an email keeps the application and clears the link")
}

func TestConstraintsRejectInvalidRows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	c := store.Conn(ctx)

	cases := map[string]string{
		"unknown email status":    `INSERT INTO emails (user_id, tracking_id, status) VALUES (gen_random_uuid(), gen_random_uuid(), 'bogus')`,
		"scheduled without time":  `INSERT INTO emails (user_id, tracking_id, status) VALUES (gen_random_uuid(), gen_random_uuid(), 'scheduled')`,
		"unknown application":     `INSERT INTO applications (user_id, job_id, status) VALUES (gen_random_uuid(), gen_random_uuid(), 'bogus')`,
		"application to no email": `INSERT INTO applications (user_id, job_id, email_id) VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid())`,
	}
	for name, q := range cases {
		_, err := c.Exec(ctx, q)
		assert.Error(t, err, name)
	}
}

func TestConstraintsRejectDuplicates(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	c := store.Conn(ctx)

	_, err := c.Exec(ctx, `INSERT INTO gmail_connections (user_id) VALUES ('00000000-0000-0000-0000-000000000001')`)
	require.NoError(t, err)
	_, err = c.Exec(ctx, `INSERT INTO gmail_connections (user_id) VALUES ('00000000-0000-0000-0000-000000000001')`)
	assert.Error(t, err, "one Gmail connection per user")

	_, err = c.Exec(ctx, `INSERT INTO emails (user_id, tracking_id) VALUES (gen_random_uuid(), '00000000-0000-0000-0000-0000000000aa')`)
	require.NoError(t, err)
	_, err = c.Exec(ctx, `INSERT INTO emails (user_id, tracking_id) VALUES (gen_random_uuid(), '00000000-0000-0000-0000-0000000000aa')`)
	assert.Error(t, err, "tracking ids are unique")

	q := `INSERT INTO applications (user_id, job_id) VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002')`
	_, err = c.Exec(ctx, q)
	require.NoError(t, err)
	_, err = c.Exec(ctx, q)
	assert.Error(t, err, "one application per user and job")
}
