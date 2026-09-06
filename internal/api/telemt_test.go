package api_test

import (
	"bytes"
	"encoding/hex"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tgwebproxy/internal/api/apitest"
	"tgwebproxy/internal/store/db"
)

type engineNodeResp struct {
	ID            uuid.UUID `json:"id"`
	Hostname      string    `json:"hostname"`
	PublicIP      string    `json:"public_ip"`
	Engine        string    `json:"engine"`
	TLSDomain     string    `json:"tls_domain"`
	ClassicPort   int       `json:"classic_port"`
	TelemtVersion string    `json:"telemt_version"`
	Dirty         bool      `json:"dirty"`
}

// createEngineNode posts a node with whatever engine fields the caller supplies
// and returns the node plus its install command.
func createEngineNode(t *testing.T, c *apitest.Client, body map[string]any) (engineNodeResp, string) {
	t.Helper()
	var out struct {
		Node           engineNodeResp `json:"node"`
		InstallCommand string         `json:"install_command"`
	}
	resp := c.Post("/api/v1/nodes", body)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create node %d: %s", resp.StatusCode, b)
	}
	c.JSON(resp, &out)
	return out.Node, out.InstallCommand
}

func TestCreateNodeDefaultsToTelemtEngine(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	n, _ := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co"})
	if n.Engine != "telemt" {
		t.Fatalf("engine = %q, want telemt", n.Engine)
	}
	// tls_domain defaults to the node's own hostname; classic_port to 8443.
	if n.TLSDomain != "n1.test" || n.ClassicPort != 8443 || n.TelemtVersion != "" {
		t.Fatalf("node %+v", n)
	}

	old, _ := createEngineNode(t, c, map[string]any{"name": "n2", "hostname": "n2.test", "acme_email": "a@b.co", "engine": "tproxy"})
	if old.Engine != "tproxy" {
		t.Fatalf("engine = %q, want tproxy", old.Engine)
	}

	custom, _ := createEngineNode(t, c, map[string]any{
		"name": "n3", "hostname": "n3.test", "acme_email": "a@b.co",
		"engine": "telemt", "tls_domain": "cdn.example.com", "classic_port": 9443,
	})
	if custom.TLSDomain != "cdn.example.com" || custom.ClassicPort != 9443 {
		t.Fatalf("node %+v", custom)
	}
}

func TestCreateNodeEngineValidation(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")

	for name, body := range map[string]map[string]any{
		"unknown engine": {"name": "x", "hostname": "e1.test", "acme_email": "a@b.co", "engine": "mtproto"},
		"bad tls domain": {"name": "x", "hostname": "e2.test", "acme_email": "a@b.co", "tls_domain": "Not A Domain"},
		"port too low":   {"name": "x", "hostname": "e3.test", "acme_email": "a@b.co", "classic_port": 443},
		"port too high":  {"name": "x", "hostname": "e4.test", "acme_email": "a@b.co", "classic_port": 70000},
	} {
		resp := c.Post("/api/v1/nodes", body)
		if resp.StatusCode != 422 {
			b, _ := io.ReadAll(resp.Body)
			t.Errorf("%s: status %d, want 422 (%s)", name, resp.StatusCode, b)
			continue
		}
		resp.Body.Close() //nolint:errcheck
	}
}

func TestPatchNodeFakeTLSSettingsMarkDirty(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co"})

	var got engineNodeResp
	c.JSON(c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"tls_domain": "mask.example.com", "classic_port": 9443}), &got)
	if got.TLSDomain != "mask.example.com" || got.ClassicPort != 9443 {
		t.Fatalf("patch %+v", got)
	}
	if !got.Dirty {
		t.Fatal("changing listener settings must mark the node dirty")
	}

	if resp := c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"classic_port": 80}); resp.StatusCode != 422 {
		t.Fatalf("classic_port 80 expected 422, got %d", resp.StatusCode)
	}
	if resp := c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"tls_domain": "no dots"}); resp.StatusCode != 422 {
		t.Fatalf("bad tls_domain expected 422, got %d", resp.StatusCode)
	}
}

