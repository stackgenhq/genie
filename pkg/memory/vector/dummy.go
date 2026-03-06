package vector

import (
	"context"
	"hash/fnv"
	"math/rand"
)

// dummyEmbedder implements embedder.Embedder with a deterministic,
// non-semantic embedding function. Suitable only for testing.
type dummyEmbedder struct{}

const dummyDimension = 1536

// GetEmbedding returns a deterministic vector derived from the full input text.
// It hashes the text with FNV-64a, seeds a PRNG, and fills every dimension
// with a uniformly distributed value in [0, 1). This ensures the entire input
// influences all dimensions, avoiding the prefix-bias of a raw-byte copy.
func (d *dummyEmbedder) GetEmbedding(_ context.Context, text string) ([]float64, error) {
	h := fnv.New64a()
	h.Write([]byte(text))
	rng := rand.New(rand.NewSource(int64(h.Sum64()))) //nolint:gosec // deterministic test embedder, not crypto

	vec := make([]float64, dummyDimension)
	for i := range vec {
		vec[i] = rng.Float64()
	}
	return vec, nil
}

// GetEmbeddingWithUsage returns the same as GetEmbedding with nil usage.
func (d *dummyEmbedder) GetEmbeddingWithUsage(ctx context.Context, text string) ([]float64, map[string]any, error) {
	emb, err := d.GetEmbedding(ctx, text)
	return emb, nil, err
}

// GetDimensions returns the fixed dimensionality of the dummy embeddings.
func (d *dummyEmbedder) GetDimensions() int {
	return dummyDimension
}
