package places

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// ---- Photon (komoot, OpenStreetMap data; no key) ----------------------------------
//
// The public instance at photon.komoot.io is free for fair use and throttles
// heavy users; set PLACES_PHOTON_URL to a self-hosted Photon for volume, or
// MAPBOX_TOKEN to use Mapbox. Data © OpenStreetMap contributors (ODbL): the
// map UI shows the attribution.

type photon struct {
	upstream
	baseURL   string
	countries []string
}

func (p *photon) Name() string { return "photon" }

type photonResponse struct {
	Features []struct {
		Geometry struct {
			Coordinates []float64 `json:"coordinates"` // [lng, lat]
		} `json:"geometry"`
		Properties struct {
			Name        string `json:"name"`
			HouseNumber string `json:"housenumber"`
			Street      string `json:"street"`
			City        string `json:"city"`
			District    string `json:"district"`
			Locality    string `json:"locality"`
			State       string `json:"state"`
			Postcode    string `json:"postcode"`
			CountryCode string `json:"countrycode"`
			Type        string `json:"type"` // house, street, city, district, locality, county, state, country, other
		} `json:"properties"`
	} `json:"features"`
}

func (p *photon) Search(ctx context.Context, query string, near *LatLng) ([]GeoResult, error) {
	limit := geocodeLimit
	if len(p.countries) > 0 {
		limit *= 3 // Photon has no country filter: over-fetch, then keep allowed countries
	}
	v := url.Values{
		"q":       {query},
		"limit":   {strconv.Itoa(limit)},
		"osm_tag": {"!railway", "!public_transport", "!highway:bus_stop"},
	}
	if near != nil {
		v.Set("lat", formatCoord(near.Lat))
		v.Set("lon", formatCoord(near.Lng))
		v.Set("zoom", "12")
	}
	var r photonResponse
	if err := p.getJSON(ctx, p.baseURL+"/api/?"+v.Encode(), &r); err != nil {
		return nil, err
	}
	return p.parse(r, geocodeLimit), nil
}

func (p *photon) Reverse(ctx context.Context, lat, lng float64) (*GeoResult, error) {
	v := url.Values{"lat": {formatCoord(lat)}, "lon": {formatCoord(lng)}, "limit": {"1"}}
	var r photonResponse
	if err := p.getJSON(ctx, p.baseURL+"/reverse?"+v.Encode(), &r); err != nil {
		return nil, err
	}
	if out := p.parse(r, 1); len(out) > 0 {
		return &out[0], nil
	}
	return nil, nil
}

