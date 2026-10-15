package domain

import (
	"errors"
	"math"
	"math/bits"
	"strings"
	"testing"
)

// The constants and checks below are ported from telemt 3.5.14:
// src/config/load/validate_web.rs, validate_web/memory.rs, validate_web/debug.rs and
// validate_web/websocket.rs, with the defaults from src/config/types/web/defaults.rs.
const (
	tmFrameHeaderBytes          = 8
	tmQueueItemCost             = 256
	tmControlExtraItems         = 16
	tmControlItemsPerStream     = 3
	tmInitialStreamWindow       = 4 * 1024 * 1024
	tmMaxHeaderBytes            = 64 * 1024
	tmMaxBodyBytes              = 16 * 1024 * 1024
	tmMaxFrameBytes             = 1024 * 1024
	tmMaxFramesPerBody          = 4096
	tmMaxTombstonesPerSession   = 4096
	tmMaxMemoryEnvelopeBytes    = 4 * 1024 * 1024 * 1024
	tmDebugRenderers            = 2
	tmDebugStatusPageBytes      = 8 * 1024 * 1024
	tmDebugGroupScratchBytes    = 4 * 1024 * 1024
	tmCarrierLearningEntryBytes = 512
	tmLaneStateBytes            = 1024
	tmOverloadConnectionBytes   = 4 * 1024
	tmCapabilityIndexEntryBytes = 64
	tmMinDebugBytesGlobal       = 4096
	tmMaxTraceWindowSecs        = 86_400
	tmMaxWebsocketBatchBytes    = 2 * 1024 * 1024
	tmWebsocketIOBufferBytes    = 64 * 1024
	tmWebsocketDriverOverhead   = 4 * 1024
	tmWebsocketFrameOverhead    = 14
	tmSemaphoreMaxPermits       = math.MaxUint64 >> 3
)

type telemtLimits struct {
	maxHeaderBytes, maxBodyBytes, maxFramePayloadBytes, carrierBatchBytes, maxFramesPerBody uint64
	maxHTTPConnections, maxHTTPOverloadConnections, maxHTTPHandlers                         uint64
	maxLaneOpenWaitsPerSession, pendingBytesPerLane, pendingItemsPerLane                    uint64
	websocketBytesGlobal                                                                    uint64
	websocketAdmissionWatermarkPct, websocketEvictionWatermarkPct                           uint64
	websocketHTTPConnectionReserve, maxWebsocketEvictionsInFlight                           uint64
	maxCarrierLearningEntries, maxBodyReaders, maxBodyBytesGlobal                           uint64
	maxSessionsGlobal, maxSessionsPerIP, maxStreamsPerSession, maxStreamsGlobal             uint64
	maxStreamHandshakes, maxTombstonesPerSession                                            uint64
	pendingBytesPerSession, pendingBytesGlobal, pendingItemsPerSession, pendingItemsGlobal  uint64
	controlBytesPerSession, controlBytesGlobal                                              uint64
	maxBootstrapsGlobal, maxBootstrapsPerIP                                                 uint64
	maxVhosts, maxProfiles, maxStaticFiles, maxStaticFileBytes, maxStaticBytes              uint64
	debugRecordsCapacity, debugBytesGlobal, memoryEnvelopeBytes                             uint64
	newBootstrapsPerMinute, newBootstrapsBurst, newSessionsPerMinute, newSessionsBurst      uint64
	newStreamsPerMinute, newStreamsBurst                                                    uint64
	debugBodyPrefixBytes, debugDecoyBodyPrefixBytes, debugDefaultWindow, debugMaxWindowSecs uint64
	websocketEvictionSecs, websocketWriteSecs                                               uint64
}

