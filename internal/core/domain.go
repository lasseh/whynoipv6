package core

import (
	"context"
	"time"
	"whynoipv6/internal/postgres/db"
)

// DomainService is a service for managing scans.
type DomainService struct {
	q *db.Queries
}

// NewDomainService creates a new ScanService.
func NewDomainService(d db.DBTX) *DomainService {
	return &DomainService{
		q: db.New(d),
	}
}

// DomainModel represents a scan.
type DomainModel struct {
	ID        int64     `json:"id"`
	Site      string    `json:"site"`
	CheckAAAA bool      `json:"check_aaaa"`
	CheckWWW  bool      `json:"check_www"`
	CheckNS   bool      `json:"check_ns"`
	CheckCurl bool      `json:"check_curl"`
	AsnID     int64     `json:"asn_id"`
	AsName    string    `json:"asn"`
	CountryID int64     `json:"country_id"`
	Country   string    `json:"country"`
	TsAAAA    time.Time `json:"ts_aaaa"`
	TsWWW     time.Time `json:"ts_www"`
	TsNS      time.Time `json:"ts_ns"`
	TsCurl    time.Time `json:"ts_curl"`
	TsCheck   time.Time `json:"ts_check"`
	TsUpdated time.Time `json:"ts_updated"`
	Rank      int64     `json:"rank"`
}

// InsertScanModel represents a scan.
type InsertScanModel struct {
	Site string `json:"site"`
}

// InsertDomain creates a new scan.
func (s *DomainService) InsertDomain(ctx context.Context, site string) error {
	err := s.q.InsertDomain(ctx, site)
	if err != nil {
		return err
	}
	return nil
}

// ListDomain lists all domains.
func (s *DomainService) ListDomain(ctx context.Context) ([]DomainModel, error) {
	domains, err := s.q.ListDomain(ctx)
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:        IntNull(d.ID),
			Site:      StringNull(d.Site),
			CheckAAAA: BoolNull(d.CheckAaaa),
			CheckWWW:  BoolNull(d.CheckWww),
			CheckNS:   BoolNull(d.CheckNs),
			CheckCurl: BoolNull(d.CheckCurl),
			AsName:    StringNull(d.Asname),
			Country:   StringNull(d.CountryName),
			TsAAAA:    TimeNull(d.TsAaaa),
			TsWWW:     TimeNull(d.TsWww),
			TsNS:      TimeNull(d.TsNs),
			TsCurl:    TimeNull(d.TsCurl),
			TsCheck:   TimeNull(d.TsCheck),
			TsUpdated: TimeNull(d.TsUpdated),
			Rank:      d.Rank,
		})
	}
	return list, nil
}

// ListDomainHeroes lists all domains.
func (s *DomainService) ListDomainHeroes(ctx context.Context) ([]DomainModel, error) {
	domains, err := s.q.ListDomainHeroes(ctx)
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:        IntNull(d.ID),
			Site:      StringNull(d.Site),
			CheckAAAA: BoolNull(d.CheckAaaa),
			CheckWWW:  BoolNull(d.CheckWww),
			CheckNS:   BoolNull(d.CheckNs),
			CheckCurl: BoolNull(d.CheckCurl),
			AsName:    StringNull(d.Asname),
			Country:   StringNull(d.CountryName),
			TsAAAA:    TimeNull(d.TsAaaa),
			TsWWW:     TimeNull(d.TsWww),
			TsNS:      TimeNull(d.TsNs),
			TsCurl:    TimeNull(d.TsCurl),
			TsCheck:   TimeNull(d.TsCheck),
			TsUpdated: TimeNull(d.TsUpdated),
			Rank:      d.Rank,
		})
	}
	return list, nil
}

// CrawlDomain lists all domains available for crawling
func (s *DomainService) CrawlDomain(ctx context.Context, offset, limit int32) ([]DomainModel, error) {
	domains, err := s.q.CrawlDomain(ctx, db.CrawlDomainParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:        d.ID,
			Site:      d.Site,
			CheckAAAA: d.CheckAaaa,
			CheckWWW:  d.CheckWww,
			CheckNS:   d.CheckNs,
			CheckCurl: d.CheckCurl,
			TsAAAA:    TimeNull(d.TsAaaa),
			TsWWW:     TimeNull(d.TsWww),
			TsNS:      TimeNull(d.TsNs),
			TsCurl:    TimeNull(d.TsCurl),
			TsCheck:   TimeNull(d.TsCheck),
			TsUpdated: TimeNull(d.TsUpdated),
		})
	}
	return list, nil
}

