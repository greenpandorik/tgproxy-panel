package keys_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"tgwebproxy/internal/domain"
	"tgwebproxy/internal/keys"
	"tgwebproxy/internal/store/db"
)

const (
	oldSecretA = "0123456789abcdef0123456789abcdef"
	oldSecretB = "fedcba9876543210fedcba9876543210"
)

func TestImportKeepsEachPersonsSecret(t *testing.T) {
	svc, st, nodeID := setup(t)
	ctx := context.Background()
	base := keys.CreateInput{Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}

	got, err := svc.Import(ctx, base, []keys.ImportItem{
		{Label: "Anna", OwnerLabel: "@anna", Secret: strings.ToUpper(oldSecretA)},
		{Label: "Boris", Secret: oldSecretB},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Label != "Anna" || got[0].OwnerLabel != "@anna" {
		t.Fatalf("imported %+v", got)
	}
	secret, _ := svc.Secret(ctx, got[0])
	if secret != oldSecretA {
		t.Fatalf("the old secret must be kept, lowercased: %s", secret)
	}
	links, _ := svc.Links(ctx, got[1].ID)
	if len(links) == 0 || !strings.Contains(links[0].TMe, oldSecretB) {
		t.Fatalf("links must use the imported secret: %+v", links)
	}

	_, err = svc.Import(ctx, base, []keys.ImportItem{{Label: "Again", Secret: oldSecretA}, {Label: "New", Secret: "11111111111111111111111111111111"}})
	var ve keys.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve["items.0"], "another user") {
		t.Fatalf("a secret that is already in use must be refused: %v", err)
	}
	n, _ := st.Q.CountKeys(ctx, db.CountKeysParams{})
	if n != 2 {
		t.Fatalf("a refused import must create nobody, have %d keys", n)
	}
}

func TestImportRefusesBadLinesUpFront(t *testing.T) {
	svc, _, nodeID := setup(t)
	base := keys.CreateInput{Type: domain.KeyPersonal, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}
	_, err := svc.Import(context.Background(), base, []keys.ImportItem{
		{Label: "ok", Secret: oldSecretA},
		{Label: "short", Secret: "abc"},
		{Label: "twin", Secret: oldSecretA},
		{Label: "", Secret: oldSecretB},
	})
	var ve keys.ValidationError
	if !errors.As(err, &ve) || ve["items.1"] == "" || !strings.Contains(ve["items.2"], "line 1") || ve["items.3"] == "" || ve["items.0"] != "" {
		t.Fatalf("validation %v", err)
	}
}

func TestImportRefusesTheServersOwnSecret(t *testing.T) {
	svc, _, nodeID := setup(t)
	base := keys.CreateInput{Type: domain.KeyShared, CarrierMode: "https", NodeIDs: []uuid.UUID{nodeID}}
	_, err := svc.Import(context.Background(), base, []keys.ImportItem{{Label: "clash", Secret: "00000000000000000000000000000000"}})
	var ve keys.ValidationError
	if !errors.As(err, &ve) || ve["items.0"] == "" {
		t.Fatalf("the default profile's secret must not be reused: %v", err)
	}
}
