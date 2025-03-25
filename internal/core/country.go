package core

import (
	"context"
	"strings"

	"whynoipv6/internal/postgres/db"

	"github.com/jackc/pgtype"
)

// CountryService is a service for managing countries.
type CountryService struct {
	q *db.Queries
}

// NewCountryService creates a new CountryService instance.
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
	country, err := s.q.GetCountryTld(ctx, strings.ToUpper(tld))
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

// ListDomainsByCountry gets a list of all country TLDs.
func (s *CountryService) ListDomainsByCountry(
	ctx context.Context,
	countryID int64,
	offset, limit int64,
) ([]DomainModel, error) {
	domains, err := s.q.ListDomainsByCountry(ctx, db.ListDomainsByCountryParams{
		CountryID: NullInt(countryID),
		Offset:    offset,
		Limit:     limit,
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

// ListDomainHeroesByCountry gets a list of all country TLDs.
func (s *CountryService) ListDomainHeroesByCountry(
	ctx context.Context,
	countryID int64,
	offset, limit int64,
) ([]DomainModel, error) {
	domains, err := s.q.ListDomainHeroesByCountry(ctx, db.ListDomainHeroesByCountryParams{
		CountryID: NullInt(countryID),
		Offset:    offset,
		Limit:     limit,
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

// CalculateCountryStats calculates the statistics for a country.
func (s *CountryService) CalculateCountryStats(ctx context.Context) error {
	return s.q.CalculateCountryStats(ctx)
}
