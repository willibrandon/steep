// Package setup provides setup wizard and configuration check components.
package setup

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// LogicalWizardStep represents the current step in the logical wizard.
type LogicalWizardStep int

const (
	LogicalStepType LogicalWizardStep = iota // Publication or Subscription
	LogicalStepTableSelection                // Select tables (publication only)
	LogicalStepOperations                    // Select DML operations (publication only)
	LogicalStepConnection                    // Connection info (subscription only)
	LogicalStepReview                        // Review generated SQL
)

// LogicalWizardMode indicates whether creating publication or subscription.
type LogicalWizardMode string

const (
	LogicalModePublication  LogicalWizardMode = "publication"
	LogicalModeSubscription LogicalWizardMode = "subscription"
)

// LogicalWizardConfig holds the wizard configuration values.
type LogicalWizardConfig struct {
	// Mode: publication or subscription
	Mode LogicalWizardMode

	// Publication config
	PublicationName  string
	AllTables        bool
	SelectedTables   map[string]bool // schema.table -> selected
	OpInsert         bool
	OpUpdate         bool
	OpDelete         bool
	OpTruncate       bool
	ReplicationUser  string // User for subscribers to connect
	ReplicationPass  string
	AutoGenPass      bool
	PasswordShown    bool

	// Subscription config
	SubscriptionName string
	PublicationNames []string // Publications to subscribe to
	ConnectionHost   string
	ConnectionPort   string
	ConnectionDB     string
	ConnectionUser   string
	ConnectionPass   string
	CopyData         bool // WITH (copy_data = true/false)
	Enabled          bool // WITH (enabled = true/false)
}

// TableInfo holds table information for selection display.
type TableInfo struct {
	Schema   string
	Name     string
	Size     int64
	RowCount int64
}

// LogicalWizardState holds the wizard UI state.
type LogicalWizardState struct {
	Step          LogicalWizardStep
	Config        LogicalWizardConfig
	Tables        []TableInfo // Available tables
	Error         string
	SelectedField int
	EditingField  int
	InputBuffer   string
	TableCursor   int  // For table selection navigation
	TableOffset   int  // Scroll offset for table list
	CreatingUser  bool // True while user creation is in progress
}

// NewLogicalWizardState creates a new logical wizard state with defaults.
func NewLogicalWizardState(tables []models.Table) *LogicalWizardState {
	// Convert models.Table to TableInfo
	tableInfos := make([]TableInfo, 0, len(tables))
	selectedTables := make(map[string]bool)

	for _, t := range tables {
		// Skip system schemas
		if t.SchemaName == "pg_catalog" || t.SchemaName == "information_schema" {
			continue
		}
		fullName := t.SchemaName + "." + t.Name
		tableInfos = append(tableInfos, TableInfo{
			Schema:   t.SchemaName,
			Name:     t.Name,
			Size:     t.TotalSize,
			RowCount: t.RowCount,
		})
		selectedTables[fullName] = false
	}

	// Generate default password for replication user
	defaultPassword, _ := GenerateReplicationPassword()

	return &LogicalWizardState{
		Step:   LogicalStepType,
		Tables: tableInfos,
		Config: LogicalWizardConfig{
			Mode:             LogicalModePublication,
			PublicationName:  "my_publication",
			AllTables:        false,
			SelectedTables:   selectedTables,
			OpInsert:         true,
			OpUpdate:         true,
			OpDelete:         true,
			OpTruncate:       false,
			ReplicationUser:  "replicator",
			ReplicationPass:  defaultPassword,
			AutoGenPass:      true,
			PasswordShown:    false,
			SubscriptionName: "my_subscription",
			PublicationNames: []string{"my_publication"},
			ConnectionHost:   "primary-host",
			ConnectionPort:   "5432",
			ConnectionDB:     "mydb",
			ConnectionUser:   "replicator",
			ConnectionPass:   "",
			CopyData:         true,
			Enabled:          true,
		},
		SelectedField: 0,
		EditingField:  -1,
		TableCursor:   0,
		TableOffset:   0,
	}
}

// LogicalWizardRenderConfig holds rendering configuration.
type LogicalWizardRenderConfig struct {
	Width    int
	Height   int
	ReadOnly bool
}

