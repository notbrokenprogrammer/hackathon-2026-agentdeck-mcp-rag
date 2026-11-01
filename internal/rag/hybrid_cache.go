package rag

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
)

const embeddingCacheVersion = 1
const maxEmbeddingCacheBytes = 256 << 20

type embeddingCacheEntry struct {
	ID      string      `json:"id"`
	Hash    string      `json:"input_hash"`
	Vectors [][]float32 `json:"vectors"`
}

type embeddingCache struct {
	Version    int                   `json:"version"`
	Model      string                `json:"model"`
	CorpusHash string                `json:"corpus_hash"`
	Entries    []embeddingCacheEntry `json:"entries"`
}

// A cache contains fingerprints and vectors, never source text or queries.
// Validate the whole file before admitting any entry. Mixing vectors from a
// different model/corpus/input format would quietly corrupt cosine rankings.
func (index *hybridIndex) readCache() (*embeddingCache, error) {
	if index.config.CachePath == "" {
		return nil, os.ErrNotExist
	}
	f, err := os.Open(index.config.CachePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() > maxEmbeddingCacheBytes || !st.Mode().IsRegular() {
		return nil, embeddingError("embedding_cache_invalid")
	}
	var cache embeddingCache
	decoder := json.NewDecoder(io.LimitReader(f, maxEmbeddingCacheBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cache); err != nil {
		return nil, embeddingError("embedding_cache_invalid")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || cache.Version != embeddingCacheVersion || cache.Model != index.config.Model || cache.CorpusHash != index.corpusHash || len(cache.Entries) > len(index.inputs) {
		return nil, embeddingError("embedding_cache_invalid")
	}
	seen := map[string]bool{}
	dimension, values := 0, 0
	for _, entry := range cache.Entries {
		info, ok := index.inputs[entry.ID]
		if !ok || seen[entry.ID] || info.hash != entry.Hash || len(entry.Vectors) != len(info.inputs) {
			return nil, embeddingError("embedding_cache_invalid")
		}
		seen[entry.ID] = true
		for _, vector := range entry.Vectors {
			if len(vector) == 0 || len(vector) > maxEmbeddingDimension || dimension != 0 && len(vector) != dimension {
				return nil, embeddingError("embedding_cache_invalid")
			}
			dimension = len(vector)
			values += len(vector)
			if values > maxEmbeddingValues {
				return nil, embeddingError("embedding_cache_invalid")
			}
			norm := 0.0
			for _, value := range vector {
				norm += float64(value) * float64(value)
			}
			if math.IsNaN(norm) || math.IsInf(norm, 0) || math.Abs(norm-1) > 0.001 {
				return nil, embeddingError("embedding_cache_invalid")
			}
		}
	}
	return &cache, nil
}

func (index *hybridIndex) loadCache() {
	cache, err := index.readCache()
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		index.code = "embedding_cache_invalid"
		return
	}
	for _, entry := range cache.Entries {
		if err := index.addVectors(entry.ID, entry.Vectors); err != nil {
			index.code = "embedding_cache_invalid"
			return
		}
	}
}

func (index *hybridIndex) mergeCache() {
	cache, err := index.readCache()
	if err != nil {
		return
	}
	for _, entry := range cache.Entries {
		// Existing immutable vectors always win. A concurrent process may
		// contribute other validated entries for this exact model/corpus.
		_ = index.addVectors(entry.ID, entry.Vectors)
	}
}

func (index *hybridIndex) saveCache() error {
	path := index.config.CachePath
	if path == "" {
		return nil
	}
	// Merge immediately before writing to avoid replacing a fuller shared
	// cache with a stale partial snapshot in the usual app/MCP case. Atomic
	// rename ensures readers in other processes never see a partial JSON file.
	// Simultaneous writers can at worst lose cached work, never mix models or
	// grant access: every subsequent reader revalidates all fingerprints.
	index.mergeCache()
	cache := embeddingCache{Version: embeddingCacheVersion, Model: index.config.Model, CorpusHash: index.corpusHash}
	index.mu.RLock()
	for id, vectors := range index.vectors {
		cache.Entries = append(cache.Entries, embeddingCacheEntry{ID: id, Hash: index.inputs[id].hash, Vectors: vectors})
	}
	index.mu.RUnlock()
	sort.Slice(cache.Entries, func(i, j int) bool { return cache.Entries[i].ID < cache.Entries[j].ID })
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".rag-vectors-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(cache); err != nil {
		return err
	}
	if st, err := f.Stat(); err != nil || st.Size() > maxEmbeddingCacheBytes {
		return embeddingError("embedding_cache_unwritable")
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
