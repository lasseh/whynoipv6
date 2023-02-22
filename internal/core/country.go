package core

import (
	"context"
	"whynoipv6/internal/postgres/db"

	"github.com/jackc/pgtype"
)

// CountryService is a service for managing countries.
type CountryService struct {
	q *db.Queries
}

// NewCountryService creates a new CountryService.
func NewCountryService(d db.DBTX) *CountryService {
	return &CountryService{
		q: db.New(d),
	}
}

// CountryModel represents a country.
type CountryModel struct {
	ID          int64          `json:"id"`
	Country     string         `json:"country"`
	CountryCode string         `json:"country_code"`
	CountryTld  string         `json:"country_tld"`
	Sites       int32          `json:"sites"`
	V6sites     int32          `json:"v6sites"`
	Percent     pgtype.Numeric `json:"percent"`
}

// GetCountryCode gets a country by CountryCode.
func (s *CountryService) GetCountryCode(ctx context.Context, code string) (CountryModel, error) {
	country, err := s.q.GetCountry(ctx, code)
	if err != nil {
		return CountryModel{}, err
	}
	return CountryModel{
		ID:          country.ID,
		Country:     country.CountryName,
		CountryCode: country.CountryCode,
		CountryTld:  country.CountryTld,
		Sites:       country.Sites,
		V6sites:     country.V6sites,
		Percent:     country.Percent,
	}, nil
}

// GetCountryTld gets a country by CountryTLD.
func (s *CountryService) GetCountryTld(ctx context.Context, tld string) (CountryModel, error) {
	country, err := s.q.GetCountryTld(ctx, tld)
	if err != nil {
		return CountryModel{}, err
	}
	return CountryModel{
		ID:          country.ID,
		Country:     country.CountryName,
		CountryCode: country.CountryCode,
		CountryTld:  country.CountryTld,
		Sites:       country.Sites,
		V6sites:     country.V6sites,
		Percent:     country.Percent,
	}, nil
}

// List all countries.
func (s *CountryService) List(ctx context.Context) ([]CountryModel, error) {
	countries, err := s.q.ListCountry(ctx)
	if err != nil {
		return nil, err
	}
	var models []CountryModel
	for _, country := range countries {
		models = append(models, CountryModel{
			ID:          country.ID,
			Country:     country.CountryName,
			CountryCode: country.CountryCode,
			CountryTld:  country.CountryTld,
			Sites:       country.Sites,
			V6sites:     country.V6sites,
			Percent:     country.Percent,
		})
	}
	return models, nil
}

// UpdateStats updates a country stats.
// Only used by CLI crawler
func (s *CountryService) UpdateStats(ctx context.Context, id int64, params CountryModel) (CountryModel, error) {
	// Check if sites is not null
	if params.Sites == 0 {
		return CountryModel{}, nil
	}

	country, err := s.q.UpdateCountryStats(ctx, db.UpdateCountryStatsParams{
		ID:      id,
		Sites:   params.Sites,
		V6sites: params.V6sites,
		Percent: params.Percent,
	})
	if err != nil {
		return CountryModel{}, err
	}
	return CountryModel{
		ID:          country.ID,
		Country:     country.CountryName,
		CountryCode: country.CountryCode,
		CountryTld:  country.CountryTld,
		Sites:       country.Sites,
		V6sites:     country.V6sites,
		Percent:     country.Percent,
	}, nil
}

// ListDomainsByCountry gets a list of all country TLDs.
func (s *CountryService) ListDomainsByCountry(ctx context.Context, countryID int64) ([]DomainModel, error) {
	domains, err := s.q.ListDomainsByCountry(ctx, NullInt(countryID))
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

// ListDomainHeroesByCountry gets a list of all country TLDs.
func (s *CountryService) ListDomainHeroesByCountry(ctx context.Context, countryID int64) ([]DomainModel, error) {
	domains, err := s.q.ListDomainHeroesByCountry(ctx, NullInt(countryID))
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
