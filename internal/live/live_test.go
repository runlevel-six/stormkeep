package live

import (
	"testing"
	"time"

	"github.com/runlevel-six/stormkeep/internal/tempest"
)

func TestStationKeepsWindows(t *testing.T) {
	now := time.Unix(1700000000, 0)
	s := New("")
	s.now = func() time.Time { return now }

	for i := range 300 { // 15 minutes of 3-second samples
		ts := now.Add(time.Duration(i-299) * 3 * time.Second)
		s.Handle(tempest.RapidWind{Time: ts, Speed: float64(i)})
	}
	s.Handle(tempest.Obs{Time: now.Add(-time.Hour), Temp: 1})
	s.Handle(tempest.Obs{Time: now, Temp: 2})
	s.Handle(tempest.Strike{Time: now.Add(-4 * time.Hour)})
	s.Handle(tempest.Strike{Time: now.Add(-time.Minute), Distance: 8})

	snap := s.Snapshot()
	if n := len(snap.Wind); n != 201 {
		t.Errorf("kept %d wind samples, want the last ten minutes (201)", n)
	}
	if snap.Obs == nil || snap.Obs.Temp != 2 {
		t.Errorf("latest obs %+v", snap.Obs)
	}
	if len(snap.Recent) != 1 {
		t.Errorf("recent obs %d, want only the one inside the window", len(snap.Recent))
	}
	if len(snap.Strikes) != 1 || snap.Strikes[0].Distance != 8 {
		t.Errorf("strikes %+v", snap.Strikes)
	}
	if !snap.LastPacket.Equal(now) {
		t.Errorf("last packet %v", snap.LastPacket)
	}

	// Time passes with no new messages: the snapshot still ages things out.
	now = now.Add(20 * time.Minute)
	if snap := s.Snapshot(); len(snap.Wind) != 0 || snap.Obs == nil {
		t.Errorf("after 20 quiet minutes: %d wind samples, obs %v", len(snap.Wind), snap.Obs)
	}
}

func TestStationFiltersBySerial(t *testing.T) {
	s := New("ST-1")
	s.Handle(tempest.Obs{Serial: "ST-2", Time: time.Now(), Temp: 99})
	if s.Snapshot().Obs != nil {
		t.Error("an observation from another station was accepted")
	}
	s.Handle(tempest.Obs{Serial: "ST-1", Time: time.Now(), Temp: 1})
	s.Handle(tempest.HubStatus{Serial: "HB-9", RSSI: -60})
	snap := s.Snapshot()
	if snap.Obs == nil || snap.Obs.Temp != 1 || snap.Hub.RSSI != -60 {
		t.Errorf("snapshot %+v", snap)
	}
}

func TestSubscribe(t *testing.T) {
	s := New("")
	c, cancel := s.Subscribe()
	s.Handle(tempest.Strike{Time: time.Now(), Distance: 3})
	select {
	case ev := <-c:
		if ev.Kind != "strike" {
			t.Errorf("kind %q", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	cancel()
	cancel() // twice is harmless
	s.Handle(tempest.Strike{Time: time.Now()})
	select {
	case ev := <-c:
		t.Errorf("event after unsubscribe: %+v", ev)
	default:
	}

	// A subscriber that never reads must not block the station.
	_, cancel2 := s.Subscribe()
	defer cancel2()
	done := make(chan struct{})
	go func() {
		for range 1000 {
			s.Handle(tempest.RapidWind{Time: time.Now()})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle blocked on a slow subscriber")
	}
}
