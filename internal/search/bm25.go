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

// defaultPartitionID is the key used when Index() replaces all documents
// in a single flat partition (backward-compatible path).
const defaultPartitionID = "__default__"

// fieldIndex holds an inverted index for a single document field.
// The outer key is the term; the inner map is docIndex -> term frequency.
type fieldIndex struct {
	postings  map[string]map[int]int
	docLens   []int
	avgDocLen float64
}

// bm25Partition groups documents and field indexes for a single server.
type bm25Partition struct {
	serverID string
	docs     []Document
	nameIdx  fieldIndex
	titleIdx fieldIndex
	descIdx  fieldIndex
	tagsIdx  fieldIndex
}

// BM25Engine implements Engine using Okapi BM25 scoring with multi-field
// support, synonym expansion, and server-partitioned indexes.
// Thread-safe via sync.RWMutex.
type BM25Engine struct {
	mu         sync.RWMutex
	partitions map[string]*bm25Partition
	synonyms   SynonymProvider

	// globalIDF is recomputed eagerly whenever partitions change.
	globalIDF map[string]float64
}

// NewBM25Engine creates a new BM25 search engine with the static synonym provider.
func NewBM25Engine() *BM25Engine {
	return &BM25Engine{
		synonyms:   StaticSynonymProvider{},
		partitions: make(map[string]*bm25Partition),
		globalIDF:  make(map[string]float64),
	}
}

// NewBM25EngineWithSynonyms creates a BM25 engine with a custom synonym provider.
func NewBM25EngineWithSynonyms(provider SynonymProvider) *BM25Engine {
	if provider == nil {
		provider = StaticSynonymProvider{}
	}
	return &BM25Engine{
		synonyms:   provider,
		partitions: make(map[string]*bm25Partition),
		globalIDF:  make(map[string]float64),
	}
}

// Index replaces all documents and rebuilds the inverted indexes.
// This clears all partitions and stores everything in a single default
// partition, preserving backward compatibility.
func (e *BM25Engine) Index(_ context.Context, docs []Document) error {
	p := buildPartition(defaultPartitionID, docs)

	e.mu.Lock()
	e.partitions = map[string]*bm25Partition{defaultPartitionID: p}
	e.recomputeGlobalIDFLocked()
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

	if len(e.partitions) == 0 {
		return nil, nil
	}

	// Accumulate scores keyed by document key.
	scores := make(map[string]float64)

	for _, p := range e.partitions {
		n := len(p.docs)
		if n == 0 {
			continue
		}
		partitionScores := make([]float64, n)
		for _, term := range expanded {
			idf := e.globalIDF[term]
			accumulateFieldPartitioned(partitionScores, p.nameIdx, term, idf, weightName)
			accumulateFieldPartitioned(partitionScores, p.titleIdx, term, idf, weightTitle)
			accumulateFieldPartitioned(partitionScores, p.descIdx, term, idf, weightDescription)
			accumulateFieldPartitioned(partitionScores, p.tagsIdx, term, idf, weightTags)
		}
		for i, score := range partitionScores {
			if score > 0 {
				scores[p.docs[i].Key] += score
			}
		}
	}

	var results []ScoredResult
	for key, score := range scores {
		results = append(results, ScoredResult{Key: key, Score: score})
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

// Remove removes a document by key. It scans all partitions and rebuilds
// the affected partition without the removed document.
func (e *BM25Engine) Remove(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for pid, p := range e.partitions {
		idx := -1
		for i, doc := range p.docs {
			if doc.Key == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}

		remaining := make([]Document, 0, len(p.docs)-1)
		remaining = append(remaining, p.docs[:idx]...)
		remaining = append(remaining, p.docs[idx+1:]...)

		if len(remaining) == 0 {
			delete(e.partitions, pid)
		} else {
			e.partitions[pid] = buildPartition(pid, remaining)
		}
		e.recomputeGlobalIDFLocked()
		return
	}
}

// AddPartition adds or replaces documents for a server partition.
func (e *BM25Engine) AddPartition(serverID string, docs []Document) error {
	p := buildPartition(serverID, docs)

	e.mu.Lock()
	e.partitions[serverID] = p
	e.recomputeGlobalIDFLocked()
	e.mu.Unlock()

	return nil
}

// RemovePartition removes all documents for a server.
func (e *BM25Engine) RemovePartition(serverID string) {
	e.mu.Lock()
	delete(e.partitions, serverID)
	e.recomputeGlobalIDFLocked()
	e.mu.Unlock()
}

// UpdatePartition is equivalent to AddPartition (full replace of docs for a server).
func (e *BM25Engine) UpdatePartition(serverID string, docs []Document) error {
	return e.AddPartition(serverID, docs)
}

// Ready always returns true for BM25Engine (no external dependencies).
func (e *BM25Engine) Ready() bool {
	return true
}

// recomputeGlobalIDFLocked recalculates IDF across all partitions.
// Must be called under a write lock.
func (e *BM25Engine) recomputeGlobalIDFLocked() {
	totalDocs := 0
	// Track unique (partition, docIdx) pairs per term to compute true document frequency.
	type docRef struct {
		partitionID string
		docIdx      int
	}
	termDocSets := make(map[string]map[docRef]struct{})

	for _, p := range e.partitions {
		totalDocs += len(p.docs)
		fields := []*fieldIndex{&p.nameIdx, &p.titleIdx, &p.descIdx, &p.tagsIdx}
		for _, fi := range fields {
			for term, postings := range fi.postings {
				if termDocSets[term] == nil {
					termDocSets[term] = make(map[docRef]struct{})
				}
				for docIdx := range postings {
					termDocSets[term][docRef{p.serverID, docIdx}] = struct{}{}
				}
			}
		}
	}

	idf := make(map[string]float64, len(termDocSets))
	n := float64(totalDocs)
	for term, docSet := range termDocSets {
		df := float64(len(docSet))
		idf[term] = math.Log((n-df+0.5)/(df+0.5) + 1.0)
	}
	e.globalIDF = idf
}

// buildPartition creates a fully-indexed partition from documents.
func buildPartition(serverID string, docs []Document) *bm25Partition {
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

	return &bm25Partition{
		serverID: serverID,
		docs:     docs,
		nameIdx:  nameIdx,
		titleIdx: titleIdx,
		descIdx:  descIdx,
		tagsIdx:  tagsIdx,
	}
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

// accumulateFieldPartitioned adds BM25 scores for a single term across one field,
// using a pre-computed global IDF value.
func accumulateFieldPartitioned(scores []float64, fi fieldIndex, term string, idf float64, weight float64) {
	posting, ok := fi.postings[term]
	if !ok {
		return
	}

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

// accumulateField adds BM25 scores for a single term across one field to the
// per-document score array. Kept for backward compatibility with any external callers.
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
