package config_test

import (
	"testing"

	"github.com/willibrandon/steep/internal/db/models"
)

// TestParameter_IsModified tests the IsModified helper method.
func TestParameter_IsModified(t *testing.T) {
	tests := []struct {
		name     string
		param    models.Parameter
		expected bool
	}{
		{
			name: "modified when setting differs from boot_val",
			param: models.Parameter{
				Name:    "shared_buffers",
				Setting: "256MB",
				BootVal: "128MB",
			},
			expected: true,
		},
		{
			name: "not modified when setting equals boot_val",
			param: models.Parameter{
				Name:    "max_connections",
				Setting: "100",
				BootVal: "100",
			},
			expected: false,
		},
		{
			name: "not modified when boot_val is empty",
			param: models.Parameter{
				Name:    "internal_param",
				Setting: "some_value",
				BootVal: "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.param.IsModified()
			if result != tt.expected {
				t.Errorf("IsModified() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestParameter_RequiresRestart tests the RequiresRestart helper method.
func TestParameter_RequiresRestart(t *testing.T) {
	tests := []struct {
		name     string
		context  string
		expected bool
	}{
		{"postmaster context requires restart", "postmaster", true},
		{"sighup context does not require restart", "sighup", false},
		{"user context does not require restart", "user", false},
		{"superuser context does not require restart", "superuser", false},
		{"backend context does not require restart", "backend", false},
		{"internal context does not require restart", "internal", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := models.Parameter{Context: tt.context}
			if got := p.RequiresRestart(); got != tt.expected {
				t.Errorf("RequiresRestart() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestParameter_RequiresReload tests the RequiresReload helper method.
func TestParameter_RequiresReload(t *testing.T) {
	tests := []struct {
		name     string
		context  string
		expected bool
	}{
		{"sighup context requires reload", "sighup", true},
		{"postmaster context does not require reload", "postmaster", false},
		{"user context does not require reload", "user", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := models.Parameter{Context: tt.context}
			if got := p.RequiresReload(); got != tt.expected {
				t.Errorf("RequiresReload() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestParameter_CanUserChange tests the CanUserChange helper method.
func TestParameter_CanUserChange(t *testing.T) {
	tests := []struct {
		name     string
		context  string
		expected bool
	}{
		{"user context can be changed", "user", true},
		{"superuser context cannot be changed by regular user", "superuser", false},
		{"postmaster context cannot be changed", "postmaster", false},
		{"sighup context cannot be changed", "sighup", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := models.Parameter{Context: tt.context}
			if got := p.CanUserChange(); got != tt.expected {
				t.Errorf("CanUserChange() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestParameter_TopLevelCategory tests the TopLevelCategory helper method.
func TestParameter_TopLevelCategory(t *testing.T) {
	tests := []struct {
		name     string
		category string
		expected string
	}{
		{
			name:     "extracts top level from nested category",
			category: "Resource Usage / Memory",
			expected: "Resource Usage",
		},
		{
			name:     "returns full category when no separator",
			category: "Connections and Authentication",
			expected: "Connections and Authentication",
		},
		{
			name:     "handles deeply nested category",
			category: "Resource Usage / Memory / Shared Memory",
			expected: "Resource Usage",
		},
		{
			name:     "handles empty category",
			category: "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := models.Parameter{Category: tt.category}
			if got := p.TopLevelCategory(); got != tt.expected {
				t.Errorf("TopLevelCategory() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestConfigData_FilterByCategory tests category filtering.
func TestConfigData_FilterByCategory(t *testing.T) {
	data := &models.ConfigData{
		Parameters: []models.Parameter{
			{Name: "shared_buffers", Category: "Resource Usage / Memory"},
			{Name: "work_mem", Category: "Resource Usage / Memory"},
			{Name: "max_connections", Category: "Connections and Authentication"},
			{Name: "wal_level", Category: "Write-Ahead Log / Settings"},
		},
	}

	// Filter by "Resource Usage"
	result := data.FilterByCategory("Resource Usage")
	if len(result) != 2 {
		t.Errorf("FilterByCategory('Resource Usage') returned %d params, want 2", len(result))
	}

	// Empty category returns all
	result = data.FilterByCategory("")
	if len(result) != 4 {
		t.Errorf("FilterByCategory('') returned %d params, want 4", len(result))
	}

	// Non-matching category returns empty
	result = data.FilterByCategory("NonExistent")
	if len(result) != 0 {
		t.Errorf("FilterByCategory('NonExistent') returned %d params, want 0", len(result))
	}
}

// TestConfigData_FilterBySearch tests search filtering.
func TestConfigData_FilterBySearch(t *testing.T) {
	data := &models.ConfigData{
		Parameters: []models.Parameter{
			{Name: "shared_buffers", ShortDesc: "Sets the number of shared memory buffers"},
			{Name: "work_mem", ShortDesc: "Sets the memory for internal sort operations"},
			{Name: "max_connections", ShortDesc: "Sets the maximum number of concurrent connections"},
		},
	}

	// Search by name
	result := data.FilterBySearch("shared")
	if len(result) != 1 {
		t.Errorf("FilterBySearch('shared') returned %d params, want 1", len(result))
	}

	// Search by description (case insensitive)
	result = data.FilterBySearch("MEMORY")
	if len(result) != 2 {
		t.Errorf("FilterBySearch('MEMORY') returned %d params, want 2", len(result))
	}

	// Empty search returns all
	result = data.FilterBySearch("")
	if len(result) != 3 {
		t.Errorf("FilterBySearch('') returned %d params, want 3", len(result))
	}

	// Non-matching search returns empty
	result = data.FilterBySearch("zzzzz")
	if len(result) != 0 {
		t.Errorf("FilterBySearch('zzzzz') returned %d params, want 0", len(result))
	}
}

// TestNewConfigData tests the constructor.
func TestNewConfigData(t *testing.T) {
	data := models.NewConfigData()

	if data == nil {
		t.Fatal("NewConfigData() returned nil")
	}

	if data.Parameters == nil {
		t.Error("Parameters slice is nil")
	}

	if data.Categories == nil {
		t.Error("Categories slice is nil")
	}

	if cap(data.Parameters) < 350 {
		t.Errorf("Parameters capacity = %d, want >= 350", cap(data.Parameters))
	}
}
