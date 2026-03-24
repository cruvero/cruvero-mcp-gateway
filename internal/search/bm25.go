package search

import (
	"context"
	"math"
	"sort"
	"sync"
)

// BM25 tuning constants.
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// Field weight multipliers for multi-field scoring.
const (
	weightName        = 3.0
	weightTitle       = 2.0
	weightDescription = 1.0
	weightTags        = 2.0
)

// fieldIndex holds an inverted index for a single document field.
// The outer key is the term; the inner map is docIndex → term frequency.
type fieldIndex struct {
	postings  map[string]map[int]int
	docLens   []int
	avgDocLen float64
}

// BM25Engine implements Engine using Okapi BM25 scoring with multi-field
// support and synonym expansion. Thread-safe via sync.RWMutex.
type BM25Engine struct {
	mu       sync.RWMutex
	docs     []Document
	synonyms SynonymProvider

	nameIdx  fieldIndex
	titleIdx fieldIndex
	descIdx  fieldIndex
	tagsIdx  fieldIndex
}

// NewBM25Engine creates a new BM25 search engine with the static synonym provider.
func NewBM25Engine() *BM25Engine {
	return &BM25Engine{synonyms: StaticSynonymProvider{}}
}

// NewBM25EngineWithSynonyms creates a BM25 engine with a custom synonym provider.
func NewBM25EngineWithSynonyms(provider SynonymProvider) *BM25Engine {
	if provider == nil {
		provider = StaticSynonymProvider{}
	}
	return &BM25Engine{synonyms: provider}
}

// Index replaces all documents and rebuilds the inverted indexes.
func (e *BM25Engine) Index(_ context.Context, docs []Document) error {
	nameIdx := newFieldIndex(len(docs))
	titleIdx := newFieldIndex(len(docs))
	descIdx := newFieldIndex(len(docs))
	tagsIdx := newFieldIndex(len(docs))

	for i, doc := range docs {
		indexField(&nameIdx, i, Tokenize(doc.Name))
		indexField(&titleIdx, i, Tokenize(doc.Title))
		indexField(&descIdx, i, Tokenize(doc.Description))

		var tagTokens []string
		for _, tag := range doc.Tags {
			tagTokens = append(tagTokens, Tokenize(tag)...)
		}
		indexField(&tagsIdx, i, tagTokens)
	}

	computeAvgDocLen(&nameIdx)
	computeAvgDocLen(&titleIdx)
	computeAvgDocLen(&descIdx)
	computeAvgDocLen(&tagsIdx)

	e.mu.Lock()
	e.docs = docs
	e.nameIdx = nameIdx
	e.titleIdx = titleIdx
	e.descIdx = descIdx
	e.tagsIdx = tagsIdx
	e.mu.Unlock()

	return nil
}

// Search tokenizes the query, expands synonyms, and returns matching
// documents sorted by descending BM25 score. Limit controls the maximum
// number of results; a value <= 0 means no limit.
func (e *BM25Engine) Search(_ context.Context, query string, limit int) ([]ScoredResult, error) {
	tokens := Tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	expanded := ExpandQueryWith(tokens, e.synonyms)

	e.mu.RLock()
	defer e.mu.RUnlock()

	n := len(e.docs)
	if n == 0 {
		return nil, nil
	}

	scores := make([]float64, n)

	for _, term := range expanded {
		accumulateField(scores, e.nameIdx, term, n, weightName)
		accumulateField(scores, e.titleIdx, term, n, weightTitle)
		accumulateField(scores, e.descIdx, term, n, weightDescription)
		accumulateField(scores, e.tagsIdx, term, n, weightTags)
	}

	var results []ScoredResult
	for i, score := range scores {
		if score > 0 {
			results = append(results, ScoredResult{
				Key:   e.docs[i].Key,
				Score: score,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Key < results[j].Key
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// Remove removes a document by key and rebuilds the index without it.
func (e *BM25Engine) Remove(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx := -1
	for i, doc := range e.docs {
		if doc.Key == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}

	remaining := make([]Document, 0, len(e.docs)-1)
	remaining = append(remaining, e.docs[:idx]...)
	remaining = append(remaining, e.docs[idx+1:]...)
	e.docs = remaining

	e.rebuildIndexesLocked()
}

// Ready always returns true for BM25Engine (no external dependencies).
func (e *BM25Engine) Ready() bool {
	return true
}

func (e *BM25Engine) rebuildIndexesLocked() {
	nameIdx := newFieldIndex(len(e.docs))
	titleIdx := newFieldIndex(len(e.docs))
	descIdx := newFieldIndex(len(e.docs))
	tagsIdx := newFieldIndex(len(e.docs))

	for i, doc := range e.docs {
		indexField(&nameIdx, i, Tokenize(doc.Name))
		indexField(&titleIdx, i, Tokenize(doc.Title))
		indexField(&descIdx, i, Tokenize(doc.Description))

		var tagTokens []string
		for _, tag := range doc.Tags {
			tagTokens = append(tagTokens, Tokenize(tag)...)
		}
		indexField(&tagsIdx, i, tagTokens)
	}

	computeAvgDocLen(&nameIdx)
	computeAvgDocLen(&titleIdx)
	computeAvgDocLen(&descIdx)
	computeAvgDocLen(&tagsIdx)

	e.nameIdx = nameIdx
	e.titleIdx = titleIdx
	e.descIdx = descIdx
	e.tagsIdx = tagsIdx
}

func newFieldIndex(docCount int) fieldIndex {
	return fieldIndex{
		postings: make(map[string]map[int]int),
		docLens:  make([]int, docCount),
	}
}

func indexField(fi *fieldIndex, docIdx int, tokens []string) {
	fi.docLens[docIdx] = len(tokens)
	for _, tok := range tokens {
		if fi.postings[tok] == nil {
			fi.postings[tok] = make(map[int]int)
		}
		fi.postings[tok][docIdx]++
	}
}

func computeAvgDocLen(fi *fieldIndex) {
	if len(fi.docLens) == 0 {
		return
	}
	total := 0
	for _, dl := range fi.docLens {
		total += dl
	}
	fi.avgDocLen = float64(total) / float64(len(fi.docLens))
}

// accumulateField adds BM25 scores for a single term across one field to the
// per-document score array.
func accumulateField(scores []float64, fi fieldIndex, term string, n int, weight float64) {
	posting, ok := fi.postings[term]
	if !ok {
		return
	}

	df := len(posting)
	idf := math.Log((float64(n)-float64(df)+0.5)/(float64(df)+0.5) + 1.0)

	avgdl := fi.avgDocLen
	if avgdl == 0 {
		avgdl = 1
	}

	for docIdx, tf := range posting {
		dl := float64(fi.docLens[docIdx])
		tfScore := (float64(tf) * (bm25K1 + 1)) / (float64(tf) + bm25K1*(1-bm25B+bm25B*dl/avgdl))
		scores[docIdx] += weight * idf * tfScore
	}
}
