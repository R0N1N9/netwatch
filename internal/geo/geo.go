// Package geo maps an IP to an approximate globe position. It uses an offline
// deterministic placement (no external calls, privacy-preserving) that clusters
// addresses onto plausible land regions. A real MaxMind/GeoLite DB can be
// slotted in behind Locate later; results are marked Approx until then.
package geo

import (
	"hash/fnv"
	"net"
)

// Loc is a globe position.
type Loc struct {
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Region string  `json:"region"`
	Approx bool    `json:"approx"`
}

type region struct {
	name                         string
	latMin, latMax, lonMin, lonMax float64
}

var regions = []region{
	{"N. America", 30, 50, -122, -74},
	{"S. America", -30, 5, -72, -42},
	{"Europe", 40, 58, -6, 28},
	{"Africa", -18, 22, 8, 36},
	{"W. Asia", 24, 42, 44, 70},
	{"S. Asia", 10, 30, 70, 90},
	{"E. Asia", 24, 42, 105, 140},
	{"Oceania", -38, -20, 134, 152},
}

// Locate returns an approximate position for an IP string.
func Locate(ipStr string) Loc {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return Loc{Region: "unknown", Approx: true}
	}
	h := fnv.New64a()
	h.Write(ip)
	v := h.Sum64()
	r := regions[int(v%uint64(len(regions)))]
	f1 := float64((v>>8)&0xffff) / 65535.0
	f2 := float64((v>>24)&0xffff) / 65535.0
	return Loc{
		Lat:    r.latMin + f1*(r.latMax-r.latMin),
		Lon:    r.lonMin + f2*(r.lonMax-r.lonMin),
		Region: r.name,
		Approx: true,
	}
}
