package accel

import "testing"

func TestNewBatch(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{"valid size", 50, 50},
		{"zero defaults to 100", 0, 100},
		{"negative defaults to 100", -1, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch := NewBatch(tt.size)
			if batch.Size() != tt.expected {
				t.Errorf("expected size %d, got %d", tt.expected, batch.Size())
			}
		})
	}
}
