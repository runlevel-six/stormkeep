package web

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/runlevel-six/stormkeep/internal/archive"
	"github.com/runlevel-six/stormkeep/internal/live"
	"github.com/runlevel-six/stormkeep/internal/tempest"
	"github.com/runlevel-six/stormkeep/internal/wx"
)

// Every value the API returns is in °C, m/s, hPa, mm and km; the page converts
// for display. Missing values are null.

// num is a float that encodes NaN as null and rounds away float noise.
type num float64

func (n num) MarshalJSON() ([]byte, error) {
	v := float64(n)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return []byte("null"), nil
	}
	return strconv.AppendFloat(nil, math.Round(v*100)/100, 'f', -1, 64), nil
}

// ts is a time encoded as Unix seconds, or null when zero.
type ts time.Time

func (t ts) MarshalJSON() ([]byte, error) {
	if time.Time(t).IsZero() {
		return []byte("null"), nil
	}
	return strconv.AppendInt(nil, time.Time(t).Unix(), 10), nil
}

type extreme struct {
	Value num `json:"v"`
	At    ts  `json:"t"`
}

func ext(e archive.Extreme) extreme { return extreme{num(e.Value), ts(e.At)} }

type current struct {
	Temp         num `json:"temp"`
	FeelsLike    num `json:"feels"`
	Dew          num `json:"dew"`
	Humidity     num `json:"humidity"`
	Pressure     num `json:"pressure"`      // sea level
	PressTrend   num `json:"pressureTrend"` // change over three hours
	StationPress num `json:"stationPressure"`
	Wind         num `json:"wind"`
	Gust         num `json:"gust"`
	Lull         num `json:"lull"`
	WindDir      num `json:"windDir"`
	RainRate     num `json:"rainRate"` // mm/h
	UV           num `json:"uv"`
	Solar        num `json:"solar"`
	Illuminance  num `json:"illuminance"`
	PrecipType   int `json:"precipType"`
}

type nowResponse struct {
	Name    string  `json:"name"`
	Time    ts      `json:"time"`   // when the current conditions were measured
	Source  string  `json:"source"` // "live" or "archive"
	Current current `json:"current"`

	WindNow    *windSample  `json:"windNow"`
	WindRecent []windSample `json:"windRecent"`

	Today struct {
		High    extreme `json:"high"`
		Low     extreme `json:"low"`
		Gust    extreme `json:"gust"`
		Strikes num     `json:"strikes"`
	} `json:"today"`

	Rain struct {
		Hour      num `json:"hour"`
		Day       num `json:"day"` // the last 24 hours
		Today     num `json:"today"`
		Yesterday num `json:"yesterday"`
		Month     num `json:"month"`
		Year      num `json:"year"`
		Started   ts  `json:"started"` // the station's last "rain has begun"
	} `json:"rain"`

	Lightning struct {
		Count3h int     `json:"count3h"`
		Last    *strike `json:"last"`
	} `json:"lightning"`

	Records struct {
		Since   ts      `json:"since"`
		High    extreme `json:"high"`
		Low     extreme `json:"low"`
		Gust    extreme `json:"gust"`
		Wettest extreme `json:"wettest"`
	} `json:"records"`

	Health struct {
		LastPacket ts  `json:"lastPacket"`
		Battery    num `json:"battery"`
		RSSI       int `json:"rssi"`
		HubRSSI    int `json:"hubRssi"`
		Archive    ts  `json:"archive"` // newest archive record
	} `json:"health"`

	Errors []string `json:"errors,omitempty"`
}

type windSample struct {
	T     ts  `json:"t"`
	Speed num `json:"s"`
	Dir   num `json:"d"`
}

type strike struct {
	T        ts  `json:"t"`
	Distance num `json:"km"`
}

func (s *server) handleNow(w http.ResponseWriter, r *http.Request) {
	s.serveCached(w, "now", 5*time.Second, func() (any, error) {
		return s.now0(r.Context()), nil
	})
}

