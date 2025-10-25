package streamlite

import (
	"testing"
)

func TestNewBaseConnector(t *testing.T) {
	name := "test-connector"
	connector := NewBaseConnector(name)

	if connector.Name() != name {
		t.Errorf("expected name %s, got %s", name, connector.Name())
	}
}

func TestBaseConnectorStart(t *testing.T) {
	connector := NewBaseConnector("test")

	if err := connector.Start(); err != nil {
		t.Errorf("Start() failed: %v", err)
	}

	if connector.startedAt.IsZero() {
		t.Error("startedAt should be set after Start()")
	}
}

func TestBaseConnectorStop(t *testing.T) {
	connector := NewBaseConnector("test")

	if err := connector.Stop(); err != nil {
		t.Errorf("Stop() failed: %v", err)
	}
}

