package relay

import (
	"math"
	"testing"
)

func TestDeterministicEmbed(t *testing.T) {
	text := "hello world"

	// Same input should produce identical output
	emb1 := DeterministicEmbed(text)
	emb2 := DeterministicEmbed(text)

	for i := 0; i < EmbeddingDim; i++ {
		if emb1[i] != emb2[i] {
			t.Errorf("embeddings not deterministic at index %d: %f != %f", i, emb1[i], emb2[i])
		}
	}
}

func TestEmbeddingNormalized(t *testing.T) {
	emb := DeterministicEmbed("test text")

	// Check unit length (magnitude should be ~1.0)
	var sumSquares float32
	for i := 0; i < EmbeddingDim; i++ {
		sumSquares += emb[i] * emb[i]
	}

	magnitude := float32(math.Sqrt(float64(sumSquares)))
	if math.Abs(float64(magnitude-1.0)) > 0.001 {
		t.Errorf("embedding not normalized: magnitude = %f", magnitude)
	}
}

func TestCosineSimilarity(t *testing.T) {
	emb1 := DeterministicEmbed("hello world")
	emb2 := DeterministicEmbed("hello world")
	emb3 := DeterministicEmbed("goodbye world")

	// Identical embeddings should have similarity = 1.0
	sim := CosineSimilarity(emb1, emb2)
	if math.Abs(float64(sim-1.0)) > 0.001 {
		t.Errorf("identical embeddings should have similarity ~1.0, got %f", sim)
	}

	// Different embeddings should have similarity < 1.0
	sim2 := CosineSimilarity(emb1, emb3)
	if sim2 >= 1.0 {
		t.Errorf("different embeddings should have similarity < 1.0, got %f", sim2)
	}

	// Similarity should be in [-1, 1]
	if sim2 < -1.0 || sim2 > 1.0 {
		t.Errorf("similarity out of range: %f", sim2)
	}
}

func TestDifferentTextsProduceDifferentEmbeddings(t *testing.T) {
	emb1 := DeterministicEmbed("text A")
	emb2 := DeterministicEmbed("text B")

	identical := true
	for i := 0; i < EmbeddingDim; i++ {
		if emb1[i] != emb2[i] {
			identical = false
			break
		}
	}

	if identical {
		t.Error("different texts produced identical embeddings")
	}
}
