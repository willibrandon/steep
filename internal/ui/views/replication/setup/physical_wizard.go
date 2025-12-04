// Package setup provides setup wizard and configuration check components.
package setup

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sethvargo/go-password/password"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// WizardStep represents the current step in the wizard.
type WizardStep int

const (
	StepUserConfig WizardStep = iota
	StepNetworkSecurity
	StepReplicationMode
	StepReview
)

// SSLMode represents SSL connection modes.
type SSLMode string

const (
	SSLDisable    SSLMode = "disable"
	SSLPrefer     SSLMode = "prefer"
	SSLRequire    SSLMode = "require"
	SSLVerifyCA   SSLMode = "verify-ca"
	SSLVerifyFull SSLMode = "verify-full"
)

// AuthMethod represents pg_hba.conf authentication methods.
type AuthMethod string

const (
	AuthScramSHA256 AuthMethod = "scram-sha-256"
	AuthMD5         AuthMethod = "md5"
	AuthPassword    AuthMethod = "password"
	AuthTrust       AuthMethod = "trust"
)

// PhysicalWizardConfig holds the wizard configuration values.
type PhysicalWizardConfig struct {
	// Step 1: User configuration
	Username      string
	Password      string
	AutoGenPass   bool
	PasswordShown bool

	// Step 2: Network & Security
	PrimaryHost string
	PrimaryPort string
	ReplicaCIDR string // CIDR or IP for pg_hba.conf (e.g., "192.168.1.0/24" or "192.168.1.100/32")
	SSLMode     SSLMode
	AuthMethod  AuthMethod

	// Step 3: Replication mode
	SyncMode     string // "sync" or "async"
	ReplicaCount int
	ReplicaNames []string
	DataDir      string // Data directory for pg_basebackup -D
}

// PhysicalWizardState holds the wizard UI state.
type PhysicalWizardState struct {
	Step          WizardStep
	Config        PhysicalWizardConfig
	Error         string
	SelectedField int // For navigation within a step
	EditingField  int // -1 if not editing, otherwise field index
	InputBuffer   string
	CreatingUser  bool // True while user creation is in progress
}

// NewPhysicalWizardState creates a new wizard state with defaults.
func NewPhysicalWizardState(primaryHost, primaryPort string) *PhysicalWizardState {
	// Generate default password
	defaultPassword, _ := GenerateReplicationPassword()

	// Default CIDR based on host
	defaultCIDR := "0.0.0.0/0" // Will be customized by user
	if primaryHost != "" && primaryHost != "localhost" && primaryHost != "127.0.0.1" {
		// Try to suggest a reasonable default based on the host
		defaultCIDR = primaryHost + "/32"
	}

	return &PhysicalWizardState{
		Step: StepUserConfig,
		Config: PhysicalWizardConfig{
			// Step 1
			Username:    "replicator",
			Password:    defaultPassword,
			AutoGenPass: true,
			// Step 2
			PrimaryHost: primaryHost,
			PrimaryPort: primaryPort,
			ReplicaCIDR: defaultCIDR,
			SSLMode:     SSLPrefer,
			AuthMethod:  AuthScramSHA256,
			// Step 3
			SyncMode:     "async",
			ReplicaCount: 1,
			ReplicaNames: []string{"replica1"},
			DataDir:      "/var/lib/postgresql/data",
		},
		SelectedField: 0,
		EditingField:  -1,
	}
}

// GenerateReplicationPassword generates a secure password for replication.
func GenerateReplicationPassword() (string, error) {
	// Generate 24-char password with:
	// - At least 4 digits
	// - At least 4 symbols
	// - No ambiguous characters (0, O, l, 1)
	return password.Generate(24, 4, 4, false, false)
}

// ValidatePasswordStrength checks if a password meets minimum requirements.
func ValidatePasswordStrength(pw string) error {
	if len(pw) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(pw)
	hasLower := regexp.MustCompile(`[a-z]`).MatchString(pw)
	hasDigit := regexp.MustCompile(`[0-9]`).MatchString(pw)
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("password must contain uppercase, lowercase, and digit")
	}
	return nil
}

// PhysicalWizardRenderConfig holds rendering configuration.
type PhysicalWizardRenderConfig struct {
	Width    int
	Height   int
	ReadOnly bool
}

