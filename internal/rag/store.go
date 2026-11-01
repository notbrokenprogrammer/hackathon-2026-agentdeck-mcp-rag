// Package rag provides immutable, role-scoped local retrieval over JSONL chunks.
package rag

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"feeders/internal/crew"
	"feeders/pkg/protocol"
)

const MaxChunkBytes = 16000

type Source struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	URL              string `json:"url,omitempty"`
	Locator          string `json:"locator,omitempty"`
	ContentMode      string `json:"content_mode,omitempty"`
	RelationToPerson string `json:"relation_to_person,omitempty"`
	Language         string `json:"language,omitempty"`
}
type Chunk struct {
	ID            string   `json:"id"`
	Text          string   `json:"text"`
	Agents        []string `json:"agents"`
	Topics        []string `json:"topics,omitempty"`
	Source        Source   `json:"source"`
	RequiresFacts []string `json:"requires_facts,omitempty"`
}
type Hit struct {
	Chunk
	Score float64 `json:"score"`
}
type SearchResult struct {
	Status    string    `json:"status"`
	Hits      []Hit     `json:"hits"`
	Retrieval Retrieval `json:"retrieval"`
}

// Retrieval reports the algorithm actually used for this particular search.
// Configuring embeddings alone never makes a result hybrid.
type Retrieval struct {
	Mode           string `json:"mode"`
	EmbeddingModel string `json:"embedding_model,omitempty"`
	DegradedReason string `json:"degraded_reason,omitempty"`
}
type document struct {
	chunk  Chunk
	terms  map[string]int
	length int
}
type Store struct {
	docs   []document
	byID   map[string]int
	facts  map[string]bool
	hybrid *hybridRef
}

// Missing file is an explicit empty corpus. Invalid existing files fail startup.
// The snapshot is loaded once, so readers never see a half-written replacement.
func Load(path string) (*Store, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Read(strings.NewReader(""))
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 32<<20 {
		return nil, errors.New("RAG file exceeds 32 MiB")
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		readers := []io.Reader{}
		files := []*os.File{}
		defer func() {
			for _, f := range files {
				f.Close()
			}
		}()
		var size int64
		for _, e := range entries {
			if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			f, err := os.Open(filepath.Join(path, e.Name()))
			if err != nil {
				return nil, err
			}
			files = append(files, f)
			st, err := f.Stat()
			if err != nil {
				return nil, err
			}
			size += st.Size()
			if size > 32<<20 {
				return nil, errors.New("RAG directory exceeds 32 MiB")
			}
			readers = append(readers, f, strings.NewReader("\n"))
		}
		return Read(io.MultiReader(readers...))
	}
	return Read(io.LimitReader(f, (32<<20)+1))
}

