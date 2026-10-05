// Command tempest-sim sends made-up Tempest broadcasts, so the dashboard and
// weewx's driver can be run without a station. The weather is a plausible
// day: a temperature curve, gusty wind that wanders, and now and then rain
// and lightning.
//
//	go run ./cmd/tempest-sim -to 127.0.0.1:50222 -speed 10
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net"
	"time"
)

func main() {
	to := flag.String("to", "127.0.0.1:50222", "where to send datagrams (255.255.255.255:50222 to broadcast)")
	speed := flag.Float64("speed", 1, "send observations this many times faster than a real station")
	serial := flag.String("serial", "ST-00000001", "sensor serial number to report")
	flag.Parse()

	conn, err := net.Dial("udp4", *to)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	const hub = "HB-00000001"
	send := func(format string, args ...any) {
		if _, err := fmt.Fprintf(conn, format, args...); err != nil {
			log.Print(err)
		}
	}
	every := func(d time.Duration) *time.Ticker { return time.NewTicker(time.Duration(float64(d) / *speed)) }
	wind, obs, status := every(3*time.Second), every(time.Minute), every(20*time.Second)
	dir, avg := 180.0, 3.0
	var gust, lull float64
	start := time.Now()
	log.Printf("sending to %s as %s, %gx speed", *to, *serial, *speed)
	for {
		select {
		case <-wind.C:
			dir = math.Mod(dir+rand.NormFloat64()*12+360, 360)
			avg = math.Max(0, avg+rand.NormFloat64()*0.3-(avg-3)*0.05)
			s := math.Max(0, avg+rand.NormFloat64()*1.2)
			gust, lull = math.Max(gust, s), math.Min(lull, s)
			send(`{"serial_number":%q,"type":"rapid_wind","hub_sn":%q,"ob":[%d,%.2f,%.0f]}`, *serial, hub, time.Now().Unix(), s, dir)
		case <-obs.C:
			// A day's temperature curve, compressed by -speed.
			hours := time.Since(start).Hours() * *speed
			temp := 22 + 6*math.Sin((hours-9)/24*2*math.Pi) + rand.NormFloat64()*0.2
			rh := math.Min(100, math.Max(20, 70-2.5*(temp-22)+rand.NormFloat64()))
			press := 1011 + math.Sin(hours/12*math.Pi)*2
			solar := math.Max(0, 800*math.Sin((hours-6)/12*math.Pi))
			rain, ptype := 0.0, 0
			if math.Mod(hours, 24) > 16 && math.Mod(hours, 24) < 17.5 {
				rain, ptype = rand.Float64()*0.4, 1
			}
			send(`{"serial_number":%q,"type":"obs_st","hub_sn":%q,"obs":[[%d,%.2f,%.2f,%.2f,%.0f,3,%.2f,%.2f,%.1f,%.0f,%.1f,%.0f,%.3f,%d,0,0,2.65,1]],"firmware_revision":181}`,
				*serial, hub, time.Now().Unix(), lull, avg, gust, dir, press, temp, rh, solar*120, solar/100, solar, rain, ptype)
			if rain > 0 && rand.IntN(4) == 0 {
				send(`{"serial_number":%q,"type":"evt_strike","hub_sn":%q,"evt":[%d,%d,%d]}`, *serial, hub, time.Now().Unix(), 5+rand.IntN(30), rand.IntN(5000))
			}
			gust, lull = 0, math.Inf(1)
		case <-status.C:
			send(`{"serial_number":%q,"type":"device_status","hub_sn":%q,"timestamp":%d,"uptime":1000,"voltage":2.65,"firmware_revision":181,"rssi":-55,"hub_rssi":-52,"sensor_status":0,"debug":0}`,
				*serial, hub, time.Now().Unix())
			send(`{"serial_number":%q,"type":"hub_status","firmware_revision":"194","uptime":1000,"rssi":-60,"timestamp":%d,"reset_flags":"PIN","seq":1}`, hub, time.Now().Unix())
		}
	}
}