// RenderPhysicalWizard renders the physical replication setup wizard.
func RenderPhysicalWizard(state *PhysicalWizardState, cfg PhysicalWizardRenderConfig) string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Physical Replication Setup Wizard"))
	b.WriteString("\n\n")

	// Step indicator
	b.WriteString(renderStepIndicator(state.Step))
	b.WriteString("\n\n")

	// Step content
	switch state.Step {
	case StepUserConfig:
		b.WriteString(renderUserConfigStep(state, cfg.ReadOnly))
	case StepNetworkSecurity:
		b.WriteString(renderNetworkSecurityStep(state))
	case StepReplicationMode:
		b.WriteString(renderReplicationModeStep(state))
	case StepReview:
		b.WriteString(renderReviewStep(state))
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
	b.WriteString(renderWizardFooter(state.Step))

	return b.String()
}

// renderStepIndicator renders the wizard step progress bar.
func renderStepIndicator(current WizardStep) string {
	steps := []string{"1. User", "2. Network", "3. Replication", "4. Review"}
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

	var parts []string
	for i, s := range steps {
		if WizardStep(i) < current {
			parts = append(parts, doneStyle.Render("✓ "+s))
		} else if WizardStep(i) == current {
			parts = append(parts, activeStyle.Render("→ "+s))
		} else {
			parts = append(parts, dimStyle.Render("  "+s))
		}
	}
	return strings.Join(parts, "  ")
}