func telemtDefaultLimits() telemtLimits {
	return telemtLimits{
		maxHeaderBytes: 16 * 1024, maxBodyBytes: 2 * mib, maxFramePayloadBytes: mib, carrierBatchBytes: 2 * mib,
		maxFramesPerBody: 4096, maxHTTPConnections: 1024, maxHTTPOverloadConnections: 64, maxHTTPHandlers: 512,
		maxLaneOpenWaitsPerSession: 16, pendingBytesPerLane: 8 * mib, pendingItemsPerLane: 1024,
		websocketBytesGlobal: 256 * mib, websocketAdmissionWatermarkPct: 75, websocketEvictionWatermarkPct: 90,
		websocketHTTPConnectionReserve: 64, maxWebsocketEvictionsInFlight: 8, maxCarrierLearningEntries: 4096,
		maxBodyReaders: 32, maxBodyBytesGlobal: 64 * mib, maxSessionsGlobal: 128, maxSessionsPerIP: 16,
		maxStreamsPerSession: 128, maxStreamsGlobal: 4096, maxStreamHandshakes: 256, maxTombstonesPerSession: 4096,
		pendingBytesPerSession: 32 * mib, pendingBytesGlobal: 512 * mib, pendingItemsPerSession: 16 * 1024,
		pendingItemsGlobal: 256 * 1024, controlBytesPerSession: 256 * 1024, controlBytesGlobal: 16 * mib,
		maxBootstrapsGlobal: 512, maxBootstrapsPerIP: 64, maxVhosts: 8, maxProfiles: 32, maxStaticFiles: 4096,
		maxStaticFileBytes: 8 * mib, maxStaticBytes: 64 * mib, debugRecordsCapacity: 65_536,
		debugBytesGlobal: 64 * mib, memoryEnvelopeBytes: 1280 * mib,
		newBootstrapsPerMinute: 1200, newBootstrapsBurst: 256, newSessionsPerMinute: 600, newSessionsBurst: 128,
		newStreamsPerMinute: 6000, newStreamsBurst: 512,
		debugBodyPrefixBytes: 4096, debugDecoyBodyPrefixBytes: 4096, debugDefaultWindow: 180, debugMaxWindowSecs: 3600,
		websocketEvictionSecs: 1, websocketWriteSecs: 30,
	}
}

func (t telemtLimits) with(l WebLimits) telemtLimits {
	t.maxSessionsGlobal, t.maxSessionsPerIP, t.maxStreamsGlobal = uint64(l.MaxSessionsGlobal), uint64(l.MaxSessionsPerIP), uint64(l.MaxStreamsGlobal)
	t.maxHTTPConnections, t.maxHTTPHandlers, t.maxHTTPOverloadConnections = uint64(l.MaxHTTPConnections), uint64(l.MaxHTTPHandlers), uint64(l.MaxHTTPOverloadConnections)
	t.maxBootstrapsGlobal, t.maxBootstrapsPerIP = uint64(l.MaxBootstrapsGlobal), uint64(l.MaxBootstrapsPerIP)
	t.newBootstrapsPerMinute, t.newBootstrapsBurst = uint64(l.NewBootstrapsPerMinute), uint64(l.NewBootstrapsBurst)
	t.newSessionsPerMinute, t.newSessionsBurst = uint64(l.NewSessionsPerMinute), uint64(l.NewSessionsBurst)
	t.newStreamsPerMinute, t.newStreamsBurst = uint64(l.NewStreamsPerMinute), uint64(l.NewStreamsBurst)
	t.maxStreamHandshakes, t.maxTombstonesPerSession = uint64(l.MaxStreamHandshakes), uint64(l.MaxTombstonesPerSession)
	t.controlBytesGlobal, t.pendingBytesGlobal, t.pendingItemsGlobal = uint64(l.ControlBytesGlobal), uint64(l.PendingBytesGlobal), uint64(l.PendingItemsGlobal)
	t.websocketBytesGlobal, t.memoryEnvelopeBytes = uint64(l.WebsocketBytesGlobal), uint64(l.MemoryEnvelopeBytes)
	return t
}

var errOverflow = errors.New("overflowed usize")

type checked struct{ err error }

func (c *checked) add(a, b uint64) uint64 {
	s, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		c.err = errOverflow
	}
	return s
}

func (c *checked) mul(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if hi != 0 {
		c.err = errOverflow
	}
	return lo
}

func satSub(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}

func within(v, lo, hi uint64) bool { return v >= lo && v <= hi }

