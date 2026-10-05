// Package tempest decodes the JSON datagrams a WeatherFlow Tempest hub
// broadcasts on the local network, and listens for them.
//
// The format is WeatherFlow's published UDP API. Every datagram is one JSON
// object with a "type"; observations arrive as positional arrays, so the field
// order below is the specification, not a choice.
package tempest

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// Obs is one obs_st message: the station's full observation, sent once a
// minute. Units are the ones the station reports: m/s, degrees, hPa (mb), °C,
// %, lux, W/m², mm, km and volts. A field the station sent as null is NaN.
type Obs struct {
	Serial         string
	Time           time.Time
	WindLull       float64 // m/s, minimum 3-second sample in the interval
	WindAvg        float64 // m/s, average over the interval
	WindGust       float64 // m/s, maximum 3-second sample in the interval
	WindDir        float64 // degrees from north
	StationPress   float64 // hPa at the station's altitude, not sea level
	Temp           float64 // °C
	Humidity       float64 // %
	Illuminance    float64 // lux
	UV             float64 // index
	Solar          float64 // W/m²
	Rain           float64 // mm fallen over the report interval
	PrecipType     int     // 0 none, 1 rain, 2 hail, 3 rain and hail
	StrikeDistance float64 // km, average over the interval
	StrikeCount    int
	Battery        float64 // volts
	Interval       int     // minutes the observation covers
}

// RapidWind is a rapid_wind message, sent every three seconds.
type RapidWind struct {
	Serial string
	Time   time.Time
	Speed  float64 // m/s
	Dir    float64 // degrees from north
}

// Strike is an evt_strike message: one lightning strike the sensor heard.
type Strike struct {
	Serial   string
	Time     time.Time
	Distance float64 // km
	Energy   float64 // unitless, as reported
}

// RainStart is an evt_precip message: rain has just begun.
type RainStart struct {
	Serial string
	Time   time.Time
}

// DeviceStatus is a device_status message about the sensor itself.
type DeviceStatus struct {
	Serial       string
	Time         time.Time
	Uptime       time.Duration
	Voltage      float64
	RSSI         int // signal at the sensor, dBm
	HubRSSI      int // the sensor's signal as heard by the hub, dBm
	SensorStatus uint32
}

// HubStatus is a hub_status message about the hub.
type HubStatus struct {
	Serial string
	Time   time.Time
	Uptime time.Duration
	RSSI   int // the hub's Wi-Fi signal, dBm
}

// ErrIgnored is returned for a well-formed message of a type this package does
// not use (the older AIR and SKY sensors, for example).
var ErrIgnored = errors.New("tempest: message type not handled")

// The fields every message type shares. Arrays hold *float64 so that a null
// in the station's output is told apart from a real zero.
type envelope struct {
	Type         string       `json:"type"`
	Serial       string       `json:"serial_number"`
	Obs          [][]*float64 `json:"obs"`
	Ob           []*float64   `json:"ob"`
	Evt          []*float64   `json:"evt"`
	Timestamp    int64        `json:"timestamp"`
	Uptime       int64        `json:"uptime"`
	Voltage      float64      `json:"voltage"`
	RSSI         int          `json:"rssi"`
	HubRSSI      int          `json:"hub_rssi"`
	SensorStatus uint32       `json:"sensor_status"`
}

// Parse decodes one datagram into an Obs, RapidWind, Strike, RainStart,
// DeviceStatus or HubStatus. Other well-formed message types return ErrIgnored.
func Parse(b []byte) (any, error) {
	var e envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("tempest: decode: %w", err)
	}
	switch e.Type {
	case "obs_st":
		// A hub that was offline can batch several observations into one
		// message; the last is the newest.
		if len(e.Obs) == 0 {
			return nil, errors.New("tempest: obs_st with no observations")
		}
		return parseObs(e.Serial, e.Obs[len(e.Obs)-1])
	case "rapid_wind":
		if len(e.Ob) < 3 {
			return nil, errors.New("tempest: short rapid_wind")
		}
		return RapidWind{Serial: e.Serial, Time: epoch(e.Ob[0]), Speed: val(e.Ob[1]), Dir: val(e.Ob[2])}, nil
	case "evt_strike":
		if len(e.Evt) < 3 {
			return nil, errors.New("tempest: short evt_strike")
		}
		return Strike{Serial: e.Serial, Time: epoch(e.Evt[0]), Distance: val(e.Evt[1]), Energy: val(e.Evt[2])}, nil
	case "evt_precip":
		if len(e.Evt) < 1 {
			return nil, errors.New("tempest: short evt_precip")
		}
		return RainStart{Serial: e.Serial, Time: epoch(e.Evt[0])}, nil
	case "device_status":
		return DeviceStatus{
			Serial: e.Serial, Time: time.Unix(e.Timestamp, 0), Uptime: time.Duration(e.Uptime) * time.Second,
			Voltage: e.Voltage, RSSI: e.RSSI, HubRSSI: e.HubRSSI, SensorStatus: e.SensorStatus,
		}, nil
	case "hub_status":
		return HubStatus{Serial: e.Serial, Time: time.Unix(e.Timestamp, 0), Uptime: time.Duration(e.Uptime) * time.Second, RSSI: e.RSSI}, nil
	case "":
		return nil, errors.New("tempest: message has no type")
	default:
		return nil, ErrIgnored
	}
}

// obsFields is the obs_st array length this package needs. Newer firmware may
// append fields; they are ignored.
const obsFields = 18

func parseObs(serial string, o []*float64) (Obs, error) {
	if len(o) < obsFields {
		return Obs{}, fmt.Errorf("tempest: obs_st has %d fields, want %d", len(o), obsFields)
	}
	if o[0] == nil {
		return Obs{}, errors.New("tempest: obs_st without a time")
	}
	return Obs{
		Serial:         serial,
		Time:           epoch(o[0]),
		WindLull:       val(o[1]),
		WindAvg:        val(o[2]),
		WindGust:       val(o[3]),
		WindDir:        val(o[4]),
		StationPress:   val(o[6]),
		Temp:           val(o[7]),
		Humidity:       val(o[8]),
		Illuminance:    val(o[9]),
		UV:             val(o[10]),
		Solar:          val(o[11]),
		Rain:           val(o[12]),
		PrecipType:     ival(o[13]),
		StrikeDistance: val(o[14]),
		StrikeCount:    ival(o[15]),
		Battery:        val(o[16]),
		Interval:       ival(o[17]),
	}, nil
}

func val(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func ival(p *float64) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

func epoch(p *float64) time.Time {
	if p == nil {
		return time.Time{}
	}
	return time.Unix(int64(*p), 0)
}
