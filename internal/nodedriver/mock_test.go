package nodedriver

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMockApplyRecordsAndFails(t *testing.T) {
	m := NewMock()
	id := uuid.New()
	ctx := context.Background()
	if _, err := m.Health(ctx, id); !errors.Is(err, ErrOffline) {
		t.Fatalf("unknown node should be offline, got %v", err)
	}
	m.SetOnline(id, true)
	res, err := m.Apply(ctx, id, ApplyRequest{ApplyProfiles: true, Profiles: []Profile{{Name: "default", Secret: "00"}}})
	if err != nil || !res.OK {
		t.Fatalf("apply %+v %v", res, err)
	}
	profiles, _ := m.GetProfiles(ctx, id)
	if len(profiles) != 1 || profiles[0].Name != "default" {
		t.Fatalf("profiles not stored: %+v", profiles)
	}
	m.FailNextApply(id, "check failed")
	res, err = m.Apply(ctx, id, ApplyRequest{ApplyProfiles: true})
	if err == nil || res.OK {
		t.Fatal("expected failure")
	}
	if len(m.Applied(id)) != 2 {
		t.Fatalf("applied = %d", len(m.Applied(id)))
	}
}
