package api

import (
	"testing"

	"tgwebproxy/internal/domain"
)

func TestCounterGrowthOnlyCountsWhatIsNew(t *testing.T) {
	str := func(s string) *string { return &s }
	previous := []domain.DiagnosticGroup{{Key: "telemt", Checks: []domain.DiagnosticCheck{{Key: "tls_front_errors", Value: str("8")}}}}
	check := func(v string) domain.DiagnosticCheck {
		return domain.DiagnosticCheck{Key: "tls_front_errors", Value: str(v)}
	}

	if _, grew := counterGrowth(previous, "telemt", check("8")); grew {
		t.Fatal("an unchanged counter must not keep an incident open")
	}
	if growth, grew := counterGrowth(previous, "telemt", check("31")); !grew || growth != 23 {
		t.Fatalf("growth = %v, %v; want 23 new since the previous run", growth, grew)
	}
	if _, grew := counterGrowth(previous, "telemt", check("2")); grew {
		t.Fatal("a counter that fell back after a telemt restart holds nothing known to be new")
	}
	if _, grew := counterGrowth(nil, "telemt", check("31")); grew {
		t.Fatal("with no previous run there is no baseline to grow from")
	}
	if _, grew := counterGrowth(previous, "web", check("31")); grew {
		t.Fatal("a reading from another group is not this check's baseline")
	}
}