// validateTelemtLimits runs telemt's [web.limits] checks for a node whose carrier list includes
// both https-lanes and a WebSocket carrier, which is what the panel's default policy sends.
func validateTelemtLimits(l telemtLimits) error {
	if err := validateLimits(l); err != nil {
		return err
	}
	if l.debugBytesGlobal < tmMinDebugBytesGlobal {
		return errors.New("web.limits.debug_bytes_global must be at least 4096 bytes")
	}
	if l.debugDefaultWindow == 0 || l.debugMaxWindowSecs == 0 || l.debugDefaultWindow > l.debugMaxWindowSecs || l.debugMaxWindowSecs > tmMaxTraceWindowSecs {
		return errors.New("web.debug windows must be non-zero, ordered, and no greater than 86400 seconds")
	}
	if l.debugBodyPrefixBytes > l.maxBodyBytes {
		return errors.New("web.debug.body_prefix_bytes must not exceed web.limits.max_body_bytes")
	}
	if l.debugBodyPrefixBytes > l.debugBytesGlobal || l.debugDecoyBodyPrefixBytes > l.debugBytesGlobal {
		return errors.New("web.debug body prefixes must not exceed web.limits.debug_bytes_global")
	}
	if l.maxHTTPHandlers < 2 {
		return errors.New("WEB https candidates require web.limits.max_http_handlers >= 2")
	}
	if l.maxHTTPHandlers < 4 {
		return errors.New("WEB https-lanes candidates require web.limits.max_http_handlers >= 4")
	}
	return validateWebsocket(l)
}

