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
	ID           int64     `json:"id"`
	Site         string    `json:"site"`
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
	Rank         int64     `json:"rank"`
}

// Status a domain can have.
const (
	IPv6Available  = "supported"   // IPv6 address found
	IPv4Only       = "unsupported" // IPv4 address found but no IPv6
	NoRecordsFound = "no_record"   // No IPv4 or IPv6 records found
)

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
func (s *DomainService) ListDomain(ctx context.Context, offset, limit int64) ([]DomainModel, error) {
	domains, err := s.q.ListDomain(ctx, db.ListDomainParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:           IntNull(d.ID),
			Site:         StringNull(d.Site),
			BaseDomain:   StringNull(d.BaseDomain),
			WwwDomain:    StringNull(d.WwwDomain),
			Nameserver:   StringNull(d.Nameserver),
			MXRecord:     StringNull(d.MxRecord),
			V6Only:       StringNull(d.V6Only),
			AsName:       StringNull(d.Asname),
			Country:      StringNull(d.CountryName),
			TsBaseDomain: TimeNull(d.TsBaseDomain),
			TsWwwDomain:  TimeNull(d.TsWwwDomain),
			TsNameserver: TimeNull(d.TsNameserver),
			TsMXRecord:   TimeNull(d.TsMxRecord),
			TsV6Only:     TimeNull(d.TsV6Only),
			TsCheck:      TimeNull(d.TsCheck),
			TsUpdated:    TimeNull(d.TsUpdated),
			Rank:         d.Rank,
		})
	}
	return list, nil
}

// ListDomainHeroes lists all domains.
func (s *DomainService) ListDomainHeroes(ctx context.Context, offset, limit int64) ([]DomainModel, error) {
	domains, err := s.q.ListDomainHeroes(ctx, db.ListDomainHeroesParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:           IntNull(d.ID),
			Site:         StringNull(d.Site),
			BaseDomain:   StringNull(d.BaseDomain),
			WwwDomain:    StringNull(d.WwwDomain),
			Nameserver:   StringNull(d.Nameserver),
			MXRecord:     StringNull(d.MxRecord),
			V6Only:       StringNull(d.V6Only),
			AsName:       StringNull(d.Asname),
			Country:      StringNull(d.CountryName),
			TsBaseDomain: TimeNull(d.TsBaseDomain),
			TsWwwDomain:  TimeNull(d.TsWwwDomain),
			TsNameserver: TimeNull(d.TsNameserver),
			TsMXRecord:   TimeNull(d.TsMxRecord),
			TsV6Only:     TimeNull(d.TsV6Only),
			TsCheck:      TimeNull(d.TsCheck),
			TsUpdated:    TimeNull(d.TsUpdated),
			Rank:         d.Rank,
		})
	}
	return list, nil
}

