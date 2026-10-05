package archive

import (
	"context"
	"math"
	"time"

	"github.com/runlevel-six/stormkeep/internal/wx"
)

// Bucket is the archive summarized over one stretch of time, for a chart.
// Values are NaN where there was no data.
type Bucket struct {
	Time     time.Time // start of the bucket
	Temp     float64   // °C, mean
	Dew      float64   // °C, mean
	Humidity float64   // %, mean
	Pressure float64   // hPa at sea level, mean
	Wind     float64   // m/s, mean
	Gust     float64   // m/s, highest
	WindDir  float64   // degrees, mean direction weighted by speed
	Rain     float64   // mm, total
	UV       float64   // highest
	Solar    float64   // W/m², mean
	Strikes  float64   // total
}

// Series summarizes the archive between from and to into buckets of the given
// width, oldest first. A bucket with no records is still returned, all NaN, so
// a chart shows the gap rather than drawing a line across it.
func (a *Archive) Series(ctx context.Context, from, to time.Time, width time.Duration) ([]Bucket, error) {
	recs, err := a.Records(ctx, from, to)
	if err != nil {
		return nil, err
	}
	n := int(to.Sub(from) / width)
	if to.Sub(from)%width != 0 {
		n++
	}
	accs := make([]acc, n)
	for _, r := range recs {
		// A record is stamped at the end of its interval: file it under the
		// bucket the interval mostly fell in.
		i := int(r.Time.Add(-time.Second).Sub(from) / width)
		if i < 0 || i >= n {
			continue
		}
		accs[i].add(r, a.altitude)
	}
	out := make([]Bucket, n)
	for i := range accs {
		out[i] = accs[i].bucket(from.Add(time.Duration(i) * width))
	}
	return out, nil
}

type mean struct{ sum, n float64 }

func (m *mean) add(v float64) {
	if !math.IsNaN(v) {
		m.sum += v
		m.n++
	}
}

func (m mean) value() float64 {
	if m.n == 0 {
		return math.NaN()
	}
	return m.sum / m.n
}

type acc struct {
	temp, dew, hum, press, wind, solar mean
	gust, uv                           float64
	rain, strikes                      float64
	anyRain, anyStrikes, any           bool
	u, v                               float64 // wind vector, for the mean direction
}

func (b *acc) add(r Record, altitude float64) {
	if !b.any {
		b.gust, b.uv = math.NaN(), math.NaN()
		b.any = true
	}
	b.temp.add(r.Temp)
	b.dew.add(wx.DewPoint(r.Temp, r.Humidity))
	b.hum.add(r.Humidity)
	b.press.add(wx.SeaLevel(r.StationPress, altitude, r.Temp))
	b.wind.add(r.Wind)
	b.solar.add(r.Solar)
	b.gust = maxNaN(b.gust, r.Gust)
	b.uv = maxNaN(b.uv, r.UV)
	if !math.IsNaN(r.Rain) {
		b.rain += r.Rain
		b.anyRain = true
	}
	if !math.IsNaN(r.Strikes) {
		b.strikes += r.Strikes
		b.anyStrikes = true
	}
	if !math.IsNaN(r.WindDir) && !math.IsNaN(r.Wind) {
		rad := r.WindDir * math.Pi / 180
		b.u += r.Wind * math.Sin(rad)
		b.v += r.Wind * math.Cos(rad)
	}
}

func (b *acc) bucket(t time.Time) Bucket {
	nan := math.NaN()
	out := Bucket{Time: t, Gust: nan, UV: nan, Rain: nan, Strikes: nan, WindDir: nan}
	out.Temp, out.Dew, out.Humidity = b.temp.value(), b.dew.value(), b.hum.value()
	out.Pressure, out.Wind, out.Solar = b.press.value(), b.wind.value(), b.solar.value()
	if !b.any {
		return out
	}
	out.Gust, out.UV = b.gust, b.uv
	if b.anyRain {
		out.Rain = b.rain
	}
	if b.anyStrikes {
		out.Strikes = b.strikes
	}
	if b.u != 0 || b.v != 0 {
		out.WindDir = math.Mod(math.Atan2(b.u, b.v)*180/math.Pi+360, 360)
	}
	return out
}

func maxNaN(a, b float64) float64 {
	switch {
	case math.IsNaN(a):
		return b
	case math.IsNaN(b):
		return a
	}
	return math.Max(a, b)
}