// UpdateDomain updates a domain.
func (s *DomainService) UpdateDomain(ctx context.Context, domain DomainModel) error {
	err := s.q.UpdateDomain(ctx, db.UpdateDomainParams{
		Site:      domain.Site,
		CheckAaaa: domain.CheckAAAA,
		CheckWww:  domain.CheckWWW,
		CheckNs:   domain.CheckNS,
		CheckCurl: domain.CheckCurl,
		AsnID:     NullInt(domain.AsnID),
		CountryID: NullInt(domain.CountryID),
		TsAaaa:    NullTime(domain.TsAAAA),
		TsWww:     NullTime(domain.TsWWW),
		TsNs:      NullTime(domain.TsNS),
		TsCurl:    NullTime(domain.TsCurl),
		TsCheck:   NullTime(domain.TsCheck),
		TsUpdated: NullTime(domain.TsUpdated),
	})
	if err != nil {
		return err
	}
	return nil
}

// ViewDomain list a domain.
func (s *DomainService) ViewDomain(ctx context.Context, domain string) (DomainModel, error) {
	d, err := s.q.ViewDomain(ctx, NullString(domain))
	if err != nil {
		return DomainModel{}, err
	}
	return DomainModel{
		ID:        IntNull(d.ID),
		Site:      StringNull(d.Site),
		CheckAAAA: BoolNull(d.CheckAaaa),
		CheckWWW:  BoolNull(d.CheckWww),
		CheckNS:   BoolNull(d.CheckNs),
		CheckCurl: BoolNull(d.CheckCurl),
		AsName:    StringNull(d.Asname),
		Country:   StringNull(d.CountryName),
		TsAAAA:    TimeNull(d.TsAaaa),
		TsWWW:     TimeNull(d.TsWww),
		TsNS:      TimeNull(d.TsNs),
		TsCurl:    TimeNull(d.TsCurl),
		TsCheck:   TimeNull(d.TsCheck),
		TsUpdated: TimeNull(d.TsUpdated),
		Rank:      d.Rank,
	}, nil
}

// DisableDomain disables a domain.
func (s *DomainService) DisableDomain(ctx context.Context, domain string) error {
	err := s.q.DisableDomain(ctx, domain)
	if err != nil {
		return err
	}
	return nil
}

// GetDomainsByName returns a list of domains by name.
func (s *DomainService) GetDomainsByName(ctx context.Context, searchString string, offset, limit int32) ([]DomainModel, error) {
	domains, err := s.q.GetDomainsByName(ctx, db.GetDomainsByNameParams{
		Column1: NullString(searchString),
		Offset:  offset,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}

	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:        IntNull(d.ID),
			Site:      StringNull(d.Site),
			CheckAAAA: BoolNull(d.CheckAaaa),
			CheckWWW:  BoolNull(d.CheckWww),
			CheckNS:   BoolNull(d.CheckNs),
			CheckCurl: BoolNull(d.CheckCurl),
			AsName:    StringNull(d.Asname),
			Country:   StringNull(d.CountryName),
			TsAAAA:    TimeNull(d.TsAaaa),
			TsWWW:     TimeNull(d.TsWww),
			TsNS:      TimeNull(d.TsNs),
			TsCurl:    TimeNull(d.TsCurl),
			TsCheck:   TimeNull(d.TsCheck),
			TsUpdated: TimeNull(d.TsUpdated),
			Rank:      d.Rank,
		})
	}
	return list, nil
}

// GetCampaignDomainsByName returns a list of domains from a campaign by name.
func (s *DomainService) GetCampaignDomainsByName(ctx context.Context, searchString string, offset, limit int32) ([]CampaignDomainModel, error) {
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
			CheckAAAA:  d.CheckAaaa,
			CheckWWW:   d.CheckWww,
			CheckNS:    d.CheckNs,
			CheckCurl:  d.CheckCurl,
			CampaignID: d.CampaignID,
		})
	}
	return list, nil
}

// ListDomainShamers lists 10-ish domains without IPv6 support.
func (s *DomainService) ListDomainShamers(ctx context.Context) ([]DomainModel, error) {
	domains, err := s.q.ListDomainShamers(ctx)
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:        d.ID,
			Site:      d.Site,
			CheckAAAA: d.CheckAaaa,
			CheckWWW:  d.CheckWww,
			CheckNS:   d.CheckNs,
			CheckCurl: d.CheckCurl,
			TsAAAA:    TimeNull(d.TsAaaa),
			TsWWW:     TimeNull(d.TsWww),
			TsNS:      TimeNull(d.TsNs),
			TsCurl:    TimeNull(d.TsCurl),
			TsCheck:   TimeNull(d.TsCheck),
			TsUpdated: TimeNull(d.TsUpdated),
		})
	}
	return list, nil
}
