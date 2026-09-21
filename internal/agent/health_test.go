package agent

import "testing"

// /proc/stat counts jiffies since boot, so one reading cannot say how busy the processor is. The
// first heartbeat after a restart therefore reports nothing rather than a number, and the panel
// shows "not available" until there are two readings to subtract.
func TestCPUSamplerNeedsTwoReadings(t *testing.T) {
	var c cpuSampler

	c.prev, c.valid = cpuTimes{}, false
	if _, ok := c.sample(); ok {
		// On a host without /proc/stat this returns false anyway; the point is it is never true.
		t.Fatal("a first reading cannot yield a utilisation")
	}
}

func TestCPUSamplerMeasuresBetweenReadings(t *testing.T) {
	var c cpuSampler
	// 1000 jiffies elapsed, 250 of them idle: three quarters of the processor was working.
	c.prev, c.valid = cpuTimes{total: 10_000, idle: 4_000}, true
	busy, ok := busyBetween(c.prev, cpuTimes{total: 11_000, idle: 4_250})
	if !ok {
		t.Fatal("two readings must yield a utilisation")
	}
	if busy < 74.9 || busy > 75.1 {
		t.Fatalf("utilisation = %v, want 75", busy)
	}
}

// Counters restart at zero when the node reboots. Subtracting across that gives a negative delta,
// which must read as "no measurement" rather than as a wild percentage.
func TestCPUSamplerIgnoresCountersThatWentBackwards(t *testing.T) {
	if _, ok := busyBetween(cpuTimes{total: 10_000, idle: 4_000}, cpuTimes{total: 500, idle: 200}); ok {
		t.Fatal("counters that went backwards must not produce a reading")
	}
}
