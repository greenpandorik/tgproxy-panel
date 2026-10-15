package nodesvc

import (
	"testing"

	"tgwebproxy/internal/store/db"
)

func TestLargeWebCapacityNeedsANewerAgent(t *testing.T) {
	n := db.Node{Engine: db.NodeEngineTelemt, AgentVersion: "2.17.4", TelemtVersion: "3.5.14"}
	if msg := WebCapacityPresetUnsupported(n, 1024); msg != "" {
		t.Fatalf("1024 on agent 2.17.4: %q", msg)
	}
	if msg := WebCapacityPresetUnsupported(n, 2048); msg == "" {
		t.Fatal("2048 must need agent 2.17.5")
	}
	n.AgentVersion = "2.17.5"
	if msg := WebCapacityPresetUnsupported(n, 2048); msg != "" {
		t.Fatalf("2048 on agent 2.17.5: %q", msg)
	}
}
