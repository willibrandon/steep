package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
// This test does NOT assume any offset values. Instead, it:
// 1. Finds rows by their actual content (PIDs)
// 2. Determines where those rows are actually rendered
// 3. Clicks at those positions
// 4. Verifies the correct row is selected
func TestMouseClickPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL testcontainer
	t.Log("Starting PostgreSQL testcontainer...")
	req := testcontainers.ContainerRequest{
		Image:        "postgres:18-alpine",
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
	cmd := exec.Command(configDir+"/steep", "--config", configPath)
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

	// Helper to get a specific line from terminal
	getLine := func(y int) string {
		termMu.Lock()
		defer termMu.Unlock()
		var buf bytes.Buffer
		cols, _ := term.Size()
		for x := 0; x < cols; x++ {
			cell := term.Cell(x, y)
			buf.WriteRune(cell.Char)
		}
		return strings.TrimRight(buf.String(), " ")
	}

	// Helper to find row with non-default background (selected row)
	findHighlightedRow := func() int {
		termMu.Lock()
		defer termMu.Unlock()
		cols, rows := term.Size()
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
				cell := term.Cell(col, row)
				if cell.BG != vt10x.DefaultBG && cell.BG != 0 {
					return row
				}
			}
		}
		return -1
	}

	// Helper to extract PID from a line (first number after "│ ")
	extractPID := func(line string) string {
		re := regexp.MustCompile(`│\s*(\d+)\s+`)
		matches := re.FindStringSubmatch(line)
		if len(matches) >= 2 {
			return matches[1]
		}
		return ""
	}

	// Helper to find which terminal row contains a specific PID
	findRowWithPID := func(pid string) int {
		termMu.Lock()
		defer termMu.Unlock()
		_, rows := term.Size()
		for row := 0; row < rows; row++ {
			var buf bytes.Buffer
			cols, _ := term.Size()
			for x := 0; x < cols; x++ {
				cell := term.Cell(x, row)
				buf.WriteRune(cell.Char)
			}
			line := buf.String()
			if strings.Contains(line, "│") {
				linePID := extractPID(line)
				if linePID == pid {
					return row
				}
			}
		}
		return -1
	}

	// Helper to send string to PTY
	send := func(s string) {
		ptmx.Write([]byte(s))
	}

	// Helper to send SGR mouse click
	sendMouseClick := func(col, row int) {
		// SGR format: ESC[<button;col;rowM (1-based coordinates)
		press := fmt.Sprintf("\x1b[<%d;%d;%dM", 0, col, row)
		release := fmt.Sprintf("\x1b[<%d;%d;%dm", 0, col, row)
		send(press)
		time.Sleep(10 * time.Millisecond)
		send(release)
	}

	// Wait for screen content
	waitForScreen := func(contains string, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if strings.Contains(getScreen(), contains) {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}

	// Wait for connection
	t.Log("Waiting for app to connect...")
	if !waitForScreen("Connected", 10*time.Second) {
		t.Logf("Warning: Did not see 'Connected'. Screen:\n%s", getScreen())
	}

	// Switch to activity view
	t.Log("Switching to activity view...")
	send("2")
	time.Sleep(1 * time.Second)

	if !waitForScreen("Activity", 5*time.Second) {
		t.Fatalf("Activity view did not load. Screen:\n%s", getScreen())
	}

	// Wait a bit more for data to load
	time.Sleep(500 * time.Millisecond)

	// Log the current screen for debugging
	screen := getScreen()
	t.Logf("Current screen:\n%s", screen)

	// Find all PIDs and their terminal row positions
	t.Log("Discovering row positions from actual screen content...")
	type rowInfo struct {
		terminalRow int
		pid         string
	}
	var dataRows []rowInfo

	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		pid := extractPID(line)
		if pid != "" {
			dataRows = append(dataRows, rowInfo{terminalRow: i, pid: pid})
			t.Logf("  Found PID %s at terminal row %d", pid, i)
		}
	}

	if len(dataRows) < 2 {
		t.Fatalf("Not enough data rows found (need at least 2, got %d)", len(dataRows))
	}

	// THE ACTUAL TEST: Click on each visible row and verify the correct row is selected
	t.Log("")
	t.Log("=== Testing mouse clicks by clicking on actual row positions ===")

	rowsToTest := len(dataRows)
	if rowsToTest > 5 {
		rowsToTest = 5
	}
	for i, row := range dataRows[:rowsToTest] {
		t.Run(fmt.Sprintf("Row%d_PID%s", i, row.pid), func(t *testing.T) {
			// First go to top to reset selection
			send("g")
			time.Sleep(200 * time.Millisecond)

			// The row is at terminal row Y (0-based)
			// SGR mouse uses 1-based coordinates
			sgrY := row.terminalRow + 1

			t.Logf("Clicking at SGR Y=%d to select row with PID %s", sgrY, row.pid)

			// Click on the row
			sendMouseClick(10, sgrY)
			time.Sleep(200 * time.Millisecond)

			// Find which row is now highlighted
			highlightedRow := findHighlightedRow()
			highlightedLine := getLine(highlightedRow)
			highlightedPID := extractPID(highlightedLine)

			t.Logf("After click: highlighted row=%d, PID=%s", highlightedRow, highlightedPID)

			// THE KEY ASSERTION: The highlighted row should have the same PID we clicked on
			if highlightedPID != row.pid {
				// Find where the highlighted PID actually is
				actualRow := findRowWithPID(highlightedPID)
				clickedRow := row.terminalRow
				offset := actualRow - clickedRow

				direction := "BELOW"
				if offset < 0 {
					direction = "ABOVE"
					offset = -offset
				}

				t.Errorf("MOUSE CLICK BUG: Clicked on PID %s (row %d), but PID %s (row %d) was selected instead - OFF BY %d rows %s",
					row.pid, clickedRow, highlightedPID, actualRow, offset, direction)
			}
		})
	}

	// Quit
	send("q")
	time.Sleep(500 * time.Millisecond)
}