// RenderLogicalWizard renders the logical replication setup wizard.
func RenderLogicalWizard(state *LogicalWizardState, cfg LogicalWizardRenderConfig) string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Logical Replication Setup Wizard"))
	b.WriteString("\n\n")

	// Step indicator
	b.WriteString(renderLogicalStepIndicator(state))
	b.WriteString("\n\n")

	// Step content
	switch state.Step {
	case LogicalStepType:
		b.WriteString(renderLogicalTypeStep(state))
	case LogicalStepTableSelection:
		b.WriteString(renderLogicalTableSelectionStep(state, cfg.Height-10))
	case LogicalStepOperations:
		b.WriteString(renderLogicalOperationsStep(state))
	case LogicalStepConnection:
		b.WriteString(renderLogicalConnectionStep(state))
	case LogicalStepReview:
		b.WriteString(renderLogicalReviewStep(state))
	}

	// Error message
	if state.Error != "" {
		b.WriteString("\n")
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		b.WriteString(errorStyle.Render("Error: " + state.Error))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(renderLogicalWizardFooter(state))

	return b.String()
}

// renderLogicalStepIndicator renders the wizard step progress bar.
func renderLogicalStepIndicator(state *LogicalWizardState) string {
	var steps []string
	if state.Config.Mode == LogicalModePublication {
		steps = []string{"1. Type", "2. Tables", "3. Operations", "4. Review"}
	} else {
		steps = []string{"1. Type", "2. Connection", "3. Review"}
	}

	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

	// Map step to display index
	displayStep := 0
	if state.Config.Mode == LogicalModePublication {
		displayStep = int(state.Step)
	} else {
		// For subscription: Type(0) -> Connection(1) -> Review(2)
		switch state.Step {
		case LogicalStepType:
			displayStep = 0
		case LogicalStepConnection:
			displayStep = 1
		case LogicalStepReview:
			displayStep = 2
		}
	}

	var parts []string
	for i, s := range steps {
		if i < displayStep {
			parts = append(parts, doneStyle.Render("✓ "+s))
		} else if i == displayStep {
			parts = append(parts, activeStyle.Render("→ "+s))
		} else {
			parts = append(parts, dimStyle.Render("  "+s))
		}
	}
	return strings.Join(parts, "  ")
}

// renderLogicalTypeStep renders Step 1: Publication or Subscription selection.
func renderLogicalTypeStep(state *LogicalWizardState) string {
	var b strings.Builder

	labelStyle := lipgloss.NewStyle().Width(20)
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("249"))

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("What do you want to create?"))
	b.WriteString("\n\n")

	// Publication option
	pubLabel := labelStyle.Render("Publication")
	pubDesc := "Publish tables from this database"
	if state.SelectedField == 0 {
		if state.Config.Mode == LogicalModePublication {
			b.WriteString(selectedStyle.Render("● " + pubLabel))
		} else {
			b.WriteString(selectedStyle.Render("○ " + pubLabel))
		}
	} else {
		if state.Config.Mode == LogicalModePublication {
			b.WriteString(valueStyle.Render("● " + pubLabel))
		} else {
			b.WriteString(valueStyle.Render("○ " + pubLabel))
		}
	}
	b.WriteString("  " + descStyle.Render(pubDesc))
	b.WriteString("\n")

	// Subscription option
	subLabel := labelStyle.Render("Subscription")
	subDesc := "Subscribe to a remote publication"
	if state.SelectedField == 1 {
		if state.Config.Mode == LogicalModeSubscription {
			b.WriteString(selectedStyle.Render("● " + subLabel))
		} else {
			b.WriteString(selectedStyle.Render("○ " + subLabel))
		}
	} else {
		if state.Config.Mode == LogicalModeSubscription {
			b.WriteString(valueStyle.Render("● " + subLabel))
		} else {
			b.WriteString(valueStyle.Render("○ " + subLabel))
		}
	}
	b.WriteString("  " + descStyle.Render(subDesc))
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("[Space/Enter] select"))

	return b.String()
}

