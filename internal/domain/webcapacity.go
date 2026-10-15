package domain

// WebCapacityStandard is telemt's own default: 128 simultaneous WEB sessions.
const WebCapacityStandard = 128

// WebCapacityPresets are the node-wide WEB session ceilings an operator can pick.
var WebCapacityPresets = []int{128, 256, 512, 1024, 2048}

// MaxNodeProfiles is the largest key cap a node can be given.
const MaxNodeProfiles = 1024

// TelemtDefaultMaxProfiles is telemt's own web.limits.max_profiles.
const TelemtDefaultMaxProfiles = 32

// WebServiceProfiles is the room web.limits.max_profiles keeps above a node's key cap for
// the node's own service profile.
const WebServiceProfiles = 1

// WebMaxProfiles is the web.limits.max_profiles a node with room for maxProfiles keys needs.
func WebMaxProfiles(maxProfiles int) int { return maxProfiles + WebServiceProfiles }

// WebCapacity is one capacity preset: the [web.limits] it sets and telemt's
// server.max_connections, the TCP ceiling Fake-TLS clients and WEB backends share.
type WebCapacity struct {
	Limits         WebLimits
	MaxConnections int64
}

// WebLimits is the part of telemt's [web.limits] a capacity preset owns. Every other key in
// the section keeps whatever the node already has.
type WebLimits struct {
	MaxSessionsGlobal          int64
	MaxSessionsPerIP           int64
	MaxStreamsGlobal           int64
	MaxHTTPConnections         int64
	MaxHTTPHandlers            int64
	MaxHTTPOverloadConnections int64
	MaxBootstrapsGlobal        int64
	MaxBootstrapsPerIP         int64
	NewBootstrapsPerMinute     int64
	NewBootstrapsBurst         int64
	NewSessionsPerMinute       int64
	NewSessionsBurst           int64
	NewStreamsPerMinute        int64
	NewStreamsBurst            int64
	MaxStreamHandshakes        int64
	MaxTombstonesPerSession    int64
	ControlBytesGlobal         int64
	PendingBytesGlobal         int64
	PendingItemsGlobal         int64
	WebsocketBytesGlobal       int64
	MemoryEnvelopeBytes        int64
}

const mib = 1024 * 1024

