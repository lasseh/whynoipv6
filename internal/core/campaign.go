package core

import (
	"context"
	"encoding/json"
	"time"
	"whynoipv6/internal/postgres/db"

	"github.com/google/uuid"
	"github.com/jackc/pgtype"
)

// CampaignService is a service for managing scans.
type CampaignService struct {
	q *db.Queries
}

// NewCampaignService creates a new ScanService.
func NewCampaignService(d db.DBTX) *CampaignService {
	return &CampaignService{
		q: db.New(d),
	}
}

// CampaignModel represents a campaign.
type CampaignModel struct {
	ID          int64     `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UUID        uuid.UUID `json:"uuid"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Count       int64     `json:"count"`
	V6Ready     int64     `json:"v6_ready"`
}

// CampaignDomainModel represents a scan.
type CampaignDomainModel struct {
	ID           int64     `json:"id"`
	Site         string    `json:"site"`
	CampaignID   uuid.UUID `json:"campaign_id"`
	BaseDomain   string    `json:"check_aaaa"`
	WwwDomain    string    `json:"check_www"`
	Nameserver   string    `json:"check_ns"`
	MXRecord     string    `json:"check_mx"`
	V6Only       string    `json:"check_curl"`
	AsnID        int64     `json:"asn_id"`
	AsName       string    `json:"asn"`
	CountryID    int64     `json:"country_id"`
	Country      string    `json:"country"`
	TsBaseDomain time.Time `json:"ts_aaaa"`
	TsWwwDomain  time.Time `json:"ts_www"`
	TsNameserver time.Time `json:"ts_ns"`
	TsMXRecord   time.Time `json:"ts_mx"`
	TsV6Only     time.Time `json:"ts_curl"`
	TsCheck      time.Time `json:"ts_check"`
	TsUpdated    time.Time `json:"ts_updated"`
}

// InsertCampaignDomain inserts a domain into a campaign.
func (s *CampaignService) InsertCampaignDomain(ctx context.Context, campaignID uuid.UUID, domain string) error {
	err := s.q.InsertCampaignDomain(ctx, db.InsertCampaignDomainParams{
		CampaignID: campaignID,
		Site:       domain,
	})
	if err != nil {
		return err
	}
	return nil
}

