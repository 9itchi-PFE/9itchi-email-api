package api

// PoolStatsResponse is GET /admin/prospects/stats: the live numbers of the shared prospect pool, what the fresh
// searches of the last 30 days took from it and, per UTC day, the searches and the credits the providers spent.
type PoolStatsResponse struct {
	Pool       PoolNumbers     `json:"pool"`
	Last30Days PoolSearchStats `json:"last_30_days"`
	Daily      []PoolDay       `json:"daily"`
}

// PoolNumbers counts the pool. Contact counts leave opted-out people out; Fresh are the people the pipeline still
// uses; BySource has every known source (0 when none).
type PoolNumbers struct {
	Companies int            `json:"companies"`
	Contacts  int            `json:"contacts"`
	Verified  int            `json:"verified"`
	HR        int            `json:"hr"`
	Fresh     int            `json:"fresh"`
	BySource  map[string]int `json:"by_source"`
}

// PoolSearchStats sums the fresh searches with stored stats. Rates are 0 to 100 (0 when nothing to divide).
type PoolSearchStats struct {
	Searches         int `json:"searches"`
	ContactsShown    int `json:"contacts_shown"`
	ContactsFromPool int `json:"contacts_from_pool"`
	PoolHitRate      int `json:"pool_hit_rate"`
	PoolOnlySearches int `json:"pool_only_searches"`
	PoolOnlyRate     int `json:"pool_only_rate"`
}

// PoolDay is one UTC day (YYYY-MM-DD): the searches, the people they took from the pool or found, and the credits
// each provider spent.
type PoolDay struct {
	Day              string         `json:"day"`
	Searches         int            `json:"searches"`
	ContactsFromPool int            `json:"contacts_from_pool"`
	ContactsFound    int            `json:"contacts_found"`
	Credits          map[string]int `json:"credits"`
}