func Read(r io.Reader) (*Store, error) {
	s := &Store{byID: map[string]int{}, hybrid: &hybridRef{}}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 128<<10)
	line, total := 0, 0
	sources := map[string]Source{}
	for scanner.Scan() {
		line++
		total += len(scanner.Bytes()) + 1
		if total > 32<<20 {
			return nil, errors.New("RAG file exceeds 32 MiB")
		}
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var c Chunk
		if err := protocol.DecodePayload(scanner.Bytes(), &c); err != nil {
			return nil, fmt.Errorf("chunk line %d: %w", line, err)
		}
		if strings.TrimSpace(c.ID) == "" || len(c.ID) > 128 || strings.TrimSpace(c.Text) == "" || len(c.Text) > MaxChunkBytes || strings.TrimSpace(c.Source.ID) == "" || strings.TrimSpace(c.Source.Title) == "" || len(c.Agents) == 0 {
			return nil, fmt.Errorf("chunk line %d: id, text, source.id/title and agents required; text <= %d bytes", line, MaxChunkBytes)
		}
		if len(c.Source.ID) > 128 || len(c.Source.Title) > 512 || len(c.Source.URL) > 2048 || len(c.Source.Locator) > 512 || len(c.Topics) > 32 || len(c.Agents) > 4 {
			return nil, fmt.Errorf("chunk line %d: metadata too large", line)
		}
		if len(c.Source.ContentMode) > 64 || len(c.Source.RelationToPerson) > 64 || len(c.Source.Language) > 16 {
			return nil, fmt.Errorf("chunk line %d: source provenance too large", line)
		}
		for _, topic := range c.Topics {
			if len(topic) > 128 {
				return nil, fmt.Errorf("chunk line %d: topic too large", line)
			}
		}
		if len(c.RequiresFacts) > 16 || strings.ContainsAny(c.ID, "[]\r\n\t ") {
			return nil, fmt.Errorf("chunk line %d: invalid id or requires_facts", line)
		}
		for _, fact := range c.RequiresFacts {
			if strings.TrimSpace(fact) == "" || len(fact) > 128 {
				return nil, fmt.Errorf("chunk line %d: invalid required fact", line)
			}
		}
		seen := map[string]bool{}
		for _, id := range c.Agents {
			if _, ok := crew.Find(id); !ok || seen[id] {
				return nil, fmt.Errorf("chunk line %d: invalid or duplicate agent %q", line, id)
			}
			seen[id] = true
		}
		if _, exists := s.byID[c.ID]; exists {
			return nil, fmt.Errorf("duplicate chunk id %q", c.ID)
		}
		if src, exists := sources[c.Source.ID]; exists && (src.Title != c.Source.Title || src.URL != c.Source.URL || src.ContentMode != c.Source.ContentMode || src.RelationToPerson != c.Source.RelationToPerson || src.Language != c.Source.Language) {
			return nil, fmt.Errorf("inconsistent source %q", c.Source.ID)
		}
		sources[c.Source.ID] = c.Source
		tokens := tokenize(c.Text + " " + c.Source.Title + " " + strings.Join(c.Topics, " "))
		terms := map[string]int{}
		for _, token := range tokens {
			terms[token]++
		}
		s.byID[c.ID] = len(s.docs)
		s.docs = append(s.docs, document{c, terms, len(tokens)})
		if len(s.docs) > 20000 {
			return nil, errors.New("RAG exceeds 20000 chunks")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return s, nil
}
func tokenize(text string) []string {
	words := strings.FieldsFunc(strings.ReplaceAll(strings.ToLower(text), "ё", "е"), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := []string{}
	for _, word := range words {
		if !stopwords[word] {
			out = append(out, normalizedTerm(word))
		}
	}
	return out
}
func allowed(c Chunk, agent string) bool {
	for _, a := range c.Agents {
		if a == agent {
			return true
		}
	}
	return false
}
func clone(c Chunk) Chunk {
	c.Agents = append([]string(nil), c.Agents...)
	c.Topics = append([]string(nil), c.Topics...)
	c.RequiresFacts = append([]string(nil), c.RequiresFacts...)
	return c
}
func (s *Store) Count(agent string) int {
	n := 0
	for _, d := range s.docs {
		if allowed(d.chunk, agent) && s.visible(d.chunk) {
			n++
		}
	}
	return n
}
func (s *Store) Total() int {
	n := 0
	for _, d := range s.docs {
		if s.visible(d.chunk) {
			n++
		}
	}
	return n
}
func (s *Store) Inventory() int { return len(s.docs) }

// Scope grants only facts resolved by the authoritative mission service.
func (s *Store) Scope(facts []string) *Store {
	out := *s
	out.facts = map[string]bool{}
	for _, f := range facts {
		out.facts[f] = true
	}
	return &out
}
func (s *Store) visible(c Chunk) bool {
	for _, f := range c.RequiresFacts {
		if !s.facts[f] {
			return false
		}
	}
	return true
}
func (s *Store) Get(agent, id string) (Chunk, error) {
	if _, ok := crew.Find(agent); !ok {
		return Chunk{}, errors.New("unknown agent")
	}
	i, ok := s.byID[id]
	if !ok || !allowed(s.docs[i].chunk, agent) || !s.visible(s.docs[i].chunk) {
		return Chunk{}, errors.New("chunk not found for this agent")
	}
	return clone(s.docs[i].chunk), nil
}
func (s *Store) Sources(agent string) []Source {
	result := []Source{}
	seen := map[string]bool{}
	for _, d := range s.docs {
		if allowed(d.chunk, agent) && s.visible(d.chunk) && !seen[d.chunk.Source.ID] {
			src := d.chunk.Source
			src.Locator = ""
			result = append(result, src)
			seen[src.ID] = true
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// Search filters permissions, agent and topic before either ranking. Without
// WithHybrid it uses BM25; configured stores fuse actual semantic cosine ranks
// and BM25 ranks. A failed embedding request is reported as an explicit fallback.
func (s *Store) Search(ctx context.Context, agent, query, topic string, limit int) (SearchResult, error) {
	out := SearchResult{Status: "no_matches", Hits: []Hit{}, Retrieval: Retrieval{Mode: "bm25"}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if _, ok := crew.Find(agent); !ok {
		return out, errors.New("unknown agent")
	}
	if len(query) > 2048 || len(tokenize(query)) == 0 || len(topic) > 128 {
		return out, errors.New("query must contain words and be <= 2048 bytes; topic <= 128 bytes")
	}
	if limit == 0 {
		limit = 5
	}
	if limit < 1 || limit > 10 {
		return out, errors.New("limit must be 1..10")
	}
	if s.Count(agent) == 0 {
		out.Status = "empty_corpus"
		return out, nil
	}
	docs := []document{}
	for _, d := range s.docs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if !allowed(d.chunk, agent) || !s.visible(d.chunk) {
			continue
		}
		if topic != "" {
			found := false
			for _, t := range d.chunk.Topics {
				if strings.EqualFold(t, topic) {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		docs = append(docs, d)
	}
	if len(docs) == 0 {
		return out, nil
	}
	var err error
	out.Hits, err = bm25(ctx, docs, query)
	if err != nil {
		return out, err
	}
	if index := s.hybridIndex(); index != nil {
		out.Retrieval.EmbeddingModel = index.config.Model
		semantic, embedErr := index.search(ctx, docs, query)
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if embedErr != nil {
			out.Retrieval.Mode = "bm25_fallback"
			out.Retrieval.DegradedReason = errorCode(embedErr)
		} else {
			out.Retrieval.Mode = "hybrid"
			out.Hits = fuseRanks(out.Hits, semantic, max(20, limit*4))
		}
	}
	if len(out.Hits) > limit {
		out.Hits = out.Hits[:limit]
	}
	if len(out.Hits) > 0 {
		out.Status = "ok"
	}
	return out, nil
}

func bm25(ctx context.Context, docs []document, query string) ([]Hit, error) {
	hits := []Hit{}
	terms := map[string]bool{}
	for _, t := range tokenize(query) {
		terms[t] = true
	}
	df := map[string]int{}
	total := 0
	for _, d := range docs {
		total += d.length
		for t := range terms {
			if d.terms[t] > 0 {
				df[t]++
			}
		}
	}
	avg := float64(total) / float64(len(docs))
	if avg == 0 {
		return hits, nil
	}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		score := 0.0
		for t := range terms {
			tf := float64(d.terms[t])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(docs)-df[t])+0.5)/(float64(df[t])+0.5))
			score += idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(d.length)/avg))
		}
		if score > 0 {
			hits = append(hits, Hit{clone(d.chunk), score})
		}
	}
	sortHits(hits)
	return hits, nil
}

// JSON returns data suitable for both MCP text content and Ollama tool messages.
func JSON(v any) string { b, _ := json.Marshal(v); return string(b) }
