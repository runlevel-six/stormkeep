package archive

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"
)

// newDB writes a database laid out the way weewx lays one out (the columns this
// package reads, plus a few it does not) and returns its path.
func newDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "weewx.sdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stmts := []string{
		`CREATE TABLE archive (dateTime INTEGER NOT NULL UNIQUE PRIMARY KEY, usUnits INTEGER NOT NULL, interval INTEGER NOT NULL,
			outTemp REAL, outHumidity REAL, pressure REAL, barometer REAL, windSpeed REAL, windGust REAL, windDir REAL,
			rain REAL, UV REAL, radiation REAL, lightning_strike_count REAL, extraTemp1 REAL)`,
		// As weewx 5 writes it: no unit_system row.
		`CREATE TABLE archive_day__metadata (name CHAR(20) NOT NULL UNIQUE PRIMARY KEY, value TEXT)`,
		`INSERT INTO archive_day__metadata VALUES ('Version', '4.0')`,
	}
	for _, obs := range []string{"outTemp", "windGust", "rain", "lightning_strike_count"} {
		stmts = append(stmts, `CREATE TABLE archive_day_`+obs+` (dateTime INTEGER NOT NULL UNIQUE PRIMARY KEY,
			min REAL, mintime INTEGER, max REAL, maxtime INTEGER, sum REAL, count INTEGER, wsum REAL, sumtime INTEGER)`)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return path, db
}

func open(t *testing.T, path string, altitude float64) *Archive {
	t.Helper()
	a, err := Open(path, altitude)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestUnitsAreNormalized(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		us   int
		row  []any // outTemp, pressure, windSpeed, rain
	}{
		{"US", US, []any{77.0, 29.92, 22.369, 0.1}},
		{"METRIC", Metric, []any{25.0, 1013.2, 36.0, 0.254}},
		{"METRICWX", MetricWX, []any{25.0, 1013.2, 10.0, 2.54}},
	}
	for _, tt := range tests {
		path, db := newDB(t)
		args := append([]any{1700000000, tt.us}, tt.row...)
		if _, err := db.Exec(`INSERT INTO archive (dateTime, usUnits, interval, outTemp, pressure, windSpeed, rain) VALUES (?, ?, 5, ?, ?, ?, ?)`, args...); err != nil {
			t.Fatal(err)
		}
		r, err := open(t, path, 0).Latest(ctx)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !near(r.Temp, 25) || math.Abs(r.StationPress-1013.2) > 0.1 || !near(r.Wind, 10) || !near(r.Rain, 2.54) {
			t.Errorf("%s: got temp %v press %v wind %v rain %v", tt.name, r.Temp, r.StationPress, r.Wind, r.Rain)
		}
		if !math.IsNaN(r.Humidity) {
			t.Errorf("%s: a NULL column should read as NaN, got %v", tt.name, r.Humidity)
		}
		if r.Interval != 5*time.Minute {
			t.Errorf("%s: interval %v", tt.name, r.Interval)
		}
	}
}

