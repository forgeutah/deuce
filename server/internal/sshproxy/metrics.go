package sshproxy

import (
	"runtime"
	"sync/atomic"
)

// Metrics holds the SSH proxy's counter primitives. All counters are
// updated lock-free via sync/atomic so the hot path doesn't take a
// mutex. There is no Prometheus dependency in v1 — these are private
// counters that future work can expose via /metrics or similar.
//
// Counter naming follows Prometheus conventions even though we don't
// register them yet:
//
//   - ConnectionsTotalOK / ConnectionsTotalFail → connections_total{result}
//   - SessionsActive                            → sessions_active   (gauge)
//   - ChannelsOpenTotalSession / Other          → channels_open_total{type}
//   - AuthAttemptsTotalOK / AuthAttemptsTotalFail → auth_attempts_total{result}
//   - GoroutinesSSH                             → goroutines_ssh    (gauge)
//
// The pointer (not value) type is intentional: Metrics is meant to be
// shared by reference from the Server and never copied. Snapshot()
// returns a flat value-typed copy for safe observation by tests and
// future exporters.
type Metrics struct {
	connectionsTotalOK   atomic.Int64
	connectionsTotalFail atomic.Int64
	sessionsActive       atomic.Int64
	channelsOpenSession  atomic.Int64
	channelsOpenOther    atomic.Int64
	authAttemptsOK       atomic.Int64
	authAttemptsFail     atomic.Int64
	acceptOverloaded     atomic.Int64
}

// newMetrics constructs a zero-valued Metrics. Counters start at 0.
func newMetrics() *Metrics {
	return &Metrics{}
}

// MetricsSnapshot is a flat value-typed copy of the Metrics counters
// at one instant. Returned by Server.Metrics() for observation; mutations
// to a snapshot do not affect the live counters.
type MetricsSnapshot struct {
	ConnectionsTotalOK   int64
	ConnectionsTotalFail int64
	SessionsActive       int64
	ChannelsOpenSession  int64
	ChannelsOpenOther    int64
	AuthAttemptsOK       int64
	AuthAttemptsFail     int64
	AcceptOverloaded     int64
	GoroutinesSSH        int64
}

// snapshot returns a value-typed copy of every counter. Note: because
// counters are read independently, the snapshot is NOT a consistent
// cut — concurrent updates may straddle the reads. That tradeoff is
// fine for observability; we don't want to take a mutex on the hot path.
func (m *Metrics) snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		ConnectionsTotalOK:   m.connectionsTotalOK.Load(),
		ConnectionsTotalFail: m.connectionsTotalFail.Load(),
		SessionsActive:       m.sessionsActive.Load(),
		ChannelsOpenSession:  m.channelsOpenSession.Load(),
		ChannelsOpenOther:    m.channelsOpenOther.Load(),
		AuthAttemptsOK:       m.authAttemptsOK.Load(),
		AuthAttemptsFail:     m.authAttemptsFail.Load(),
		AcceptOverloaded:     m.acceptOverloaded.Load(),
		GoroutinesSSH:        int64(runtime.NumGoroutine()),
	}
}

// incConnectionOK records a successful handshake-and-accept.
func (m *Metrics) incConnectionOK() { m.connectionsTotalOK.Add(1) }

// incConnectionFail records a failed handshake (auth denied,
// pre-handshake timeout, malformed input, etc.).
func (m *Metrics) incConnectionFail() { m.connectionsTotalFail.Add(1) }

// incSessionsActive / decSessionsActive bracket the lifetime of an
// authenticated SSH connection. Tracked separately from
// ConnectionsTotalOK because sessions_active is a gauge.
func (m *Metrics) incSessionsActive() { m.sessionsActive.Add(1) }
func (m *Metrics) decSessionsActive() { m.sessionsActive.Add(-1) }

// incChannelOpenSession / incChannelOpenOther split channels_open_total
// by ChannelType. "session" is the only type the proxy accepts; "other"
// counts rejected types so the metric still records the attempt.
func (m *Metrics) incChannelOpenSession() { m.channelsOpenSession.Add(1) }
func (m *Metrics) incChannelOpenOther()   { m.channelsOpenOther.Add(1) }

// incAuthAttemptOK / incAuthAttemptFail count auth attempts by outcome.
// Driven from AuthLogCallback so every method (publickey, none, etc.)
// is recorded.
func (m *Metrics) incAuthAttemptOK()   { m.authAttemptsOK.Add(1) }
func (m *Metrics) incAuthAttemptFail() { m.authAttemptsFail.Add(1) }

// incAcceptOverloaded records an accept-loop rejection due to the
// goroutine cap. Useful for tuning GoroutineCap.
func (m *Metrics) incAcceptOverloaded() { m.acceptOverloaded.Add(1) }
