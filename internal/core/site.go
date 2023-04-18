package core

import (
	"context"
	"whynoipv6/internal/postgres/db"
)

// SiteService is a service for managing sites.
type SiteService struct {
	q *db.Queries
}

// NewSiteService creates a new instance of SiteService.
func NewSiteService(d db.DBTX) *SiteService {
	return &SiteService{
		q: db.New(d),
	}
}

// SiteModel represents a site entity.
type SiteModel struct {
	ID   int64  `json:"id"`   // Unique identifier for the site.
	Site string `json:"site"` // URL or name of the site.
}

// ListSite retrieves a list of sites with pagination support.
// It accepts a context, offset, and limit as parameters.
// Returns a slice of SiteModel and an error if any.
func (s *SiteService) ListSite(ctx context.Context, offset, limit int32) ([]SiteModel, error) {
	sites, err := s.q.ListSites(ctx, db.ListSitesParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return []SiteModel{}, err
	}

	// Convert the list of sites into a slice of SiteModel.
	var siteModels []SiteModel
	for _, site := range sites {
		siteModels = append(siteModels, SiteModel{
			ID:   site.ID,
			Site: site.Site,
		})
	}
	return siteModels, nil
}