// renderLogicalTableSelectionStep renders table selection for publication.
func renderLogicalTableSelectionStep(state *LogicalWizardState, maxHeight int) string {
	var b strings.Builder

	labelStyle := lipgloss.NewStyle().Width(20)
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	checkedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Select Tables to Publish"))
	b.WriteString("\n\n")

	// Publication name field
	nameLabel := labelStyle.Render("Publication Name:")
	if state.SelectedField == 0 {
		if state.EditingField == 0 {
			b.WriteString(selectedStyle.Render(nameLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(nameLabel + " " + state.Config.PublicationName))
		}
	} else {
		b.WriteString(nameLabel + " " + valueStyle.Render(state.Config.PublicationName))
	}
	b.WriteString("\n\n")

	// All tables toggle
	allTablesLabel := labelStyle.Render("Publish All Tables:")
	allTablesVal := "No"
	if state.Config.AllTables {
		allTablesVal = "Yes"
	}
	if state.SelectedField == 1 {
		b.WriteString(selectedStyle.Render(allTablesLabel + " " + allTablesVal))
	} else {
		b.WriteString(allTablesLabel + " " + valueStyle.Render(allTablesVal))
	}
	b.WriteString("\n\n")

	// Table list (if not all tables)
	if !state.Config.AllTables {
		b.WriteString("Tables:\n")

		// Calculate visible rows
		visibleRows := maxHeight - 2
		if visibleRows < 5 {
			visibleRows = 5
		}
		if visibleRows > len(state.Tables) {
			visibleRows = len(state.Tables)
		}

		// Adjust scroll offset
		if state.TableCursor < state.TableOffset {
			state.TableOffset = state.TableCursor
		}
		if state.TableCursor >= state.TableOffset+visibleRows {
			state.TableOffset = state.TableCursor - visibleRows + 1
		}

		// Header
		headerFmt := "  %-30s %10s %12s"
		b.WriteString(hintStyle.Render(fmt.Sprintf(headerFmt, "Table", "Size", "Rows")))
		b.WriteString("\n")

		// Table rows
		for i := state.TableOffset; i < state.TableOffset+visibleRows && i < len(state.Tables); i++ {
			t := state.Tables[i]
			fullName := t.Schema + "." + t.Name
			checked := state.Config.SelectedTables[fullName]

			checkbox := "[ ]"
			if checked {
				checkbox = checkedStyle.Render("[✓]")
			}

			sizeStr := formatSize(t.Size)
			rowStr := formatCount(t.RowCount)

			// Warning for large tables (>1GB)
			warning := ""
			if t.Size > 1024*1024*1024 {
				warning = warnStyle.Render(" ⚠")
			}

			rowText := fmt.Sprintf("%s %-27s %10s %12s%s", checkbox, truncateString(fullName, 27), sizeStr, rowStr, warning)

			if state.SelectedField == 2 && i == state.TableCursor {
				b.WriteString(selectedStyle.Render(rowText))
			} else {
				b.WriteString(rowText)
			}
			b.WriteString("\n")
		}

		// Show large table warning if any selected
		hasLargeTable := false
		for fullName, selected := range state.Config.SelectedTables {
			if selected {
				for _, t := range state.Tables {
					if t.Schema+"."+t.Name == fullName && t.Size > 1024*1024*1024 {
						hasLargeTable = true
						break
					}
				}
			}
		}
		if hasLargeTable {
			b.WriteString("\n")
			b.WriteString(warnStyle.Render("⚠ Large tables (>1GB) may take significant time for initial sync"))
		}
	}

	// Replication User section
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Replication User (for subscribers)"))
	b.WriteString("\n\n")

	// Username field
	userLabel := labelStyle.Render("Username:")
	if state.SelectedField == 3 {
		if state.EditingField == 3 {
			b.WriteString(selectedStyle.Render(userLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(userLabel + " " + state.Config.ReplicationUser))
		}
	} else {
		b.WriteString(userLabel + " " + valueStyle.Render(state.Config.ReplicationUser))
	}
	b.WriteString("\n")

	// Password mode field
	passMode := "Auto-generated"
	if !state.Config.AutoGenPass {
		passMode = "Manual"
	}
	passModeLabel := labelStyle.Render("Password Mode:")
	if state.SelectedField == 4 {
		b.WriteString(selectedStyle.Render(passModeLabel + " " + passMode))
	} else {
		b.WriteString(passModeLabel + " " + valueStyle.Render(passMode))
	}
	b.WriteString("\n")

	// Password field
	passDisplay := "••••••••••••••••••••••••"
	if state.Config.PasswordShown {
		passDisplay = state.Config.ReplicationPass
	}
	passLabel := labelStyle.Render("Password:")
	if state.SelectedField == 5 {
		if state.EditingField == 5 {
			b.WriteString(selectedStyle.Render(passLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(passLabel + " " + passDisplay))
		}
	} else {
		b.WriteString(passLabel + " " + valueStyle.Render(passDisplay))
	}
	b.WriteString("\n")

	// Creating user status
	if state.CreatingUser {
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("Creating user..."))
	}

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("[Space] toggle  [Enter] edit  [v] show/hide pw  [r] regen pw  [y] copy pw  [c] create user"))

	return b.String()
}

// renderLogicalOperationsStep renders DML operation selection.
func renderLogicalOperationsStep(state *LogicalWizardState) string {
	var b strings.Builder

	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	hintSelectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250")) // Brighter for selected rows
	checkedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Select Operations to Publish"))
	b.WriteString("\n\n")

	ops := []struct {
		name    string
		enabled *bool
		desc    string
	}{
		{"INSERT", &state.Config.OpInsert, "Publish INSERT operations"},
		{"UPDATE", &state.Config.OpUpdate, "Publish UPDATE operations"},
		{"DELETE", &state.Config.OpDelete, "Publish DELETE operations"},
		{"TRUNCATE", &state.Config.OpTruncate, "Publish TRUNCATE operations (PG11+)"},
	}

	for i, op := range ops {
		checkbox := "[ ]"
		if *op.enabled {
			checkbox = checkedStyle.Render("[✓]")
		}

		if state.SelectedField == i {
			// Selected row: use brighter hint color for contrast
			label := fmt.Sprintf("%s %-10s %s", checkbox, op.name, hintSelectedStyle.Render(op.desc))
			b.WriteString(selectedStyle.Render(label))
		} else {
			// Non-selected row
			label := fmt.Sprintf("%s %-10s %s", checkbox, op.name, hintStyle.Render(op.desc))
			b.WriteString(label)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("[Space] toggle"))

	return b.String()
}

// renderLogicalConnectionStep renders connection configuration for subscription.
func renderLogicalConnectionStep(state *LogicalWizardState) string {
	var b strings.Builder

	labelStyle := lipgloss.NewStyle().Width(20)
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	checkedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Subscription Configuration"))
	b.WriteString("\n\n")

	fields := []struct {
		label string
		value *string
	}{
		{"Subscription Name:", &state.Config.SubscriptionName},
		{"Publication Name:", nil}, // Special handling for array
		{"Host:", &state.Config.ConnectionHost},
		{"Port:", &state.Config.ConnectionPort},
		{"Database:", &state.Config.ConnectionDB},
		{"User:", &state.Config.ConnectionUser},
		{"Password:", &state.Config.ConnectionPass},
	}

	fieldIdx := 0
	for _, f := range fields {
		label := labelStyle.Render(f.label)

		var value string
		if f.value != nil {
			value = *f.value
		} else {
			// Publication names
			value = strings.Join(state.Config.PublicationNames, ", ")
		}

		// Mask password
		if f.label == "Password:" && value != "" {
			value = strings.Repeat("•", len(value))
		}

		if state.SelectedField == fieldIdx {
			if state.EditingField == fieldIdx {
				b.WriteString(selectedStyle.Render(label + " " + state.InputBuffer + "▌"))
			} else {
				b.WriteString(selectedStyle.Render(label + " " + value))
			}
		} else {
			b.WriteString(label + " " + valueStyle.Render(value))
		}
		b.WriteString("\n")
		fieldIdx++
	}

	b.WriteString("\n")

	// Copy data toggle
	copyLabel := labelStyle.Render("Copy Existing Data:")
	copyVal := "[ ] No"
	if state.Config.CopyData {
		copyVal = checkedStyle.Render("[✓]") + " Yes"
	}
	if state.SelectedField == fieldIdx {
		b.WriteString(selectedStyle.Render(copyLabel + " " + copyVal))
	} else {
		b.WriteString(copyLabel + " " + valueStyle.Render(copyVal))
	}
	b.WriteString("\n")
	fieldIdx++

	// Enabled toggle
	enabledLabel := labelStyle.Render("Enable Immediately:")
	enabledVal := "[ ] No"
	if state.Config.Enabled {
		enabledVal = checkedStyle.Render("[✓]") + " Yes"
	}
	if state.SelectedField == fieldIdx {
		b.WriteString(selectedStyle.Render(enabledLabel + " " + enabledVal))
	} else {
		b.WriteString(enabledLabel + " " + valueStyle.Render(enabledVal))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("[Enter] edit  [Space] toggle"))

	return b.String()
}

// renderLogicalReviewStep renders the review step with generated SQL.
func renderLogicalReviewStep(state *LogicalWizardState) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(styles.ColorAccent)
	codeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)
	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	execBadgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

	b.WriteString(headerStyle.Render("Review Generated SQL"))
	b.WriteString("\n\n")

	commands := generateLogicalCommands(state)

	for i, cmd := range commands {
		label := cmd.Label
		if cmd.Executable {
			label += " " + execBadgeStyle.Render("[SQL]")
		}
		b.WriteString(labelStyle.Render(label))
		b.WriteString("\n")
		if state.SelectedField == i {
			b.WriteString(selectedStyle.Render(cmd.Command))
		} else {
			b.WriteString(codeStyle.Render(cmd.Command))
		}
		b.WriteString("\n\n")
	}

	if IsLogicalSelectedCommandExecutable(state) {
		b.WriteString(hintStyle.Render("[y] copy  [x] execute"))
	} else {
		b.WriteString(hintStyle.Render("[y] copy"))
	}

	return b.String()
}