// The public IP is editable after creation: a NAT host's installer can register the egress
// address, and the fix is to correct it in the panel. A change is desired state (it reaches
// the node on the next apply), so it marks the node dirty; anything but an IPv4 is refused.
func TestPatchNodePublicIPValidatesAndMarksDirty(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co", "public_ip": "104.239.66.187"})
	if n.Dirty {
		t.Fatal("a fresh node must not be dirty")
	}

	var got engineNodeResp
	c.JSON(c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"public_ip": " 104.239.66.129 "}), &got)
	if got.PublicIP != "104.239.66.129" {
		t.Fatalf("public_ip = %q", got.PublicIP)
	}
	if !got.Dirty {
		t.Fatal("changing public_ip must mark the node dirty")
	}

	for name, ip := range map[string]string{
		"not an ip": "example.com",
		"ipv6":      "2001:db8::1",
		"port":      "104.239.66.129:443",
		"short":     "104.239.66",
	} {
		resp := c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"public_ip": ip})
		if resp.StatusCode != 422 {
			t.Fatalf("%s: expected 422, got %d", name, resp.StatusCode)
		}
		resp.Body.Close() //nolint:errcheck
	}
	// The same rule on create.
	resp := c.Post("/api/v1/nodes", map[string]any{"name": "x", "hostname": "e9.test", "acme_email": "a@b.co", "public_ip": "not-an-ip"})
	if resp.StatusCode != 422 {
		t.Fatalf("create with a bad public_ip expected 422, got %d", resp.StatusCode)
	}
	resp.Body.Close() //nolint:errcheck
	// Clearing it is still allowed (the installer fills it in again).
	c.JSON(c.Patch("/api/v1/nodes/"+n.ID.String(), map[string]any{"public_ip": ""}), &got)
	if got.PublicIP != "" {
		t.Fatalf("public_ip = %q, want empty", got.PublicIP)
	}
}

func installToken(t *testing.T, cmd string) string {
	t.Helper()
	m := regexp.MustCompile(`/install/([^/]+)\.sh`).FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("no install token in %q", cmd)
	}
	return m[1]
}

func TestRegisterStoresPublicIPAndRequiresItForTelemt(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, cmd := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co"})
	token := installToken(t, cmd)
	anon := h.Anonymous()

	// telemt needs the address for web.vhosts.public_addr, so a registration
	// without one is refused - and must not burn the single-use install token.
	resp := anon.Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test", "agent_version": "0.1.0"})
	if resp.StatusCode != 422 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("register without public_ip: %d %s", resp.StatusCode, b)
	}
	resp.Body.Close() //nolint:errcheck

	resp = anon.Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test", "agent_version": "0.1.0", "public_ip": "203.0.113.9"})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("register with public_ip: %d %s", resp.StatusCode, b)
	}
	resp.Body.Close() //nolint:errcheck
	var got engineNodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if got.PublicIP != "203.0.113.9" {
		t.Fatalf("public_ip = %q", got.PublicIP)
	}
}

func TestRegisterTProxyNodeWithoutPublicIP(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	_, cmd := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co", "engine": "tproxy"})
	token := installToken(t, cmd)
	resp := h.Anonymous().Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test", "tproxy_version": "52a5feb"})
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("tproxy register: %d %s", resp.StatusCode, b)
	}
	resp.Body.Close() //nolint:errcheck
}

// TestRegisterKeepsExistingPublicIP: the panel operator may have typed the
// address in at create time; the script's guess must not overwrite it.
func TestRegisterKeepsExistingPublicIP(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, cmd := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co", "public_ip": "198.51.100.1"})
	token := installToken(t, cmd)
	resp := h.Anonymous().Post("/api/v1/install/"+token+"/register", map[string]string{"hostname": "n1.test", "public_ip": "203.0.113.9"})
	if resp.StatusCode != 200 {
		t.Fatalf("register %d", resp.StatusCode)
	}
	resp.Body.Close() //nolint:errcheck
	var got engineNodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &got)
	if got.PublicIP != "198.51.100.1" {
		t.Fatalf("public_ip = %q, want the operator-supplied one", got.PublicIP)
	}
}

type telemtLimitsJSON struct {
	DataQuotaBytes   int64 `json:"data_quota_bytes"`
	RateLimitUpBps   int64 `json:"rate_limit_up_bps"`
	RateLimitDownBps int64 `json:"rate_limit_down_bps"`
	MaxUniqueIPs     int   `json:"max_unique_ips"`
	MaxTCPConns      int   `json:"max_tcp_conns"`
}

type linkKindJSON struct {
	Kind string `json:"kind"`
	TMe  string `json:"tme"`
	Tg   string `json:"tg"`
}

