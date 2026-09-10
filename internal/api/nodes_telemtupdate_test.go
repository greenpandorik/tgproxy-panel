package api_test

import (
	"testing"

	"tgwebproxy/internal/nodedriver"
	"tgwebproxy/internal/store/db"
)

func TestTelemtUpdateListRecoversJobAfterPanelRestart(t *testing.T) {
	h, c, node := ownerWithNode(t)
	h.Mock.SetOnline(node.ID, true)
	if err := h.Store.Q.SetNodeHeartbeat(t.Context(), db.SetNodeHeartbeatParams{
		ID: node.ID, Status: db.NodeStatusOnline, TelemtVersion: "3.5.6",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Store.Q.CreateTelemtUpdateJob(t.Context(), db.CreateTelemtUpdateJobParams{
		NodeID: node.ID, FromVersion: "3.5.6", ToVersion: "3.5.7",
	}); err != nil {
		t.Fatal(err)
	}
	h.Mock.ScriptTelemtUpdate(node.ID, nodedriver.TelemtUpdate{
		Phase: "done", Outcome: "rolled_back", FromVersion: "3.5.6", ToVersion: "3.5.7", RolledBack: true,
	})
	var result struct {
		Running any `json:"running"`
		Items   []struct {
			Status string `json:"status"`
		} `json:"items"`
	}
	c.JSON(c.Get("/api/v1/nodes/"+node.ID.String()+"/telemt-update"), &result)
	if result.Running != nil || len(result.Items) != 1 || result.Items[0].Status != "rolled_back" {
		t.Fatalf("recovered update: %+v", result)
	}
}