func validateLimits(l telemtLimits) error {
	switch {
	case !within(l.maxHeaderBytes, 8192, tmMaxHeaderBytes):
		return errors.New("web.limits.max_header_bytes must be within [8192, 65536]")
	case !within(l.maxBodyBytes, tmFrameHeaderBytes, tmMaxBodyBytes):
		return errors.New("web.limits.max_body_bytes must be within [8, 16777216]")
	case !within(l.maxFramePayloadBytes, 1, tmMaxFrameBytes):
		return errors.New("web.limits.max_frame_payload_bytes must be within [1, 1048576]")
	case !within(l.maxFramesPerBody, 1, tmMaxFramesPerBody):
		return errors.New("web.limits.max_frames_per_body must be within [1, 4096]")
	case !within(l.maxTombstonesPerSession, 1, tmMaxTombstonesPerSession):
		return errors.New("web.limits.max_tombstones_per_session must be within [1, 4096]")
	case l.pendingBytesPerLane <= tmFrameHeaderBytes+tmQueueItemCost:
		return errors.New("web.limits.pending_bytes_per_lane must preserve one non-empty DATA frame")
	case l.carrierBatchBytes > l.maxBodyBytes || l.carrierBatchBytes < l.maxFramePayloadBytes+tmFrameHeaderBytes:
		return errors.New("web.limits.carrier_batch_bytes must fit max_body_bytes and one maximum frame")
	case l.maxFramePayloadBytes > tmInitialStreamWindow:
		return errors.New("web.limits.max_frame_payload_bytes must not exceed the initial stream window")
	}
	positive := map[string]uint64{
		"max_http_connections": l.maxHTTPConnections, "max_http_overload_connections": l.maxHTTPOverloadConnections,
		"max_http_handlers": l.maxHTTPHandlers, "max_lane_open_waits_per_session": l.maxLaneOpenWaitsPerSession,
		"pending_bytes_per_lane": l.pendingBytesPerLane, "pending_items_per_lane": l.pendingItemsPerLane,
		"max_websocket_evictions_in_flight": l.maxWebsocketEvictionsInFlight, "max_carrier_learning_entries": l.maxCarrierLearningEntries,
		"max_body_readers": l.maxBodyReaders, "max_body_bytes_global": l.maxBodyBytesGlobal,
		"max_sessions_global": l.maxSessionsGlobal, "max_sessions_per_ip": l.maxSessionsPerIP,
		"max_streams_per_session": l.maxStreamsPerSession, "max_streams_global": l.maxStreamsGlobal,
		"max_stream_handshakes": l.maxStreamHandshakes, "pending_bytes_per_session": l.pendingBytesPerSession,
		"pending_bytes_global": l.pendingBytesGlobal, "pending_items_per_session": l.pendingItemsPerSession,
		"pending_items_global": l.pendingItemsGlobal, "control_bytes_per_session": l.controlBytesPerSession,
		"control_bytes_global": l.controlBytesGlobal, "max_bootstraps_global": l.maxBootstrapsGlobal,
		"max_bootstraps_per_ip": l.maxBootstrapsPerIP, "max_vhosts": l.maxVhosts, "max_profiles": l.maxProfiles,
		"max_static_files": l.maxStaticFiles, "max_static_file_bytes": l.maxStaticFileBytes,
		"max_static_bytes": l.maxStaticBytes, "debug_records_capacity": l.debugRecordsCapacity,
		"debug_bytes_global": l.debugBytesGlobal, "memory_envelope_bytes": l.memoryEnvelopeBytes,
		"new_bootstraps_per_minute": l.newBootstrapsPerMinute, "new_bootstraps_burst": l.newBootstrapsBurst,
		"new_sessions_per_minute": l.newSessionsPerMinute, "new_sessions_burst": l.newSessionsBurst,
		"new_streams_per_minute": l.newStreamsPerMinute, "new_streams_burst": l.newStreamsBurst,
	}
	for field, v := range positive {
		if v == 0 {
			return errors.New("web.limits." + field + " must be > 0")
		}
	}
	for _, v := range []uint64{l.maxHTTPConnections, l.maxHTTPOverloadConnections, l.maxHTTPHandlers, l.maxBodyReaders, l.maxBodyBytesGlobal, l.maxStreamHandshakes} {
		if v > tmSemaphoreMaxPermits {
			return errors.New("web.limits exceeds Tokio semaphore capacity")
		}
	}
	for _, v := range []uint64{l.newBootstrapsPerMinute, l.newBootstrapsBurst, l.newSessionsPerMinute, l.newSessionsBurst, l.newStreamsPerMinute, l.newStreamsBurst} {
		if v > math.MaxUint32 {
			return errors.New("web.limits rate does not fit u32")
		}
	}
	if l.maxStreamsPerSession > math.MaxUint16 {
		return errors.New("web.limits.max_streams_per_session must fit synthetic source ports")
	}
	if l.maxSessionsPerIP > l.maxSessionsGlobal ||
		l.maxStreamsPerSession > l.maxStreamsGlobal ||
		l.maxStreamHandshakes > l.maxStreamsGlobal ||
		l.maxBootstrapsPerIP > l.maxBootstrapsGlobal ||
		l.maxHTTPHandlers > l.maxHTTPConnections ||
		l.maxBodyReaders > l.maxHTTPHandlers ||
		l.maxLaneOpenWaitsPerSession > l.maxStreamsPerSession ||
		l.pendingBytesPerSession > l.pendingBytesGlobal ||
		l.pendingItemsPerSession > l.pendingItemsGlobal ||
		l.pendingBytesPerLane > l.pendingBytesPerSession ||
		l.pendingItemsPerLane > l.pendingItemsPerSession ||
		l.controlBytesPerSession > l.controlBytesGlobal ||
		l.controlBytesPerSession > l.pendingBytesPerSession ||
		l.controlBytesGlobal > l.pendingBytesGlobal ||
		l.maxStaticFileBytes > l.maxStaticBytes {
		return errors.New("web.limits per-owner ceilings must not exceed global ceilings")
	}
	c := &checked{}
	controlItemsPerSession := c.add(tmControlExtraItems, c.mul(l.maxStreamsPerSession, tmControlItemsPerStream))
	controlItemsGlobal := c.mul(controlItemsPerSession, l.maxSessionsGlobal)
	const controlFrameCost = tmFrameHeaderBytes + 4 + tmQueueItemCost
	requiredControlBytesPerSession := c.mul(controlItemsPerSession, controlFrameCost)
	requiredControlBytesGlobal := c.mul(controlItemsGlobal, controlFrameCost)
	if c.err != nil {
		return c.err
	}
	if controlItemsPerSession >= l.pendingItemsPerSession ||
		controlItemsGlobal >= l.pendingItemsGlobal ||
		requiredControlBytesPerSession > l.controlBytesPerSession ||
		requiredControlBytesGlobal > l.controlBytesGlobal {
		return errors.New("web.limits control reserves must cover bounded control frames and leave data capacity")
	}
	uplinkBytes := c.add(c.mul(l.maxFramesPerBody, tmQueueItemCost), l.maxBodyBytes)
	const minimumDownlinkFrameBytes = tmFrameHeaderBytes + 1 + tmQueueItemCost
	sessionRequiredBytes := c.add(c.add(l.controlBytesPerSession, uplinkBytes), minimumDownlinkFrameBytes)
	sessionRequiredItems := c.add(c.add(controlItemsPerSession, l.maxFramesPerBody), 1)
	globalRequiredBytes := c.add(c.add(l.controlBytesGlobal, uplinkBytes), minimumDownlinkFrameBytes)
	globalRequiredItems := c.add(c.add(controlItemsGlobal, l.maxFramesPerBody), 1)
	if c.err != nil {
		return c.err
	}
	if sessionRequiredBytes > l.pendingBytesPerSession ||
		sessionRequiredItems > l.pendingItemsPerSession ||
		globalRequiredBytes > l.pendingBytesGlobal ||
		globalRequiredItems > l.pendingItemsGlobal {
		return errors.New("web.limits pending ceilings must preserve one uplink batch and downlink progress")
	}
	return validateMemory(l)
}

