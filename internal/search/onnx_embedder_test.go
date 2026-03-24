package search

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewOnnxEmbedder_MissingTokenizer(t *testing.T) {
	t.Parallel()
	_, err := NewOnnxEmbedder("/nonexistent/lib.so", "/nonexistent/model.onnx", "/nonexistent/tokenizer.json")
	if err == nil {
		t.Fatal("expected error for missing tokenizer, got nil")
	}
	if want := "read tokenizer"; !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
}

func TestNewOnnxEmbedder_InvalidTokenizer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tokPath := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(tokPath, []byte("not-json{{{"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewOnnxEmbedder("/nonexistent/lib.so", "/nonexistent/model.onnx", tokPath)
	if err == nil {
		t.Fatal("expected error for invalid tokenizer, got nil")
	}
	if want := "load tokenizer"; !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
}

func TestOnnxEmbedder_ReadyAndClose(t *testing.T) {
	t.Parallel()
	e := &OnnxEmbedder{}
	e.ready.Store(true)

	if !e.Ready(context.Background()) {
		t.Fatal("expected Ready() to return true")
	}

	_ = e.Close()

	if e.Ready(context.Background()) {
		t.Fatal("expected Ready() to return false after Close()")
	}
}

func TestOnnxEmbedder_EmbedWhenClosed(t *testing.T) {
	t.Parallel()
	e := &OnnxEmbedder{}
	e.ready.Store(false)

	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error when embedding on closed embedder")
	}
	if want := "embedder is closed"; !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
}

func TestOnnxEmbedder_EmbedEmptyTexts(t *testing.T) {
	t.Parallel()
	e := &OnnxEmbedder{}
	e.ready.Store(true)

	result, err := e.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for empty texts, got %v", result)
	}
}

func TestMeanPool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		hidden    []float32
		mask      []int64
		batchSize int
		seqLen    int
		wantErr   bool
	}{
		{
			name:      "single token, full mask",
			hidden:    make([]float32, hiddenSize),
			mask:      []int64{1},
			batchSize: 1,
			seqLen:    1,
		},
		{
			name:      "two tokens, partial mask",
			hidden:    make([]float32, 2*hiddenSize),
			mask:      []int64{1, 0},
			batchSize: 1,
			seqLen:    2,
		},
		{
			name:      "size mismatch",
			hidden:    []float32{1, 2, 3},
			mask:      []int64{1},
			batchSize: 1,
			seqLen:    1,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := meanPool(tt.hidden, tt.mask, tt.batchSize, tt.seqLen)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.batchSize {
				t.Fatalf("expected %d results, got %d", tt.batchSize, len(result))
			}
			for i, vec := range result {
				if len(vec) != hiddenSize {
					t.Fatalf("result[%d] has %d dims, want %d", i, len(vec), hiddenSize)
				}
			}
		})
	}
}

func TestMeanPool_AttentionMaskWeighting(t *testing.T) {
	t.Parallel()
	// Two tokens: [1.0, 0, 0, ...] and [0, 2.0, 0, ...] with mask [1, 1].
	// Mean should be [0.5, 1.0, 0, ...] then L2-normalized.
	hidden := make([]float32, 2*hiddenSize)
	hidden[0] = 1.0
	hidden[hiddenSize+1] = 2.0
	mask := []int64{1, 1}

	results, err := meanPool(hidden, mask, 1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vec := results[0]

	// Before normalization, vec would be [0.5, 1.0, 0, ...].
	// L2 norm = sqrt(0.25 + 1.0) = sqrt(1.25).
	expectedNorm := math.Sqrt(1.25)
	wantD0 := float32(0.5 / expectedNorm)
	wantD1 := float32(1.0 / expectedNorm)

	if math.Abs(float64(vec[0]-wantD0)) > 1e-6 {
		t.Fatalf("vec[0] = %f, want %f", vec[0], wantD0)
	}
	if math.Abs(float64(vec[1]-wantD1)) > 1e-6 {
		t.Fatalf("vec[1] = %f, want %f", vec[1], wantD1)
	}

	// Verify L2 normalization: vector should have unit norm.
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1.0) > 1e-5 {
		t.Fatalf("expected unit norm, got %f", norm)
	}
}

func TestMeanPool_ZeroMask(t *testing.T) {
	t.Parallel()
	// All masked: should produce zero vector (no normalization on zero).
	hidden := make([]float32, hiddenSize)
	hidden[0] = 5.0
	mask := []int64{0}

	results, err := meanPool(hidden, mask, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, v := range results[0] {
		if v != 0 {
			t.Fatalf("expected zero at dim %d, got %f", i, v)
		}
	}
}