var webCapacityPresets = map[int]WebCapacity{
	128: {MaxConnections: 10000, Limits: WebLimits{
		MaxSessionsGlobal: 128, MaxSessionsPerIP: 16, MaxStreamsGlobal: 4096,
		MaxHTTPConnections: 1024, MaxHTTPHandlers: 512, MaxHTTPOverloadConnections: 64,
		MaxBootstrapsGlobal: 512, MaxBootstrapsPerIP: 64,
		NewBootstrapsPerMinute: 1200, NewBootstrapsBurst: 256,
		NewSessionsPerMinute: 600, NewSessionsBurst: 128,
		NewStreamsPerMinute: 6000, NewStreamsBurst: 512,
		MaxStreamHandshakes: 256, MaxTombstonesPerSession: 4096,
		ControlBytesGlobal: 16 * mib, PendingBytesGlobal: 512 * mib, PendingItemsGlobal: 256 * 1024,
		WebsocketBytesGlobal: 256 * mib, MemoryEnvelopeBytes: 1280 * mib,
	}},
	256: {MaxConnections: 15000, Limits: WebLimits{
		MaxSessionsGlobal: 256, MaxSessionsPerIP: 32, MaxStreamsGlobal: 8192,
		MaxHTTPConnections: 2048, MaxHTTPHandlers: 1024, MaxHTTPOverloadConnections: 128,
		MaxBootstrapsGlobal: 1024, MaxBootstrapsPerIP: 128,
		NewBootstrapsPerMinute: 2400, NewBootstrapsBurst: 512,
		NewSessionsPerMinute: 1200, NewSessionsBurst: 256,
		NewStreamsPerMinute: 12000, NewStreamsBurst: 1024,
		MaxStreamHandshakes: 512, MaxTombstonesPerSession: 2048,
		ControlBytesGlobal: 32 * mib, PendingBytesGlobal: 640 * mib, PendingItemsGlobal: 512 * 1024,
		WebsocketBytesGlobal: 320 * mib, MemoryEnvelopeBytes: 1536 * mib,
	}},
	512: {MaxConnections: 20000, Limits: WebLimits{
		MaxSessionsGlobal: 512, MaxSessionsPerIP: 48, MaxStreamsGlobal: 16384,
		MaxHTTPConnections: 4096, MaxHTTPHandlers: 2048, MaxHTTPOverloadConnections: 256,
		MaxBootstrapsGlobal: 2048, MaxBootstrapsPerIP: 192,
		NewBootstrapsPerMinute: 4800, NewBootstrapsBurst: 1024,
		NewSessionsPerMinute: 2400, NewSessionsBurst: 512,
		NewStreamsPerMinute: 24000, NewStreamsBurst: 2048,
		MaxStreamHandshakes: 1024, MaxTombstonesPerSession: 1024,
		ControlBytesGlobal: 64 * mib, PendingBytesGlobal: 768 * mib, PendingItemsGlobal: 1024 * 1024,
		WebsocketBytesGlobal: 384 * mib, MemoryEnvelopeBytes: 2048 * mib,
	}},
	1024: {MaxConnections: 40000, Limits: WebLimits{
		MaxSessionsGlobal: 1024, MaxSessionsPerIP: 64, MaxStreamsGlobal: 32768,
		MaxHTTPConnections: 8192, MaxHTTPHandlers: 4096, MaxHTTPOverloadConnections: 512,
		MaxBootstrapsGlobal: 4096, MaxBootstrapsPerIP: 256,
		NewBootstrapsPerMinute: 9600, NewBootstrapsBurst: 2048,
		NewSessionsPerMinute: 4800, NewSessionsBurst: 1024,
		NewStreamsPerMinute: 48000, NewStreamsBurst: 4096,
		MaxStreamHandshakes: 2048, MaxTombstonesPerSession: 512,
		ControlBytesGlobal: 128 * mib, PendingBytesGlobal: 1024 * mib, PendingItemsGlobal: 2048 * 1024,
		WebsocketBytesGlobal: 512 * mib, MemoryEnvelopeBytes: 2560 * mib,
	}},
	2048: {MaxConnections: 80000, Limits: WebLimits{
		MaxSessionsGlobal: 2048, MaxSessionsPerIP: 96, MaxStreamsGlobal: 65536,
		MaxHTTPConnections: 16384, MaxHTTPHandlers: 8192, MaxHTTPOverloadConnections: 1024,
		MaxBootstrapsGlobal: 8192, MaxBootstrapsPerIP: 384,
		NewBootstrapsPerMinute: 19200, NewBootstrapsBurst: 4096,
		NewSessionsPerMinute: 9600, NewSessionsBurst: 2048,
		NewStreamsPerMinute: 96000, NewStreamsBurst: 8192,
		MaxStreamHandshakes: 4096, MaxTombstonesPerSession: 256,
		ControlBytesGlobal: 256 * mib, PendingBytesGlobal: 1536 * mib, PendingItemsGlobal: 4096 * 1024,
		WebsocketBytesGlobal: 768 * mib, MemoryEnvelopeBytes: 3584 * mib,
	}},
}

// ValidWebCapacity reports whether n is one of WebCapacityPresets.
func ValidWebCapacity(n int) bool {
	_, ok := webCapacityPresets[n]
	return ok
}

// WebCapacityPreset returns the values of one preset.
func WebCapacityPreset(n int) (WebCapacity, bool) {
	c, ok := webCapacityPresets[n]
	return c, ok
}

// Fields lists the limits as telemt keys, in a stable order.
func (l WebLimits) Fields() []WebLimitField {
	return []WebLimitField{
		{"max_sessions_global", l.MaxSessionsGlobal},
		{"max_sessions_per_ip", l.MaxSessionsPerIP},
		{"max_streams_global", l.MaxStreamsGlobal},
		{"max_http_connections", l.MaxHTTPConnections},
		{"max_http_handlers", l.MaxHTTPHandlers},
		{"max_http_overload_connections", l.MaxHTTPOverloadConnections},
		{"max_bootstraps_global", l.MaxBootstrapsGlobal},
		{"max_bootstraps_per_ip", l.MaxBootstrapsPerIP},
		{"new_bootstraps_per_minute", l.NewBootstrapsPerMinute},
		{"new_bootstraps_burst", l.NewBootstrapsBurst},
		{"new_sessions_per_minute", l.NewSessionsPerMinute},
		{"new_sessions_burst", l.NewSessionsBurst},
		{"new_streams_per_minute", l.NewStreamsPerMinute},
		{"new_streams_burst", l.NewStreamsBurst},
		{"max_stream_handshakes", l.MaxStreamHandshakes},
		{"max_tombstones_per_session", l.MaxTombstonesPerSession},
		{"control_bytes_global", l.ControlBytesGlobal},
		{"pending_bytes_global", l.PendingBytesGlobal},
		{"pending_items_global", l.PendingItemsGlobal},
		{"websocket_bytes_global", l.WebsocketBytesGlobal},
		{"memory_envelope_bytes", l.MemoryEnvelopeBytes},
	}
}

// WebLimitField is one [web.limits] key and its value.
type WebLimitField struct {
	Key   string
	Value int64
}
