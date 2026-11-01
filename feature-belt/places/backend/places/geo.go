package places

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const earthRadiusM = 6371008.8

// BBox is a bounding box in GeoJSON order. MinLng > MaxLng means the box
// crosses the antimeridian (e.g. 170,-10,-170,10).
type BBox struct {
	MinLng, MinLat, MaxLng, MaxLat float64
}

func validLat(f float64) bool { return !math.IsNaN(f) && f >= -90 && f <= 90 }
func validLng(f float64) bool { return !math.IsNaN(f) && f >= -180 && f <= 180 }

// parseBBox reads "minLng,minLat,maxLng,maxLat".
func parseBBox(raw string) (BBox, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return BBox{}, fmt.Errorf("expected 4 numbers")
	}
	var v [4]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return BBox{}, fmt.Errorf("%q is not a number", p)
		}
		v[i] = f
	}
	b := BBox{MinLng: v[0], MinLat: v[1], MaxLng: v[2], MaxLat: v[3]}
	if !validLng(b.MinLng) || !validLng(b.MaxLng) || !validLat(b.MinLat) || !validLat(b.MaxLat) {
		return BBox{}, fmt.Errorf("longitudes must be -180 to 180 and latitudes -90 to 90")
	}
	if b.MinLat > b.MaxLat {
		return BBox{}, fmt.Errorf("minLat is above maxLat")
	}
	return b, nil
}

// Contains reports whether the point is inside the box.
func (b BBox) Contains(lat, lng float64) bool {
	if lat < b.MinLat || lat > b.MaxLat {
		return false
	}
	if b.MinLng <= b.MaxLng {
		return lng >= b.MinLng && lng <= b.MaxLng
	}
	return lng >= b.MinLng || lng <= b.MaxLng
}

// bboxAround returns a box enclosing radiusM around p, used to prefilter on
// the (lat, lng) index before exact haversine ordering.
func bboxAround(p LatLng, radiusM float64) BBox {
	dLat := radiusM / earthRadiusM * 180 / math.Pi
	b := BBox{MinLat: math.Max(-90, p.Lat-dLat), MaxLat: math.Min(90, p.Lat+dLat)}
	cos := math.Cos(p.Lat * math.Pi / 180)
	if b.MinLat <= -90 || b.MaxLat >= 90 || cos < 0.01 {
		b.MinLng, b.MaxLng = -180, 180 // a pole is inside: every longitude
		return b
	}
	dLng := dLat / cos
	if dLng >= 180 {
		b.MinLng, b.MaxLng = -180, 180
		return b
	}
	b.MinLng, b.MaxLng = p.Lng-dLng, p.Lng+dLng
	if b.MinLng < -180 {
		b.MinLng += 360
	}
	if b.MaxLng > 180 {
		b.MaxLng -= 360
	}
	return b
}

// haversineM is the great-circle distance in metres (mirrors distanceSQL).
func haversineM(a, b LatLng) float64 {
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(b.Lat-a.Lat), rad(b.Lng-a.Lng)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// clusterCellPx is the on-screen size of a clustering grid cell at 256 px
// tiles: points within roughly this many pixels of each other merge.
const clusterCellPx = 64

// clusterCell is the grid cell size in normalised Web Mercator units (the
// world is 1×1) at zoom z, so cells are square on screen at every latitude.
func clusterCell(zoom int) float64 {
	return float64(clusterCellPx) / (256 * math.Pow(2, float64(zoom)))
}