func validateMemory(l telemtLimits) error {
	c := &checked{}
	bodyReservation := c.mul(l.maxBodyReaders, l.maxBodyBytes)
	if c.err != nil {
		return c.err
	}
	if bodyReservation > l.maxBodyBytesGlobal || l.maxBodyBytesGlobal > math.MaxUint32 {
		return errors.New("web.limits max_body_readers * max_body_bytes must fit max_body_bytes_global and u32")
	}
	httpHeaderReservation := c.mul(l.maxHTTPConnections, l.maxHeaderBytes)
	overloadConnectionReservation := c.mul(l.maxHTTPOverloadConnections, tmOverloadConnectionBytes)
	debugRingIndex := c.mul(l.debugRecordsCapacity, 8)
	statusPages := c.mul(tmDebugRenderers, tmDebugStatusPageBytes)
	debugReservation := c.add(c.add(l.debugBytesGlobal, c.mul(debugRingIndex, tmDebugRenderers)), c.mul(tmDebugRenderers, tmDebugGroupScratchBytes))
	carrierLearningReservation := c.mul(l.maxCarrierLearningEntries, tmCarrierLearningEntryBytes)
	capabilityIndexReservation := c.mul(l.maxProfiles, tmCapabilityIndexEntryBytes)
	laneStateReservation := c.mul(c.mul(c.add(c.add(l.maxStreamsPerSession, l.maxTombstonesPerSession), 1), l.maxSessionsGlobal), tmLaneStateBytes)
	reserved := l.pendingBytesGlobal
	for _, v := range []uint64{
		l.maxBodyBytesGlobal, l.maxStaticBytes, debugRingIndex, statusPages, debugReservation,
		carrierLearningReservation, capabilityIndexReservation, laneStateReservation,
		httpHeaderReservation, overloadConnectionReservation,
	} {
		reserved = c.add(reserved, v)
	}
	if c.err != nil {
		return c.err
	}
	if reserved > l.memoryEnvelopeBytes || l.memoryEnvelopeBytes > tmMaxMemoryEnvelopeBytes {
		return errors.New("web.limits memory reservations must fit memory_envelope_bytes within 4 GiB")
	}
	return nil
}