// now0 assembles /api/now. A failed query costs the fields it would have
// filled, not the whole response: the live part is still worth showing when
// the archive is unreadable, and the other way round.
func (s *server) now0(ctx context.Context) nowResponse {
	var resp nowResponse
	resp.Name = s.cfg.Name
	resp.Health.Battery = num(math.NaN())
	fail := func(what string, err error) {
		s.cfg.Log.Warn("api/now", "part", what, "err", err)
		resp.Errors = append(resp.Errors, what)
	}
	now := s.now()
	loc := now.Location()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tomorrow := midnight.AddDate(0, 0, 1)
	alt := s.cfg.Altitude
	snap := s.cfg.Station.Snapshot()

	latest, latestErr := s.cfg.Archive.Latest(ctx)
	if latestErr != nil {
		fail("archive", latestErr)
	} else {
		resp.Health.Archive = ts(latest.Time)
	}

	// Current conditions: the live observation when there is one, else the
	// newest archive record (just after a restart, say).
	curTemp, curPress := math.NaN(), math.NaN()
	var curTime time.Time
	switch {
	case snap.Obs != nil:
		o := snap.Obs
		curTime, curTemp, curPress = o.Time, o.Temp, o.StationPress
		resp.Source = "live"
		resp.Current = current{
			Temp: num(o.Temp), FeelsLike: num(wx.FeelsLike(o.Temp, o.Humidity, o.WindAvg)),
			Dew: num(wx.DewPoint(o.Temp, o.Humidity)), Humidity: num(o.Humidity),
			Pressure: num(wx.SeaLevel(o.StationPress, alt, o.Temp)), StationPress: num(o.StationPress),
			Wind: num(o.WindAvg), Gust: num(o.WindGust), Lull: num(o.WindLull), WindDir: num(o.WindDir),
			RainRate: num(rate(o.Rain, time.Duration(o.Interval)*time.Minute)),
			UV:       num(o.UV), Solar: num(o.Solar), Illuminance: num(o.Illuminance), PrecipType: o.PrecipType,
		}
		resp.Health.Battery = num(o.Battery)
	case latestErr == nil:
		r := latest
		curTime, curTemp, curPress = r.Time, r.Temp, r.StationPress
		resp.Source = "archive"
		resp.Current = current{
			Temp: num(r.Temp), FeelsLike: num(wx.FeelsLike(r.Temp, r.Humidity, r.Wind)),
			Dew: num(wx.DewPoint(r.Temp, r.Humidity)), Humidity: num(r.Humidity),
			Pressure: num(wx.SeaLevel(r.StationPress, alt, r.Temp)), StationPress: num(r.StationPress),
			Wind: num(r.Wind), Gust: num(r.Gust), Lull: num(math.NaN()), WindDir: num(r.WindDir),
			RainRate: num(rate(r.Rain, r.Interval)), UV: num(r.UV), Solar: num(r.Solar), Illuminance: num(math.NaN()),
		}
	default:
		resp.Current = current{Temp: num(math.NaN())}
	}
	resp.Time = ts(curTime)
	resp.Current.PressTrend = num(math.NaN())
	if !curTime.IsZero() {
		if then, err := s.cfg.Archive.Before(ctx, curTime.Add(-3*time.Hour)); err == nil && curTime.Sub(then.Time) < 3*time.Hour+30*time.Minute {
			resp.Current.PressTrend = num(wx.SeaLevel(curPress, alt, curTemp) - wx.SeaLevel(then.StationPress, alt, then.Temp))
		}
	}

	if n := len(snap.Wind); n > 0 {
		resp.WindRecent = make([]windSample, n)
		for i, v := range snap.Wind {
			resp.WindRecent[i] = windSample{ts(v.Time), num(v.Speed), num(v.Dir)}
		}
		resp.WindNow = &resp.WindRecent[n-1]
	}

	// Rain the station has reported since weewx last wrote a record: the
	// archive lags by up to one archive interval, and in a downpour that
	// matters.
	liveRain := 0.0
	if latestErr == nil {
		for _, o := range snap.Recent {
			if o.Time.After(latest.Time) && !math.IsNaN(o.Rain) {
				liveRain += o.Rain
			}
		}
	}

	if today, err := s.cfg.Archive.Summary(ctx, midnight, tomorrow); err != nil {
		fail("today", err)
	} else {
		hi, lo := today.TempHi, today.TempLo
		// The live reading can be past the archive's extremes until the next
		// record is written.
		if snap.Obs != nil && !math.IsNaN(curTemp) && !curTime.Before(midnight) {
			if math.IsNaN(hi.Value) || curTemp > hi.Value {
				hi = archive.Extreme{Value: curTemp, At: curTime}
			}
			if math.IsNaN(lo.Value) || curTemp < lo.Value {
				lo = archive.Extreme{Value: curTemp, At: curTime}
			}
		}
		gust := today.GustMax
		for _, o := range snap.Recent {
			if !o.Time.Before(midnight) && !math.IsNaN(o.WindGust) && (math.IsNaN(gust.Value) || o.WindGust > gust.Value) {
				gust = archive.Extreme{Value: o.WindGust, At: o.Time}
			}
		}
		resp.Today.High, resp.Today.Low, resp.Today.Gust = ext(hi), ext(lo), ext(gust)
		resp.Today.Strikes = num(today.Strikes)
		resp.Rain.Today = num(today.Rain + liveRain)
	}
	if y, err := s.cfg.Archive.Summary(ctx, midnight.AddDate(0, 0, -1), midnight); err != nil {
		fail("yesterday", err)
	} else {
		resp.Rain.Yesterday = num(y.Rain)
	}
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	if m, err := s.cfg.Archive.Summary(ctx, monthStart, tomorrow); err != nil {
		fail("month", err)
	} else {
		resp.Rain.Month = num(m.Rain + liveRain)
	}
	yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
	if y, err := s.cfg.Archive.Summary(ctx, yearStart, tomorrow); err != nil {
		fail("year", err)
	} else {
		resp.Rain.Year = num(y.Rain + liveRain)
	}
	for _, span := range []struct {
		d   time.Duration
		out *num
	}{{time.Hour, &resp.Rain.Hour}, {24 * time.Hour, &resp.Rain.Day}} {
		if v, err := s.cfg.Archive.RainBetween(ctx, now.Add(-span.d), now); err != nil {
			fail("rain", err)
		} else {
			*span.out = num(v + liveRain)
		}
	}
	resp.Rain.Started = ts(snap.RainStart)

	resp.Lightning.Count3h = len(snap.Strikes)
	if n := len(snap.Strikes); n > 0 {
		k := snap.Strikes[n-1]
		resp.Lightning.Last = &strike{ts(k.Time), num(k.Distance)}
	}

	if first, err := s.cfg.Archive.First(ctx); err != nil {
		fail("records", err)
	} else if rec, err := s.cfg.Archive.Summary(ctx, time.Unix(0, 0), tomorrow); err != nil {
		fail("records", err)
	} else {
		resp.Records.Since = ts(first)
		resp.Records.High, resp.Records.Low = ext(rec.TempHi), ext(rec.TempLo)
		resp.Records.Gust, resp.Records.Wettest = ext(rec.GustMax), ext(rec.WettestDay)
	}

	resp.Health.LastPacket = ts(snap.LastPacket)
	if snap.Device.Voltage > 0 {
		resp.Health.Battery = num(snap.Device.Voltage)
	}
	resp.Health.RSSI, resp.Health.HubRSSI = snap.Device.RSSI, snap.Hub.RSSI
	return resp
}