// renderUserConfigStep renders Step 1: User configuration.
func renderUserConfigStep(state *PhysicalWizardState, readOnly bool) string {
	var b strings.Builder

	labelStyle := lipgloss.NewStyle().Width(20)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Replication User Configuration"))
	b.WriteString("\n\n")

	// Username field
	usernameLabel := labelStyle.Render("Username:")
	if state.SelectedField == 0 {
		if state.EditingField == 0 {
			b.WriteString(selectedStyle.Render(usernameLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(usernameLabel + " " + state.Config.Username))
		}
	} else {
		b.WriteString(usernameLabel + " " + valueStyle.Render(state.Config.Username))
	}
	b.WriteString("\n")

	// Password mode field
	passMode := "Auto-generated"
	if !state.Config.AutoGenPass {
		passMode = "Manual"
	}
	passModeLabel := labelStyle.Render("Password Mode:")
	if state.SelectedField == 1 {
		b.WriteString(selectedStyle.Render(passModeLabel + " " + passMode))
	} else {
		b.WriteString(passModeLabel + " " + valueStyle.Render(passMode))
	}
	b.WriteString("\n")

	// Password field
	passDisplay := "••••••••••••••••••••••••"
	if state.Config.PasswordShown {
		passDisplay = state.Config.Password
	}
	passLabel := labelStyle.Render("Password:")
	if state.SelectedField == 2 {
		if state.EditingField == 2 {
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
	b.WriteString(hintStyle.Render("[Enter] edit  [Space] toggle mode  [v] show/hide  [r] regenerate  [y] copy pw  [c] create user"))

	return b.String()
}

// renderNetworkSecurityStep renders Step 2: Network & Security configuration.
func renderNetworkSecurityStep(state *PhysicalWizardState) string {
	var b strings.Builder

	labelStyle := lipgloss.NewStyle().Width(20)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Network & Security Configuration"))
	b.WriteString("\n\n")

	// Primary Host
	hostLabel := labelStyle.Render("Primary Host:")
	if state.SelectedField == 0 {
		if state.EditingField == 0 {
			b.WriteString(selectedStyle.Render(hostLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(hostLabel + " " + state.Config.PrimaryHost))
		}
	} else {
		b.WriteString(hostLabel + " " + valueStyle.Render(state.Config.PrimaryHost))
	}
	b.WriteString("\n")

	// Primary Port
	portLabel := labelStyle.Render("Primary Port:")
	if state.SelectedField == 1 {
		if state.EditingField == 1 {
			b.WriteString(selectedStyle.Render(portLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(portLabel + " " + state.Config.PrimaryPort))
		}
	} else {
		b.WriteString(portLabel + " " + valueStyle.Render(state.Config.PrimaryPort))
	}
	b.WriteString("\n")

	// Replica CIDR (for pg_hba.conf)
	cidrLabel := labelStyle.Render("Replica CIDR:")
	if state.SelectedField == 2 {
		if state.EditingField == 2 {
			b.WriteString(selectedStyle.Render(cidrLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(cidrLabel + " " + state.Config.ReplicaCIDR))
		}
	} else {
		b.WriteString(cidrLabel + " " + valueStyle.Render(state.Config.ReplicaCIDR))
	}
	b.WriteString("\n")

	// Warning for 0.0.0.0/0
	if state.Config.ReplicaCIDR == "0.0.0.0/0" {
		b.WriteString(warningStyle.Render("  ⚠ Warning: 0.0.0.0/0 allows connections from any IP"))
		b.WriteString("\n")
	}

	// SSL Mode
	sslLabel := labelStyle.Render("SSL Mode:")
	sslValue := string(state.Config.SSLMode)
	if state.SelectedField == 3 {
		b.WriteString(selectedStyle.Render(sslLabel + " " + sslValue))
	} else {
		b.WriteString(sslLabel + " " + valueStyle.Render(sslValue))
	}
	b.WriteString("\n")

	// Auth Method
	authLabel := labelStyle.Render("Auth Method:")
	authValue := string(state.Config.AuthMethod)
	if state.SelectedField == 4 {
		b.WriteString(selectedStyle.Render(authLabel + " " + authValue))
	} else {
		b.WriteString(authLabel + " " + valueStyle.Render(authValue))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("[Enter] edit text fields  [Space] cycle options"))
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("CIDR examples: 192.168.1.0/24, 10.0.0.5/32, 0.0.0.0/0"))

	return b.String()
}

// renderReplicationModeStep renders Step 3: Replication mode configuration.
func renderReplicationModeStep(state *PhysicalWizardState) string {
	var b strings.Builder

	labelStyle := lipgloss.NewStyle().Width(20)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Replication Mode Configuration"))
	b.WriteString("\n\n")

	// Sync mode field
	syncLabel := labelStyle.Render("Mode:")
	syncValue := "Asynchronous"
	if state.Config.SyncMode == "sync" {
		syncValue = "Synchronous"
	}
	if state.SelectedField == 0 {
		b.WriteString(selectedStyle.Render(syncLabel + " " + syncValue))
	} else {
		b.WriteString(syncLabel + " " + valueStyle.Render(syncValue))
	}
	b.WriteString("\n")

	// Replica count field
	countLabel := labelStyle.Render("Replica Count:")
	countValue := fmt.Sprintf("%d", state.Config.ReplicaCount)
	if state.SelectedField == 1 {
		b.WriteString(selectedStyle.Render(countLabel + " " + countValue))
	} else {
		b.WriteString(countLabel + " " + valueStyle.Render(countValue))
	}
	b.WriteString("\n")

	// Replica names
	for i := 0; i < state.Config.ReplicaCount && i < len(state.Config.ReplicaNames); i++ {
		nameLabel := labelStyle.Render(fmt.Sprintf("Replica %d Name:", i+1))
		fieldIdx := 2 + i
		if state.SelectedField == fieldIdx {
			if state.EditingField == fieldIdx {
				b.WriteString(selectedStyle.Render(nameLabel + " " + state.InputBuffer + "▌"))
			} else {
				b.WriteString(selectedStyle.Render(nameLabel + " " + state.Config.ReplicaNames[i]))
			}
		} else {
			b.WriteString(nameLabel + " " + valueStyle.Render(state.Config.ReplicaNames[i]))
		}
		b.WriteString("\n")
	}

	// Data directory
	dataDirFieldIdx := 2 + state.Config.ReplicaCount
	dataDirLabel := labelStyle.Render("Data Directory:")
	if state.SelectedField == dataDirFieldIdx {
		if state.EditingField == dataDirFieldIdx {
			b.WriteString(selectedStyle.Render(dataDirLabel + " " + state.InputBuffer + "▌"))
		} else {
			b.WriteString(selectedStyle.Render(dataDirLabel + " " + state.Config.DataDir))
		}
	} else {
		b.WriteString(dataDirLabel + " " + valueStyle.Render(state.Config.DataDir))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("[Enter] edit  [Space] toggle mode  [+/-] replica count"))

	return b.String()
}

// renderReviewStep renders Step 4: Review and generated commands.
func renderReviewStep(state *PhysicalWizardState) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(styles.ColorAccent)
	codeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	execBadgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green for executable

	b.WriteString(headerStyle.Render("Review Configuration"))
	b.WriteString("\n\n")

	// Summary
	b.WriteString(labelStyle.Render("Configuration Summary:"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  User: %s\n", state.Config.Username))
	b.WriteString(fmt.Sprintf("  Primary: %s:%s\n", state.Config.PrimaryHost, state.Config.PrimaryPort))
	b.WriteString(fmt.Sprintf("  Replica CIDR: %s\n", state.Config.ReplicaCIDR))
	b.WriteString(fmt.Sprintf("  SSL: %s, Auth: %s\n", state.Config.SSLMode, state.Config.AuthMethod))
	b.WriteString(fmt.Sprintf("  Mode: %s, Replicas: %d\n", state.Config.SyncMode, state.Config.ReplicaCount))
	b.WriteString("\n")

	// Generated commands
	commands := generateCommands(state)

	currentSection := ""
	for i, cmd := range commands {
		// Section headers
		if cmd.Section != currentSection {
			currentSection = cmd.Section
			b.WriteString(sectionStyle.Render(currentSection))
			b.WriteString("\n\n")
		}

		// Label with executable badge
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

	// Show execute hint only if current selection is executable
	if IsSelectedCommandExecutable(state) {
		b.WriteString(hintStyle.Render("[y] copy  [x] execute  [j/k] navigate"))
	} else {
		b.WriteString(hintStyle.Render("[y] copy  [j/k] navigate"))
	}

	return b.String()
}

// CommandOutput represents a generated command with label.
type CommandOutput struct {
	Section    string // Section header (e.g., "On Primary Server", "On Replica Server")
	Label      string
	Command    string
	Executable bool // True if this command can be executed via SQL on the connected database
}

// generateCommands generates all setup commands based on configuration.
func generateCommands(state *PhysicalWizardState) []CommandOutput {
	var commands []CommandOutput
	cfg := state.Config

	// === PRIMARY SERVER COMMANDS ===

	// 1. Create replication user SQL
	createUserSQL := GenerateCreateUserSQL(cfg.Username, cfg.Password)
	commands = append(commands, CommandOutput{
		Section:    "── On Primary Server ──",
		Label:      "1. Create Replication User:",
		Command:    createUserSQL,
		Executable: true, // Can execute via SQL
	})

	// 2. pg_hba.conf entry
	pgHbaEntry := GeneratePgHbaEntry(cfg.Username, cfg.ReplicaCIDR, cfg.AuthMethod)
	commands = append(commands, CommandOutput{
		Section:    "── On Primary Server ──",
		Label:      "2. Add to pg_hba.conf:",
		Command:    pgHbaEntry,
		Executable: false, // Requires file system access
	})

	// 3. Reload config (after editing pg_hba.conf)
	commands = append(commands, CommandOutput{
		Section:    "── On Primary Server ──",
		Label:      "3. Reload configuration:",
		Command:    "SELECT pg_reload_conf();",
		Executable: true, // Can execute via SQL
	})

	// 4. Synchronous standby names (if sync mode)
	if cfg.SyncMode == "sync" {
		syncStandbyNames := GenerateSyncStandbyNames(cfg.ReplicaNames[:cfg.ReplicaCount])
		commands = append(commands, CommandOutput{
			Section:    "── On Primary Server ──",
			Label:      "4. Set synchronous standbys (ALTER SYSTEM):",
			Command:    fmt.Sprintf("ALTER SYSTEM SET %s", syncStandbyNames),
			Executable: true, // Can execute via SQL
		})
	}

	// === REPLICA SERVER COMMANDS ===

	// pg_basebackup command (for each replica)
	for i := 0; i < cfg.ReplicaCount && i < len(cfg.ReplicaNames); i++ {
		basebackupCmd := GeneratePgBasebackupCommand(
			cfg.PrimaryHost,
			cfg.PrimaryPort,
			cfg.Username,
			cfg.DataDir,
		)
		label := fmt.Sprintf("pg_basebackup for %s:", cfg.ReplicaNames[i])
		if cfg.ReplicaCount == 1 {
			label = "Run pg_basebackup:"
		}
		commands = append(commands, CommandOutput{
			Section:    "── On Replica Server ──",
			Label:      label,
			Command:    basebackupCmd,
			Executable: false, // Shell command on replica server
		})
	}

	// primary_conninfo for each replica
	for i := 0; i < cfg.ReplicaCount && i < len(cfg.ReplicaNames); i++ {
		connInfo := GeneratePrimaryConnInfo(
			cfg.PrimaryHost,
			cfg.PrimaryPort,
			cfg.Username,
			cfg.ReplicaNames[i],
			cfg.SSLMode,
		)
		label := fmt.Sprintf("primary_conninfo for %s:", cfg.ReplicaNames[i])
		if cfg.ReplicaCount == 1 {
			label = "Add to postgresql.auto.conf:"
		}
		commands = append(commands, CommandOutput{
			Section:    "── On Replica Server ──",
			Label:      label,
			Command:    connInfo,
			Executable: false, // Config file on replica server
		})
	}

	// Standby signal file
	commands = append(commands, CommandOutput{
		Section:    "── On Replica Server ──",
		Label:      "Create standby signal file:",
		Command:    fmt.Sprintf("touch %s/standby.signal", cfg.DataDir),
		Executable: false, // Shell command on replica server
	})

	return commands
}

// GenerateCreateUserSQL generates SQL to create a replication user.
func GenerateCreateUserSQL(username, password string) string {
	// Escape single quotes in password
	escapedPass := strings.ReplaceAll(password, "'", "''")
	return fmt.Sprintf("CREATE USER %s WITH REPLICATION LOGIN PASSWORD '%s';", username, escapedPass)
}

// GeneratePgHbaEntry generates a pg_hba.conf entry for replication.
func GeneratePgHbaEntry(username, cidr string, authMethod AuthMethod) string {
	return fmt.Sprintf("host    replication    %s    %s    %s", username, cidr, authMethod)
}

// GeneratePgBasebackupCommand generates the pg_basebackup command.
func GeneratePgBasebackupCommand(host, port, user, dataDir string) string {
	return fmt.Sprintf("pg_basebackup -h %s -p %s -U %s -D %s -Fp -Xs -P -R",
		host, port, user, dataDir)
}

// GeneratePrimaryConnInfo generates the primary_conninfo for postgresql.auto.conf.
func GeneratePrimaryConnInfo(host, port, user, appName string, sslMode SSLMode) string {
	return fmt.Sprintf("primary_conninfo = 'host=%s port=%s user=%s application_name=%s sslmode=%s'",
		host, port, user, appName, sslMode)
}

// GenerateSyncStandbyNames generates synchronous_standby_names setting.
func GenerateSyncStandbyNames(replicaNames []string) string {
	if len(replicaNames) == 0 {
		return "synchronous_standby_names = ''"
	}
	// FIRST 1 (replica1, replica2, ...) - any one must be sync
	return fmt.Sprintf("synchronous_standby_names = 'FIRST 1 (%s)'", strings.Join(replicaNames, ", "))
}

// renderWizardFooter renders navigation hints for the wizard.
func renderWizardFooter(step WizardStep) string {
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	var hints []string
	hints = append(hints, "[j/k]nav")

	if step > StepUserConfig {
		hints = append(hints, "[<]back")
	}
	if step < StepReview {
		hints = append(hints, "[>]next")
	}

	hints = append(hints, "[esc/q]cancel")

	return footerStyle.Render(strings.Join(hints, " "))
}

// GetCommandCount returns the number of commands in the review step.
func GetCommandCount(state *PhysicalWizardState) int {
	commands := generateCommands(state)
	return len(commands)
}

// GetSelectedCommand returns the currently selected command text.
func GetSelectedCommand(state *PhysicalWizardState) string {
	commands := generateCommands(state)
	if state.SelectedField >= 0 && state.SelectedField < len(commands) {
		return commands[state.SelectedField].Command
	}
	return ""
}

// IsSelectedCommandExecutable returns true if the selected command can be executed via SQL.
func IsSelectedCommandExecutable(state *PhysicalWizardState) bool {
	commands := generateCommands(state)
	if state.SelectedField >= 0 && state.SelectedField < len(commands) {
		return commands[state.SelectedField].Executable
	}
	return false
}

// GetSelectedCommandLabel returns the label of the currently selected command.
func GetSelectedCommandLabel(state *PhysicalWizardState) string {
	commands := generateCommands(state)
	if state.SelectedField >= 0 && state.SelectedField < len(commands) {
		return commands[state.SelectedField].Label
	}
	return ""
}

// GetMaxFieldForStep returns the maximum field index for a given step.
func GetMaxFieldForStep(state *PhysicalWizardState) int {
	switch state.Step {
	case StepUserConfig:
		return 2 // username, password mode, password
	case StepNetworkSecurity:
		return 4 // host, port, cidr, ssl, auth
	case StepReplicationMode:
		return 2 + state.Config.ReplicaCount // mode, count, replica names..., data dir
	case StepReview:
		return GetCommandCount(state) - 1
	default:
		return 0
	}
}

// CycleSSLMode cycles through SSL modes.
func CycleSSLMode(current SSLMode) SSLMode {
	modes := []SSLMode{SSLDisable, SSLPrefer, SSLRequire, SSLVerifyCA, SSLVerifyFull}
	for i, m := range modes {
		if m == current {
			return modes[(i+1)%len(modes)]
		}
	}
	return SSLPrefer
}

// CycleAuthMethod cycles through authentication methods.
func CycleAuthMethod(current AuthMethod) AuthMethod {
	methods := []AuthMethod{AuthScramSHA256, AuthMD5, AuthPassword, AuthTrust}
	for i, m := range methods {
		if m == current {
			return methods[(i+1)%len(methods)]
		}
	}
	return AuthScramSHA256
}