func (p *photon) parse(r photonResponse, limit int) []GeoResult {
	out := []GeoResult{}
	seen := map[string]bool{}
	for _, f := range r.Features {
		pr := f.Properties
		if len(f.Geometry.Coordinates) < 2 || !allowedCountry(p.countries, pr.CountryCode) {
			continue
		}
		g := GeoResult{
			Region:      pr.State,
			Postcode:    pr.Postcode,
			CountryCode: strings.ToUpper(pr.CountryCode),
			Lng:         f.Geometry.Coordinates[0],
			Lat:         f.Geometry.Coordinates[1],
			City:        firstNonEmpty(pr.City, pr.Locality, pr.District),
		}
		switch {
		case pr.Street != "" && pr.HouseNumber != "":
			g.Street = pr.HouseNumber + " " + pr.Street
		case pr.Street != "":
			g.Street = pr.Street
		}
		switch pr.Type {
		case "street":
			if g.Street == "" {
				g.Street = pr.Name
			}
		case "city":
			g.City = firstNonEmpty(pr.Name, g.City)
		case "state", "country":
			g.Region = firstNonEmpty(g.Region, pr.Name)
		default:
			if pr.Name != g.Street {
				g.Name = pr.Name
			}
		}
		g.Label = label(g)
		if g.Label == "" || seen[g.Label] {
			continue
		}
		seen[g.Label] = true
		out = append(out, g)
		if len(out) == limit {
			break
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- Mapbox Geocoding v6 (MAPBOX_TOKEN) ----------------------------------------------

const mapboxBaseURL = "https://api.mapbox.com/search/geocode/v6"

type mapbox struct {
	upstream
	token     string
	baseURL   string
	countries []string
}

func (m *mapbox) Name() string { return "mapbox" }

type mapboxName struct {
	Name string `json:"name"`
}

type mapboxResponse struct {
	Features []struct {
		Properties struct {
			FeatureType string `json:"feature_type"`
			Name        string `json:"name"`
			FullAddress string `json:"full_address"`
			Coordinates struct {
				Longitude float64 `json:"longitude"`
				Latitude  float64 `json:"latitude"`
			} `json:"coordinates"`
			Context struct {
				Address  *mapboxName `json:"address"`
				Street   *mapboxName `json:"street"`
				Postcode *mapboxName `json:"postcode"`
				Place    *mapboxName `json:"place"`
				Locality *mapboxName `json:"locality"`
				Region   *struct {
					Name       string `json:"name"`
					RegionCode string `json:"region_code"`
				} `json:"region"`
				Country *struct {
					CountryCode string `json:"country_code"`
				} `json:"country"`
			} `json:"context"`
		} `json:"properties"`
	} `json:"features"`
}

func (m *mapbox) params(v url.Values) url.Values {
	v.Set("access_token", m.token)
	if len(m.countries) > 0 {
		v.Set("country", strings.Join(m.countries, ","))
	}
	return v
}

func (m *mapbox) Search(ctx context.Context, query string, near *LatLng) ([]GeoResult, error) {
	v := url.Values{
		"q":            {query},
		"autocomplete": {"true"},
		"limit":        {strconv.Itoa(geocodeLimit)},
		"types":        {"address,street,postcode,place,locality,neighborhood"},
	}
	if near != nil {
		v.Set("proximity", formatCoord(near.Lng)+","+formatCoord(near.Lat))
	}
	var r mapboxResponse
	if err := m.getJSON(ctx, m.baseURL+"/forward?"+m.params(v).Encode(), &r); err != nil {
		return nil, err
	}
	return m.parse(r, geocodeLimit), nil
}

func (m *mapbox) Reverse(ctx context.Context, lat, lng float64) (*GeoResult, error) {
	v := url.Values{
		"longitude": {formatCoord(lng)},
		"latitude":  {formatCoord(lat)},
		"types":     {"address,street,postcode,place"},
		"limit":     {"1"},
	}
	var r mapboxResponse
	if err := m.getJSON(ctx, m.baseURL+"/reverse?"+m.params(v).Encode(), &r); err != nil {
		return nil, err
	}
	if out := m.parse(r, 1); len(out) > 0 {
		return &out[0], nil
	}
	return nil, nil
}

func (m *mapbox) parse(r mapboxResponse, limit int) []GeoResult {
	out := []GeoResult{}
	for _, f := range r.Features {
		pr := f.Properties
		c := pr.Context
		g := GeoResult{Lat: pr.Coordinates.Latitude, Lng: pr.Coordinates.Longitude}
		if c.Country != nil {
			g.CountryCode = strings.ToUpper(c.Country.CountryCode)
		}
		if !allowedCountry(m.countries, g.CountryCode) {
			continue
		}
		switch pr.FeatureType {
		case "address":
			g.Street = pr.Name
			if c.Address != nil && c.Address.Name != "" {
				g.Street = c.Address.Name
			}
		case "street":
			g.Street = pr.Name
		case "place":
			g.City = pr.Name
		case "postcode":
			g.Postcode = pr.Name
		default: // locality, neighborhood
			g.Name = pr.Name
		}
		if g.City == "" && c.Place != nil {
			g.City = c.Place.Name
		}
		if g.City == "" && c.Locality != nil {
			g.City = c.Locality.Name
		}
		if g.Postcode == "" && c.Postcode != nil {
			g.Postcode = c.Postcode.Name
		}
		if c.Region != nil {
			g.Region = c.Region.Name
			if c.Region.RegionCode != "" && g.CountryCode == "US" {
				g.Region = strings.ToUpper(c.Region.RegionCode)
			}
		}
		g.Label = firstNonEmpty(pr.FullAddress, label(g))
		if g.Label == "" {
			continue
		}
		out = append(out, g)
		if len(out) == limit {
			break
		}
	}
	return out
}
