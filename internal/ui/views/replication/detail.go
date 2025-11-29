package replication

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views/replication/repviz"
)

// renderDetail renders the detail view with improved styling.
func (v *ReplicationView) renderDetail() string {
	// Title style - MarginTop adds space before title, MarginBottom adds space after
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginTop(1)

	// Get title from first line if available
	title := "Details"
	startIdx := 0
	if len(v.detailLines) > 0 {
		title = v.detailLines[0]
		startIdx = 1 // Skip title in content
	}

	// Content
	var content string
	if len(v.detailLines) <= 1 {
		content = styles.InfoStyle.Render("No details available")
	} else {
		maxLines := v.height - 7 // Reserve space for title, footer, and borders
		contentLines := v.detailLines[startIdx:]

		endIdx := min(v.detailScrollOffset+maxLines, len(contentLines))
		startContent := v.detailScrollOffset
		if startContent > len(contentLines) {
			startContent = 0
		}
		visibleLines := contentLines[startContent:endIdx]

		// Pad to fill height
		for len(visibleLines) < maxLines {
			visibleLines = append(visibleLines, "")
		}
		content = strings.Join(visibleLines, "\n")
	}

	// Footer with scroll info
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginTop(1)

	scrollInfo := ""
	contentLen := len(v.detailLines) - 1 // Exclude title
	maxLines := v.height - 7
	if contentLen > maxLines {
		scrollInfo = fmt.Sprintf(" (%d/%d)", v.detailScrollOffset+1, contentLen)
	}

	footer := footerStyle.Render("[j/k]scroll [g/G]top/bottom [esc/q]back" + scrollInfo)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(title),
		content,
		footer,
	)
}

// renderDropSlotConfirm renders the drop slot confirmation dialog.
func (v *ReplicationView) renderDropSlotConfirm() string {
	content := fmt.Sprintf("Drop replication slot '%s'?\n\n"+
		"This may cause data loss if a replica is still using this slot.\n\n"+
		"Press Y to confirm, N or Esc to cancel",
		v.dropSlotName)

	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}

// renderWizardExecConfirm renders the wizard command execution confirmation dialog.
func (v *ReplicationView) renderWizardExecConfirm() string {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	codeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)

	content := fmt.Sprintf("%s\n\n%s\n\n"+
		"Execute this command on the connected database?\n\n"+
		"Press Y to execute, N or Esc to cancel",
		labelStyle.Render(v.wizardExecLabel),
		codeStyle.Render(v.wizardExecCommand))

	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Width(min(80, v.width-4)).
		Render(content)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		dialog,
	)
}

func (v *ReplicationView) prepareReplicaDetail() {
	if v.selectedIdx >= len(v.data.Replicas) {
		return
	}
	r := v.data.Replicas[v.selectedIdx]

	// Styles for detail view
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	lsnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // Light blue for LSN

	// Color-coded state
	stateStyle := valueStyle
	switch r.State {
	case "streaming":
		stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	case "startup", "catchup":
		stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	case "backup":
		stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")) // Blue
	}

	// Color-coded sync state
	syncStyle := valueStyle
	switch r.SyncState {
	case models.SyncStateSync:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	case models.SyncStateAsync:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252")) // Default
	case models.SyncStatePotential:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	case models.SyncStateQuorum:
		syncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")) // Blue
	}

	// Color-coded lag values
	byteLagStyle := valueStyle
	if r.ByteLag > 100*1024*1024 { // > 100MB
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	} else if r.ByteLag > 10*1024*1024 { // > 10MB
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	} else if r.ByteLag > 0 {
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	} else {
		byteLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for 0
	}

	timeLagStyle := valueStyle
	if r.ReplayLag > 5*time.Second {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	} else if r.ReplayLag > time.Second {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	} else if r.ReplayLag > 0 {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	} else {
		timeLagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for 0
	}

	// Build detail lines
	lines := []string{
		"Replica Details: " + r.ApplicationName,
		labelStyle.Render("Client Address:  ") + valueStyle.Render(r.ClientAddr),
		labelStyle.Render("State:           ") + stateStyle.Render(r.State),
		labelStyle.Render("Sync State:      ") + syncStyle.Render(r.SyncState.String()),
		"",
	}

	// Add WAL Pipeline visualization
	pipelineLines := strings.Split(repviz.RenderWALPipeline(&r, v.width-4), "\n")
	lines = append(lines, pipelineLines...)
	lines = append(lines, "")

	// Add detailed WAL positions
	lines = append(lines,
		sectionStyle.Render("WAL Positions"),
		labelStyle.Render("  Sent LSN:      ")+lsnStyle.Render(r.SentLSN),
		labelStyle.Render("  Write LSN:     ")+lsnStyle.Render(r.WriteLSN),
		labelStyle.Render("  Flush LSN:     ")+lsnStyle.Render(r.FlushLSN),
		labelStyle.Render("  Replay LSN:    ")+lsnStyle.Render(r.ReplayLSN),
		"",
		sectionStyle.Render("Lag"),
		labelStyle.Render("  Byte Lag:      ")+byteLagStyle.Render(r.FormatByteLag()),
		labelStyle.Render("  Write Lag:     ")+timeLagStyle.Render(formatDuration(r.WriteLag)),
		labelStyle.Render("  Flush Lag:     ")+timeLagStyle.Render(formatDuration(r.FlushLag)),
		labelStyle.Render("  Replay Lag:    ")+timeLagStyle.Render(formatDuration(r.ReplayLag)),
		"",
		labelStyle.Render("Backend Start:   ")+valueStyle.Render(r.BackendStart.Format("2006-01-02 15:04:05")),
	)

	v.detailLines = lines
	v.detailScrollOffset = 0
}

