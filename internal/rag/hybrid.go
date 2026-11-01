package rag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// HybridConfig uses a local Ollama embedding model. HTTPClient is injectable for
// tests; no chat model, synthetic vector or lexical approximation is substituted.
type HybridConfig struct {
	URL, Model, CachePath string
	HTTPClient            *http.Client
	QueryTimeout          time.Duration
	BatchSize             int
}

type HybridStatus struct {
	Configured bool   `json:"configured"`
	Ready      bool   `json:"ready"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Model      string `json:"model,omitempty"`
	Count      int    `json:"count"`
	Total      int    `json:"total"`
}

// A pointer is shared by Store.Scope; the Store itself stays safe to copy.
type hybridRef struct {
	mu    sync.RWMutex
	index *hybridIndex
}

type embeddingDocument struct {
	hash   string
	inputs []string
}

type hybridIndex struct {
	config       HybridConfig
	corpusHash   string
	docs         []document
	inputs       map[string]embeddingDocument
	prepare      chan struct{}
	requests     chan struct{}
	mu           sync.RWMutex
	vectors      map[string][][]float32 // normalized real model outputs, one per part
	dimension    int
	values       int
	code         string
	cacheWarning bool
}

const maxEmbeddingDimension = 8192
const maxEmbeddingValues = 32 << 20 // 128 MiB of vector values per snapshot

// WithHybrid configures an immutable model/corpus snapshot. A corrupt or stale
// cache is discarded and rebuilt by Prepare or the next bounded lazy search.
// Configuration errors fail immediately; model/service errors remain observable
// in HybridStatus and in each search's explicit BM25 fallback metadata.
func (s *Store) WithHybrid(config HybridConfig) error {
	if s.hybrid == nil {
		return errors.New("hybrid retrieval requires a store created by Read or Load")
	}
	u, err := url.Parse(strings.TrimSpace(config.URL))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("embedding URL must be a local HTTP(S) origin")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("embedding URL must use localhost or a loopback IP")
	}
	if port := u.Port(); port != "" {
		p, err := net.LookupPort("tcp", port)
		if err != nil || p < 1 || p > 65535 || strings.IndexFunc(port, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return errors.New("embedding URL port must be 1..65535")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return errors.New("embedding URL port must be 1..65535")
	}
	config.URL = strings.TrimRight(u.String(), "/")
	config.Model = strings.TrimSpace(config.Model)
	if config.Model == "" || len(config.Model) > 256 || strings.IndexFunc(config.Model, unicode.IsSpace) >= 0 || strings.IndexFunc(config.Model, unicode.IsControl) >= 0 {
		return errors.New("embedding model must be a nonempty model name")
	}
	if config.QueryTimeout == 0 {
		config.QueryTimeout = 8 * time.Second
	}
	if config.QueryTimeout < 0 || config.QueryTimeout > time.Minute {
		return errors.New("embedding query timeout must be positive and <= 1 minute")
	}
	if config.BatchSize == 0 {
		config.BatchSize = 8
	}
	if config.BatchSize < 1 || config.BatchSize > 32 {
		return errors.New("embedding batch size must be 1..32")
	}
	client := &http.Client{}
	if config.HTTPClient != nil {
		*client = *config.HTTPClient
	} else {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil // local corpus must never go through HTTP_PROXY
		client.Transport = transport
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	config.HTTPClient = client
	index := &hybridIndex{config: config, docs: s.docs, inputs: map[string]embeddingDocument{}, vectors: map[string][][]float32{}, prepare: make(chan struct{}, 1), requests: make(chan struct{}, 1), code: "embedding_index_not_ready"}
	// Canonical ordering makes cache identity independent of JSONL file order.
	chunks := make([]Chunk, len(s.docs))
	for i, d := range s.docs {
		chunks[i] = d.chunk
		inputs := documentInputs(config.Model, d.chunk)
		b, _ := json.Marshal(inputs)
		index.inputs[d.chunk.ID] = embeddingDocument{hash: digest(b), inputs: inputs}
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ID < chunks[j].ID })
	b, _ := json.Marshal(chunks)
	index.corpusHash = digest(b)
	index.loadCache()
	if len(index.vectors) == len(s.docs) {
		index.code = "ready"
	}
	s.hybrid.mu.Lock()
	s.hybrid.index = index
	s.hybrid.mu.Unlock()
	return nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Store) hybridIndex() *hybridIndex {
	if s == nil || s.hybrid == nil {
		return nil
	}
	s.hybrid.mu.RLock()
	defer s.hybrid.mu.RUnlock()
	return s.hybrid.index
}

func (s *Store) HybridStatus() HybridStatus {
	index := s.hybridIndex()
	if index == nil {
		return HybridStatus{Code: "disabled", Message: "Semantic retrieval is disabled; BM25 is available."}
	}
	index.mu.RLock()
	defer index.mu.RUnlock()
	return HybridStatus{Configured: true, Ready: (index.code == "ready" || index.code == "embedding_cache_unwritable") && len(index.vectors) == len(index.docs), Code: index.code, Message: statusMessage(index.code), Model: index.config.Model, Count: len(index.vectors), Total: len(index.docs)}
}

func statusMessage(code string) string {
	switch code {
	case "ready":
		return "Semantic index is ready; searches fuse vector similarity and BM25."
	case "embedding_model_missing":
		return "The configured embedding model is not installed in Ollama."
	case "embedding_service_unavailable":
		return "The local Ollama embedding service is unavailable."
	case "embedding_timeout":
		return "The embedding request timed out; BM25 remains available."
	case "embedding_response_invalid":
		return "Ollama returned an invalid embedding response."
	case "embedding_cache_invalid":
		return "The embedding cache is invalid or stale and will be rebuilt."
	case "embedding_cache_unwritable":
		return "The embedding cache could not be saved; in-memory vectors remain available."
	default:
		return "The semantic index is being prepared; BM25 remains available."
	}
}

type embeddingError string

func (e embeddingError) Error() string { return statusMessage(string(e)) }
func errorCode(err error) string {
	var e embeddingError
	if errors.As(err, &e) {
		return string(e)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "embedding_timeout"
	}
	return "embedding_service_unavailable"
}

func (index *hybridIndex) record(err error) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if err != nil {
		index.code = errorCode(err)
	} else if len(index.vectors) == len(index.docs) {
		index.code = "ready"
		if index.cacheWarning {
			index.code = "embedding_cache_unwritable"
		}
	} else {
		index.code = "embedding_index_not_ready"
	}
}

// Prepare is intended for application startup in a cancellable background job.
// It indexes the complete trusted local corpus. Search still filters permissions
// before selecting vectors, and lazy Search only embeds eligible documents.
func (s *Store) Prepare(ctx context.Context) error {
	index := s.hybridIndex()
	if index == nil {
		return nil
	}
	err := index.ensure(ctx, index.docs)
	if ctx.Err() == nil {
		index.record(err)
	}
	return err
}

func (index *hybridIndex) hasAll(docs []document) bool {
	index.mu.RLock()
	defer index.mu.RUnlock()
	for _, d := range docs {
		if _, ok := index.vectors[d.chunk.ID]; !ok {
			return false
		}
	}
	return true
}

func acquire(ctx context.Context, gate chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (index *hybridIndex) ensure(ctx context.Context, docs []document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if index.hasAll(docs) {
		return nil
	}
	if err := acquire(ctx, index.prepare); err != nil {
		return err
	}
	defer func() { <-index.prepare }()
	// Another request or process may already have populated the missing parts.
	index.mergeCache()
	type part struct {
		id     string
		number int
	}
	inputs := []string{}
	parts := []part{}
	pending := map[string][][]float32{}
	index.mu.RLock()
	for _, d := range docs {
		if _, ok := index.vectors[d.chunk.ID]; ok {
			continue
		}
		info := index.inputs[d.chunk.ID]
		pending[d.chunk.ID] = make([][]float32, len(info.inputs))
		for j, input := range info.inputs {
			inputs = append(inputs, input)
			parts = append(parts, part{d.chunk.ID, j})
		}
	}
	index.mu.RUnlock()
	for first := 0; first < len(inputs); first += index.config.BatchSize {
		last := min(first+index.config.BatchSize, len(inputs))
		// A startup Prepare may take minutes, but no individual HTTP call is
		// unbounded. Per-batch gating lets already indexed searches interleave.
		batchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		vectors, err := index.embed(batchCtx, inputs[first:last])
		cancel()
		if err != nil {
			return err
		}
		for j, vector := range vectors {
			p := parts[first+j]
			pending[p.id][p.number] = vector
			if p.number == len(pending[p.id])-1 {
				if err := index.addVectors(p.id, pending[p.id]); err != nil {
					return err
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(inputs) > 0 {
		if err := index.saveCache(); err != nil {
			// Persistence is not part of semantic correctness. A successful query
			// still reports hybrid, while status exposes the cache failure.
			index.mu.Lock()
			index.cacheWarning = true
			index.mu.Unlock()
		} else {
			index.mu.Lock()
			index.cacheWarning = false
			index.mu.Unlock()
		}
	}
	return nil
}

func (index *hybridIndex) addVectors(id string, vectors [][]float32) error {
	index.mu.Lock()
	defer index.mu.Unlock()
	if _, exists := index.vectors[id]; exists {
		return nil
	}
	dimension := len(vectors[0])
	if index.dimension != 0 && index.dimension != dimension || index.values+dimension*len(vectors) > maxEmbeddingValues {
		return embeddingError("embedding_response_invalid")
	}
	index.dimension = dimension
	index.values += dimension * len(vectors)
	index.vectors[id] = vectors
	return nil
}

func (index *hybridIndex) embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if err := acquire(ctx, index.requests); err != nil {
		return nil, err
	}
	defer func() { <-index.requests }()
	body, _ := json.Marshal(struct {
		Model    string   `json:"model"`
		Input    []string `json:"input"`
		Truncate bool     `json:"truncate"`
	}{index.config.Model, inputs, false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, index.config.URL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, embeddingError("embedding_service_unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := index.config.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, embeddingError("embedding_service_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, embeddingError("embedding_model_missing")
	}
	if response.StatusCode != http.StatusOK {
		return nil, embeddingError("embedding_service_unavailable")
	}
	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
	if err := decoder.Decode(&result); err != nil || len(result.Embeddings) != len(inputs) {
		return nil, embeddingError("embedding_response_invalid")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, embeddingError("embedding_response_invalid")
	}
	vectors := make([][]float32, len(result.Embeddings))
	dimension := 0
	for i, raw := range result.Embeddings {
		v, err := normalize(raw)
		if err != nil || dimension != 0 && len(v) != dimension {
			return nil, embeddingError("embedding_response_invalid")
		}
		dimension = len(v)
		vectors[i] = v
	}
	index.mu.RLock()
	expected := index.dimension
	index.mu.RUnlock()
	if expected != 0 && dimension != expected {
		return nil, embeddingError("embedding_response_invalid")
	}
	return vectors, nil
}

func normalize(raw []float64) ([]float32, error) {
	if len(raw) == 0 || len(raw) > maxEmbeddingDimension {
		return nil, embeddingError("embedding_response_invalid")
	}
	norm := 0.0
	for _, value := range raw {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, embeddingError("embedding_response_invalid")
		}
		norm = math.Hypot(norm, value)
	}
	if norm == 0 || math.IsInf(norm, 0) {
		return nil, embeddingError("embedding_response_invalid")
	}
	out := make([]float32, len(raw))
	for i, value := range raw {
		out[i] = float32(value / norm)
	}
	return out, nil
}

func (index *hybridIndex) search(ctx context.Context, docs []document, query string) ([]Hit, error) {
	bounded, cancel := context.WithTimeout(ctx, index.config.QueryTimeout)
	defer cancel()
	if err := index.ensure(bounded, docs); err != nil {
		if ctx.Err() == nil {
			index.record(err)
		}
		return nil, err
	}
	queries, err := index.embed(bounded, queryInputs(index.config.Model, query))
	if err != nil {
		if ctx.Err() == nil {
			index.record(err)
		}
		return nil, err
	}
	hits := []Hit{}
	index.mu.RLock()
	for _, d := range docs {
		if err = bounded.Err(); err != nil {
			break
		}
		best := 0.0
		for _, v := range index.vectors[d.chunk.ID] {
			for _, q := range queries {
				score := 0.0
				for j, value := range v {
					score += float64(value) * float64(q[j])
				}
				best = math.Max(best, score)
			}
		}
		if best > 0 {
			hits = append(hits, Hit{clone(d.chunk), best})
		}
	}
	index.mu.RUnlock()
	if err != nil {
		index.record(err)
		return nil, err
	}
	index.record(nil)
	sortHits(hits)
	return hits, nil
}

func sortHits(hits []Hit) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].ID < hits[j].ID
		}
		return hits[i].Score > hits[j].Score
	})
}

// Reciprocal rank fusion combines rankings, not incomparable raw BM25/cosine
// scales. Both lists already contain only permitted agent/topic documents.
func fuseRanks(lexical, semantic []Hit, candidates int) []Hit {
	merged := map[string]Hit{}
	for _, list := range [][]Hit{lexical, semantic} {
		for rank, hit := range list[:min(len(list), candidates)] {
			entry, ok := merged[hit.ID]
			if !ok {
				entry.Chunk = hit.Chunk
			}
			entry.Score += 1.0 / (60 + float64(rank+1))
			merged[hit.ID] = entry
		}
	}
	out := make([]Hit, 0, len(merged))
	for _, hit := range merged {
		out = append(out, hit)
	}
	sortHits(out)
	return out
}

func gemma(model string) bool {
	name := strings.TrimSuffix(strings.Split(model, ":")[0], "/")
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	return strings.EqualFold(name, "embeddinggemma")
}

func documentInputs(model string, chunk Chunk) []string {
	prefix := chunk.Source.Title + "\n\n"
	if gemma(model) {
		prefix = "title: " + chunk.Source.Title + " | text: "
	}
	// 1,200 text bytes + at most 530 prefix bytes conservatively fits the
	// 2,048-token EmbeddingGemma context even for byte-fallback tokenization.
	// Each part keeps its own vector, so text near the tail is never discarded
	// or diluted by averaging unrelated sections of a long source chunk.
	parts := splitBytes(chunk.Text, 1200)
	for i := range parts {
		parts[i] = prefix + parts[i]
	}
	return parts
}

func queryInputs(model, query string) []string {
	parts := splitBytes(query, 1400)
	if gemma(model) {
		for i := range parts {
			parts[i] = "task: search result | query: " + parts[i]
		}
	}
	return parts
}

func splitBytes(text string, size int) []string {
	parts := []string{}
	for len(text) > size {
		cut := size
		for !utf8.RuneStart(text[cut]) {
			cut--
		}
		// Prefer a word boundary without dropping any characters.
		for i := cut; i > cut/2; i-- {
			if text[i-1] == ' ' || text[i-1] == '\n' {
				cut = i
				break
			}
		}
		parts = append(parts, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}