// generateLogicalCommands generates SQL commands based on configuration.
func generateLogicalCommands(state *LogicalWizardState) []CommandOutput {
	var commands []CommandOutput
	cfg := state.Config

	if cfg.Mode == LogicalModePublication {
		// Generate CREATE PUBLICATION
		sql := GenerateCreatePublicationSQL(cfg)
		commands = append(commands, CommandOutput{
			Section:    "",
			Label:      "CREATE PUBLICATION:",
			Command:    sql,
			Executable: true,
		})
	} else {
		// Generate CREATE SUBSCRIPTION
		sql := GenerateCreateSubscriptionSQL(cfg)
		commands = append(commands, CommandOutput{
			Section:    "",
			Label:      "CREATE SUBSCRIPTION:",
			Command:    sql,
			Executable: true,
		})
	}

	return commands
}

// GenerateCreatePublicationSQL generates CREATE PUBLICATION SQL.
func GenerateCreatePublicationSQL(cfg LogicalWizardConfig) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("CREATE PUBLICATION %s", cfg.PublicationName))

	if cfg.AllTables {
		b.WriteString(" FOR ALL TABLES")
	} else {
		// Collect selected tables
		var tables []string
		for fullName, selected := range cfg.SelectedTables {
			if selected {
				tables = append(tables, fullName)
			}
		}
		if len(tables) > 0 {
			b.WriteString(" FOR TABLE ")
			b.WriteString(strings.Join(tables, ", "))
		}
	}

	// WITH options for operations
	var withOpts []string
	if !cfg.OpInsert {
		withOpts = append(withOpts, "publish = 'update, delete'")
	} else if !cfg.OpUpdate {
		withOpts = append(withOpts, "publish = 'insert, delete'")
	} else if !cfg.OpDelete {
		withOpts = append(withOpts, "publish = 'insert, update'")
	}

	// Build publish list if not all default
	if cfg.OpInsert && cfg.OpUpdate && cfg.OpDelete && cfg.OpTruncate {
		withOpts = append(withOpts, "publish = 'insert, update, delete, truncate'")
	} else if !(cfg.OpInsert && cfg.OpUpdate && cfg.OpDelete && !cfg.OpTruncate) {
		// Non-default combination
		var ops []string
		if cfg.OpInsert {
			ops = append(ops, "insert")
		}
		if cfg.OpUpdate {
			ops = append(ops, "update")
		}
		if cfg.OpDelete {
			ops = append(ops, "delete")
		}
		if cfg.OpTruncate {
			ops = append(ops, "truncate")
		}
		if len(ops) > 0 {
			withOpts = append(withOpts, fmt.Sprintf("publish = '%s'", strings.Join(ops, ", ")))
		}
	}

	if len(withOpts) > 0 {
		b.WriteString(" WITH (")
		b.WriteString(strings.Join(withOpts, ", "))
		b.WriteString(")")
	}

	b.WriteString(";")
	return b.String()
}

