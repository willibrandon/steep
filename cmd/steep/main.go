package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/willibrandon/steep/internal/app"
	"github.com/willibrandon/steep/internal/logger"
	"golang.org/x/term"
)

func main() {
	// Parse command-line flags
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	readonlyFlag := flag.Bool("readonly", false, "Disable destructive operations (kill, cancel)")
	flag.Parse()

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
	os.Exit(0)
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
