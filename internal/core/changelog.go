package core

import (
	"context"
	"errors"
	"time"
	"whynoipv6/internal/postgres/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
)

// ChangelogService is a service for managing changelogs.
type ChangelogService struct {
	q *db.Queries
}

// NewChangelogService creates a new ChangelogService.
func NewChangelogService(d db.DBTX) *ChangelogService {
	return &ChangelogService{
		q: db.New(d),
	}
}

// ChangelogModel represents a changelog entry.
type ChangelogModel struct {
	ID         int64     `json:"id"`
	Ts         time.Time `json:"timestamp"`
	DomainID   int32     `json:"domain_id,omitempty"`
	CampaignID uuid.UUID `json:"campaign_id,omitempty"`
	Site       string    `json:"site"`
	Message    string    `json:"message"`
	IPv6Status bool      `json:"ipv6_status"`
}

// Create creates a new changelog entry.
func (s *ChangelogService) Create(ctx context.Context, params ChangelogModel) (ChangelogModel, error) {
	changelog, err := s.q.CreateChangelog(ctx, db.CreateChangelogParams{
		DomainID:   params.DomainID,
		Message:    params.Message,
		Ipv6Status: params.IPv6Status,
	})
	if err != nil {
		return ChangelogModel{}, err
	}

	return ChangelogModel{
		ID:         changelog.ID,
		Ts:         changelog.Ts,
		DomainID:   changelog.DomainID,
		Message:    changelog.Message,
		IPv6Status: changelog.Ipv6Status,
	}, nil
}

// CampaignCreate creates a new changelog entry for campaign table.
func (s *ChangelogService) CampaignCreate(ctx context.Context, params ChangelogModel) (ChangelogModel, error) {
	changelog, err := s.q.CreateCampaignChangelog(ctx, db.CreateCampaignChangelogParams{
		DomainID:   params.DomainID,
		CampaignID: params.CampaignID,
		Message:    params.Message,
		Ipv6Status: params.IPv6Status,
	})
	if err != nil {
		return ChangelogModel{}, err
	}

	return ChangelogModel{
		ID:         changelog.ID,
		Ts:         changelog.Ts,
		DomainID:   changelog.DomainID,
		CampaignID: changelog.CampaignID,
		Message:    changelog.Message,
		IPv6Status: changelog.Ipv6Status,
	}, nil
}

// List lists all changelog entries.
func (s *ChangelogService) List(ctx context.Context, offset, limit int64) ([]ChangelogModel, error) {
	changelogs, err := s.q.ListChangelog(ctx, db.ListChangelogParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	var models []ChangelogModel
	for _, changelog := range changelogs {
		models = append(models, ChangelogModel{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			Site:       changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.Ipv6Status,
		})
	}
	return models, nil
}

// CampaignList lists all changelog entries for campaign table.
func (s *ChangelogService) CampaignList(ctx context.Context, offset, limit int64) ([]ChangelogModel, error) {
	changelogs, err := s.q.ListCampaignChangelog(ctx, db.ListCampaignChangelogParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	var models []ChangelogModel
	for _, changelog := range changelogs {
		models = append(models, ChangelogModel{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			Site:       changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.Ipv6Status,
		})
	}
	return models, nil
}

// GetChangelogByDomain gets all changelog entries for a domain name.
func (s *ChangelogService) GetChangelogByDomain(ctx context.Context, site string, offset, limit int64) ([]ChangelogModel, error) {
	// Get all changelog entries for site id
	changelogs, err := s.q.GetChangelogByDomain(ctx, db.GetChangelogByDomainParams{
		Site:   site,
		Offset: offset,
		Limit:  limit,
	})
	if err == pgx.ErrNoRows {
		return []ChangelogModel{}, errors.New("domain not found")
	}
	if err != nil {
		return []ChangelogModel{}, err
	}
	var models []ChangelogModel
	for _, changelog := range changelogs {
		models = append(models, ChangelogModel{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			DomainID:   changelog.DomainID,
			Site:       changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.Ipv6Status,
		})
	}
	return models, nil
}

// GetChangelogByCampaign gets all changelog entries for a campaign.
func (s *ChangelogService) GetChangelogByCampaign(ctx context.Context, campaignID uuid.UUID, offset, limit int64) ([]ChangelogModel, error) {
	// Get all changelog entries for site id
	changelogs, err := s.q.GetChangelogByCampaign(ctx, db.GetChangelogByCampaignParams{
		CampaignID: campaignID,
		Offset:     offset,
		Limit:      limit,
	})
	if err == pgx.ErrNoRows {
		return []ChangelogModel{}, errors.New("campaign not found")
	}
	if err != nil {
		return []ChangelogModel{}, err
	}
	var models []ChangelogModel
	for _, changelog := range changelogs {
		models = append(models, ChangelogModel{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			DomainID:   changelog.DomainID,
			CampaignID: changelog.CampaignID,
			Site:       changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.Ipv6Status,
		})
	}
	return models, nil
}

// GetChangelogByCampaignDomain gets all changelog entries for a campaign and domain.
func (s *ChangelogService) GetChangelogByCampaignDomain(ctx context.Context, campaignID uuid.UUID, site string, offset, limit int64) ([]ChangelogModel, error) {
	// Get all changelog entries for site id
	changelogs, err := s.q.GetChangelogByCampaignDomain(ctx, db.GetChangelogByCampaignDomainParams{
		CampaignID: campaignID,
		Site:       site,
		Offset:     offset,
		Limit:      limit,
	})
	if err == pgx.ErrNoRows {
		return []ChangelogModel{}, errors.New("campaign not found")
	}
	if err != nil {
		return []ChangelogModel{}, err
	}
	var models []ChangelogModel
	for _, changelog := range changelogs {
		models = append(models, ChangelogModel{
			ID:         changelog.ID,
			Ts:         changelog.Ts,
			DomainID:   changelog.DomainID,
			CampaignID: changelog.CampaignID,
			Site:       changelog.Site,
			Message:    changelog.Message,
			IPv6Status: changelog.Ipv6Status,
		})
	}
	return models, nil
}
