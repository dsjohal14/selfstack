// Package accel provides utilities for accelerated batch processing.
package accel

// Batch represents a batch processing helper
type Batch struct {
	size int
}

// NewBatch creates a new batch processor with the given size
func NewBatch(size int) *Batch {
	if size <= 0 {
		size = 100
	}
	return &Batch{size: size}
}

// Size returns the batch size
func (b *Batch) Size() int {
	return b.size
}