func validateWebsocket(l telemtLimits) error {
	if !within(l.websocketAdmissionWatermarkPct, 1, 99) ||
		!within(l.websocketEvictionWatermarkPct, 1, 99) ||
		l.websocketAdmissionWatermarkPct >= l.websocketEvictionWatermarkPct {
		return errors.New("web.limits WebSocket watermarks must satisfy 1 <= admission < eviction < 100")
	}
	if l.websocketBytesGlobal == 0 {
		return errors.New("web.limits.websocket_bytes_global must be > 0")
	}
	if l.websocketEvictionSecs > l.websocketWriteSecs {
		return errors.New("web.timeouts.websocket_eviction_secs must not exceed websocket_write_secs")
	}
	if l.carrierBatchBytes > tmMaxWebsocketBatchBytes {
		return errors.New("WebSocket carriers require web.limits.carrier_batch_bytes <= 2097152")
	}
	if l.websocketHTTPConnectionReserve == 0 || l.websocketHTTPConnectionReserve >= l.maxHTTPConnections {
		return errors.New("WebSocket carriers require websocket_http_connection_reserve within [1, max_http_connections)")
	}
	if l.maxWebsocketEvictionsInFlight > satSub(l.maxHTTPConnections, l.websocketHTTPConnectionReserve) {
		return errors.New("web.limits.max_websocket_evictions_in_flight must not exceed WebSocket connection capacity")
	}
	c := &checked{}
	socketBase := c.add(c.mul(tmWebsocketIOBufferBytes, 2), tmWebsocketDriverOverhead)
	minimumProgress := c.add(c.mul(c.add(l.carrierBatchBytes, tmWebsocketFrameOverhead), 2), socketBase)
	queueProgress := c.add(c.add(l.maxBodyBytes, c.mul(l.maxFramesPerBody, tmQueueItemCost)), l.carrierBatchBytes)
	if c.err != nil {
		return c.err
	}
	if l.websocketBytesGlobal < minimumProgress {
		return errors.New("web.limits.websocket_bytes_global must preserve one socket read and write")
	}
	dataBytes := satSub(l.pendingBytesGlobal, l.controlBytesGlobal)
	if l.websocketBytesGlobal > satSub(dataBytes, queueProgress) {
		return errors.New("web.limits.websocket_bytes_global must leave bounded queue progress")
	}
	return nil
}

func TestTelemtDefaultsPassTheMirroredValidation(t *testing.T) {
	if err := validateTelemtLimits(telemtDefaultLimits()); err != nil {
		t.Fatalf("telemt's own defaults must pass: %v", err)
	}
	old := telemtDefaultLimits()
	old.memoryEnvelopeBytes = 768 * mib
	if err := validateTelemtLimits(old); err == nil {
		t.Fatal("telemt's own test rejects the defaults with a 768 MiB envelope")
	}
}

func presetLimits(t *testing.T, n int) WebLimits {
	t.Helper()
	c, ok := WebCapacityPreset(n)
	if !ok {
		t.Fatalf("preset %d is missing", n)
	}
	return c.Limits
}

// validateProfileCount is the profile count rule from validate_web/vhosts.rs.
func validateProfileCount(profiles int, l telemtLimits) error {
	if uint64(profiles) > l.maxProfiles {
		return errors.New("WEB profiles exceed web.limits.max_profiles")
	}
	return nil
}

func TestEveryWebCapacityPresetIsValidForTelemt(t *testing.T) {
	for _, n := range WebCapacityPresets {
		l := presetLimits(t, n)
		if l.MaxSessionsGlobal != int64(n) {
			t.Fatalf("preset %d sets max_sessions_global to %d", n, l.MaxSessionsGlobal)
		}
		for _, keys := range []int{1, 128, MaxNodeProfiles} {
			tl := telemtDefaultLimits().with(l)
			tl.maxProfiles = uint64(WebMaxProfiles(keys))
			if err := validateTelemtLimits(tl); err != nil {
				t.Fatalf("preset %d with room for %d keys is refused by telemt's rules: %v", n, keys, err)
			}
			if err := validateProfileCount(keys+WebServiceProfiles, tl); err != nil {
				t.Fatalf("preset %d: %v", n, err)
			}
		}
	}
}

func TestTelemtDefaultProfilesCapRefusesTheThirtyThirdKey(t *testing.T) {
	l := telemtDefaultLimits()
	if l.maxProfiles != TelemtDefaultMaxProfiles {
		t.Fatalf("telemt's default max_profiles is %d", TelemtDefaultMaxProfiles)
	}
	if err := validateProfileCount(TelemtDefaultMaxProfiles+1, l); err == nil {
		t.Fatal("telemt refuses a 33rd WEB profile under its default cap")
	}
	l.maxProfiles = uint64(WebMaxProfiles(128))
	if err := validateProfileCount(128+WebServiceProfiles, l); err != nil {
		t.Fatalf("a node with room for 128 keys must take them all: %v", err)
	}
}

