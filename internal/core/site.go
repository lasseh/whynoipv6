package core

import (
	"context"
	"whynoipv6/internal/postgres/db"
)

// SiteService is a service for managing sites.
type SiteService struct {
	q *db.Queries
}

// NewSiteService creates a new SiteService.
func NewSiteService(d db.DBTX) *SiteService {
	return &SiteService{
		q: db.New(d),
	}
}

// SiteModel represents a site.
type SiteModel struct {
	ID   int64  `json:"id"`
	Site string `json:"site"`
}

// ListSite lists all sites.
func (s *SiteService) ListSite(ctx context.Context, offset, limit int32) ([]SiteModel, error) {
	sites, err := s.q.ListSites(ctx, db.ListSitesParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return []SiteModel{}, err
	}
	var models []SiteModel
	for _, site := range sites {
		models = append(models, SiteModel{
			ID:   site.ID,
			Site: site.Site,
		})
	}
	return models, nil
}