type keyTelemtResp struct {
	ID           uuid.UUID        `json:"id"`
	Secret       string           `json:"secret"`
	TelemtLimits telemtLimitsJSON `json:"telemt_limits"`
	Links        []struct {
		NodeID   uuid.UUID `json:"node_id"`
		Hostname string    `json:"hostname"`
		Kind     string    `json:"kind"`
		TMe      string    `json:"tme"`
		Tg       string    `json:"tg"`
	} `json:"links"`
}

func TestKeyTelemtLimitsRoundTripAndValidation(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co"})

	limits := map[string]any{"data_quota_bytes": 1 << 30, "rate_limit_up_bps": 1000000, "rate_limit_down_bps": 2000000, "max_unique_ips": 3, "max_tcp_conns": 64}
	var k keyTelemtResp
	resp := c.Post("/api/v1/keys", map[string]any{
		"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https",
		"node_ids": []string{n.ID.String()}, "telemt_limits": limits,
	})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &k)
	if k.TelemtLimits.DataQuotaBytes != 1<<30 || k.TelemtLimits.MaxTCPConns != 64 || k.TelemtLimits.MaxUniqueIPs != 3 {
		t.Fatalf("telemt_limits %+v", k.TelemtLimits)
	}

	// Creating the key already made the node dirty; clear it so the assertion
	// below is about the limits change and nothing else.
	if err := h.Store.Q.SetNodeDirty(t.Context(), db.SetNodeDirtyParams{ID: n.ID, Dirty: false}); err != nil {
		t.Fatal(err)
	}

	var patched keyTelemtResp
	c.JSON(c.Patch("/api/v1/keys/"+k.ID.String(), map[string]any{"telemt_limits": map[string]any{"max_tcp_conns": 8}}), &patched)
	if patched.TelemtLimits.MaxTCPConns != 8 || patched.TelemtLimits.DataQuotaBytes != 0 {
		t.Fatalf("patched limits %+v", patched.TelemtLimits)
	}

	// A patch that does not mention telemt_limits leaves them alone.
	var untouched keyTelemtResp
	c.JSON(c.Patch("/api/v1/keys/"+k.ID.String(), map[string]any{"note": "hi"}), &untouched)
	if untouched.TelemtLimits.MaxTCPConns != 8 {
		t.Fatalf("unrelated patch changed limits: %+v", untouched.TelemtLimits)
	}

	// A GET as a writer carries the stored limits back.
	var got keyTelemtResp
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()), &got)
	if got.TelemtLimits.MaxTCPConns != 8 {
		t.Fatalf("get limits %+v", got.TelemtLimits)
	}

	// Limits reach the node through the desired state, so changing them has to
	// mark every node the key is bound to dirty even though no profile row moves.
	var node engineNodeResp
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()), &node)
	if !node.Dirty {
		t.Fatal("changing telemt limits must mark the bound node dirty")
	}

	for name, bad := range map[string]map[string]any{
		"negative quota": {"data_quota_bytes": -1},
		"negative conns": {"max_tcp_conns": -5},
		"quota too big":  {"data_quota_bytes": 200 * 1024 * 1024 * 1024 * 1024},
	} {
		resp := c.Post("/api/v1/keys", map[string]any{
			"label": "bad", "type": "SHARED", "carrier_mode": "https",
			"node_ids": []string{n.ID.String()}, "telemt_limits": bad,
		})
		if resp.StatusCode != 422 {
			b, _ := io.ReadAll(resp.Body)
			t.Errorf("%s: create status %d, want 422 (%s)", name, resp.StatusCode, b)
		} else {
			resp.Body.Close() //nolint:errcheck
		}
		if resp := c.Patch("/api/v1/keys/"+k.ID.String(), map[string]any{"telemt_limits": bad}); resp.StatusCode != 422 {
			t.Errorf("%s: patch status %d, want 422", name, resp.StatusCode)
		}
	}
}