func TestStandardWebCapacityIsTelemtDefault(t *testing.T) {
	c, _ := WebCapacityPreset(WebCapacityStandard)
	if got, want := telemtDefaultLimits().with(c.Limits), telemtDefaultLimits(); got != want {
		t.Fatalf("the standard preset must be telemt's defaults exactly:\n got %+v\nwant %+v", got, want)
	}
	if c.MaxConnections != 10000 {
		t.Fatalf("telemt's default server.max_connections is 10000, the standard preset sets %d", c.MaxConnections)
	}
}

func TestWebCapacityPresetsGrowMonotonically(t *testing.T) {
	prev, _ := WebCapacityPreset(WebCapacityPresets[0])
	for _, n := range WebCapacityPresets[1:] {
		cur, _ := WebCapacityPreset(n)
		if cur.MaxConnections <= prev.MaxConnections {
			t.Fatalf("preset %d does not raise server.max_connections", n)
		}
		pf, cf := prev.Limits.Fields(), cur.Limits.Fields()
		for i := range cf {
			if cf[i].Key == "max_tombstones_per_session" {
				if cf[i].Value > pf[i].Value {
					t.Fatalf("preset %d raises tombstones, which grows lane memory with the session count", n)
				}
				continue
			}
			if cf[i].Value <= pf[i].Value {
				t.Fatalf("preset %d does not raise %s (%d after %d)", n, cf[i].Key, cf[i].Value, pf[i].Value)
			}
		}
		prev = cur
	}
}

func TestBrokenWebCapacityFailsTheMemoryRule(t *testing.T) {
	l := presetLimits(t, 1024)
	l.MaxTombstonesPerSession = 4096
	err := validateTelemtLimits(telemtDefaultLimits().with(l))
	if err == nil || !strings.Contains(err.Error(), "memory_envelope_bytes") {
		t.Fatalf("1024 sessions with 4096 tombstones reserve over 4 GiB of lane state; got %v", err)
	}
	l = presetLimits(t, 1024)
	l.MemoryEnvelopeBytes = 5 * 1024 * mib
	if err := validateTelemtLimits(telemtDefaultLimits().with(l)); err == nil {
		t.Fatal("an envelope above 4 GiB is refused")
	}
	l = presetLimits(t, 1024)
	l.ControlBytesGlobal = 64 * mib
	if err := validateTelemtLimits(telemtDefaultLimits().with(l)); err == nil || !strings.Contains(err.Error(), "control reserves") {
		t.Fatalf("1024 sessions need more than 64 MiB of control reserve; got %v", err)
	}
	l = presetLimits(t, 1024)
	l.MaxSessionsPerIP = 2048
	if err := validateTelemtLimits(telemtDefaultLimits().with(l)); err == nil || !strings.Contains(err.Error(), "per-owner") {
		t.Fatalf("a per-address ceiling above the global one is refused; got %v", err)
	}
	l = presetLimits(t, 1024)
	l.PendingBytesGlobal = 100 * mib
	if err := validateTelemtLimits(telemtDefaultLimits().with(l)); err == nil {
		t.Fatal("a pending ceiling below the control reserve is refused")
	}
	tl := telemtDefaultLimits()
	tl.maxProfiles = 70_000
	if err := validateTelemtLimits(tl); err == nil || !strings.Contains(err.Error(), "memory_envelope_bytes") {
		t.Fatalf("70000 profiles overflow the standard envelope through the capability index; got %v", err)
	}
}

func TestHTTPSLanesNeedFourHandlers(t *testing.T) {
	l := telemtDefaultLimits()
	l.maxHTTPHandlers, l.maxBodyReaders = 3, 3
	if err := validateTelemtLimits(l); err == nil || !strings.Contains(err.Error(), "https-lanes") {
		t.Fatalf("three handlers are refused for https-lanes; got %v", err)
	}
}

func TestValidWebCapacity(t *testing.T) {
	for _, n := range []int{128, 256, 512, 1024, 2048} {
		if !ValidWebCapacity(n) {
			t.Fatalf("%d is a preset", n)
		}
	}
	for _, n := range []int{0, 64, 200, 4096} {
		if ValidWebCapacity(n) {
			t.Fatalf("%d is not a preset", n)
		}
	}
}
