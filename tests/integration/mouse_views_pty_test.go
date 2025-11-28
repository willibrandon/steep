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
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testPTYHelper provides common PTY testing functionality
type testPTYHelper struct {
	t      *testing.T
	ptmx   *os.File
	term   vt10x.Terminal
	termMu sync.Mutex
}

func newTestPTYHelper(t *testing.T, ptmx *os.File, term vt10x.Terminal) *testPTYHelper {
	h := &testPTYHelper{
		t:    t,
		ptmx: ptmx,
		term: term,
	}

	// Start reading from PTY
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			h.termMu.Lock()
			term.Write(buf[:n])
			h.termMu.Unlock()
		}
	}()

	return h
}

func (h *testPTYHelper) getScreen() string {
	h.termMu.Lock()
	defer h.termMu.Unlock()
	return h.term.String()
}

func (h *testPTYHelper) getLine(y int) string {
	h.termMu.Lock()
	defer h.termMu.Unlock()
	var buf bytes.Buffer
	cols, _ := h.term.Size()
	for x := 0; x < cols; x++ {
		cell := h.term.Cell(x, y)
		buf.WriteRune(cell.Char)
	}
	return strings.TrimRight(buf.String(), " ")
}

func (h *testPTYHelper) findHighlightedRow() int {
	h.termMu.Lock()
	defer h.termMu.Unlock()
	cols, rows := h.term.Size()
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := h.term.Cell(col, row)
			if cell.BG != vt10x.DefaultBG && cell.BG != 0 {
				return row
			}
		}
	}
	return -1
}

func (h *testPTYHelper) send(s string) {
	h.ptmx.Write([]byte(s))
}

func (h *testPTYHelper) sendMouseClick(col, row int) {
	// SGR format: ESC[<button;col;rowM (1-based coordinates)
	press := fmt.Sprintf("\x1b[<%d;%d;%dM", 0, col, row)
	release := fmt.Sprintf("\x1b[<%d;%d;%dm", 0, col, row)
	h.send(press)
	time.Sleep(10 * time.Millisecond)
	h.send(release)
}

func (h *testPTYHelper) waitForScreen(contains string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(h.getScreen(), contains) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// setupTestContainer creates a PostgreSQL container and returns connection info
func setupTestContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string, string) {
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

	host, _ := container.Host(ctx)
	port, _ := container.MappedPort(ctx, "5432")
	t.Logf("PostgreSQL ready at %s:%s", host, port.Port())

	return container, host, port.Port()
}

// buildSteep builds the steep binary in the given directory
func buildSteep(t *testing.T, configDir string) string {
	t.Log("Building steep binary...")
	binaryPath := configDir + "/steep"
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/steep")
	buildCmd.Dir = "/Users/brandon/src/steep"
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build steep: %v\n%s", err, output)
	}
	return binaryPath
}

