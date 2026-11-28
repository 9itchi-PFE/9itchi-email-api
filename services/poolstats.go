package services

import (
	"context"
	"math"
	"time"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/repositories"
)

const (
	// statsDays is how many UTC days before today the daily series covers (today included: statsDays + 1 entries).
	statsDays = 30
	// statsWindow is the window of the last_30_days totals.
	statsWindow = 30 * 24 * time.Hour
)

// poolSources are the contact sources by_source always lists (0 when none).
var poolSources = []string{"website", "hunter", "apollo", "quickenrich", "offer", "import"}

// PoolStatsService gives the admin numbers of the shared prospect pool. Handlers stay thin.
type PoolStatsService interface {
	// GET /admin/prospects/stats: the pool, the pool hit rates of the last 30 days and the daily series
	Stats(ctx context.Context) (*api.PoolStatsResponse, *models.ApiError)
}

// PoolStatsDeps are the collaborators of the pool stats service (all mockable).
type PoolStatsDeps struct {
	Repo repositories.PoolStatsRepository
	// CreditProviders are the providers of the chain that spend credits: every daily entry lists them (0 when none).
	CreditProviders []string
	Now             func() time.Time
}

type poolStatsService struct {
	PoolStatsDeps
}

func NewPoolStatsService(d PoolStatsDeps) PoolStatsService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &poolStatsService{d}
}

func (s *poolStatsService) Stats(ctx context.Context) (*api.PoolStatsResponse, *models.ApiError) {
	now := s.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	first := today.AddDate(0, 0, -statsDays)

	counts, err := s.Repo.PoolCounts(ctx, now.Add(-contactFreshness))
	if err != nil {
		return nil, repoError(err, "stats not found")
	}
	totals, err := s.Repo.SearchTotals(ctx, now.Add(-statsWindow))
	if err != nil {
		return nil, repoError(err, "stats not found")
	}
	days, err := s.Repo.SearchDays(ctx, first)
	if err != nil {
		return nil, repoError(err, "stats not found")
	}
	credits, err := s.Repo.CreditDays(ctx, first)
	if err != nil {
		return nil, repoError(err, "stats not found")
	}

	bySource := map[string]int{}
	for _, src := range poolSources {
		bySource[src] = 0
	}
	for src, n := range counts.BySource {
		bySource[src] = n
	}
	shown := totals.ContactsFromPool + totals.ContactsFound
	out := &api.PoolStatsResponse{
		Pool: api.PoolNumbers{
			Companies: counts.Companies, Contacts: counts.Contacts, Verified: counts.Verified, HR: counts.HR,
			Fresh: counts.Fresh, BySource: bySource,
		},
		Last30Days: api.PoolSearchStats{
			Searches: totals.Searches, ContactsShown: shown, ContactsFromPool: totals.ContactsFromPool,
			PoolHitRate: percent(totals.ContactsFromPool, shown), PoolOnlySearches: totals.PoolOnly,
			PoolOnlyRate: percent(totals.PoolOnly, totals.Searches),
		},
		Daily: make([]api.PoolDay, 0, statsDays+1),
	}

	index := map[string]int{}
	for i := 0; i <= statsDays; i++ {
		day := first.AddDate(0, 0, i).Format(time.DateOnly)
		index[day] = i
		d := api.PoolDay{Day: day, Credits: map[string]int{}}
		for _, p := range s.CreditProviders {
			d.Credits[p] = 0
		}
		out.Daily = append(out.Daily, d)
	}
	for _, d := range days {
		if i, ok := index[d.Day]; ok {
			out.Daily[i].Searches, out.Daily[i].ContactsFromPool, out.Daily[i].ContactsFound = d.Searches, d.ContactsFromPool, d.ContactsFound
		}
	}
	for _, c := range credits {
		if i, ok := index[c.Day]; ok {
			out.Daily[i].Credits[c.Provider] += c.Credits
		}
	}
	return out, nil
}

// percent is round(100*part/whole) as an int, 0 when whole is 0.
func percent(part, whole int) int {
	if whole <= 0 {
		return 0
	}
	return int(math.Round(100 * float64(part) / float64(whole)))
}
