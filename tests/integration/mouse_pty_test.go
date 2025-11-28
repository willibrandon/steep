package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestMouseClickPTY tests mouse click row selection using a real PTY and terminal emulator.
// This is a true end-to-end test: the actual steep binary runs in a pseudo-terminal,
// real SGR mouse escape sequences are sent, and vt10x parses the terminal output.
func TestMouseClickPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL testcontainer
	t.Log("Starting PostgreSQL testcontainer...")
	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}
	defer container.Terminate(ctx)

	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "5432")
	t.Logf("PostgreSQL ready at %s:%s", host, port.Port())

	// Create config file
	configDir := t.TempDir()
	configPath := configDir + "/config.yaml"
	configContent := fmt.Sprintf(`
connection:
  host: %s
  port: %s
  database: testdb
  user: test
  password: test
  sslmode: disable

ui:
  refresh_interval: 60s
`, host, port.Port())

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Build the steep binary
	t.Log("Building steep binary...")
	buildCmd := exec.Command("go", "build", "-o", configDir+"/steep", "./cmd/steep")
	buildCmd.Dir = "/Users/brandon/src/steep"
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build steep: %v\n%s", err, output)
	}

	// Run steep in a PTY
	t.Log("Starting steep in PTY...")
	t.Logf("Config path: %s", configPath)
	t.Logf("Config content:\n%s", configContent)

	cmd := exec.Command(configDir+"/steep", "--config", configPath)
	// Set environment variables for database connection as backup
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		fmt.Sprintf("PGHOST=%s", host),
		fmt.Sprintf("PGPORT=%s", port.Port()),
		"PGDATABASE=testdb",
		"PGUSER=test",
		"PGPASSWORD=test",
		"PGSSLMODE=disable",
	)

	// Create PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("Failed to start PTY: %v", err)
	}
	defer func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Set terminal size
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatalf("Failed to set PTY size: %v", err)
	}

	// Create vt10x terminal emulator
	term := vt10x.New(vt10x.WithSize(120, 30))

	// Read from PTY and feed to vt10x
	var termMu sync.Mutex
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			termMu.Lock()
			term.Write(buf[:n])
			termMu.Unlock()
		}
	}()

	// Helper to get terminal content
	getScreen := func() string {
		termMu.Lock()
		defer termMu.Unlock()
		return term.String()
	}

	// Helper to get terminal content with cell info for debugging
	getScreenWithAttrs := func() string {
		termMu.Lock()
		defer termMu.Unlock()
		var buf bytes.Buffer
		cols, rows := term.Size()
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
				cell := term.Cell(col, row)
				if cell.FG != vt10x.DefaultFG || cell.BG != vt10x.DefaultBG {
					buf.WriteString(fmt.Sprintf("[%c:fg=%d,bg=%d]", cell.Char, cell.FG, cell.BG))
				} else {
					buf.WriteRune(cell.Char)
				}
			}
			buf.WriteRune('\n')
		}
		return buf.String()
	}

	// Helper to find row with non-default background (selected row)
	findHighlightedRow := func() int {
		termMu.Lock()
		defer termMu.Unlock()
		cols, rows := term.Size()
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
				cell := term.Cell(col, row)
				// Check for non-default background (selection highlight)
				if cell.BG != vt10x.DefaultBG && cell.BG != 0 {
					return row
				}
			}
		}
		return -1
	}

	_ = getScreenWithAttrs // silence unused warning

	// Helper to send string to PTY
	send := func(s string) {
		ptmx.Write([]byte(s))
	}

	// Helper to send SGR mouse click
	// SGR format: ESC[<button;col;rowM for press, ESC[<button;col;rowm for release
	// button: 0=left, 1=middle, 2=right
	// col/row are 1-based
	sendMouseClick := func(col, row int) {
		// Send press then release
		press := fmt.Sprintf("\x1b[<%d;%d;%dM", 0, col, row)
		release := fmt.Sprintf("\x1b[<%d;%d;%dm", 0, col, row)
		send(press)
		time.Sleep(10 * time.Millisecond)
		send(release)
	}

	// Wait for app to start and connect to database
	t.Log("Waiting for app to initialize and connect...")

	// Wait until we see "Connected" or the activity view header
	waitForScreen := func(contains string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			screen := getScreen()
			if strings.Contains(screen, contains) {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}

	// Wait for connection (look for connected status or activity data)
	if !waitForScreen("Connected", 10*time.Second) {
		screen := getScreen()
		t.Logf("Warning: Did not see 'Connected' in screen. Current screen:\n%s", screen)
	}

	// Switch to activity view (press '2')
	t.Log("Switching to activity view...")
	send("2")
	time.Sleep(1 * time.Second)

	// Wait for activity view to render
	if !waitForScreen("Activity", 5*time.Second) {
		screen := getScreen()
		t.Logf("Warning: Did not see 'Activity' in screen. Current screen:\n%s", screen)
	}

	// Capture initial screen
	screen := getScreen()
	t.Logf("Screen after switching to activity view:\n%s", screen)

	// Find the selected row indicator in the output
	// The bubbles table uses ANSI highlighting for selected rows
	// We need to find where data rows start and what row is selected

	// Go to top first
	send("g")
	time.Sleep(200 * time.Millisecond)

	// Find initial highlighted row
	initialHighlight := findHighlightedRow()
	t.Logf("Initial highlighted row: %d", initialHighlight)

	// Now test mouse clicks at different Y positions
	// We'll click and then check which row is highlighted
	//
	// From observing the screen layout:
	// - Row 0: "Steep - PostgreSQL Monitoring"
	// - Rows 1-3: Connection info box
	// - Row 4: "Activity Monitoring"
	// - Row 5: Table border
	// - Row 6: Header "PID User Database..."
	// - Row 7: Header separator
	// - Row 8+: Data rows
	//
	// So first data row is at terminal row 8 (0-based).
	// SGR mouse Y is 1-based, so SGR Y=9 should hit terminal row 8.
	firstDataRowTerminal := 8

	testCases := []struct {
		sgrY            int    // 1-based SGR Y coordinate
		expectedDataRow int    // Expected 0-based data row index (-1 = should not change)
		description     string
	}{
		{sgrY: 8, expectedDataRow: -1, description: "Header separator (should be ignored)"},
		{sgrY: 9, expectedDataRow: 0, description: "First data row"},
		{sgrY: 10, expectedDataRow: 1, description: "Second data row"},
		{sgrY: 11, expectedDataRow: 2, description: "Third data row"},
		{sgrY: 12, expectedDataRow: 3, description: "Fourth data row"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("SGR_Y=%d", tc.sgrY), func(t *testing.T) {
			// Go to top first (select row 0)
			send("g")
			time.Sleep(100 * time.Millisecond)

			// Check highlighted row before click
			beforeHighlight := findHighlightedRow()

			// Send mouse click
			sendMouseClick(10, tc.sgrY)
			time.Sleep(200 * time.Millisecond)

			// Check highlighted row after click
			afterHighlight := findHighlightedRow()

			// Calculate expected terminal row
			expectedTerminalRow := firstDataRowTerminal + tc.expectedDataRow
			if tc.expectedDataRow < 0 {
				expectedTerminalRow = beforeHighlight // Should not change
			}

			// Log for debugging
			t.Logf("Click at SGR Y=%d (%s)", tc.sgrY, tc.description)
			t.Logf("Highlighted row: before=%d, after=%d, expected=%d", beforeHighlight, afterHighlight, expectedTerminalRow)

			// Assert correct row was selected
			if tc.expectedDataRow < 0 {
				// Click should be ignored
				if afterHighlight != beforeHighlight {
					t.Errorf("Click at SGR Y=%d should be ignored, but highlight changed from %d to %d",
						tc.sgrY, beforeHighlight, afterHighlight)
				}
			} else {
				// Click should select the expected row
				if afterHighlight != expectedTerminalRow {
					actualDataRow := afterHighlight - firstDataRowTerminal
					t.Errorf("Click at SGR Y=%d: expected data row %d (terminal row %d), got data row %d (terminal row %d) - OFF BY %d",
						tc.sgrY, tc.expectedDataRow, expectedTerminalRow, actualDataRow, afterHighlight, actualDataRow-tc.expectedDataRow)
				}
			}
		})
	}

	// Send quit command
	send("q")
	time.Sleep(500 * time.Millisecond)
}

// findSelectedRowInScreen parses the terminal output to find which row appears selected.
// Returns -1 if no selected row found.
func findSelectedRowInScreen(screen string) int {
	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		// Look for selection indicator (highlighted background)
		// This is tricky because vt10x.String() may or may not preserve ANSI codes
		if strings.Contains(line, "►") || strings.Contains(line, ">") {
			return i
		}
	}
	return -1
}

// captureScreenWithANSI reads raw bytes from terminal including ANSI codes
func captureScreenWithANSI(term vt10x.Terminal) string {
	var buf bytes.Buffer
	cols, rows := term.Size()
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := term.Cell(col, row)
			buf.WriteRune(cell.Char)
		}
		buf.WriteRune('\n')
	}
	return buf.String()
}