// createConfig creates a config file and returns its path
func createConfig(t *testing.T, configDir, host, port string) string {
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
`, host, port)

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	return configPath
}

// startSteepPTY starts steep in a PTY and returns helper
func startSteepPTY(t *testing.T, binaryPath, configPath, host, port string) (*testPTYHelper, *exec.Cmd, func()) {
	t.Log("Starting steep in PTY...")
	cmd := exec.Command(binaryPath, "--config", configPath)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		fmt.Sprintf("PGHOST=%s", host),
		fmt.Sprintf("PGPORT=%s", port),
		"PGDATABASE=testdb",
		"PGUSER=test",
		"PGPASSWORD=test",
		"PGSSLMODE=disable",
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("Failed to start PTY: %v", err)
	}

	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatalf("Failed to set PTY size: %v", err)
	}

	term := vt10x.New(vt10x.WithSize(120, 30))
	helper := newTestPTYHelper(t, ptmx, term)

	cleanup := func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}

	return helper, cmd, cleanup
}

// TestLocksViewMouseClick tests mouse click row selection in the Locks view
func TestLocksViewMouseClick(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	container, host, port := setupTestContainer(t, ctx)
	defer container.Terminate(ctx)

	// Create some locks by opening transactions
	connStr := fmt.Sprintf("host=%s port=%s dbname=testdb user=test password=test sslmode=disable", host, port)

	// Create a table and generate some lock activity
	conn1, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn1.Close(ctx)

	// Create test table
	_, err = conn1.Exec(ctx, "CREATE TABLE IF NOT EXISTS lock_test (id SERIAL PRIMARY KEY, data TEXT)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert some data
	_, err = conn1.Exec(ctx, "INSERT INTO lock_test (data) VALUES ('test1'), ('test2'), ('test3')")
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// Start transactions that will hold locks
	var lockConns []*pgx.Conn
	for i := 0; i < 3; i++ {
		conn, err := pgx.Connect(ctx, connStr)
		if err != nil {
			t.Fatalf("Failed to connect for lock: %v", err)
		}
		lockConns = append(lockConns, conn)

		// Start transaction and acquire lock
		_, err = conn.Exec(ctx, "BEGIN")
		if err != nil {
			t.Fatalf("Failed to begin transaction: %v", err)
		}
		_, err = conn.Exec(ctx, fmt.Sprintf("SELECT * FROM lock_test WHERE id = %d FOR UPDATE", i+1))
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}
	}
	defer func() {
		for _, conn := range lockConns {
			conn.Exec(ctx, "ROLLBACK")
			conn.Close(ctx)
		}
	}()

	configDir := t.TempDir()
	binaryPath := buildSteep(t, configDir)
	configPath := createConfig(t, configDir, host, port)
	helper, _, cleanup := startSteepPTY(t, binaryPath, configPath, host, port)
	defer cleanup()

	// Wait for connection
	t.Log("Waiting for app to connect...")
	if !helper.waitForScreen("Connected", 10*time.Second) {
		t.Logf("Warning: Did not see 'Connected'. Screen:\n%s", helper.getScreen())
	}

	// Switch to locks view (key "4")
	t.Log("Switching to locks view...")
	helper.send("4")
	time.Sleep(1 * time.Second)

	if !helper.waitForScreen("Locks", 5*time.Second) {
		t.Fatalf("Locks view did not load. Screen:\n%s", helper.getScreen())
	}

	time.Sleep(500 * time.Millisecond)

	screen := helper.getScreen()
	t.Logf("Current screen:\n%s", screen)

	// Extract PIDs from the locks view - look for PID column
	// Locks view format: "    61 transa...  Exclusiv..."
	extractLockPID := func(line string) string {
		// Skip header lines
		if strings.Contains(line, "PID") && strings.Contains(line, "Type") {
			return ""
		}
		if strings.Contains(line, "─") || strings.Contains(line, "┌") || strings.Contains(line, "└") {
			return ""
		}
		// Match PID at start of data row (after leading spaces)
		re := regexp.MustCompile(`^\s+(\d+)\s+\w+`)
		matches := re.FindStringSubmatch(line)
		if len(matches) >= 2 {
			return matches[1]
		}
		return ""
	}

	// Find rows with lock data
	type rowInfo struct {
		terminalRow int
		pid         string
	}
	var dataRows []rowInfo

	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		pid := extractLockPID(line)
		if pid != "" {
			dataRows = append(dataRows, rowInfo{terminalRow: i, pid: pid})
			t.Logf("  Found PID %s at terminal row %d", pid, i)
		}
	}

	if len(dataRows) < 2 {
		t.Skipf("Not enough lock rows found (need at least 2, got %d) - this may be expected if no locks are held", len(dataRows))
	}

	t.Log("")
	t.Log("=== Testing Locks view mouse clicks ===")

	// Find the first data row position to establish the data area
	firstDataRow := dataRows[0].terminalRow

	// Helper to find highlighted row ONLY in data area (skip tabs/headers)
	findHighlightedDataRow := func() (int, string) {
		screen := helper.getScreen()
		lines := strings.Split(screen, "\n")
		for i := firstDataRow; i < len(lines); i++ {
			line := lines[i]
			// Check if this line has a PID (is a data row)
			pid := extractLockPID(line)
			if pid == "" {
				continue
			}
			// Check if this row is highlighted by looking at the terminal cells
			highlighted := func() bool {
				helper.termMu.Lock()
				defer helper.termMu.Unlock()
				cols, _ := helper.term.Size()
				for col := 0; col < cols; col++ {
					cell := helper.term.Cell(col, i)
					if cell.BG != vt10x.DefaultBG && cell.BG != 0 {
						return true
					}
				}
				return false
			}()
			if highlighted {
				return i, pid
			}
		}
		return -1, ""
	}

	rowsToTest := len(dataRows)
	if rowsToTest > 5 {
		rowsToTest = 5
	}

	for i, row := range dataRows[:rowsToTest] {
		t.Run(fmt.Sprintf("Row%d_PID%s", i, row.pid), func(t *testing.T) {
			helper.send("g")
			time.Sleep(200 * time.Millisecond)

			sgrY := row.terminalRow + 1
			t.Logf("Clicking at SGR Y=%d to select row with PID %s", sgrY, row.pid)

			helper.sendMouseClick(10, sgrY)
			time.Sleep(200 * time.Millisecond)

			highlightedRow, highlightedPID := findHighlightedDataRow()
			t.Logf("After click: highlighted data row=%d, PID=%s", highlightedRow, highlightedPID)

			if highlightedPID != row.pid {
				clickedRow := row.terminalRow
				offset := highlightedRow - clickedRow

				direction := "BELOW"
				if offset < 0 {
					direction = "ABOVE"
					offset = -offset
				}

				t.Errorf("LOCKS VIEW MOUSE CLICK BUG: Clicked on PID %s (row %d), but PID %s (row %d) was selected instead - OFF BY %d rows %s",
					row.pid, clickedRow, highlightedPID, highlightedRow, offset, direction)
			}
		})
	}

	helper.send("q")
	time.Sleep(500 * time.Millisecond)
}

// TestTablesViewMouseClick tests mouse click row selection in the Tables view
func TestTablesViewMouseClick(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	container, host, port := setupTestContainer(t, ctx)
	defer container.Terminate(ctx)

	// Create some tables
	connStr := fmt.Sprintf("host=%s port=%s dbname=testdb user=test password=test sslmode=disable", host, port)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close(ctx)

	// Create test tables
	for i := 1; i <= 5; i++ {
		_, err = conn.Exec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS test_table_%d (id SERIAL PRIMARY KEY, data TEXT)", i))
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}
		// Insert some data so tables have stats
		_, err = conn.Exec(ctx, fmt.Sprintf("INSERT INTO test_table_%d (data) VALUES ('data')", i))
		if err != nil {
			t.Fatalf("Failed to insert data: %v", err)
		}
	}

	// Run ANALYZE to populate stats
	_, err = conn.Exec(ctx, "ANALYZE")
	if err != nil {
		t.Fatalf("Failed to analyze: %v", err)
	}

	configDir := t.TempDir()
	binaryPath := buildSteep(t, configDir)
	configPath := createConfig(t, configDir, host, port)
	helper, _, cleanup := startSteepPTY(t, binaryPath, configPath, host, port)
	defer cleanup()

	t.Log("Waiting for app to connect...")
	if !helper.waitForScreen("Connected", 10*time.Second) {
		t.Logf("Warning: Did not see 'Connected'. Screen:\n%s", helper.getScreen())
	}

	// Switch to tables view (key "5")
	t.Log("Switching to tables view...")
	helper.send("5")
	time.Sleep(1 * time.Second)

	// Handle pgstattuple extension dialog if it appears
	if helper.waitForScreen("pgstattuple", 2*time.Second) {
		t.Log("Dismissing pgstattuple dialog...")
		helper.send("n") // Skip installation
		time.Sleep(500 * time.Millisecond)
	}

	if !helper.waitForScreen("Tables", 5*time.Second) {
		t.Fatalf("Tables view did not load. Screen:\n%s", helper.getScreen())
	}

	// Expand public schema if collapsed
	helper.send("l") // expand
	time.Sleep(300 * time.Millisecond)

	screen := helper.getScreen()
	t.Logf("Current screen:\n%s", screen)

	// Extract table/schema names from the tables view
	// Tables view format:
	// "▼ public" or "▸ public" for schemas
	// "   ├─ test_table_1" or "   └─ test_table_1" for tables
	extractTableName := func(line string) string {
		// Skip headers and separators
		if strings.Contains(line, "Schema/Table") || strings.Contains(line, "──") {
			return ""
		}
		if strings.Contains(line, "Skipped") || strings.Contains(line, "│") {
			return ""
		}
		// Match schema "▼ public" or "▸ public"
		schemaRe := regexp.MustCompile(`[▼▸]\s+(\w+)`)
		if matches := schemaRe.FindStringSubmatch(line); len(matches) >= 2 {
			return matches[1]
		}
		// Match table "├─ test_table_1" or "└─ test_table_1"
		tableRe := regexp.MustCompile(`[├└]─\s+(\w+)`)
		if matches := tableRe.FindStringSubmatch(line); len(matches) >= 2 {
			return matches[1]
		}
		return ""
	}

	type rowInfo struct {
		terminalRow int
		name        string
		isSchema    bool
	}
	var dataRows []rowInfo

	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		name := extractTableName(line)
		if name != "" {
			// Check if this is a schema (▼ or ▸) or a table (├─ or └─)
			isSchema := strings.Contains(line, "▼") || strings.Contains(line, "▸")
			dataRows = append(dataRows, rowInfo{terminalRow: i, name: name, isSchema: isSchema})
			itemType := "table"
			if isSchema {
				itemType = "schema"
			}
			t.Logf("  Found %s '%s' at terminal row %d", itemType, name, i)
		}
	}

	// Filter to only test table rows (not schemas) since clicking schema toggles expand/collapse
	var tableRows []rowInfo
	for _, row := range dataRows {
		if !row.isSchema {
			tableRows = append(tableRows, row)
		}
	}

	if len(tableRows) < 2 {
		t.Skipf("Not enough table rows found (need at least 2, got %d)", len(tableRows))
	}

	t.Log("")
	t.Log("=== Testing Tables view mouse clicks ===")

	// Helper to get selected index from footer "X / Y" pattern
	getSelectedIndex := func() int {
		screen := helper.getScreen()
		re := regexp.MustCompile(`(\d+)\s*/\s*\d+\s*│`)
		matches := re.FindStringSubmatch(screen)
		if len(matches) >= 2 {
			var idx int
			fmt.Sscanf(matches[1], "%d", &idx)
			return idx - 1 // Convert 1-based to 0-based
		}
		return -1
	}

	rowsToTest := len(tableRows)
	if rowsToTest > 5 {
		rowsToTest = 5
	}

	// First go to a known position (bottom) so we can see actual movement
	helper.send("G") // Go to bottom
	time.Sleep(300 * time.Millisecond)

	startIdx := getSelectedIndex()
	t.Logf("Starting at index %d (bottom)", startIdx)

	// Test clicking on table rows (skip schema to avoid expand/collapse side effects)
	// Table rows start at index 1 in the tree (index 0 is the schema)
	for i, row := range tableRows[:rowsToTest] {
		expectedIdx := i + 1 // +1 because index 0 is the schema "public"
		t.Run(fmt.Sprintf("Table%d_%s", i, row.name), func(t *testing.T) {
			sgrY := row.terminalRow + 1
			t.Logf("Clicking at SGR Y=%d to select table '%s' (expected index %d)", sgrY, row.name, expectedIdx)

			helper.sendMouseClick(10, sgrY)
			time.Sleep(200 * time.Millisecond)

			selectedIdx := getSelectedIndex()
			t.Logf("After click: selected index=%d (expected %d)", selectedIdx, expectedIdx)

			if selectedIdx != expectedIdx {
				offset := selectedIdx - expectedIdx
				direction := "BELOW"
				if offset < 0 {
					direction = "ABOVE"
					offset = -offset
				}

				t.Errorf("TABLES VIEW MOUSE CLICK BUG: Clicked on table '%s' (expected index %d), but index %d was selected - OFF BY %d rows %s",
					row.name, expectedIdx, selectedIdx, offset, direction)
			}
		})
	}

	helper.send("q")
	time.Sleep(500 * time.Millisecond)
}

// TestReplicationViewMouseClick tests mouse click row selection in the Replication view (Slots tab)
func TestReplicationViewMouseClick(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start PostgreSQL with logical replication enabled for slots
	t.Log("Starting PostgreSQL with wal_level=logical...")
	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		Cmd: []string{
			"postgres",
			"-c", "wal_level=logical",
			"-c", "max_replication_slots=10",
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
		t.Fatalf("Failed to start PostgreSQL: %v", err)
	}
	defer container.Terminate(ctx)

	host, _ := container.Host(ctx)
	mappedPort, _ := container.MappedPort(ctx, "5432")
	port := mappedPort.Port()
	t.Logf("PostgreSQL ready at %s:%s", host, port)

	// Create 5 logical replication slots
	connStr := fmt.Sprintf("host=%s port=%s dbname=testdb user=test password=test sslmode=disable", host, port)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	for i := 1; i <= 5; i++ {
		slotName := fmt.Sprintf("test_slot_%d", i)
		_, err = conn.Exec(ctx, fmt.Sprintf("SELECT pg_create_logical_replication_slot('%s', 'test_decoding')", slotName))
		if err != nil {
			t.Fatalf("Failed to create slot %s: %v", slotName, err)
		}
		t.Logf("Created replication slot: %s", slotName)
	}
	conn.Close(ctx)

	configDir := t.TempDir()
	binaryPath := buildSteep(t, configDir)
	configPath := createConfig(t, configDir, host, port)
	helper, _, cleanup := startSteepPTY(t, binaryPath, configPath, host, port)
	defer cleanup()

	t.Log("Waiting for app to connect...")
	if !helper.waitForScreen("Connected", 10*time.Second) {
		t.Logf("Warning: Did not see 'Connected'. Screen:\n%s", helper.getScreen())
	}

	// Switch to replication view (key "6")
	t.Log("Switching to replication view...")
	helper.send("6")
	time.Sleep(1 * time.Second)

	screen := helper.getScreen()
	if !strings.Contains(screen, "Replication") {
		t.Fatalf("Replication view did not load. Screen:\n%s", screen)
	}

	// Switch to Slots tab using 'l' key (next tab within view)
	t.Log("Switching to Slots tab...")
	helper.send("l") // Next tab within Replication view
	time.Sleep(500 * time.Millisecond)

	screen = helper.getScreen()
	t.Logf("After 'l' key, screen:\n%s", screen)

	// Slots is the second tab, so one 'l' should get us there
	// If not, try again
	if !strings.Contains(screen, "test_slot") && !strings.Contains(screen, "Slot Name") {
		helper.send("l")
		time.Sleep(500 * time.Millisecond)
		screen = helper.getScreen()
		t.Logf("After second 'l' key, screen:\n%s", screen)
	}

	// Find slot rows - they contain "test_slot_X"
	type rowInfo struct {
		terminalRow int
		name        string
	}
	var dataRows []rowInfo

	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		// Look for our test slots
		if strings.Contains(line, "test_slot_") {
			// Extract slot name
			re := regexp.MustCompile(`(test_slot_\d+)`)
			matches := re.FindStringSubmatch(line)
			name := fmt.Sprintf("slot_%d", len(dataRows))
			if len(matches) >= 2 {
				name = matches[1]
			}
			dataRows = append(dataRows, rowInfo{terminalRow: i, name: name})
			t.Logf("  Found slot '%s' at terminal row %d", name, i)
		}
	}

	if len(dataRows) < 2 {
		t.Logf("Screen content for debugging:\n%s", screen)
		t.Fatalf("Not enough slot rows found (need at least 2, got %d)", len(dataRows))
	}

	t.Log("")
	t.Log("=== Testing Replication view mouse clicks ===")

	// Helper to get selected index from footer "X / Y" pattern
	getSelectedIndex := func() int {
		screen := helper.getScreen()
		re := regexp.MustCompile(`(\d+)\s*/\s*\d+\s*│`)
		matches := re.FindStringSubmatch(screen)
		if len(matches) >= 2 {
			var idx int
			fmt.Sscanf(matches[1], "%d", &idx)
			return idx - 1 // Convert 1-based to 0-based
		}
		return -1
	}

	rowsToTest := len(dataRows)
	if rowsToTest > 5 {
		rowsToTest = 5
	}

	// Start from bottom to see actual movement
	helper.send("G")
	time.Sleep(300 * time.Millisecond)
	startIdx := getSelectedIndex()
	t.Logf("Starting at index %d", startIdx)

	for i, row := range dataRows[:rowsToTest] {
		t.Run(fmt.Sprintf("Row%d_%s", i, row.name), func(t *testing.T) {
			sgrY := row.terminalRow + 1
			t.Logf("Clicking at SGR Y=%d to select row '%s' (data row %d)", sgrY, row.name, i)

			helper.sendMouseClick(10, sgrY)
			time.Sleep(200 * time.Millisecond)

			selectedIdx := getSelectedIndex()
			t.Logf("After click: selected index=%d (expected %d)", selectedIdx, i)

			if selectedIdx != i {
				offset := selectedIdx - i
				direction := "BELOW"
				if offset < 0 {
					direction = "ABOVE"
					offset = -offset
				}

				t.Errorf("REPLICATION VIEW MOUSE CLICK BUG: Clicked on row %d ('%s'), but row %d was selected - OFF BY %d rows %s",
					i, row.name, selectedIdx, offset, direction)
			}
		})
	}

	helper.send("q")
	time.Sleep(500 * time.Millisecond)
}

// TestConfigurationViewMouseClick tests mouse click row selection in the Configuration view
func TestConfigurationViewMouseClick(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	container, host, port := setupTestContainer(t, ctx)
	defer container.Terminate(ctx)

	configDir := t.TempDir()
	binaryPath := buildSteep(t, configDir)
	configPath := createConfig(t, configDir, host, port)
	helper, _, cleanup := startSteepPTY(t, binaryPath, configPath, host, port)
	defer cleanup()

	t.Log("Waiting for app to connect...")
	if !helper.waitForScreen("Connected", 10*time.Second) {
		t.Logf("Warning: Did not see 'Connected'. Screen:\n%s", helper.getScreen())
	}

	// Switch to configuration view (key "8")
	t.Log("Switching to configuration view...")
	helper.send("8")
	time.Sleep(1 * time.Second)

	if !helper.waitForScreen("Configuration", 5*time.Second) {
		t.Fatalf("Configuration view did not load. Screen:\n%s", helper.getScreen())
	}

	time.Sleep(500 * time.Millisecond)

	screen := helper.getScreen()
	t.Logf("Current screen:\n%s", screen)

	// Find config parameter rows - they have "│" separators and parameter names
	type rowInfo struct {
		terminalRow int
		name        string
	}
	var dataRows []rowInfo

	lines := strings.Split(screen, "\n")
	for i, line := range lines {
		// Skip header/separator lines
		if strings.Contains(line, "────") || strings.Contains(line, "Name") && strings.Contains(line, "Value") {
			continue
		}
		// Look for data rows with parameter names (contain │ and start with letters)
		if strings.Contains(line, "│") {
			// Extract the first column (parameter name)
			re := regexp.MustCompile(`^\s*([a-z_]+)\s+│`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				dataRows = append(dataRows, rowInfo{terminalRow: i, name: matches[1]})
				t.Logf("  Found param '%s' at terminal row %d", matches[1], i)
			}
		}
	}

	if len(dataRows) < 2 {
		t.Fatalf("Not enough config rows found (need at least 2, got %d)", len(dataRows))
	}

	t.Log("")
	t.Log("=== Testing Configuration view mouse clicks ===")

	// Helper to get selected index from footer "Sort: Name ↑  X / Y" pattern
	// Configuration view's footer has: "Sort: Name ↑  5 / 353" (no trailing │)
	getSelectedIndex := func() int {
		screen := helper.getScreen()
		// Match "Sort: <column> <arrow>  X / Y" pattern at end of footer
		re := regexp.MustCompile(`Sort:\s+\w+\s+[↑↓]\s+(\d+)\s*/\s*\d+`)
		matches := re.FindStringSubmatch(screen)
		if len(matches) >= 2 {
			var idx int
			fmt.Sscanf(matches[1], "%d", &idx)
			return idx - 1 // Convert 1-based to 0-based
		}
		return -1
	}

	rowsToTest := len(dataRows)
	if rowsToTest > 5 {
		rowsToTest = 5
	}

	// Start from top (default position) so scrollOffset=0 and offsets are clear
	helper.send("g")
	time.Sleep(300 * time.Millisecond)
	startIdx := getSelectedIndex()
	t.Logf("Starting at index %d (top)", startIdx)

	for i, row := range dataRows[:rowsToTest] {
		t.Run(fmt.Sprintf("Row%d_%s", i, row.name), func(t *testing.T) {
			sgrY := row.terminalRow + 1
			t.Logf("Clicking at SGR Y=%d to select param '%s' (data row %d)", sgrY, row.name, i)

			helper.sendMouseClick(10, sgrY)
			time.Sleep(200 * time.Millisecond)

			selectedIdx := getSelectedIndex()
			t.Logf("After click: selected index=%d (expected %d)", selectedIdx, i)

			if selectedIdx != i {
				offset := selectedIdx - i
				direction := "BELOW"
				if offset < 0 {
					direction = "ABOVE"
					offset = -offset
				}

				t.Errorf("CONFIGURATION VIEW MOUSE CLICK BUG: Clicked on row %d ('%s'), but row %d was selected - OFF BY %d rows %s",
					i, row.name, selectedIdx, offset, direction)
			}
		})
	}

	helper.send("q")
	time.Sleep(500 * time.Millisecond)
}
