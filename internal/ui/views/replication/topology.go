package replication

import (
	"github.com/willibrandon/steep/internal/ui/views/replication/repviz"
)

// renderTopology renders the topology view using xlab/treeprint.
// T036: Implement topology tree rendering using xlab/treeprint showing primary at root
// T037: Add support for cascading replication (replica-to-replica chains) in tree structure
// T038: Display sync state indicators (sync, async, potential, quorum) on each node
// T039: Show lag bytes next to each replica node in topology
func (v *ReplicationView) renderTopology() string {
	config := repviz.TopologyConfig{
		Width:     v.width,
		Height:    v.height,
		IsPrimary: v.data.IsPrimary,
	}
	return repviz.RenderTopology(v.data, config)
}
