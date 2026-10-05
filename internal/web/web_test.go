package web

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runlevel-six/stormkeep/internal/archive"
	"github.com/runlevel-six/stormkeep/internal/live"
	"github.com/runlevel-six/stormkeep/internal/tempest"
)

// weewxDB writes a small database in weewx's layout with one record every five
// minutes for the last two days, in weewx's METRICWX units.
func weewxDB(t *testing.T, now time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "weewx.sdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE archive (dateTime INTEGER PRIMARY KEY, usUnits INTEGER, interval INTEGER, outTemp REAL, outHumidity REAL,
			pressure REAL, windSpeed REAL, windGust REAL, windDir REAL, rain REAL, UV REAL, radiation REAL, lightning_strike_count REAL)`,
		`CREATE TABLE archive_day__metadata (name TEXT PRIMARY KEY, value TEXT)`,
		`INSERT INTO archive_day__metadata VALUES ('Version', '4.0')`,
	}
	for _, obs := range []string{"outTemp", "windGust", "rain", "lightning_strike_count"} {
		stmts = append(stmts, `CREATE TABLE archive_day_`+obs+` (dateTime INTEGER PRIMARY KEY, min REAL, mintime INTEGER, max REAL, maxtime INTEGER, sum REAL, count INTEGER, wsum REAL, sumtime INTEGER)`)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	end := now.Truncate(5 * time.Minute)
	for ts := end.Add(-48 * time.Hour); !ts.After(end); ts = ts.Add(5 * time.Minute) {
		if _, err := db.Exec(`INSERT INTO archive VALUES (?, 17, 5, 20, 60, 1010, 3, 5, 180, 0.2, 1, 100, 0)`, ts.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, q := range []string{
		`INSERT INTO archive_day_outTemp VALUES (?, 15, ?, 25, ?, 0, 0, 0, 0)`,
		`INSERT INTO archive_day_windGust VALUES (?, 0, ?, 9, ?, 0, 0, 0, 0)`,
		`INSERT INTO archive_day_rain VALUES (?, 0, ?, 0, ?, 4.2, 0, 0, 0)`,
		`INSERT INTO archive_day_lightning_strike_count VALUES (?, 0, ?, 0, ?, 3, 0, 0, 0)`,
	} {
		if _, err := db.Exec(q, midnight.Unix(), midnight.Unix()+3600, midnight.Unix()+7200); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func newHandler(t *testing.T, dbPath string, cfg Config) (http.Handler, *live.Station) {
	h, st, _ := newHandlerArchive(t, dbPath, cfg)
	return h, st
}

func newHandlerArchive(t *testing.T, dbPath string, cfg Config) (http.Handler, *live.Station, *archive.Archive) {
	t.Helper()
	arc, err := archive.Open(dbPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { arc.Close() })
	st := live.New("")
	cfg.Archive, cfg.Station = arc, st
	if cfg.Name == "" {
		cfg.Name = "Test station"
	}
	if cfg.Done == nil {
		cfg.Done = make(chan struct{})
	}
	return Handler(cfg), st, arc
}

func get(t *testing.T, h http.Handler, path, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if remote != "" {
		req.RemoteAddr = remote
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAllowFrom(t *testing.T) {
	h, _ := newHandler(t, weewxDB(t, time.Now()), Config{AllowFrom: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	tests := []struct {
		path, remote string
		want         int
	}{
		{"/", "10.1.2.3:5555", http.StatusOK},
		{"/", "192.0.2.10:5555", http.StatusForbidden},
		{"/api/now", "192.0.2.10:5555", http.StatusForbidden},
		{"/api/now", "[::ffff:10.1.2.3]:5555", http.StatusOK},
		{"/healthz", "192.0.2.10:5555", http.StatusOK}, // the kubelet probes from the node
	}
	for _, tt := range tests {
		if got := get(t, h, tt.path, tt.remote).Code; got != tt.want {
			t.Errorf("%s from %s: %d, want %d", tt.path, tt.remote, got, tt.want)
		}
	}
}

func TestHeaders(t *testing.T) {
	h, _ := newHandler(t, weewxDB(t, time.Now()), Config{Version: "v9.9.9"})
	w := get(t, h, "/", "")
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP %q", csp)
	}
	if w.Header().Get("X-Robots-Tag") == "" {
		t.Error("noindex header missing by default")
	}
	if body := w.Body.String(); !strings.Contains(body, "app.js?v=v9.9.9") || !strings.Contains(body, "<title>Test station</title>") {
		t.Error("index does not carry the version and name")
	}
	h, _ = newHandler(t, weewxDB(t, time.Now()), Config{AllowIndexing: true})
	if get(t, h, "/", "").Header().Get("X-Robots-Tag") != "" {
		t.Error("AllowIndexing should drop the noindex header")
	}
	if w := get(t, h, "/static/app.css?v=x", ""); w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("static: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNowFromArchiveThenLive(t *testing.T) {
	now := time.Now()
	h, st, arc := newHandlerArchive(t, weewxDB(t, now), Config{})

	m := decode(t, get(t, h, "/api/now", ""))
	if m["source"] != "archive" {
		t.Errorf("before any broadcast: source %v", m["source"])
	}
	cur := m["current"].(map[string]any)
	if cur["temp"] != 20.0 || cur["humidity"] != 60.0 {
		t.Errorf("current from archive: %v", cur)
	}
	if cur["pressureTrend"] != 0.0 {
		t.Errorf("flat pressure should trend 0, got %v", cur["pressureTrend"])
	}
	today := m["today"].(map[string]any)
	if today["high"].(map[string]any)["v"] != 25.0 {
		t.Errorf("today %v", today)
	}
	if rain := m["rain"].(map[string]any); rain["today"] != 4.2 {
		t.Errorf("rain %v", rain)
	}

	st.Handle(tempest.Obs{Time: now, Temp: 30, Humidity: 50, StationPress: 1012, WindAvg: 2, WindGust: 11, WindDir: 90, Rain: 0.5, Interval: 1, Battery: 2.6})
	st.Handle(tempest.RapidWind{Time: now, Speed: 4, Dir: 100})
	st.Handle(tempest.Strike{Time: now, Distance: 12})
	// The /api/now cache holds for five seconds; a fresh handler skips it.
	h2 := Handler(Config{Name: "x", Archive: arc, Station: st, Done: make(chan struct{})})
	m = decode(t, get(t, h2, "/api/now", ""))
	if m["source"] != "live" {
		t.Fatalf("after a broadcast: source %v", m["source"])
	}
	cur = m["current"].(map[string]any)
	if cur["temp"] != 30.0 || cur["rainRate"] != 30.0 {
		t.Errorf("live current %v", cur)
	}
	if hi := m["today"].(map[string]any)["high"].(map[string]any)["v"]; hi != 30.0 {
		t.Errorf("a live reading above the archive's high should become the high, got %v", hi)
	}
	if g := m["today"].(map[string]any)["gust"].(map[string]any)["v"]; g != 11.0 {
		t.Errorf("a live gust above the archive's should become today's gust, got %v", g)
	}
	if rain := m["rain"].(map[string]any)["today"]; rain != 4.7 {
		t.Errorf("rain today should add the live minute: %v", rain)
	}
	if l := m["lightning"].(map[string]any); l["count3h"] != 1.0 {
		t.Errorf("lightning %v", l)
	}
	if w := m["windNow"].(map[string]any); w["s"] != 4.0 {
		t.Errorf("windNow %v", w)
	}
}

func TestNowWithoutArchive(t *testing.T) {
	h, st := newHandler(t, filepath.Join(t.TempDir(), "missing.sdb"), Config{})
	st.Handle(tempest.Obs{Time: time.Now(), Temp: 18, Humidity: 40, StationPress: 1000, Interval: 1})
	m := decode(t, get(t, h, "/api/now", ""))
	if m["source"] != "live" || m["current"].(map[string]any)["temp"] != 18.0 {
		t.Errorf("live data should survive a missing archive: %v", m)
	}
	errs, _ := m["errors"].([]any)
	if len(errs) == 0 || errs[0] != "archive" {
		t.Errorf("errors %v", m["errors"])
	}
	if w := get(t, h, "/api/history?range=day", ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("history without an archive: %d", w.Code)
	}
}

func TestHistory(t *testing.T) {
	h, _ := newHandler(t, weewxDB(t, time.Now()), Config{})
	if w := get(t, h, "/api/history?range=decade", ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad range: %d", w.Code)
	}
	for name, n := range map[string]int{"day": 144, "week": 168, "month": 120, "year": 365} {
		m := decode(t, get(t, h, "/api/history?range="+name, ""))
		for _, key := range []string{"t", "temp", "dew", "humidity", "pressure", "wind", "gust", "windDir", "rain", "uv", "solar", "strikes"} {
			if got := len(m[key].([]any)); got != n {
				t.Errorf("%s: %s has %d points, want %d", name, key, got, n)
			}
		}
	}
	m := decode(t, get(t, h, "/api/history?range=day", ""))
	// A 10-minute bucket holds two 5-minute records of 0.2 mm.
	if r := m["rain"].([]any)[10]; r != 0.4 {
		t.Errorf("rain per bucket %v", r)
	}
}

// The newest point of the day chart is the 10-minute bucket now is in. weewx
// stamps a record with the end of its interval, so for the first five minutes
// of every ten that bucket has no record yet and must read as a gap, with the
// point before it holding the newest data. This failed in CI at 16:43 and
// passed locally at 16:46, before the clock was fixed.
func TestHistoryNewestBucket(t *testing.T) {
	base := time.Date(2026, 10, 5, 16, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		minute    int
		newestNil bool
	}{
		{43, true},  // records up to 16:40, which belongs to 16:30-16:40
		{46, false}, // the 16:45 record lands in 16:40-16:50
	} {
		now := base.Add(time.Duration(tt.minute) * time.Minute)
		h, _ := newHandler(t, weewxDB(t, now), Config{Now: func() time.Time { return now }})
		temps := decode(t, get(t, h, "/api/history?range=day", ""))["temp"].([]any)
		newest, before := temps[len(temps)-1], temps[len(temps)-2]
		if tt.newestNil {
			if newest != nil || before != 20.0 {
				t.Errorf("16:%d: newest %v, before it %v; want a gap, then 20", tt.minute, newest, before)
			}
		} else if newest != 20.0 {
			t.Errorf("16:%d: newest %v, want 20", tt.minute, newest)
		}
	}
}

func TestNumEncodesNaNAsNull(t *testing.T) {
	b, err := json.Marshal([]num{num(math.NaN()), num(1.23456), num(math.Inf(1)), 0})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[null,1.23,null,0]" {
		t.Errorf("got %s", b)
	}
	b, _ = json.Marshal(struct{ T ts }{})
	if string(b) != `{"T":null}` {
		t.Errorf("zero time: %s", b)
	}
}

func TestStream(t *testing.T) {
	done := make(chan struct{})
	h, st := newHandler(t, weewxDB(t, time.Now()), Config{Done: done})
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	go func() {
		// The handler subscribes after it sends the headers; keep sending
		// until the reader has what it needs.
		for ctx.Err() == nil {
			st.Handle(tempest.RapidWind{Time: time.Unix(1700000000, 0), Speed: 3.3, Dir: 45})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if strings.HasPrefix(sc.Text(), "data:") {
			break
		}
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "event: wind") || !strings.Contains(got, `data: {"t":1700000000,"s":3.3,"d":45}`) {
		t.Errorf("stream said:\n%s", got)
	}

	// Shutting down ends the stream.
	close(done)
	for sc.Scan() {
	}
}
