package handlers

import (
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// Every gateway read goes through enrichGatewayWithHealth. It rebuilt the
// gateway field by field and left the box assignment behind.
func TestAGatewayReadKeepsItsBox(t *testing.T) {
	h := &GatewaysHandler{}
	box := 7
	got := h.enrichGatewayWithHealth(models.Gateway{ID: 1, EdgeAgentID: &box})
	if got.EdgeAgentID == nil || *got.EdgeAgentID != box {
		t.Fatalf("edge_agent_id = %v, want %d", got.EdgeAgentID, box)
	}
	if got := h.enrichGatewayWithHealth(models.Gateway{ID: 2}); got.EdgeAgentID != nil {
		t.Fatalf("a server gateway read back edge_agent_id = %d", *got.EdgeAgentID)
	}
}