// CrawlCampaignDomain lists all domains available for crawling
func (s *CampaignService) CrawlCampaignDomain(ctx context.Context, offset, limit int64) ([]CampaignDomainModel, error) {
	domains, err := s.q.CrawlCampaignDomain(ctx, db.CrawlCampaignDomainParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	var list []CampaignDomainModel
	for _, d := range domains {
		list = append(list, CampaignDomainModel{
			ID:           d.ID,
			Site:         d.Site,
			CampaignID:   d.CampaignID,
			BaseDomain:   d.BaseDomain,
			WwwDomain:    d.WwwDomain,
			Nameserver:   d.Nameserver,
			MXRecord:     d.MxRecord,
			V6Only:       d.V6Only,
			TsBaseDomain: TimeNull(d.TsBaseDomain),
			TsWwwDomain:  TimeNull(d.TsWwwDomain),
			TsNameserver: TimeNull(d.TsNameserver),
			TsMXRecord:   TimeNull(d.TsMxRecord),
			TsV6Only:     TimeNull(d.TsV6Only),
			TsCheck:      TimeNull(d.TsCheck),
			TsUpdated:    TimeNull(d.TsUpdated),
		})
	}
	return list, nil
}

// UpdateCampaignDomain updates a domain.
func (s *CampaignService) UpdateCampaignDomain(ctx context.Context, domain CampaignDomainModel) error {
	err := s.q.UpdateCampaignDomain(ctx, db.UpdateCampaignDomainParams{
		Site:         domain.Site,
		CampaignID:   domain.CampaignID,
		BaseDomain:   domain.BaseDomain,
		WwwDomain:    domain.WwwDomain,
		Nameserver:   domain.Nameserver,
		MxRecord:     domain.MXRecord,
		V6Only:       domain.V6Only,
		AsnID:        NullInt(domain.AsnID),
		CountryID:    NullInt(domain.CountryID),
		TsBaseDomain: NullTime(domain.TsBaseDomain),
		TsWwwDomain:  NullTime(domain.TsWwwDomain),
		TsNameserver: NullTime(domain.TsNameserver),
		TsMxRecord:   NullTime(domain.TsMXRecord),
		TsV6Only:     NullTime(domain.TsV6Only),
		TsCheck:      NullTime(domain.TsCheck),
		TsUpdated:    NullTime(domain.TsUpdated),
	})
	if err != nil {
		return err
	}
	return nil
}

// ViewCampaignDomain list a domain.
func (s *CampaignService) ViewCampaignDomain(ctx context.Context, uuid uuid.UUID, domain string) (CampaignDomainModel, error) {
	d, err := s.q.ViewCampaignDomain(ctx, db.ViewCampaignDomainParams{
		Site:       domain,
		CampaignID: uuid,
	})
	if err != nil {
		return CampaignDomainModel{}, err
	}
	return CampaignDomainModel{
		ID:           d.ID,
		Site:         d.Site,
		BaseDomain:   d.BaseDomain,
		WwwDomain:    d.WwwDomain,
		Nameserver:   d.Nameserver,
		MXRecord:     d.MxRecord,
		V6Only:       d.V6Only,
		AsName:       StringNull(d.Asname),
		Country:      StringNull(d.CountryName),
		TsBaseDomain: TimeNull(d.TsBaseDomain),
		TsWwwDomain:  TimeNull(d.TsWwwDomain),
		TsNameserver: TimeNull(d.TsNameserver),
		TsMXRecord:   TimeNull(d.TsMxRecord),
		TsV6Only:     TimeNull(d.TsV6Only),
		TsCheck:      TimeNull(d.TsCheck),
		TsUpdated:    TimeNull(d.TsUpdated),
	}, nil
}

// DisableCampaignDomain disables a domain.
func (s *CampaignService) DisableCampaignDomain(ctx context.Context, domain string) error {
	err := s.q.DisableCampaignDomain(ctx, domain)
	if err != nil {
		return err
	}
	return nil
}

// ListCampaign list all campaigns.
func (s *CampaignService) ListCampaign(ctx context.Context) ([]CampaignModel, error) {
	campaigns, err := s.q.ListCampaign(ctx)
	if err != nil {
		return nil, err
	}
	var list []CampaignModel
	for _, c := range campaigns {
		list = append(list, CampaignModel{
			ID:          c.ID,
			CreatedAt:   c.CreatedAt,
			UUID:        c.Uuid,
			Name:        c.Name,
			Description: c.Description,
			Count:       c.DomainCount,
			V6Ready:     c.V6ReadyCount,
		})
	}
	return list, nil
}

// GetCampaign returns a campaign.
func (s *CampaignService) GetCampaign(ctx context.Context, id uuid.UUID) (CampaignModel, error) {
	c, err := s.q.GetCampaignByUUID(ctx, id)
	if err != nil {
		return CampaignModel{}, err
	}
	return CampaignModel{
		ID:          c.ID,
		CreatedAt:   c.CreatedAt,
		UUID:        c.Uuid,
		Name:        c.Name,
		Description: c.Description,
		Count:       c.DomainCount,
		V6Ready:     c.V6ReadyCount,
	}, nil
}

// CreateCampaign creates a new campaign and returns the new CampaignModel.
func (s *CampaignService) CreateCampaign(ctx context.Context, name, description string) (CampaignModel, error) {
	c, err := s.q.CreateCampaign(ctx, db.CreateCampaignParams{
		Name:        name,
		Description: description,
	})
	if err != nil {
		return CampaignModel{}, err
	}
	return CampaignModel{
		ID:          c.ID,
		UUID:        c.Uuid,
		Name:        c.Name,
		Description: c.Description,
	}, nil
}

// CreateOrUpdateCampaign creates or updates a campaign.
func (s *CampaignService) CreateOrUpdateCampaign(ctx context.Context, campaign CampaignModel) (CampaignModel, error) {
	c, err := s.q.CreateOrUpdateCampaign(ctx, db.CreateOrUpdateCampaignParams{
		Uuid:        campaign.UUID,
		Name:        campaign.Name,
		Description: campaign.Description,
	})
	if err != nil {
		return CampaignModel{}, err
	}
	return CampaignModel{
		ID:          c.ID,
		UUID:        c.Uuid,
		Name:        c.Name,
		Description: c.Description,
	}, nil
}

// ListCampaignDomain lists all domains for a campaign.
func (s *CampaignService) ListCampaignDomain(ctx context.Context, campaignID uuid.UUID, offset, limit int64) ([]CampaignDomainModel, error) {
	domains, err := s.q.ListCampaignDomain(ctx, db.ListCampaignDomainParams{
		CampaignID: campaignID,
		Offset:     offset,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	var list []CampaignDomainModel
	for _, d := range domains {
		list = append(list, CampaignDomainModel{
			ID:           d.ID,
			Site:         d.Site,
			CampaignID:   d.CampaignID,
			BaseDomain:   d.BaseDomain,
			WwwDomain:    d.WwwDomain,
			Nameserver:   d.Nameserver,
			MXRecord:     d.MxRecord,
			V6Only:       d.V6Only,
			TsBaseDomain: TimeNull(d.TsBaseDomain),
			TsWwwDomain:  TimeNull(d.TsWwwDomain),
			TsNameserver: TimeNull(d.TsNameserver),
			TsMXRecord:   TimeNull(d.TsMxRecord),
			TsV6Only:     TimeNull(d.TsV6Only),
			TsCheck:      TimeNull(d.TsCheck),
			TsUpdated:    TimeNull(d.TsUpdated),
			AsName:       StringNull(d.Asname),
			Country:      StringNull(d.CountryName),
		})
	}
	return list, nil
}

// DeleteCampaignDomain deletes a domain from a campaign.
func (s *CampaignService) DeleteCampaignDomain(ctx context.Context, campaignID uuid.UUID, domain string) error {
	err := s.q.DeleteCampaignDomain(ctx, db.DeleteCampaignDomainParams{
		CampaignID: campaignID,
		Site:       domain,
	})
	if err != nil {
		return err
	}
	return nil
}

// GetCampaignDomainsByName returns a list of domains from a campaign by name.
func (s *CampaignService) GetCampaignDomainsByName(ctx context.Context, searchString string, offset, limit int64) ([]CampaignDomainModel, error) {
	domains, err := s.q.GetCampaignDomainsByName(ctx, db.GetCampaignDomainsByNameParams{
		Column1: NullString(searchString),
		Offset:  offset,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}

	var list []CampaignDomainModel
	for _, d := range domains {
		list = append(list, CampaignDomainModel{
			ID:         d.ID,
			Site:       d.Site,
			BaseDomain: d.BaseDomain,
			WwwDomain:  d.WwwDomain,
			Nameserver: d.Nameserver,
			MXRecord:   d.MxRecord,
			V6Only:     d.V6Only,
			CampaignID: d.CampaignID,
		})
	}
	return list, nil
}

// CampaignDomainLog represents a crawler data point.
type CampaignDomainLog struct {
	ID   int64
	Time time.Time
	Data pgtype.JSONB
}

// StoreCampaignDomainLog saves a crawl log for a campaign domain.
func (s *CampaignService) StoreCampaignDomainLog(ctx context.Context, domain int64, data any) error {
	// Encode the data to a []byte
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Create a new pgtype.JSONB struct
	jsonb := &pgtype.JSONB{}

	// Set the data on the pgtype.JSONB struct
	if err := jsonb.Set(dataBytes); err != nil {
		return err
	}

	return s.q.StoreCampaignDomainLog(ctx, db.StoreCampaignDomainLogParams{
		DomainID: domain,
		Data:     *jsonb,
	})
}

// GetCampaignDomainLog retrieves all the logs for a specified domain.
func (s *CampaignService) GetCampaignDomainLog(ctx context.Context, uuid uuid.UUID, domain string) ([]CampaignDomainLog, error) {
	// Get the domain ID from the database
	d, err := s.q.ViewCampaignDomain(ctx, db.ViewCampaignDomainParams{
		CampaignID: uuid,
		Site:       domain,
	})
	if err != nil {
		return []CampaignDomainLog{}, err
	}

	logs, err := s.q.GetCampaignDomainLog(ctx, d.ID)
	if err != nil {
		return nil, err
	}

	var logList []CampaignDomainLog
	for _, log := range logs {
		logList = append(logList, CampaignDomainLog{
			ID:   log.ID,
			Time: log.Time,
			Data: log.Data,
		})
	}
	return logList, nil
}
