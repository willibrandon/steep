package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/app"
	"github.com/willibrandon/steep/internal/logger"
	"golang.org/x/term"
)

func main() {
	// Custom usage
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Steep - PostgreSQL Monitoring TUI")
		fmt.Fprintf(os.Stderr, "\nUsage: %s [options]\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}

	// Parse command-line flags
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	readonlyFlag := flag.Bool("readonly", false, "Disable destructive operations (kill, cancel)")
	bannerFlag := flag.Bool("banner", false, "Show animated banner")
	cpuprofile := flag.String("cpuprofile", "", "Write CPU profile to file")
	flag.Parse()

	// Start CPU profiling if requested
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatalf("Could not create CPU profile: %v\n", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("Could not start CPU profile: %v\n", err)
		}
		defer pprof.StopCPUProfile()
	}

	// Show animated banner then continue to TUI
	if *bannerFlag {
		showAnimatedBanner()
	}

	// Initialize logger
	if *debugFlag {
		logger.InitLogger(logger.LevelDebug)
		logger.Info("Debug logging enabled")
		// Print log location to stderr so user knows where to find logs
		fmt.Fprintf(os.Stderr, "Debug mode: Logs are written to %s/steep.log\n", os.TempDir())
	} else {
		logger.InitLogger(logger.LevelInfo)
	}
	defer logger.Close()

	// Create the application model
	logger.Debug("Creating application model")
	model, err := app.New(*readonlyFlag)
	if err != nil {
		logger.Error("Failed to create application", "error", err)
		log.Fatalf("Failed to create application: %v\n", err)
	}
	logger.Debug("Application model created successfully")

	// Validate terminal size
	if err := validateTerminalSize(); err != nil {
		logger.Error("Terminal size validation failed", "error", err)
		log.Fatalf("%v\n", err)
	}
	logger.Debug("Terminal size validation passed")

	// Create the Bubbletea program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	// Set program reference for progress updates from goroutines
	model.SetProgram(p)

	// Run the program
	finalModel, err := p.Run()
	if err != nil {
		log.Fatalf("Error running program: %v\n", err)
	}

	// Cleanup
	if m, ok := finalModel.(app.Model); ok {
		m.Cleanup()
	} else if m, ok := finalModel.(*app.Model); ok {
		m.Cleanup()
	}

	fmt.Println("Steep exited successfully")
}

// validateTerminalSize checks if the terminal meets minimum size requirements
func validateTerminalSize() error {
	const (
		minWidth  = 80
		minHeight = 24
	)

	// Get terminal size from stdout
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		// If we can't get terminal size, log but don't fail
		// This allows the app to run in non-terminal environments
		logger.Warn("Could not determine terminal size", "error", err)
		return nil
	}

	logger.Debug("Terminal size detected",
		"width", width,
		"height", height,
	)

	if width < minWidth || height < minHeight {
		fmt.Fprintf(os.Stderr,
			"Terminal too small: %dx%d (minimum required: %dx%d)\nPlease resize your terminal and try again.\n",
			width, height, minWidth, minHeight,
		)
		os.Exit(1)
	}

	return nil
}

// showAnimatedBanner displays an animated ASCII banner then clears for TUI
func showAnimatedBanner() {
	// PostgreSQL blue (#336791)
	logoColor := lipgloss.Color("67") // Closest to PostgreSQL brand blue

	lines := []string{
		" ██████╗████████╗███████╗███████╗██████╗ ",
		"██╔════╝╚══██╔══╝██╔════╝██╔════╝██╔══██╗",
		"╚█████╗    ██║   █████╗  █████╗  ██████╔╝",
		" ╚═══██╗   ██║   ██╔══╝  ██╔══╝  ██╔═══╝ ",
		"██████╔╝   ██║   ███████╗███████╗██║     ",
		"╚═════╝    ╚═╝   ╚══════╝╚══════╝╚═╝     ",
	}

	tagline := "PostgreSQL Monitoring TUI"
	version := "v0.1.0"

	// Clear screen and move cursor to top
	fmt.Print("\033[2J\033[H")
	fmt.Println()

	// Animate each line appearing
	style := lipgloss.NewStyle().Foreground(logoColor).Bold(true)
	for _, line := range lines {
		fmt.Println(style.Render(line))
		time.Sleep(60 * time.Millisecond)
	}

	// Show tagline and version
	fmt.Println()
	tagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	fmt.Println(tagStyle.Render(tagline))
	fmt.Println(version)

	// Hold for a moment before transitioning to TUI
	time.Sleep(1500 * time.Millisecond)

	// Clear screen before TUI takes over
	fmt.Print("\033[2J\033[H")
}
