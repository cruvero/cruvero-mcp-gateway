package search

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

const hiddenSize = 384

// OnnxEmbedder implements Embedder using an in-process ONNX model loaded from
// disk. It replaces the previous approach that relied on
// all-minilm-l6-v2-go (whose model.onnx was broken by a Git LFS pointer in
// the Go module cache).
type OnnxEmbedder struct {
	mu      sync.Mutex
	tk      *tokenizer.Tokenizer
	session *ort.DynamicAdvancedSession
	ready   atomic.Bool
}

// NewOnnxEmbedder loads the all-MiniLM-L6-v2 ONNX model and tokenizer from
// the given file paths, using the specified ONNX Runtime shared library.
func NewOnnxEmbedder(runtimePath, modelPath, tokenizerPath string) (*OnnxEmbedder, error) {
	tkData, err := os.ReadFile(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("read tokenizer %s: %w", tokenizerPath, err)
	}
	tk, err := pretrained.FromReader(bytes.NewBuffer(tkData))
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	ort.SetSharedLibraryPath(runtimePath)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("init onnx runtime: %w", err)
	}
	envCleanup := true
	defer func() {
		if envCleanup {
			_ = ort.DestroyEnvironment()
		}
	}()

	modelData, err := os.ReadFile(modelPath)
	if err != nil {
		return nil, fmt.Errorf("read model %s: %w", modelPath, err)
	}

	inputNames := []string{"input_ids", "attention_mask", "token_type_ids"}
	outputNames := []string{"last_hidden_state"}

	session, err := ort.NewDynamicAdvancedSessionWithONNXData(modelData, inputNames, outputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("create onnx session: %w", err)
	}

	envCleanup = false
	e := &OnnxEmbedder{tk: tk, session: session}
	e.ready.Store(true)
	return e, nil
}

// Embed returns a vector embedding for each input text.
func (e *OnnxEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.ready.Load() {
		return nil, fmt.Errorf("embedder is closed")
	}
	return e.computeBatch(texts)
}

// Ready reports whether the ONNX model is loaded and operational.
func (e *OnnxEmbedder) Ready(_ context.Context) bool {
	return e.ready.Load()
}

// Close shuts down the ONNX runtime session.
func (e *OnnxEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ready.Store(false)
	if e.session != nil {
		_ = e.session.Destroy()
	}
	return ort.DestroyEnvironment()
}

func (e *OnnxEmbedder) computeBatch(sentences []string) ([][]float32, error) {
	inputBatch := make([]tokenizer.EncodeInput, len(sentences))
	for i, s := range sentences {
		inputBatch[i] = tokenizer.NewSingleEncodeInput(tokenizer.NewInputSequence(s))
	}

	encodings, err := e.tk.EncodeBatch(inputBatch, true)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}
	if len(encodings) == 0 {
		return nil, nil
	}

	batchSize := len(encodings)
	// EncodeBatch pads all sequences to the longest in the batch (configured
	// in the tokenizer.json padding strategy), so seqLength is uniform.
	seqLength := len(encodings[0].Ids)
	inputShape := ort.NewShape(int64(batchSize), int64(seqLength))

	inputIDs := make([]int64, batchSize*seqLength)
	attentionMask := make([]int64, batchSize*seqLength)
	tokenTypeIDs := make([]int64, batchSize*seqLength)

	for b := range batchSize {
		off := b * seqLength
		for i, id := range encodings[b].Ids {
			inputIDs[off+i] = int64(id)
		}
		for i, mask := range encodings[b].AttentionMask {
			attentionMask[off+i] = int64(mask)
		}
		for i, typeID := range encodings[b].TypeIds {
			tokenTypeIDs[off+i] = int64(typeID)
		}
	}

	inputIDsTensor, err := ort.NewTensor(inputShape, inputIDs)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer func() { _ = inputIDsTensor.Destroy() }()

	attentionMaskTensor, err := ort.NewTensor(inputShape, attentionMask)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer func() { _ = attentionMaskTensor.Destroy() }()

	tokenTypeIDsTensor, err := ort.NewTensor(inputShape, tokenTypeIDs)
	if err != nil {
		return nil, fmt.Errorf("create token_type_ids tensor: %w", err)
	}
	defer func() { _ = tokenTypeIDsTensor.Destroy() }()

	// The HuggingFace model outputs per-token embeddings (batch, seq_len, hidden).
	outputShape := ort.NewShape(int64(batchSize), int64(seqLength), hiddenSize)
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer func() { _ = outputTensor.Destroy() }()

	err = e.session.Run(
		[]ort.Value{inputIDsTensor, attentionMaskTensor, tokenTypeIDsTensor},
		[]ort.Value{outputTensor},
	)
	if err != nil {
		return nil, fmt.Errorf("run inference: %w", err)
	}

	flat := outputTensor.GetData()
	return meanPool(flat, attentionMask, batchSize, seqLength)
}

// meanPool applies attention-mask-weighted mean pooling over the token
// dimension to produce a single sentence embedding per batch element, then
// L2-normalizes each vector.
func meanPool(hidden []float32, mask []int64, batchSize, seqLen int) ([][]float32, error) {
	expected := batchSize * seqLen * hiddenSize
	if len(hidden) != expected {
		return nil, fmt.Errorf("output size mismatch: got %d, want %d", len(hidden), expected)
	}

	results := make([][]float32, batchSize)
	for b := range batchSize {
		vec := make([]float32, hiddenSize)
		var maskSum float32
		for t := range seqLen {
			m := float32(mask[b*seqLen+t])
			maskSum += m
			off := (b*seqLen + t) * hiddenSize
			for h := range hiddenSize {
				vec[h] += hidden[off+h] * m
			}
		}
		if maskSum > 0 {
			for h := range hiddenSize {
				vec[h] /= maskSum
			}
		}
		// L2-normalize for cosine similarity.
		var norm float64
		for _, v := range vec {
			norm += float64(v) * float64(v)
		}
		if norm > 0 {
			invNorm := float32(1.0 / math.Sqrt(norm))
			for h := range vec {
				vec[h] *= invNorm
			}
		}
		results[b] = vec
	}
	return results, nil
}