func TestKeyLinksCarryBothKindsForTelemtNodes(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	tel, _ := createEngineNode(t, c, map[string]any{"name": "tel", "hostname": "tel.test", "acme_email": "a@b.co", "classic_port": 9443})
	old, _ := createEngineNode(t, c, map[string]any{"name": "old", "hostname": "old.test", "acme_email": "a@b.co", "engine": "tproxy"})

	var k keyTelemtResp
	resp := c.Post("/api/v1/keys", map[string]any{
		"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https",
		"node_ids": []string{tel.ID.String(), old.ID.String()},
	})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	c.JSON(resp, &k)

	wantTLSSecret := "ee" + k.Secret + hex.EncodeToString([]byte("tel.test"))

	// The flat links array on the key JSON keeps working and now carries a kind.
	kinds := map[string]map[string]string{}
	for _, l := range k.Links {
		kinds[l.Hostname+"/"+l.Kind] = map[string]string{"tme": l.TMe, "tg": l.Tg}
	}
	if len(k.Links) != 3 {
		t.Fatalf("links %+v, want web+tls for telemt and web for tproxy", k.Links)
	}
	if _, ok := kinds["old.test/tls"]; ok {
		t.Error("tproxy nodes must not advertise a fake-tls link")
	}
	tls, ok := kinds["tel.test/tls"]
	if !ok {
		t.Fatalf("no fake-tls link: %+v", k.Links)
	}
	if tls["tme"] != "https://t.me/proxy?server=tel.test&port=9443&secret="+wantTLSSecret {
		t.Fatalf("tls tme = %s", tls["tme"])
	}
	if !strings.HasPrefix(tls["tg"], "tg://proxy?server=tel.test&port=9443&secret=ee") {
		t.Fatalf("tls tg = %s", tls["tg"])
	}
	web, ok := kinds["tel.test/web"]
	if !ok || web["tme"] != "https://t.me/webproxy?server=tel.test&secret="+k.Secret {
		t.Fatalf("web link %v", web)
	}

	// GET /keys/{id}/links groups by node and reports the engine.
	var links struct {
		Items []struct {
			NodeID   uuid.UUID      `json:"node_id"`
			NodeName string         `json:"node_name"`
			Hostname string         `json:"hostname"`
			Engine   string         `json:"engine"`
			Links    []linkKindJSON `json:"links"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/keys/"+k.ID.String()+"/links"), &links)
	if len(links.Items) != 2 {
		t.Fatalf("grouped links %+v", links.Items)
	}
	for _, it := range links.Items {
		switch it.Hostname {
		case "tel.test":
			if it.Engine != "telemt" || len(it.Links) != 2 || it.Links[0].Kind != "web" || it.Links[1].Kind != "tls" {
				t.Errorf("telemt node links %+v", it)
			}
		case "old.test":
			if it.Engine != "tproxy" || len(it.Links) != 1 || it.Links[0].Kind != "web" {
				t.Errorf("tproxy node links %+v", it)
			}
		default:
			t.Errorf("unexpected node %q", it.Hostname)
		}
	}

	// QR selects the kind; web is the default.
	for _, q := range []string{"", "&kind=web", "&kind=tls"} {
		resp := c.Get("/api/v1/keys/" + k.ID.String() + "/qr?node=" + tel.ID.String() + q)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || !bytes.HasPrefix(body, []byte("\x89PNG")) {
			t.Errorf("qr%q status %d", q, resp.StatusCode)
		}
	}
	if resp := c.Get("/api/v1/keys/" + k.ID.String() + "/qr?node=" + old.ID.String() + "&kind=tls"); resp.StatusCode != 404 {
		t.Errorf("tls qr for a tproxy node: %d, want 404", resp.StatusCode)
	}
	if resp := c.Get("/api/v1/keys/" + k.ID.String() + "/qr?node=" + tel.ID.String() + "&kind=bogus"); resp.StatusCode != 400 {
		t.Errorf("unknown kind: %d, want 400", resp.StatusCode)
	}
}

func TestSubscriptionCarriesBothLinkKinds(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	tel, _ := createEngineNode(t, c, map[string]any{"name": "tel", "hostname": "tel.test", "acme_email": "a@b.co", "classic_port": 9443})
	var k keyTelemtResp
	c.JSON(c.Post("/api/v1/keys", map[string]any{
		"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{tel.ID.String()},
	}), &k)

	var sub struct {
		URL string `json:"url"`
	}
	c.JSON(c.Post("/api/v1/keys/"+k.ID.String()+"/subscription", nil), &sub)
	i := strings.Index(sub.URL, "/s/")
	if i < 0 {
		t.Fatalf("subscription url %q", sub.URL)
	}
	token := sub.URL[i+len("/s/"):]

	anon := h.Anonymous()
	pageResp := anon.Get("/s/" + token)
	body, _ := io.ReadAll(pageResp.Body)
	page := string(body)
	if pageResp.StatusCode != 200 {
		t.Fatalf("page %d", pageResp.StatusCode)
	}
	for _, want := range []string{"t.me/webproxy?server=tel.test", "t.me/proxy?server=tel.test&amp;port=9443"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}

	var out struct {
		Locations []struct {
			Hostname string         `json:"hostname"`
			TMe      string         `json:"tme"`
			Links    []linkKindJSON `json:"links"`
		} `json:"locations"`
	}
	anon.JSON(anon.Get("/s/"+token+".json"), &out)
	if len(out.Locations) != 1 {
		t.Fatalf("locations %+v", out.Locations)
	}
	loc := out.Locations[0]
	if loc.TMe == "" || !strings.Contains(loc.TMe, "webproxy") {
		t.Errorf("legacy tme field = %q", loc.TMe)
	}
	if len(loc.Links) != 2 || loc.Links[0].Kind != "web" || loc.Links[1].Kind != "tls" {
		t.Fatalf("location links %+v", loc.Links)
	}
	if !strings.Contains(loc.Links[1].TMe, "port=9443&secret=ee") {
		t.Errorf("tls link %q", loc.Links[1].TMe)
	}
}

// TestNodeProfilesCarryKeyLabelAndLimits: the node's Profiles tab prints a profile next to the
// key that produced it, so the listing carries the key's label, expiry and telemt limits. The
// node's own default profile has no key and must come back with those fields empty rather than
// with somebody else's values.
func TestNodeProfilesCarryKeyLabelAndLimits(t *testing.T) {
	h := apitest.New(t)
	h.CreateAdmin("root", "pass-123456", "owner")
	c := h.Login("root", "pass-123456")
	n, _ := createEngineNode(t, c, map[string]any{"name": "n1", "hostname": "n1.test", "acme_email": "a@b.co"})

	expires := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	var k keyTelemtResp
	resp := c.Post("/api/v1/keys", map[string]any{
		"label": "Ivan", "type": "PERSONAL", "carrier_mode": "https", "node_ids": []string{n.ID.String()},
		"expires_at":    expires.Format(time.RFC3339),
		"telemt_limits": map[string]any{"data_quota_bytes": 1 << 30, "rate_limit_down_bps": 2000000, "max_tcp_conns": 64},
	})
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create key %d: %s", resp.StatusCode, b)
	}
	c.JSON(resp, &k)

	var list struct {
		Items []struct {
			Name         string     `json:"name"`
			AccessKeyID  *uuid.UUID `json:"access_key_id"`
			KeyLabel     string     `json:"key_label"`
			KeyExpiresAt *time.Time `json:"key_expires_at"`
			TelemtLimits struct {
				DataQuotaBytes   int64 `json:"data_quota_bytes"`
				RateLimitDownBps int64 `json:"rate_limit_down_bps"`
				MaxTCPConns      int   `json:"max_tcp_conns"`
			} `json:"telemt_limits"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+n.ID.String()+"/profiles"), &list)
	if len(list.Items) != 2 {
		t.Fatalf("profiles = %d, want the default plus the key's", len(list.Items))
	}

	bound, def := -1, -1
	for i, p := range list.Items {
		if p.AccessKeyID != nil && *p.AccessKeyID == k.ID {
			bound = i
		} else if p.Name == "default" {
			def = i
		}
	}
	if bound < 0 || def < 0 {
		t.Fatalf("profiles %+v", list.Items)
	}

	got := list.Items[bound]
	if got.KeyLabel != "Ivan" {
		t.Errorf("key_label = %q, want Ivan", got.KeyLabel)
	}
	if got.KeyExpiresAt == nil || !got.KeyExpiresAt.UTC().Equal(expires) {
		t.Errorf("key_expires_at = %v, want %v", got.KeyExpiresAt, expires)
	}
	if got.TelemtLimits.DataQuotaBytes != 1<<30 || got.TelemtLimits.RateLimitDownBps != 2000000 || got.TelemtLimits.MaxTCPConns != 64 {
		t.Errorf("telemt_limits %+v", got.TelemtLimits)
	}

	// The default profile belongs to the node, not to a key: no label, no expiry, no limits.
	if d := list.Items[def]; d.KeyLabel != "" || d.KeyExpiresAt != nil || d.TelemtLimits.DataQuotaBytes != 0 {
		t.Errorf("default profile carries key fields: %+v", d)
	}
}
