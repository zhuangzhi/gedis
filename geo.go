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

// 地理位置（Geo）实现，基于 Geohash 编码和有序集合（ZSet）存储。
// 使用 Haversine 公式计算球面距离。
package gedis

import "math"

const (
	earthRadiusMeters = 6372797.560856 // 地球半径（米）
	geoHashMaxBits    = 52             // Geohash 最大比特数
	geoLatMin         = -85.05112878   // 最小纬度
	geoLatMax         = 85.05112878    // 最大纬度
	geoLonMin         = -180.0         // 最小经度
	geoLonMax         = 180.0          // 最大经度
)

// geohashEncode 对经纬度进行 Geohash 编码，返回 52 位哈希值。
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

// geohashDecode 对 Geohash 编码进行解码，返回经纬度。
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

// GeoAdd 添加地理位置坐标。内部使用 Geohash 作为 ZSet 的 score。
func (db *RedisDB) GeoAdd(key string, lon, lat float64, member string) int {
	hash := geohashEncode(lon, lat)
	score := float64(hash)
	pb := Buf(member)
	n := db.ZAddBuffer(key, score, pb)
	pb.Close()
	return n
}

// GeoDist 计算两个地理位置成员之间的距离。
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

// GeoRadius 获取以指定坐标为中心、指定半径范围内的地理位置成员。
// 对应 Redis: GEORADIUS key longitude latitude radius unit
// 优化：返回 *ZSlices 替代 []string，成员在 Arena 中零拷贝读取。
// 调用方用 zs.Len()/zs.Get(i) 遍历后用 zs.Close() 归还底层缓冲区。
func (db *RedisDB) GeoRadius(key string, lon, lat, radius float64, unit string) *ZSlices {
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

	result := NewZSlices()

	x := int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, zsl.headerOff, 0)))
	for x != 0 {
		xMemberOff := int(db.arena.ReadUint32(zslNodeMemberOff(db.arena, x)))
		member := db.arena.GetSlice(xMemberOff, db.arena.SizeAt(xMemberOff))

		hash := uint64(db.arena.ReadFloat64(zslNodeScoreOff(db.arena, x)))
		mlon, mlat := geohashDecode(hash)
		dist := haversineDistance(lon, lat, mlon, mlat)

		if dist <= radiusMeters {
			result.Add(member)
		}

		x = int(db.arena.ReadUint32(zslLevelForwardOff(db.arena, x, 0)))
	}

	result.Finish()
	return result
}

// GeoRadiusByMember 获取以指定成员位置为中心、指定半径范围内的成员。
// 对应 Redis: GEORADIUSBYMEMBER key member radius unit
// 优化：返回 *ZSlices 替代 []string，内部委托 GeoRadius。
// 调用方用 zs.Len()/zs.Get(i) 遍历后用 zs.Close() 归还底层缓冲区。
func (db *RedisDB) GeoRadiusByMember(key, member string, radius float64, unit string) *ZSlices {
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
	pb := Buf(member)
	score, found := db.ZScoreBuffer(key, pb)
	pb.Close()
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
