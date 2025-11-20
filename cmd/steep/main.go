package main

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/willibrandon/steep/internal/app"
)

func main() {
	// Create the application model
	model, err := app.New()
	if err != nil {
		log.Fatalf("Failed to create application: %v\n", err)
	}

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