// GenerateCreateSubscriptionSQL generates CREATE SUBSCRIPTION SQL.
func GenerateCreateSubscriptionSQL(cfg LogicalWizardConfig) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("CREATE SUBSCRIPTION %s", cfg.SubscriptionName))
	b.WriteString("\n  CONNECTION '")

	// Build connection string
	connParts := []string{
		fmt.Sprintf("host=%s", cfg.ConnectionHost),
		fmt.Sprintf("port=%s", cfg.ConnectionPort),
		fmt.Sprintf("dbname=%s", cfg.ConnectionDB),
		fmt.Sprintf("user=%s", cfg.ConnectionUser),
	}
	if cfg.ConnectionPass != "" {
		connParts = append(connParts, fmt.Sprintf("password=%s", cfg.ConnectionPass))
	}
	b.WriteString(strings.Join(connParts, " "))
	b.WriteString("'\n  PUBLICATION ")
	b.WriteString(strings.Join(cfg.PublicationNames, ", "))

	// WITH options
	var withOpts []string
	if !cfg.CopyData {
		withOpts = append(withOpts, "copy_data = false")
	}
	if !cfg.Enabled {
		withOpts = append(withOpts, "enabled = false")
	}

	if len(withOpts) > 0 {
		b.WriteString("\n  WITH (")
		b.WriteString(strings.Join(withOpts, ", "))
		b.WriteString(")")
	}

	b.WriteString(";")
	return b.String()
}

