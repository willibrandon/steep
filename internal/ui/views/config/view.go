// Package config provides the Configuration Viewer view.
package config

import "github.com/willibrandon/steep/internal/db/models"

// ConfigDataMsg contains refreshed configuration data.
type ConfigDataMsg struct {
	Data  *models.ConfigData
	Error error
}

// RefreshConfigMsg triggers a data refresh.
type RefreshConfigMsg struct{}