func (v *ReplicationView) prepareSlotDetail() {
	if v.slotSelectedIdx >= len(v.data.Slots) {
		return
	}
	s := v.data.Slots[v.slotSelectedIdx]

	// Styles for detail view
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	lsnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // Light blue for LSN

	// Color-coded active status
	activeStr := "No"
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow for inactive
	if s.Active {
		activeStr = "Yes"
		activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for active
	}

	// Color-coded slot type
	typeStyle := valueStyle
	if s.SlotType == models.SlotTypeLogical {
		typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("183")) // Purple for logical
	} else {
		typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // Blue for physical
	}

	// Color-coded WAL retention
	retainedStyle := valueStyle
	if !s.Active && s.RetainedBytes > 800*1024*1024 { // > 800MB and inactive
		retainedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	} else if !s.Active && s.RetainedBytes > 100*1024*1024 { // > 100MB and inactive
		retainedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	} else if s.RetainedBytes > 0 {
		retainedStyle = valueStyle
	} else {
		retainedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for 0
	}

	// Color-coded WAL status
	walStatusStyle := valueStyle
	switch s.WALStatus {
	case "reserved":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	case "extended":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	case "unreserved":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	case "lost":
		walStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // Bold red
	}

	// Active PID display
	activePIDStr := "-"
	if s.ActivePID > 0 {
		activePIDStr = fmt.Sprintf("%d", s.ActivePID)
	}

	// Check for orphaned slot
	isOrphaned := s.IsOrphaned(24 * time.Hour)
	orphanedWarning := ""
	if isOrphaned {
		orphanedWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(" (inactive >24h)")
	}

	v.detailLines = []string{
		"Slot Details: " + s.SlotName,
		labelStyle.Render("Type:            ") + typeStyle.Render(s.SlotType.String()),
		labelStyle.Render("Database:        ") + valueStyle.Render(s.Database),
		labelStyle.Render("Active:          ") + activeStyle.Render(activeStr) + orphanedWarning,
		labelStyle.Render("Active PID:      ") + valueStyle.Render(activePIDStr),
		"",
		sectionStyle.Render("WAL Retention"),
		labelStyle.Render("  Restart LSN:   ") + lsnStyle.Render(s.RestartLSN),
		labelStyle.Render("  Retained:      ") + retainedStyle.Render(s.FormatRetainedBytes()),
		labelStyle.Render("  WAL Status:    ") + walStatusStyle.Render(s.WALStatus),
	}
	v.detailScrollOffset = 0
}