// renderLogicalWizardFooter renders navigation hints.
func renderLogicalWizardFooter(state *LogicalWizardState) string {
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	var hints []string
	hints = append(hints, "[j/k]nav")

	if state.Step > LogicalStepType {
		hints = append(hints, "[<]back")
	}
	if state.Step < LogicalStepReview {
		hints = append(hints, "[>]next")
	}

	hints = append(hints, "[esc/q]cancel")

	return footerStyle.Render(strings.Join(hints, " "))
}

// GetLogicalCommandCount returns the number of commands in the review step.
func GetLogicalCommandCount(state *LogicalWizardState) int {
	commands := generateLogicalCommands(state)
	return len(commands)
}

// GetLogicalSelectedCommand returns the currently selected command text.
func GetLogicalSelectedCommand(state *LogicalWizardState) string {
	commands := generateLogicalCommands(state)
	if state.SelectedField >= 0 && state.SelectedField < len(commands) {
		return commands[state.SelectedField].Command
	}
	return ""
}

// IsLogicalSelectedCommandExecutable returns true if the selected command can be executed.
func IsLogicalSelectedCommandExecutable(state *LogicalWizardState) bool {
	commands := generateLogicalCommands(state)
	if state.SelectedField >= 0 && state.SelectedField < len(commands) {
		return commands[state.SelectedField].Executable
	}
	return false
}

// GetLogicalMaxFieldForStep returns the maximum field index for a given step.
func GetLogicalMaxFieldForStep(state *LogicalWizardState) int {
	switch state.Step {
	case LogicalStepType:
		return 1 // publication, subscription
	case LogicalStepTableSelection:
		return 5 // name, all tables toggle, table list, username, pass mode, password
	case LogicalStepOperations:
		return 3 // insert, update, delete, truncate
	case LogicalStepConnection:
		return 8 // name, pub, host, port, db, user, pass, copy_data, enabled
	case LogicalStepReview:
		return GetLogicalCommandCount(state) - 1
	default:
		return 0
	}
}

// GetNextLogicalStep returns the next step based on current step and mode.
func GetNextLogicalStep(state *LogicalWizardState) LogicalWizardStep {
	if state.Config.Mode == LogicalModePublication {
		switch state.Step {
		case LogicalStepType:
			return LogicalStepTableSelection
		case LogicalStepTableSelection:
			return LogicalStepOperations
		case LogicalStepOperations:
			return LogicalStepReview
		}
	} else {
		switch state.Step {
		case LogicalStepType:
			return LogicalStepConnection
		case LogicalStepConnection:
			return LogicalStepReview
		}
	}
	return state.Step
}

// GetPrevLogicalStep returns the previous step based on current step and mode.
func GetPrevLogicalStep(state *LogicalWizardState) LogicalWizardStep {
	if state.Config.Mode == LogicalModePublication {
		switch state.Step {
		case LogicalStepTableSelection:
			return LogicalStepType
		case LogicalStepOperations:
			return LogicalStepTableSelection
		case LogicalStepReview:
			return LogicalStepOperations
		}
	} else {
		switch state.Step {
		case LogicalStepConnection:
			return LogicalStepType
		case LogicalStepReview:
			return LogicalStepConnection
		}
	}
	return state.Step
}

// Helper functions

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatCount(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
