package relay

import "testing"

func TestNew(t *testing.T) {
	r := New()

	if r == nil {
		t.Fatal("New() returned nil")
	}

	if !r.IsEnabled() {
		t.Error("new relay should be enabled by default")
	}
}

func TestEnableDisable(t *testing.T) {
	r := New()

	r.Disable()
	if r.IsEnabled() {
		t.Error("relay should be disabled after Disable()")
	}

	r.Enable()
	if !r.IsEnabled() {
		t.Error("relay should be enabled after Enable()")
	}
}

