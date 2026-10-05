package archive

// weewx's three unit systems, as stored in the usUnits column.
const (
	US       = 1  // °F, mph, inHg, inches
	Metric   = 16 // °C, km/h, mbar, cm
	MetricWX = 17 // °C, m/s, mbar, mm
)

// units converts from one weewx unit system to °C, m/s, hPa and mm.
type units int64

func (u units) temp(v float64) float64 {
	if u == US {
		return (v - 32) * 5 / 9
	}
	return v
}

func (u units) speed(v float64) float64 {
	switch u {
	case US:
		return v * 0.44704
	case Metric:
		return v / 3.6
	}
	return v
}

func (u units) press(v float64) float64 {
	if u == US {
		return v * 33.8638866667
	}
	return v
}

func (u units) rain(v float64) float64 {
	switch u {
	case US:
		return v * 25.4
	case Metric:
		return v * 10
	}
	return v
}
