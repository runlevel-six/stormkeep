// Package wx computes the derived quantities a station does not measure:
// dew point, the "feels like" temperature and sea-level pressure.
//
// Everything is computed the same way for live readings and for history, so a
// chart never shows a step where one source hands over to the other.
package wx

import "math"

// DewPoint returns the dew point in °C for a temperature in °C and relative
// humidity in percent, by the Magnus formula (Alduchov and Eskridge
// coefficients), good to a few tenths of a degree over normal weather.
func DewPoint(tempC, rh float64) float64 {
	if math.IsNaN(tempC) || math.IsNaN(rh) || rh <= 0 {
		return math.NaN()
	}
	const a, b = 17.625, 243.04
	g := math.Log(rh/100) + a*tempC/(b+tempC)
	return b * g / (a - g)
}

// FeelsLike returns the apparent temperature in °C: the US National Weather
// Service heat index when it is hot and humid enough for that to apply, its
// wind chill when it is cold and windy enough, and the air temperature
// otherwise. windMS is the average wind in m/s.
func FeelsLike(tempC, rh, windMS float64) float64 {
	if math.IsNaN(tempC) {
		return math.NaN()
	}
	f := cToF(tempC)
	mph := windMS * 2.2369362920544
	switch {
	case f >= 80 && !math.IsNaN(rh):
		return fToC(heatIndexF(f, rh))
	case f <= 50 && mph > 3:
		return fToC(35.74 + 0.6215*f - 35.75*math.Pow(mph, 0.16) + 0.4275*f*math.Pow(mph, 0.16))
	default:
		return tempC
	}
}

// heatIndexF is the NWS heat index (Rothfusz regression with the NWS
// adjustments), in °F.
func heatIndexF(t, rh float64) float64 {
	simple := 0.5 * (t + 61 + (t-68)*1.2 + rh*0.094)
	if (simple+t)/2 < 80 {
		return simple
	}
	hi := -42.379 + 2.04901523*t + 10.14333127*rh - 0.22475541*t*rh -
		6.83783e-3*t*t - 5.481717e-2*rh*rh + 1.22874e-3*t*t*rh +
		8.5282e-4*t*rh*rh - 1.99e-6*t*t*rh*rh
	switch {
	case rh < 13 && t >= 80 && t <= 112:
		hi -= (13 - rh) / 4 * math.Sqrt((17-math.Abs(t-95))/17)
	case rh > 85 && t >= 80 && t <= 87:
		hi += (rh - 85) / 10 * (87 - t) / 5
	}
	return hi
}

// SeaLevel reduces a station pressure in hPa to sea level, given the station's
// altitude in meters and the air temperature in °C, using the hypsometric
// equation with the standard lapse rate. At altitude 0 it returns the station
// pressure unchanged.
func SeaLevel(stationHPa, altitudeM, tempC float64) float64 {
	if altitudeM == 0 || math.IsNaN(stationHPa) {
		return stationHPa
	}
	if math.IsNaN(tempC) {
		tempC = 15 // the standard atmosphere's, rather than no answer at all
	}
	const lapse = 0.0065 // K/m
	return stationHPa * math.Pow(1-lapse*altitudeM/(tempC+lapse*altitudeM+273.15), -5.257)
}

func cToF(c float64) float64 { return c*9/5 + 32 }
func fToC(f float64) float64 { return (f - 32) * 5 / 9 }
