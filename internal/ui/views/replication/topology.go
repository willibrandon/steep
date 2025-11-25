package replication

import (
	"github.com/willibrandon/steep/internal/ui/views/replication/repviz"
)

// renderTopology renders the topology view with expandable WAL pipelines per node.
// T036: Implement topology tree rendering using xlab/treeprint showing primary at root
// T037: Add support for cascading replication (replica-to-replica chains) in tree structure
// T038: Display sync state indicators (sync, async, potential, quorum) on each node
// T039: Show lag bytes next to each replica node in topology
func (v *ReplicationView) renderTopology() string {
	config := repviz.TopologyConfig{
		Width:       v.width,
		Height:      v.height,
		IsPrimary:   v.data.IsPrimary,
		SelectedIdx: v.topologySelectedIdx,
		Expanded:    v.topologyExpanded,
	}
	return repviz.RenderTopology(v.data, config)
}

// toggleTopologyExpansion toggles the pipeline expansion for the currently selected replica.
func (v *ReplicationView) toggleTopologyExpansion() {
	if len(v.data.Replicas) == 0 || v.topologySelectedIdx >= len(v.data.Replicas) {
		return
	}
	appName := v.data.Replicas[v.topologySelectedIdx].ApplicationName
	v.topologyExpanded[appName] = !v.topologyExpanded[appName]
}

// topologyNavigateUp moves selection up in the topology view.
func (v *ReplicationView) topologyNavigateUp() {
	if v.topologySelectedIdx > 0 {
		v.topologySelectedIdx--
	}
}

// topologyNavigateDown moves selection down in the topology view.
func (v *ReplicationView) topologyNavigateDown() {
	if v.topologySelectedIdx < len(v.data.Replicas)-1 {
		v.topologySelectedIdx++
	}
}
