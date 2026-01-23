package relay

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

const (
	// EmbeddingDim is the fixed dimension for all embeddings
	EmbeddingDim = 128
)

// Embedding represents a fixed-dimension vector
type Embedding [EmbeddingDim]float32

// DeterministicEmbed creates a reproducible embedding from text
// Uses SHA256 hash to generate deterministic pseudo-random values
// This is NOT a real semantic embedding, but deterministic for testing
// TODO: Replace with sentence-transformers or OpenAI embeddings in production
func DeterministicEmbed(text string) Embedding {
	var emb Embedding

	// Use SHA256 to get deterministic bytes from text
	hash := sha256.Sum256([]byte(text))

	// Convert hash bytes to float32 values in [-1, 1]
	for i := 0; i < EmbeddingDim; i++ {
		// Use 4 bytes per float, wrap around hash if needed
		byteIdx := (i * 4) % len(hash)

		// Convert 4 bytes to uint32, then normalize to [-1, 1]
		var val uint32
		if byteIdx+4 <= len(hash) {
			val = binary.BigEndian.Uint32(hash[byteIdx : byteIdx+4])
		} else {
			// Need to wrap around - rehash
			nextHash := sha256.Sum256(hash[:])
			val = binary.BigEndian.Uint32(nextHash[0:4])
			hash = nextHash
		}

		// Normalize uint32 to [-1, 1]
		emb[i] = float32(val)/float32(math.MaxUint32)*2.0 - 1.0
	}

	// Normalize to unit length for cosine similarity
	return normalize(emb)
}

// CosineSimilarity computes cosine similarity between two embeddings
// Returns value in [-1, 1] where 1 = identical, -1 = opposite
func CosineSimilarity(a, b Embedding) float32 {
	var dot float32
	for i := 0; i < EmbeddingDim; i++ {
		dot += a[i] * b[i]
	}
	return dot // Already normalized, so dot product = cosine
}

func normalize(v Embedding) Embedding {
	var sum float32
	for i := 0; i < EmbeddingDim; i++ {
		sum += v[i] * v[i]
	}

	mag := float32(math.Sqrt(float64(sum)))
	if mag == 0 {
		return v
	}

	var result Embedding
	for i := 0; i < EmbeddingDim; i++ {
		result[i] = v[i] / mag
	}
	return result
}