// rate turns rain over an interval into mm per hour.
func rate(mm float64, over time.Duration) float64 {
	if over <= 0 || math.IsNaN(mm) {
		return math.NaN()
	}
	return mm * float64(time.Hour) / float64(over)
}

// The chart ranges: how far back, and how wide each point is. Widths are
// chosen to give a few hundred points, which is as many as a chart can show.
var ranges = map[string]struct{ span, width time.Duration }{
	"day":   {24 * time.Hour, 10 * time.Minute},
	"week":  {7 * 24 * time.Hour, time.Hour},
	"month": {30 * 24 * time.Hour, 6 * time.Hour},
	"year":  {365 * 24 * time.Hour, 24 * time.Hour},
}

type historyResponse struct {
	Range    string `json:"range"`
	From     ts     `json:"from"`
	To       ts     `json:"to"`
	Width    int64  `json:"width"` // seconds per point
	Time     []ts   `json:"t"`
	Temp     []num  `json:"temp"`
	Dew      []num  `json:"dew"`
	Humidity []num  `json:"humidity"`
	Pressure []num  `json:"pressure"`
	Wind     []num  `json:"wind"`
	Gust     []num  `json:"gust"`
	WindDir  []num  `json:"windDir"`
	Rain     []num  `json:"rain"`
	UV       []num  `json:"uv"`
	Solar    []num  `json:"solar"`
	Strikes  []num  `json:"strikes"`
}

