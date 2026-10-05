// Package live holds what the station has said most recently, and tells
// subscribers when it says something new.
package live

import (
	"sync"
	"time"

	"github.com/runlevel-six/stormkeep/internal/tempest"
)

// How much of each message kind is kept in memory.
const (
	windWindow   = 10 * time.Minute // rapid wind samples, for the live wind trace
	strikeWindow = 3 * time.Hour    // lightning strikes
	obsWindow    = 30 * time.Minute // full observations, for rain since the last archive record
)

// Event is something a subscriber is told about. Data is one of the tempest
// message types.
type Event struct {
	Kind string // "obs", "wind", "strike", "rain", "status"
	Data any
}

// Station is the live state of one station. The zero value is not usable;
// call New.
type Station struct {
	serial string // when set, messages from other sensors are ignored
	now    func() time.Time

	mu         sync.RWMutex
	obs        []tempest.Obs
	wind       []tempest.RapidWind
	strikes    []tempest.Strike
	rainStart  time.Time
	device     tempest.DeviceStatus
	hub        tempest.HubStatus
	lastPacket time.Time
	subs       map[chan Event]struct{}
}

// New returns a Station. If serial is not empty, only messages from that
// sensor (and any hub) are accepted, which matters only when a neighbor's
// station broadcasts onto the same network.
func New(serial string) *Station {
	return &Station{serial: serial, now: time.Now, subs: map[chan Event]struct{}{}}
}

// Handle records one decoded message and notifies subscribers.
func (s *Station) Handle(msg any) {
	var ev Event
	s.mu.Lock()
	now := s.now()
	switch m := msg.(type) {
	case tempest.Obs:
		if !s.accept(m.Serial) {
			s.mu.Unlock()
			return
		}
		s.obs = append(trim(s.obs, now.Add(-obsWindow), func(o tempest.Obs) time.Time { return o.Time }), m)
		ev = Event{"obs", m}
	case tempest.RapidWind:
		if !s.accept(m.Serial) {
			s.mu.Unlock()
			return
		}
		s.wind = append(trim(s.wind, now.Add(-windWindow), func(w tempest.RapidWind) time.Time { return w.Time }), m)
		ev = Event{"wind", m}
	case tempest.Strike:
		if !s.accept(m.Serial) {
			s.mu.Unlock()
			return
		}
		s.strikes = append(trim(s.strikes, now.Add(-strikeWindow), func(k tempest.Strike) time.Time { return k.Time }), m)
		ev = Event{"strike", m}
	case tempest.RainStart:
		if !s.accept(m.Serial) {
			s.mu.Unlock()
			return
		}
		s.rainStart = m.Time
		ev = Event{"rain", m}
	case tempest.DeviceStatus:
		if !s.accept(m.Serial) {
			s.mu.Unlock()
			return
		}
		s.device = m
		ev = Event{"status", m}
	case tempest.HubStatus:
		s.hub = m
		ev = Event{"status", m}
	default:
		s.mu.Unlock()
		return
	}
	s.lastPacket = now
	subs := make([]chan Event, 0, len(s.subs))
	for c := range s.subs {
		subs = append(subs, c)
	}
	s.mu.Unlock()

	for _, c := range subs {
		// A subscriber that is not keeping up misses events rather than
		// stalling the station for everyone.
		select {
		case c <- ev:
		default:
		}
	}
}

func (s *Station) accept(serial string) bool { return s.serial == "" || serial == s.serial }

// Subscribe returns a channel of events and a function that ends the
// subscription.
func (s *Station) Subscribe() (<-chan Event, func()) {
	c := make(chan Event, 64)
	s.mu.Lock()
	s.subs[c] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	return c, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, c)
			s.mu.Unlock()
		})
	}
}

// Snapshot is a copy of the live state.
type Snapshot struct {
	Obs        *tempest.Obs        // the newest observation, nil if none yet
	Recent     []tempest.Obs       // observations in the last half hour, oldest first
	Wind       []tempest.RapidWind // rapid wind in the last ten minutes, oldest first
	Strikes    []tempest.Strike    // strikes in the last three hours, oldest first
	RainStart  time.Time
	Device     tempest.DeviceStatus
	Hub        tempest.HubStatus
	LastPacket time.Time
}

// Snapshot returns the live state, dropping anything that has aged out even if
// no new message has arrived to push it out.
func (s *Station) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now()
	snap := Snapshot{
		Recent:     trim(append([]tempest.Obs(nil), s.obs...), now.Add(-obsWindow), func(o tempest.Obs) time.Time { return o.Time }),
		Wind:       trim(append([]tempest.RapidWind(nil), s.wind...), now.Add(-windWindow), func(w tempest.RapidWind) time.Time { return w.Time }),
		Strikes:    trim(append([]tempest.Strike(nil), s.strikes...), now.Add(-strikeWindow), func(k tempest.Strike) time.Time { return k.Time }),
		RainStart:  s.rainStart,
		Device:     s.device,
		Hub:        s.hub,
		LastPacket: s.lastPacket,
	}
	if len(s.obs) > 0 {
		o := s.obs[len(s.obs)-1]
		snap.Obs = &o
	}
	return snap
}

// trim drops the leading elements older than cutoff. Messages arrive in time
// order, so the old ones are always at the front.
func trim[T any](xs []T, cutoff time.Time, at func(T) time.Time) []T {
	i := 0
	for i < len(xs) && at(xs[i]).Before(cutoff) {
		i++
	}
	return xs[i:]
}
