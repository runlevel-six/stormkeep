package archive

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"
)

// Extreme is a highest or lowest value and when it happened. A zero At means
// there was no data.
type Extreme struct {
	Value float64
	At    time.Time
}

// Summary is what the day summary tables say about a span of whole days.
type Summary struct {
	TempHi, TempLo Extreme // °C
	GustMax        Extreme // m/s
	WettestDay     Extreme // mm, At is the start of the day
	Rain           float64 // mm in total
	Strikes        float64 // lightning strikes in total; NaN if not recorded
}

// Summary reads the day summaries for the days that start in [from, to).
// weewx starts each day at local midnight in its own time zone, so from and
// to should be local midnights in that zone too.
func (a *Archive) Summary(ctx context.Context, from, to time.Time) (Summary, error) {
	us, err := a.unitSystem(ctx)
	if err != nil {
		return Summary{}, err
	}
	u := units(us)
	lo, hi := from.Unix(), to.Unix()

	var s Summary
	if s.TempHi, err = a.extreme(ctx, "outTemp", "max", "DESC", lo, hi); err != nil {
		return s, err
	}
	if s.TempLo, err = a.extreme(ctx, "outTemp", "min", "ASC", lo, hi); err != nil {
		return s, err
	}
	if s.GustMax, err = a.extreme(ctx, "windGust", "max", "DESC", lo, hi); err != nil {
		return s, err
	}
	s.TempHi.Value, s.TempLo.Value = u.temp(s.TempHi.Value), u.temp(s.TempLo.Value)
	s.GustMax.Value = u.speed(s.GustMax.Value)

	var day sql.NullInt64
	var wettest sql.NullFloat64
	err = a.db.QueryRowContext(ctx,
		"SELECT dateTime, sum FROM archive_day_rain WHERE dateTime >= ? AND dateTime < ? AND sum IS NOT NULL ORDER BY sum DESC LIMIT 1",
		lo, hi).Scan(&day, &wettest)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		s.WettestDay = Extreme{Value: math.NaN()}
	case err != nil:
		return s, err
	default:
		s.WettestDay = Extreme{Value: u.rain(wettest.Float64), At: time.Unix(day.Int64, 0)}
	}

	if s.Rain, err = a.total(ctx, "rain", lo, hi); err != nil {
		return s, err
	}
	s.Rain = u.rain(s.Rain)
	if s.Strikes, err = a.total(ctx, "lightning_strike_count", lo, hi); err != nil {
		return s, err
	}
	return s, nil
}

// extreme reads the max or min of one observation type's day summaries.
// The names are compile-time constants from this file, never user input.
func (a *Archive) extreme(ctx context.Context, obs, col, order string, lo, hi int64) (Extreme, error) {
	q := "SELECT " + col + ", " + col + "time FROM archive_day_" + obs +
		" WHERE dateTime >= ? AND dateTime < ? AND " + col + " IS NOT NULL ORDER BY " + col + " " + order + " LIMIT 1"
	var v sql.NullFloat64
	var at sql.NullInt64
	err := a.db.QueryRowContext(ctx, q, lo, hi).Scan(&v, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Extreme{Value: math.NaN()}, nil
	}
	if err != nil {
		return Extreme{}, err
	}
	return Extreme{Value: v.Float64, At: time.Unix(at.Int64, 0)}, nil
}

// total sums one observation type's day sums. A database created with an
// older schema may not have the table at all, which reads as NaN, not an
// error.
func (a *Archive) total(ctx context.Context, obs string, lo, hi int64) (float64, error) {
	var v sql.NullFloat64
	err := a.db.QueryRowContext(ctx,
		"SELECT sum(sum) FROM archive_day_"+obs+" WHERE dateTime >= ? AND dateTime < ?", lo, hi).Scan(&v)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return math.NaN(), nil
		}
		return math.NaN(), err
	}
	if !v.Valid {
		return 0, nil
	}
	return v.Float64, nil
}