func TestSeries(t *testing.T) {
	ctx := context.Background()
	path, db := newDB(t)
	start := time.Unix(1700000000, 0)
	// Two hours of 5-minute records, but nothing in the second half of the
	// first hour.
	for i := 1; i <= 24; i++ {
		if i > 6 && i <= 12 {
			continue
		}
		ts := start.Add(time.Duration(i) * 5 * time.Minute).Unix()
		if _, err := db.Exec(`INSERT INTO archive (dateTime, usUnits, interval, outTemp, outHumidity, pressure, windSpeed, windGust, windDir, rain, UV)
			VALUES (?, 17, 5, ?, 50, 1000, 2, ?, ?, 0.5, ?)`, ts, float64(i), float64(i), []float64{350, 10}[i%2], float64(i%3)); err != nil {
			t.Fatal(err)
		}
	}
	b, err := open(t, path, 0).Series(ctx, start, start.Add(2*time.Hour), 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 4 {
		t.Fatalf("got %d buckets, want 4", len(b))
	}
	if !b[0].Time.Equal(start) || !b[3].Time.Equal(start.Add(90*time.Minute)) {
		t.Errorf("bucket times %v .. %v", b[0].Time, b[3].Time)
	}
	// Records 1..6 end in the first half hour.
	if !near(b[0].Temp, 3.5) || !near(b[0].Gust, 6) || !near(b[0].Rain, 3) || !near(b[0].UV, 2) {
		t.Errorf("first bucket: %+v", b[0])
	}
	if !math.IsNaN(b[1].Temp) || !math.IsNaN(b[1].Rain) || !math.IsNaN(b[1].Gust) {
		t.Errorf("empty bucket should be NaN: %+v", b[1])
	}
	// Equal winds from 350° and 10° average to north, not 180°.
	if d := b[0].WindDir; !(d > 359.9 || d < 0.1) {
		t.Errorf("mean of 350° and 10° = %v, want 0", d)
	}
	if math.IsNaN(b[0].Dew) {
		t.Errorf("dew point should be computed from temperature and humidity")
	}
}

func TestSeaLevelPressureUsesAltitude(t *testing.T) {
	ctx := context.Background()
	path, db := newDB(t)
	start := time.Unix(1700000000, 0)
	if _, err := db.Exec(`INSERT INTO archive (dateTime, usUnits, interval, outTemp, pressure) VALUES (?, 17, 5, 20, 1000)`, start.Add(5*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	b, err := open(t, path, 50).Series(ctx, start, start.Add(time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if b[0].Pressure < 1005 || b[0].Pressure > 1007 {
		t.Errorf("50 m: sea-level pressure %v, want about 1006", b[0].Pressure)
	}
}

func TestSummary(t *testing.T) {
	ctx := context.Background()
	path, db := newDB(t)
	day := func(d int) int64 { return 1700000000 + int64(d)*86400 }
	// The summaries take their unit system from the archive records.
	if _, err := db.Exec(`INSERT INTO archive (dateTime, usUnits, interval) VALUES (?, 1, 5)`, day(0)+300); err != nil {
		t.Fatal(err)
	}
	for d, row := range [][]float64{
		// min, max (°F), max gust (mph), rain (in), strikes
		{50, 70, 20, 0, 0},
		{40, 90, 30, 1, 5},
		{60, 80, 10, 0.5, 2},
	} {
		ts := day(d)
		for _, q := range []struct {
			obs      string
			min, max float64
			sum      float64
		}{
			{"outTemp", row[0], row[1], 0},
			{"windGust", 0, row[2], 0},
			{"rain", 0, 0, row[3]},
			{"lightning_strike_count", 0, 0, row[4]},
		} {
			if _, err := db.Exec(`INSERT INTO archive_day_`+q.obs+` VALUES (?, ?, ?, ?, ?, ?, 288, 0, 0)`,
				ts, q.min, ts+100, q.max, ts+200, q.sum); err != nil {
				t.Fatal(err)
			}
		}
	}
	s, err := open(t, path, 0).Summary(ctx, time.Unix(day(0), 0), time.Unix(day(3), 0))
	if err != nil {
		t.Fatal(err)
	}
	if !near(s.TempHi.Value, 32.22) || s.TempHi.At.Unix() != day(1)+200 {
		t.Errorf("high %+v", s.TempHi)
	}
	if !near(s.TempLo.Value, 4.44) || s.TempLo.At.Unix() != day(1)+100 {
		t.Errorf("low %+v", s.TempLo)
	}
	if !near(s.GustMax.Value, 13.41) {
		t.Errorf("gust %+v", s.GustMax)
	}
	if !near(s.Rain, 38.1) || !near(s.WettestDay.Value, 25.4) || s.WettestDay.At.Unix() != day(1) {
		t.Errorf("rain %v wettest %+v", s.Rain, s.WettestDay)
	}
	if s.Strikes != 7 {
		t.Errorf("strikes %v", s.Strikes)
	}

	// One day only.
	s, err = open(t, path, 0).Summary(ctx, time.Unix(day(2), 0), time.Unix(day(3), 0))
	if err != nil {
		t.Fatal(err)
	}
	if !near(s.TempHi.Value, 26.67) || !near(s.Rain, 12.7) {
		t.Errorf("one day: %+v", s)
	}

	// No days at all.
	s, err = open(t, path, 0).Summary(ctx, time.Unix(day(10), 0), time.Unix(day(11), 0))
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(s.TempHi.Value) || !s.TempHi.At.IsZero() || s.Rain != 0 {
		t.Errorf("empty span: %+v", s)
	}
}

func TestMissingDatabase(t *testing.T) {
	a := open(t, filepath.Join(t.TempDir(), "absent.sdb"), 0)
	if err := a.Ping(context.Background()); err == nil {
		t.Error("Ping on a missing file should fail")
	}
}