func (v *ReplicationView) preparePubDetail() {
	if v.pubSelectedIdx >= len(v.data.Publications) {
		return
	}
	p := v.data.Publications[v.pubSelectedIdx]

	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(18)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	tableStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	// All tables styling
	allTablesStr := "No"
	allTablesStyle := valueStyle
	if p.AllTables {
		allTablesStr = "Yes"
		allTablesStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}

	// Operations styling
	opsStyle := valueStyle
	allOps := p.Insert && p.Update && p.Delete && p.Truncate
	if allOps {
		opsStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	} else if p.Insert || p.Update || p.Delete {
		opsStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	}

	// Subscriber count styling
	subStyle := valueStyle
	if p.SubscriberCount > 0 {
		subStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	} else {
		subStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	}

	v.detailLines = []string{
		titleStyle.Render("Publication Details: " + p.Name),
		labelStyle.Render("All Tables:") + allTablesStyle.Render(allTablesStr),
		labelStyle.Render("Table Count:") + valueStyle.Render(fmt.Sprintf("%d", p.TableCount)),
		labelStyle.Render("Subscribers:") + subStyle.Render(fmt.Sprintf("%d", p.SubscriberCount)),
		"",
		headerStyle.Render("Operations"),
		labelStyle.Render("INSERT:") + v.formatBoolValue(p.Insert),
		labelStyle.Render("UPDATE:") + v.formatBoolValue(p.Update),
		labelStyle.Render("DELETE:") + v.formatBoolValue(p.Delete),
		labelStyle.Render("TRUNCATE:") + v.formatBoolValue(p.Truncate),
		labelStyle.Render("Combined:") + opsStyle.Render(p.OperationFlags()),
	}

	if len(p.Tables) > 0 {
		v.detailLines = append(v.detailLines, "", headerStyle.Render("Published Tables"))
		for _, t := range p.Tables {
			v.detailLines = append(v.detailLines, "  "+tableStyle.Render(t))
		}
	}

	v.detailScrollOffset = 0
}

func (v *ReplicationView) prepareSubDetail() {
	if v.subSelectedIdx >= len(v.data.Subscriptions) {
		return
	}
	s := v.data.Subscriptions[v.subSelectedIdx]

	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(18)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	pubStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	// Enabled styling
	enabledStr := "No"
	enabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	if s.Enabled {
		enabledStr = "Yes"
		enabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}

	// Lag styling
	lagStr := s.FormatByteLag()
	lagStyle := valueStyle
	if s.ByteLag > 10*1024*1024 {
		lagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	} else if s.ByteLag > 1024*1024 {
		lagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	} else if s.ByteLag > 0 {
		lagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}

	v.detailLines = []string{
		titleStyle.Render("Subscription Details: " + s.Name),
		labelStyle.Render("Enabled:") + enabledStyle.Render(enabledStr),
		labelStyle.Render("Byte Lag:") + lagStyle.Render(lagStr),
		"",
		headerStyle.Render("Connection"),
		labelStyle.Render("Connection Info:") + valueStyle.Render(truncateWithEllipsis(s.ConnInfo, 60)),
		"",
		headerStyle.Render("LSN Positions"),
		labelStyle.Render("Received LSN:") + valueStyle.Render(s.ReceivedLSN),
		labelStyle.Render("Latest End LSN:") + valueStyle.Render(s.LatestEndLSN),
	}

	// Timing info if available
	if !s.LastMsgSendTime.IsZero() {
		v.detailLines = append(v.detailLines, "", headerStyle.Render("Timing"))
		v.detailLines = append(v.detailLines, labelStyle.Render("Last Msg Sent:")+valueStyle.Render(s.LastMsgSendTime.Format("2006-01-02 15:04:05")))
		v.detailLines = append(v.detailLines, labelStyle.Render("Last Msg Recv:")+valueStyle.Render(s.LastMsgReceiptTime.Format("2006-01-02 15:04:05")))
	}

	if len(s.Publications) > 0 {
		v.detailLines = append(v.detailLines, "", headerStyle.Render("Subscribed Publications"))
		for _, p := range s.Publications {
			v.detailLines = append(v.detailLines, "  "+pubStyle.Render(p))
		}
	}

	v.detailScrollOffset = 0
}

// formatBoolValue returns a styled Yes/No string
func (v *ReplicationView) formatBoolValue(val bool) string {
	if val {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("Yes")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("No")
}