// CrawlDomain lists all domains available for crawling
func (s *DomainService) CrawlDomain(ctx context.Context, lastProcessedID, limit int64) ([]DomainModel, error) {
	domains, err := s.q.CrawlDomain(ctx, db.CrawlDomainParams{
		ID:    lastProcessedID,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:           d.ID,
			Site:         d.Site,
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

// UpdateDomain updates a domain.
func (s *DomainService) UpdateDomain(ctx context.Context, domain DomainModel) error {
	err := s.q.UpdateDomain(ctx, db.UpdateDomainParams{
		Site:         domain.Site,
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

// ViewDomain list a domain.
func (s *DomainService) ViewDomain(ctx context.Context, domain string) (DomainModel, error) {
	d, err := s.q.ViewDomain(ctx, NullString(domain))
	if err != nil {
		return DomainModel{}, err
	}
	return DomainModel{
		ID:           IntNull(d.ID),
		Site:         StringNull(d.Site),
		BaseDomain:   StringNull(d.BaseDomain),
		WwwDomain:    StringNull(d.WwwDomain),
		Nameserver:   StringNull(d.Nameserver),
		MXRecord:     StringNull(d.MxRecord),
		V6Only:       StringNull(d.V6Only),
		AsName:       StringNull(d.Asname),
		Country:      StringNull(d.CountryName),
		TsBaseDomain: TimeNull(d.TsBaseDomain),
		TsWwwDomain:  TimeNull(d.TsWwwDomain),
		TsNameserver: TimeNull(d.TsNameserver),
		TsMXRecord:   TimeNull(d.TsMxRecord),
		TsV6Only:     TimeNull(d.TsV6Only),
		TsCheck:      TimeNull(d.TsCheck),
		TsUpdated:    TimeNull(d.TsUpdated),
		Rank:         d.Rank,
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
func (s *DomainService) GetDomainsByName(ctx context.Context, searchString string, offset, limit int64) ([]DomainModel, error) {
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
			ID:           IntNull(d.ID),
			Site:         StringNull(d.Site),
			BaseDomain:   StringNull(d.BaseDomain),
			WwwDomain:    StringNull(d.WwwDomain),
			Nameserver:   StringNull(d.Nameserver),
			MXRecord:     StringNull(d.MxRecord),
			V6Only:       StringNull(d.V6Only),
			AsName:       StringNull(d.Asname),
			Country:      StringNull(d.CountryName),
			TsBaseDomain: TimeNull(d.TsBaseDomain),
			TsWwwDomain:  TimeNull(d.TsWwwDomain),
			TsNameserver: TimeNull(d.TsNameserver),
			TsMXRecord:   TimeNull(d.TsMxRecord),
			TsV6Only:     TimeNull(d.TsV6Only),
			TsCheck:      TimeNull(d.TsCheck),
			TsUpdated:    TimeNull(d.TsUpdated),
			Rank:         d.Rank,
		})
	}
	return list, nil
}

// GetCampaignDomainsByName returns a list of domains from a campaign by name.
// func (s *DomainService) GetCampaignDomainsByName(ctx context.Context, searchString string, offset, limit int32) ([]CampaignDomainModel, error) {
// 	domains, err := s.q.GetCampaignDomainsByName(ctx, db.GetCampaignDomainsByNameParams{
// 		Column1: NullString(searchString),
// 		Offset:  offset,
// 		Limit:   limit,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}

// 	var list []CampaignDomainModel
// 	for _, d := range domains {
// 		list = append(list, CampaignDomainModel{
// 			ID:         d.ID,
// 			Site:       d.Site,
// 			BaseDomain:  d.BaseDomain,
// 			WwwDomain:   d.WwwDomain,
// 			Nameserver:    d.Nameserver,
// 			V6Only:  d.V6Only,
// 			CampaignID: d.CampaignID,
// 		})
// 	}
// 	return list, nil
// }

// ListDomainShamers lists 10-ish domains without IPv6 support.
func (s *DomainService) ListDomainShamers(ctx context.Context) ([]DomainModel, error) {
	domains, err := s.q.ListDomainShamers(ctx)
	if err != nil {
		return nil, err
	}
	var list []DomainModel
	for _, d := range domains {
		list = append(list, DomainModel{
			ID:           d.ID,
			Site:         d.Site,
			BaseDomain:   d.BaseDomain,
			WwwDomain:    d.WwwDomain,
			Nameserver:   d.Nameserver,
			V6Only:       d.V6Only,
			TsBaseDomain: TimeNull(d.TsBaseDomain),
			TsWwwDomain:  TimeNull(d.TsWwwDomain),
			TsNameserver: TimeNull(d.TsNameserver),
			TsV6Only:     TimeNull(d.TsV6Only),
			TsCheck:      TimeNull(d.TsCheck),
			TsUpdated:    TimeNull(d.TsUpdated),
		})
	}
	return list, nil
}

// InitSpaceTimestamps spaces out the timestamps for all domains.
// Is used to prevent all domains from being crawled at the same time.
func (s *DomainService) InitSpaceTimestamps(ctx context.Context) error {
	err := s.q.InitSpaceTimestamps(ctx)
	if err != nil {
		return err
	}
	return nil
}

// CrawlerStat represents a domain statistic.
type CrawlerStat struct {
	Domains       int64 `json:"domains"`
	BaseDomain    int64 `json:"base_domain"`
	WwwDomain     int64 `json:"www_domain"`
	Nameserver    int64 `json:"nameserver"`
	MxRecord      int64 `json:"mx_record"`
	Heroes        int64 `json:"heroes"`
	TopHeroes     int64 `json:"top_heroes"`
	TopNameserver int64 `json:"top_nameserver"`
}

// CrawlerStats retrieves the statistics for all crawled domains.
func (s *DomainService) CrawlerStats(ctx context.Context) (CrawlerStat, error) {
	stats, err := s.q.CrawlerStats(ctx)
	if err != nil {
		return CrawlerStat{}, err
	}
	return CrawlerStat{
		Domains:       stats.Domains,
		BaseDomain:    stats.BaseDomain,
		WwwDomain:     stats.WwwDomain,
		Nameserver:    stats.Nameserver,
		MxRecord:      stats.MxRecord,
		Heroes:        stats.Heroes,
		TopHeroes:     stats.TopHeroes,
		TopNameserver: stats.TopNameserver,
	}, nil
}
