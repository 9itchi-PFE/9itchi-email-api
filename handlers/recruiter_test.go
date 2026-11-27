package handlers

import (
	"context"
	"net/http"
	"testing"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"

	"github.com/stretchr/testify/assert"
)

type mockPoolStats struct {
	failing *models.ApiError
}

func (m *mockPoolStats) Stats(context.Context) (*api.PoolStatsResponse, *models.ApiError) {
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.PoolStatsResponse{
		Pool:       api.PoolNumbers{Contacts: 3, BySource: map[string]int{"website": 3}},
		Last30Days: api.PoolSearchStats{Searches: 1, ContactsShown: 2, ContactsFromPool: 2, PoolHitRate: 100},
		Daily:      []api.PoolDay{{Day: "2026-11-27", Credits: map[string]int{"hunter": 0}}},
	}, nil
}

func TestProspectStatsHandler(t *testing.T) {
	w := call(ProspectStats(&mockPoolStats{}), http.MethodGet, "/admin/prospects/stats", "/admin/prospects/stats", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{
		"pool": {"companies": 0, "contacts": 3, "verified": 0, "hr": 0, "fresh": 0, "by_source": {"website": 3}},
		"last_30_days": {"searches": 1, "contacts_shown": 2, "contacts_from_pool": 2, "pool_hit_rate": 100,
			"pool_only_searches": 0, "pool_only_rate": 0},
		"daily": [{"day": "2026-11-27", "searches": 0, "contacts_from_pool": 0, "contacts_found": 0, "credits": {"hunter": 0}}]
	}`, w.Body.String())

	w = call(ProspectStats(&mockPoolStats{failing: &models.ApiError{Code: http.StatusInternalServerError, Error: assert.AnError}}),
		http.MethodGet, "/admin/prospects/stats", "/admin/prospects/stats", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