func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "day"
	}
	rg, ok := ranges[name]
	if !ok {
		http.Error(w, "range must be day, week, month or year", http.StatusBadRequest)
		return
	}
	s.serveCached(w, "history:"+name, time.Minute, func() (any, error) {
		now := s.now()
		var to time.Time
		if rg.width >= 24*time.Hour {
			// Whole local days, so each point is one calendar day.
			to = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		} else {
			to = now.Truncate(rg.width).Add(rg.width)
		}
		from := to.Add(-rg.span)
		buckets, err := s.cfg.Archive.Series(r.Context(), from, to, rg.width)
		if err != nil {
			return nil, err
		}
		h := historyResponse{Range: name, From: ts(from), To: ts(to), Width: int64(rg.width / time.Second)}
		for _, b := range buckets {
			h.Time = append(h.Time, ts(b.Time))
			h.Temp = append(h.Temp, num(b.Temp))
			h.Dew = append(h.Dew, num(b.Dew))
			h.Humidity = append(h.Humidity, num(b.Humidity))
			h.Pressure = append(h.Pressure, num(b.Pressure))
			h.Wind = append(h.Wind, num(b.Wind))
			h.Gust = append(h.Gust, num(b.Gust))
			h.WindDir = append(h.WindDir, num(b.WindDir))
			h.Rain = append(h.Rain, num(b.Rain))
			h.UV = append(h.UV, num(b.UV))
			h.Solar = append(h.Solar, num(b.Solar))
			h.Strikes = append(h.Strikes, num(b.Strikes))
		}
		return h, nil
	})
}

// serveCached answers from a short-lived cache, so a room full of open tabs
// costs one set of queries, not one per tab.
func (s *server) serveCached(w http.ResponseWriter, key string, ttl time.Duration, build func() (any, error)) {
	s.mu.Lock()
	c, ok := s.cache[key]
	s.mu.Unlock()
	if !ok || s.now().Sub(c.at) > ttl {
		v, err := build()
		if err != nil {
			s.cfg.Log.Error("api", "key", key, "err", err)
			http.Error(w, "the weather archive could not be read", http.StatusServiceUnavailable)
			return
		}
		body, err := json.Marshal(v)
		if err != nil {
			s.cfg.Log.Error("api encode", "key", key, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		c = cached{at: s.now(), body: body}
		s.mu.Lock()
		s.cache[key] = c
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(c.body)
}

// handleStream is a server-sent event stream of what the station says, so the
// page can move the wind needle every three seconds and refetch /api/now when
// a new observation lands.
func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}
	events, cancel := s.cfg.Station.Subscribe()
	defer cancel()

	// A comment line now and then, so that idle-connection timeouts in
	// between do not cut a quiet stream.
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.cfg.Done:
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case ev := <-events:
			data, ok := streamData(ev)
			if !ok {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

func streamData(ev live.Event) ([]byte, bool) {
	var v any
	switch m := ev.Data.(type) {
	case tempest.RapidWind:
		v = windSample{ts(m.Time), num(m.Speed), num(m.Dir)}
	case tempest.Obs:
		v = map[string]ts{"t": ts(m.Time)}
	case tempest.Strike:
		v = strike{ts(m.Time), num(m.Distance)}
	case tempest.RainStart:
		v = map[string]ts{"t": ts(m.Time)}
	default:
		return nil, false
	}
	b, err := json.Marshal(v)
	return b, err == nil
}
