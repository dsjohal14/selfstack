// Package streamlite provides data ingestion connectors for streaming changes from various sources.
package streamlite

import "time"

// Connector represents a data source connector
type Connector interface {
	Name() string
	Start() error
	Stop() error
}

// BaseConnector provides common functionality for all connectors
type BaseConnector struct {
	name      string
	startedAt time.Time
}

// NewBaseConnector creates a new base connector
func NewBaseConnector(name string) *BaseConnector {
	return &BaseConnector{
		name: name,
	}
}

// Name returns the connector name
func (c *BaseConnector) Name() string {
	return c.name
}

// Start marks the connector as started
func (c *BaseConnector) Start() error {
	c.startedAt = time.Now()
	return nil
}

// Stop is a placeholder for cleanup
func (c *BaseConnector) Stop() error {
	return nil
}
