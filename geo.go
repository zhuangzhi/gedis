// MIT License
//
// Copyright (c) 2026 Gedis Authors
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package gedis

import "math"

const (
	earthRadiusMeters = 6372797.560856
	geoHashMaxBits    = 52
	geoLatMin         = -85.05112878
	geoLatMax         = 85.05112878
	geoLonMin         = -180.0
	geoLonMax         = 180.0
)

func geohashEncode(lon, lat float64) uint64 {
	lonRange := [2]float64{geoLonMin, geoLonMax}
	latRange := [2]float64{geoLatMin, geoLatMax}

	var hash uint64
	for i := 0; i < geoHashMaxBits; i++ {
		hash <<= 1
		if i%2 == 0 {
			mid := (lonRange[0] + lonRange[1]) / 2
			if lon >= mid {
				hash |= 1
				lonRange[0] = mid
			} else {
				lonRange[1] = mid
			}
		} else {
			mid := (latRange[0] + latRange[1]) / 2
			if lat >= mid {
				hash |= 1
				latRange[0] = mid
			} else {
				latRange[1] = mid
			}
		}
	}
	return hash
}

func geohashDecode(hash uint64) (lon, lat float64) {
	lonRange := [2]float64{geoLonMin, geoLonMax}
	latRange := [2]float64{geoLatMin, geoLatMax}

	for i := 0; i < geoHashMaxBits; i++ {
		bit := (hash >> (geoHashMaxBits - 1 - i)) & 1
		if i%2 == 0 {
			mid := (lonRange[0] + lonRange[1]) / 2
			if bit == 1 {
				lonRange[0] = mid
			} else {
				lonRange[1] = mid
			}
		} else {
			mid := (latRange[0] + latRange[1]) / 2
			if bit == 1 {
				latRange[0] = mid
			} else {
				latRange[1] = mid
			}
		}
	}

	lon = (lonRange[0] + lonRange[1]) / 2
	lat = (latRange[0] + latRange[1]) / 2
	return
}

func (db *RedisDB) GeoAdd(key string, lon, lat float64, member string) int {
	hash := geohashEncode(lon, lat)
	score := float64(hash)
	return db.ZAdd(key, score, []byte(member))
}

func (db *RedisDB) GeoDist(key, member1, member2, unit string) float64 {
	_, lon1, lat1, ok1 := db.geoGetCoords(key, member1)
	_, lon2, lat2, ok2 := db.geoGetCoords(key, member2)
	if !ok1 || !ok2 {
		return -1
	}

	dist := haversineDistance(lon1, lat1, lon2, lat2)

	switch unit {
	case "m":
		return dist
	case "km":
		return dist / 1000
	case "mi":
		return dist / 1609.34
	case "ft":
		return dist / 0.3048
	default:
		return dist
	}
}

func (db *RedisDB) GeoRadius(key string, lon, lat, radius float64, unit string) []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keyBytes := []byte(key)
	headOff, ok := db.dict.Get(keyBytes)
	if !ok {
		return nil
	}

	enc := db.ObjectEncoding(headOff)
	dataOff := db.ObjectDataOffset(headOff)

	if enc != ObjEncodingSkiplist {
		return nil
	}

	radiusMeters := convertToMeters(radius, unit)
	zsl := zslLoadFromArena(db.arena, dataOff)

	var result []string

	x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
	for x != 0 {
		xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
		member := db.arena.ReadBytes(xMemberOff, db.arena.SizeAt(xMemberOff))

		hash := uint64(db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x)))
		mlon, mlat := geohashDecode(hash)
		dist := haversineDistance(lon, lat, mlon, mlat)

		if dist <= radiusMeters {
			result = append(result, string(member))
		}

		x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
	}

	return result
}

func (db *RedisDB) GeoRadiusByMember(key, member string, radius float64, unit string) []string {
	_, lon, lat, ok := db.geoGetCoords(key, member)
	if !ok {
		return nil
	}
	return db.GeoRadius(key, lon, lat, radius, unit)
}

func (db *RedisDB) GeoPos(key string, members ...string) [][2]float64 {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var result [][2]float64

	for _, member := range members {
		_, lon, lat, ok := db.geoGetCoords(key, member)
		if ok {
			result = append(result, [2]float64{lon, lat})
		} else {
			result = append(result, [2]float64{0, 0})
		}
	}

	return result
}

func (db *RedisDB) geoGetCoords(key, member string) (hash uint64, lon, lat float64, ok bool) {
	score, found := db.ZScore(key, []byte(member))
	if !found {
		return 0, 0, 0, false
	}

	hash = uint64(score)
	lon, lat = geohashDecode(hash)
	return hash, lon, lat, true
}

func haversineDistance(lon1, lat1, lon2, lat2 float64) float64 {
	dlon := degToRad(lon2 - lon1)
	dlat := degToRad(lat2 - lat1)

	lat1r := degToRad(lat1)
	lat2r := degToRad(lat2)

	a := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Sin(dlon/2)*math.Sin(dlon/2)*math.Cos(lat1r)*math.Cos(lat2r)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMeters * c
}

func degToRad(deg float64) float64 {
	return deg * math.Pi / 180
}

func convertToMeters(val float64, unit string) float64 {
	switch unit {
	case "m":
		return val
	case "km":
		return val * 1000
	case "mi":
		return val * 1609.34
	case "ft":
		return val * 0.3048
	default:
		return val
	}
}
