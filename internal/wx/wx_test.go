package wx

import (
	"math"
	"testing"
)

func near(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

func TestDewPoint(t *testing.T) {
	tests := []struct{ t, rh, want float64 }{
		{25, 70, 19.1}, // a humid summer afternoon
		{20, 100, 20},  // saturated: dew point is the temperature
		{0, 50, -9.2},  // cold and dry
		{35, 20, 8.8},  // hot and dry
		{-10, 80, -12.8},
	}
	for _, tt := range tests {
		if got := DewPoint(tt.t, tt.rh); !near(got, tt.want, 0.2) {
			t.Errorf("DewPoint(%v, %v) = %.2f, want %.1f", tt.t, tt.rh, got, tt.want)
		}
	}
	if !math.IsNaN(DewPoint(20, 0)) || !math.IsNaN(DewPoint(math.NaN(), 50)) {
		t.Error("impossible inputs should give NaN")
	}
}

// Expected values from the NWS heat index and wind chill tables, in °F.
func TestFeelsLike(t *testing.T) {
	f := func(c float64) float64 { return c*9/5 + 32 }
	c := func(f float64) float64 { return (f - 32) * 5 / 9 }
	mph := func(v float64) float64 { return v / 2.2369362920544 }

	tests := []struct {
		name          string
		tempF, rh, ws float64
		wantF         float64
	}{
		{"heat index 90F 60%", 90, 60, 0, 100},
		{"heat index 100F 40%", 100, 40, 0, 109},
		{"heat index 80F 40% (simple formula)", 80, 40, 0, 80},
		{"wind chill 30F 15mph", 30, 50, mph(15), 19},
		{"wind chill 0F 25mph", 0, 50, mph(25), -24},
		{"mild: air temperature", 65, 50, mph(20), 65},
		{"cold but calm: air temperature", 40, 50, mph(2), 40},
	}
	for _, tt := range tests {
		got := f(FeelsLike(c(tt.tempF), tt.rh, tt.ws))
		if !near(got, tt.wantF, 1) {
			t.Errorf("%s: got %.1f°F, want %.0f°F", tt.name, got, tt.wantF)
		}
	}
}

func TestSeaLevel(t *testing.T) {
	if got := SeaLevel(1000, 0, 20); got != 1000 {
		t.Errorf("altitude 0 should be unchanged, got %v", got)
	}
	// About 1 hPa per 8 m near sea level.
	if got := SeaLevel(1000, 50, 20); !near(got, 1005.9, 0.3) {
		t.Errorf("50 m: got %.2f, want ~1005.9", got)
	}
	// The standard atmosphere at 1609 m: 834.3 hPa and 4.54 °C reduce to
	// its sea-level 1013.25 hPa.
	if got := SeaLevel(834.3, 1609, 4.54); !near(got, 1013.25, 0.5) {
		t.Errorf("1609 m: got %.2f, want 1013.25", got)
	}
	if got := SeaLevel(1000, 50, math.NaN()); math.IsNaN(got) {
		t.Error("a missing temperature should fall back, not give NaN")
	}
}
