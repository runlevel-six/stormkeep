package tempest

import (
	"context"
	"errors"
	"math"
	"net"
	"testing"
	"time"
)

// Shapes taken from a real hub; serial numbers and values are made up.
const (
	obsMsg      = `{"serial_number":"ST-00000001","type":"obs_st","hub_sn":"HB-00000001","obs":[[1700000000,0.58,1.05,1.76,333,3,1011.79,25.53,70.08,68318,5.77,569,0.000000,0,0,0,2.663,1]],"firmware_revision":181}`
	windMsg     = `{"serial_number":"ST-00000001","type":"rapid_wind","hub_sn":"HB-00000001","ob":[1700000003,0.59,309]}`
	strikeMsg   = `{"serial_number":"ST-00000001","type":"evt_strike","hub_sn":"HB-00000001","evt":[1700000010,27,3848]}`
	precipMsg   = `{"serial_number":"ST-00000001","type":"evt_precip","hub_sn":"HB-00000001","evt":[1700000020]}`
	deviceMsg   = `{"serial_number":"ST-00000001","type":"device_status","hub_sn":"HB-00000001","timestamp":1700000030,"uptime":66308689,"voltage":2.663,"firmware_revision":181,"rssi":-50,"hub_rssi":-49,"sensor_status":655871,"debug":0}`
	hubMsg      = `{"serial_number":"HB-00000001","type":"hub_status","firmware_revision":"194","uptime":474374,"rssi":-65,"timestamp":1700000040,"reset_flags":"PIN,SFT,HRDFLT","seq":47385,"radio_stats":[26,1,0,3,60035],"mqtt_stats":[7,0]}`
	nullObsMsg  = `{"serial_number":"ST-00000001","type":"obs_st","obs":[[1700000060,null,null,null,null,3,1011.5,null,70,0,0,0,0,0,0,0,2.6,1]]}`
	batchObsMsg = `{"serial_number":"ST-00000001","type":"obs_st","obs":[[1700000000,0,1,2,90,3,1000,10,50,0,0,0,0.1,1,0,0,2.6,1],[1700000060,0,1,2,90,3,1001,11,51,0,0,0,0.2,1,0,0,2.6,1]]}`
)

func TestParseObs(t *testing.T) {
	m, err := Parse([]byte(obsMsg))
	if err != nil {
		t.Fatal(err)
	}
	o, ok := m.(Obs)
	if !ok {
		t.Fatalf("got %T, want Obs", m)
	}
	want := Obs{
		Serial: "ST-00000001", Time: time.Unix(1700000000, 0),
		WindLull: 0.58, WindAvg: 1.05, WindGust: 1.76, WindDir: 333,
		StationPress: 1011.79, Temp: 25.53, Humidity: 70.08, Illuminance: 68318,
		UV: 5.77, Solar: 569, Battery: 2.663, Interval: 1,
	}
	if o != want {
		t.Errorf("got  %+v\nwant %+v", o, want)
	}
}

func TestParseObsNulls(t *testing.T) {
	m, err := Parse([]byte(nullObsMsg))
	if err != nil {
		t.Fatal(err)
	}
	o := m.(Obs)
	if !math.IsNaN(o.WindAvg) || !math.IsNaN(o.Temp) {
		t.Errorf("null fields should be NaN, got wind %v temp %v", o.WindAvg, o.Temp)
	}
	if o.StationPress != 1011.5 {
		t.Errorf("pressure = %v", o.StationPress)
	}
}

func TestParseObsBatchTakesNewest(t *testing.T) {
	m, err := Parse([]byte(batchObsMsg))
	if err != nil {
		t.Fatal(err)
	}
	if o := m.(Obs); o.Time.Unix() != 1700000060 || o.Temp != 11 {
		t.Errorf("got time %v temp %v, want the second observation", o.Time.Unix(), o.Temp)
	}
}

func TestParseOthers(t *testing.T) {
	tests := []struct {
		msg  string
		want any
	}{
		{windMsg, RapidWind{Serial: "ST-00000001", Time: time.Unix(1700000003, 0), Speed: 0.59, Dir: 309}},
		{strikeMsg, Strike{Serial: "ST-00000001", Time: time.Unix(1700000010, 0), Distance: 27, Energy: 3848}},
		{precipMsg, RainStart{Serial: "ST-00000001", Time: time.Unix(1700000020, 0)}},
		{deviceMsg, DeviceStatus{Serial: "ST-00000001", Time: time.Unix(1700000030, 0), Uptime: 66308689 * time.Second, Voltage: 2.663, RSSI: -50, HubRSSI: -49, SensorStatus: 655871}},
		{hubMsg, HubStatus{Serial: "HB-00000001", Time: time.Unix(1700000040, 0), Uptime: 474374 * time.Second, RSSI: -65}},
	}
	for _, tt := range tests {
		got, err := Parse([]byte(tt.msg))
		if err != nil {
			t.Errorf("%s: %v", tt.msg, err)
			continue
		}
		if got != tt.want {
			t.Errorf("got  %+v\nwant %+v", got, tt.want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, msg := range []string{
		`not json`,
		`{"serial_number":"x"}`,
		`{"type":"obs_st","obs":[]}`,
		`{"type":"obs_st","obs":[[1,2,3]]}`,
		`{"type":"rapid_wind","ob":[1]}`,
		// What the driver this replaces would have run with eval().
		`__import__('os').system('true')`,
	} {
		if _, err := Parse([]byte(msg)); err == nil || errors.Is(err, ErrIgnored) {
			t.Errorf("%q: got %v, want a decode error", msg, err)
		}
	}
	if _, err := Parse([]byte(`{"type":"obs_air","obs":[[1]]}`)); !errors.Is(err, ErrIgnored) {
		t.Errorf("obs_air: got %v, want ErrIgnored", err)
	}
}

// Two listeners on one port must both receive a datagram: that is what lets
// this run beside weewx.
func TestListenSharesThePort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find a free port, then release it for the two listeners.
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.LocalAddr().String()
	probe.Close()

	got := make(chan any, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			errs <- Listen(ctx, addr, func(m any) { got <- m }, nil)
		}()
	}

	// Unicast to loopback reaches one SO_REUSEADDR socket, not both, so this
	// checks that both binds succeed and that delivery works; broadcast fan-out
	// is the kernel's job.
	deadline := time.After(3 * time.Second)
	for {
		conn, err := net.Dial("udp4", addr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte(windMsg)); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		select {
		case m := <-got:
			if _, ok := m.(RapidWind); !ok {
				t.Fatalf("got %T", m)
			}
			cancel()
			for range 2 {
				if err := <-errs; err != nil {
					t.Fatalf("listener: %v", err)
				}
			}
			return
		case err := <-errs:
			t.Fatalf("listener failed early: %v", err)
		case <-deadline:
			t.Fatal("no datagram received")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
