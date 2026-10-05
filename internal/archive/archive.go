// Package archive reads the SQLite database weewx keeps: the archive table of
// fixed-interval records, and the per-day summary tables weewx maintains
// beside it.
//
// It only ever reads. weewx is the one writer; this opens the file read-only
// and waits out weewx's write locks rather than competing for them.
package archive

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Archive is a read-only handle on a weewx database.
type Archive struct {
	db       *sql.DB
	altitude float64 // meters, for sea-level pressure
}

// Open opens the weewx database at path read-only. The file need not exist
// yet: weewx creates it on first start, and every query reports the error
// until then.
func Open(path string, altitudeM float64) (*Archive, error) {
	// busy_timeout because weewx holds a write lock for a moment every archive
	// interval, and a reader that gives up immediately would fail at random.
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	return &Archive{db: db, altitude: altitudeM}, nil
}

func (a *Archive) Close() error { return a.db.Close() }

// Ping checks the database can be read.
func (a *Archive) Ping(ctx context.Context) error {
	var n int
	return a.db.QueryRowContext(ctx, "SELECT count(*) FROM archive WHERE dateTime > 0 LIMIT 1").Scan(&n)
}

// unitSystem returns the weewx unit system the day summaries are stored in:
// the database's own, which is the one its archive records carry. (Each
// record names its unit system, but the summary tables do not, and weewx
// refuses to write a record in a different one.)
func (a *Archive) unitSystem(ctx context.Context) (int, error) {
	var us int
	if err := a.db.QueryRowContext(ctx, "SELECT usUnits FROM archive ORDER BY dateTime DESC LIMIT 1").Scan(&us); err != nil {
		return 0, fmt.Errorf("archive: unit system: %w", err)
	}
	return us, nil
}

// Record is one archive record, normalized to °C, m/s, hPa and mm whatever
// unit system weewx stored it in. Missing values are NaN.
type Record struct {
	Time         time.Time
	Interval     time.Duration
	Temp         float64 // °C
	Humidity     float64 // %
	StationPress float64 // hPa
	Wind         float64 // m/s
	Gust         float64 // m/s
	WindDir      float64 // degrees
	Rain         float64 // mm over the interval
	UV           float64
	Solar        float64 // W/m²
	Strikes      float64 // count over the interval
}

const recordCols = "dateTime, usUnits, interval, outTemp, outHumidity, pressure, windSpeed, windGust, windDir, rain, UV, radiation, lightning_strike_count"

func scanRecord(rows interface{ Scan(...any) error }) (Record, error) {
	var (
		ts, us, interval                  int64
		temp, hum, press, wind, gust, dir sql.NullFloat64
		rain, uv, solar, strikes          sql.NullFloat64
	)
	if err := rows.Scan(&ts, &us, &interval, &temp, &hum, &press, &wind, &gust, &dir, &rain, &uv, &solar, &strikes); err != nil {
		return Record{}, err
	}
	u := units(us)
	return Record{
		Time:         time.Unix(ts, 0),
		Interval:     time.Duration(interval) * time.Minute,
		Temp:         u.temp(f(temp)),
		Humidity:     f(hum),
		StationPress: u.press(f(press)),
		Wind:         u.speed(f(wind)),
		Gust:         u.speed(f(gust)),
		WindDir:      f(dir),
		Rain:         u.rain(f(rain)),
		UV:           f(uv),
		Solar:        f(solar),
		Strikes:      f(strikes),
	}, nil
}

// Latest returns the newest archive record.
func (a *Archive) Latest(ctx context.Context) (Record, error) {
	row := a.db.QueryRowContext(ctx, "SELECT "+recordCols+" FROM archive ORDER BY dateTime DESC LIMIT 1")
	return scanRecord(row)
}

// Before returns the newest record at or before t.
func (a *Archive) Before(ctx context.Context, t time.Time) (Record, error) {
	row := a.db.QueryRowContext(ctx, "SELECT "+recordCols+" FROM archive WHERE dateTime <= ? ORDER BY dateTime DESC LIMIT 1", t.Unix())
	return scanRecord(row)
}

// First returns the time of the oldest record.
func (a *Archive) First(ctx context.Context) (time.Time, error) {
	var ts int64
	err := a.db.QueryRowContext(ctx, "SELECT min(dateTime) FROM archive").Scan(&ts)
	return time.Unix(ts, 0), err
}

// Records returns every record with from < time <= to, oldest first. weewx
// stamps a record with the end of its interval, hence the open start.
func (a *Archive) Records(ctx context.Context, from, to time.Time) ([]Record, error) {
	rows, err := a.db.QueryContext(ctx,
		"SELECT "+recordCols+" FROM archive WHERE dateTime > ? AND dateTime <= ? ORDER BY dateTime",
		from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RainBetween returns the rain in mm recorded with from < time <= to.
func (a *Archive) RainBetween(ctx context.Context, from, to time.Time) (float64, error) {
	rows, err := a.db.QueryContext(ctx,
		"SELECT usUnits, sum(rain) FROM archive WHERE dateTime > ? AND dateTime <= ? GROUP BY usUnits",
		from.Unix(), to.Unix())
	if err != nil {
		return math.NaN(), err
	}
	defer rows.Close()
	total := 0.0
	for rows.Next() {
		var us int64
		var sum sql.NullFloat64
		if err := rows.Scan(&us, &sum); err != nil {
			return math.NaN(), err
		}
		if sum.Valid {
			total += units(us).rain(sum.Float64)
		}
	}
	return total, rows.Err()
}

func f(n sql.NullFloat64) float64 {
	if !n.Valid {
		return math.NaN()
	}
	return n.Float64
}
