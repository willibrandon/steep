package models

// PanelStatus represents the visual state of a dashboard panel.
type PanelStatus int

const (
	PanelNormal PanelStatus = iota
	PanelWarning
	PanelCritical
)

// DashboardPanel represents a single metric display panel in the UI.
type DashboardPanel struct {
	Label  string      `json:"label"`
	Value  string      `json:"value"`
	Unit   string      `json:"unit"`
	Status PanelStatus `json:"status"`
}

// NewDashboardPanel creates a panel with normal status.
func NewDashboardPanel(label, value, unit string) DashboardPanel {
	return DashboardPanel{
		Label:  label,
		Value:  value,
		Unit:   unit,
		Status: PanelNormal,
	}
}

// SetWarning sets the panel status to warning.
func (p *DashboardPanel) SetWarning() {
	p.Status = PanelWarning
}

// SetCritical sets the panel status to critical.
func (p *DashboardPanel) SetCritical() {
	p.Status = PanelCritical
}

// SetNormal sets the panel status to normal.
func (p *DashboardPanel) SetNormal() {
	p.Status = PanelNormal
}

// IsWarning returns true if panel is in warning state.
func (p *DashboardPanel) IsWarning() bool {
	return p.Status == PanelWarning
}

// IsCritical returns true if panel is in critical state.
func (p *DashboardPanel) IsCritical() bool {
	return p.Status == PanelCritical
}
