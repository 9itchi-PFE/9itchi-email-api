package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
)

// companySource is companies.source for a company first known from an offer link.
const companySource = "link"

// companyQuery is a search at one company as normalized: the cache key and the query stored with the search. Goal
// and contract read like a prospectQuery's (the recruiter draft reads them).
type companyQuery struct {
	Company  string `json:"company"`
	Domain   string `json:"domain"`
	Goal     string `json:"goal"`
	Contract string `json:"contract"`
	Contacts int    `json:"contacts"`
}

// companyNamed is the newest pool company whose name is this one, ignoring case, accents, punctuation, spaces and
// legal endings ("Datanomix" is "DATANOMIX, Inc."); nil when none.
func (s *recruiterService) companyNamed(ctx context.Context, name string) (*db.Company, error) {
	key := companyKey(name)
	first := strings.Fields(strings.ToLower(name))
	if key == "" || len(first) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.CompaniesNamed(ctx, strings.TrimRight(first[0], ",.;:"), 20)
	if err != nil {
		return nil, err
	}
	for _, c := range rows {
		if companyKey(c.Name) == key {
			return &c, nil
		}
	}
	return nil, nil
}

// legalForms are the endings a company name may carry or not: matching ignores them.
var legalForms = map[string]bool{
	"inc": true, "incorporated": true, "llc": true, "llp": true, "ltd": true, "limited": true, "corp": true,
	"corporation": true, "co": true, "company": true, "plc": true, "gmbh": true, "ag": true, "bv": true, "nv": true,
	"sa": true, "sarl": true, "suarl": true, "sas": true, "sasu": true, "spa": true, "srl": true, "group": true,
	"groupe": true, "holding": true,
}

// companyKey is a company name folded for matching: lower case, no accents, letters and digits only, legal endings
// out, words glued ("Data Nomix SARL" and "datanomix" have the same key).
func companyKey(name string) string {
	ws := words(fold(name))
	for len(ws) > 1 && legalForms[ws[len(ws)-1]] {
		ws = ws[:len(ws)-1]
	}
	return strings.Join(ws, "")
}

// AtCompany finds the right people at one company: the pool first (by domain, else by name), else the finders chain.
// The people found join the student's recruiters and the pool. Nobody found is a 404 and is not counted.
func (s *recruiterService) AtCompany(ctx context.Context, userID string, req api.CompanyRecruiterRequest) (*api.RecruiterSearchResponse, *models.ApiError) {
	name := strings.Join(strings.Fields(req.Company), " ")
	if name == "" {
		return nil, apiError(http.StatusBadRequest, "company is required")
	}
	domain, ok := hostOf(req.Domain)
	if !ok {
		return nil, apiError(http.StatusBadRequest, "invalid domain")
	}
	q := companyQuery{
		Company: strings.ToLower(name), Domain: domain, Goal: strings.ToLower(strings.Join(strings.Fields(req.Goal), " ")),
		Contacts: perCompany,
	}
	key, _ := json.Marshal(q)
	sum := sha256.Sum256(key)
	hash := hex.EncodeToString(sum[:])
	start, _ := s.tunisDay()

	if prev, err := s.Repo.LatestSearch(ctx, userID, hash, start); err == nil {
		items, err := s.Repo.ListBySearch(ctx, userID, prev.ID)
		if err != nil {
			return nil, repoError(err, "search not found")
		}
		return s.searchResponse(ctx, userID, prev, items, true, api.ProspectStats{})
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, repoError(err, "search not found")
	}
	if apiErr := s.searchLeft(ctx, userID); apiErr != nil {
		return nil, apiErr
	}

	var stats api.ProspectStats
	cand := &candidate{company: db.Company{Name: name, Domain: domain, Source: companySource}}
	if domain != "" {
		saved, created, err := s.savePoolCompany(ctx, cand.company)
		if err != nil {
			return nil, repoError(err, "company not found")
		}
		cand.company = *saved
		if created {
			stats.CompaniesDiscovered++
		} else {
			stats.CompaniesFromPool++
		}
	} else if pooled, err := s.companyNamed(ctx, name); err != nil {
		return nil, repoError(err, "company not found")
	} else if pooled != nil {
		cand.company = *pooled
		stats.CompaniesFromPool++
	}

	pctx, cancel := context.WithTimeout(ctx, prospectTimeout)
	people, pooled, err := s.companyPeople(pctx, cand, map[string]bool{})
	cancel()
	if err != nil {
		return nil, repoError(err, "company not found")
	}
	if len(people) == 0 {
		return nil, apiError(http.StatusNotFound, "no recruiter found at this company")
	}
	if cand.company.ID != "" && stats == (api.ProspectStats{}) {
		// Asked by name: the finder told the domain.
		stats.CompaniesDiscovered++
	}
	rankPeople(people, cand.company.SizeRange)
	picks := make([]prospect, 0, perCompany)
	for _, p := range people[:min(len(people), perCompany)] {
		picks = append(picks, prospect{cand: cand, contact: p, fromPool: pooled})
	}
	if pooled {
		stats.ContactsFromPool = len(picks)
	} else {
		stats.ContactsFound = len(picks)
	}
	search, saved, err := s.Repo.SaveSearch(ctx, &db.RecruiterSearch{
		UserID: userID, Query: key, QueryHash: hash, Provider: searchProvider,
		Stats: &db.SearchStats{
			CompaniesFromPool: stats.CompaniesFromPool, CompaniesDiscovered: stats.CompaniesDiscovered,
			ContactsFromPool: stats.ContactsFromPool, ContactsFound: stats.ContactsFound,
		},
	}, toProspectRows(picks))
	if err != nil {
		return nil, repoError(err, "search not found")
	}
	return s.searchResponse(ctx, userID, search, saved, false, stats)
}
