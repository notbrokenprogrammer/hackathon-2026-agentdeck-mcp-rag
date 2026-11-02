package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

type embeddingTransport func(*http.Request) (*http.Response, error)

func (f embeddingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func embeddingResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

// No listening sockets or installed models are needed: the test exercises the
// real Ollama HTTP serialization/validation path with known semantic vectors.
func embeddingClient(t *testing.T, model string, vector func(string) []float64) *http.Client {
	t.Helper()
	return &http.Client{Transport: embeddingTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("wrong embedding request: %s %s", r.Method, r.URL)
		}
		var request struct {
			Model    string   `json:"model"`
			Input    []string `json:"input"`
			Truncate *bool    `json:"truncate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != model || request.Truncate == nil || *request.Truncate || len(request.Input) == 0 {
			t.Errorf("invalid model/input/truncate request: %+v, %v", request, err)
		}
		vectors := make([][]float64, len(request.Input))
		for i, input := range request.Input {
			vectors[i] = vector(input)
		}
		return embeddingResponse(200, JSON(map[string]any{"embeddings": vectors})), nil
	})}
}

func hybridChunk(id, text string) Chunk {
	return Chunk{ID: id, Text: text, Agents: []string{"dzhanibekov"}, Topics: []string{"systems"}, Source: Source{ID: id, Title: "Archive"}}
}

func hybridStore(t *testing.T, chunks ...Chunk) *Store {
	t.Helper()
	lines := []string{}
	for _, chunk := range chunks {
		lines = append(lines, JSON(chunk))
	}
	s, err := Read(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func configureHybrid(t *testing.T, s *Store, config HybridConfig) {
	t.Helper()
	if config.URL == "" {
		config.URL = "http://127.0.0.1:11434"
	}
	if config.Model == "" {
		config.Model = "test-embed:1"
	}
	if err := s.WithHybrid(config); err != nil {
		t.Fatal(err)
	}
}

func TestHybridSemanticOnlyMatchUsesRealEmbeddingProtocol(t *testing.T) {
	s := hybridStore(t, hybridChunk("return", "Капсула совершила мягкое приводнение."), hybridChunk("filter", "Клапан воздушного фильтра заменён."))
	query := "возвращение экипажа домой"
	before, err := s.Search(context.Background(), "dzhanibekov", query, "", 1)
	if err != nil || len(before.Hits) != 0 || before.Retrieval.Mode != "bm25" {
		t.Fatalf("legacy lexical search: %+v %v", before, err)
	}
	var requests atomic.Int32
	client := embeddingClient(t, "embeddinggemma", func(input string) []float64 {
		requests.Add(1)
		switch {
		case strings.HasPrefix(input, "task: search result | query: "):
			return []float64{0.99, 0.01}
		case strings.HasPrefix(input, "title: Archive | text: ") && strings.Contains(input, "приводнение"):
			return []float64{3, 0} // response normalization is exercised too
		case strings.HasPrefix(input, "title: Archive | text: "):
			return []float64{0, 2}
		default:
			t.Errorf("missing model-specific retrieval prefix: %q", input)
			return []float64{0, 1}
		}
	})
	configureHybrid(t, s, HybridConfig{Model: "embeddinggemma", HTTPClient: client})
	after, err := s.Search(context.Background(), "dzhanibekov", query, "", 1)
	if err != nil || after.Status != "ok" || len(after.Hits) != 1 || after.Hits[0].ID != "return" || after.Retrieval.Mode != "hybrid" || after.Retrieval.EmbeddingModel != "embeddinggemma" || after.Retrieval.DegradedReason != "" {
		t.Fatalf("semantic retrieval: %+v %v", after, err)
	}
	if requests.Load() != 3 || !s.HybridStatus().Ready {
		t.Fatalf("model was not actually called: %d %+v", requests.Load(), s.HybridStatus())
	}
}

func TestHybridFiltersBeforeBothRankingsAndLazyEmbedding(t *testing.T) {
	public := hybridChunk("public", "battery maintenance")
	secret := hybridChunk("secret", "battery battery battery PRIVATE-FACT")
	secret.RequiresFacts = []string{"bridge_access"}
	otherAgent := hybridChunk("other-agent", "battery PRIVATE-AGENT")
	otherAgent.Agents = []string{"savinykh"}
	otherTopic := hybridChunk("other-topic", "battery PRIVATE-TOPIC")
	otherTopic.Topics = []string{"biology"}
	s := hybridStore(t, public, secret, otherAgent, otherTopic)
	scoped := s.Scope(nil) // must see configuration added after this copy
	var inputs []string
	var mu sync.Mutex
	client := embeddingClient(t, "test-embed:1", func(input string) []float64 {
		mu.Lock()
		inputs = append(inputs, input)
		mu.Unlock()
		return []float64{1, 0}
	})
	configureHybrid(t, s, HybridConfig{HTTPClient: client})
	r, err := scoped.Search(context.Background(), "dzhanibekov", "battery", "systems", 10)
	if err != nil || len(r.Hits) != 1 || r.Hits[0].ID != "public" || r.Retrieval.Mode != "hybrid" || math.Abs(r.Hits[0].Score-2.0/61) > 1e-12 {
		t.Fatalf("permission filter: %+v %v", r, err)
	}
	mu.Lock()
	for _, input := range inputs {
		if strings.Contains(input, "PRIVATE-") {
			t.Errorf("lazy embedding received unauthorized candidate: %q", input)
		}
	}
	mu.Unlock()
	if s.HybridStatus().Count != 1 || scoped.HybridStatus().Count != 1 {
		t.Fatal("Scope did not share cache safely")
	}
	// Even a full trusted background index must not leak or statistically boost
	// documents that are forbidden to the requesting agent/topic/fact scope.
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, err = scoped.Search(context.Background(), "dzhanibekov", "battery", "systems", 10)
	if err != nil || len(r.Hits) != 1 || r.Hits[0].ID != "public" || math.Abs(r.Hits[0].Score-2.0/61) > 1e-12 {
		t.Fatalf("precomputed index leaked candidates/statistics: %+v %v", r, err)
	}
	unlocked, err := s.Scope([]string{"bridge_access"}).Search(context.Background(), "dzhanibekov", "battery", "systems", 10)
	if err != nil || len(unlocked.Hits) != 2 {
		t.Fatalf("authorized fact did not unlock document: %+v %v", unlocked, err)
	}
	if _, err := scoped.Get("dzhanibekov", "secret"); err == nil {
		t.Fatal("unlock changed original scope")
	}
}

func TestHybridReciprocalRankFusion(t *testing.T) {
	a, b, c := hybridChunk("a", "one"), hybridChunk("b", "two"), hybridChunk("c", "three")
	got := fuseRanks([]Hit{{a, 1000}, {b, 0.01}}, []Hit{{c, 0.99}, {b, 0.4}}, 20)
	if len(got) != 3 || got[0].ID != "b" || math.Abs(got[0].Score-2.0/62) > 1e-12 || got[1].ID != "a" || got[2].ID != "c" {
		t.Fatalf("RRF must combine ranks with deterministic ID ties: %+v", got)
	}
	if got := fuseRanks([]Hit{{a, 1}, {b, 1}}, []Hit{{c, 1}, {b, 1}}, 1); len(got) != 2 {
		t.Fatal("candidate window ignored", got)
	}
}

func TestHybridCacheRoundTripIdentityCorruptionAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vectors.json")
	chunk := hybridChunk("battery", "battery confidential corpus text")
	var calls atomic.Int32
	client := embeddingClient(t, "test-embed:1", func(input string) []float64 {
		calls.Add(1)
		return []float64{0.4, 0.8}
	})
	s := hybridStore(t, chunk)
	configureHybrid(t, s, HybridConfig{CachePath: path, HTTPClient: client})
	if err := s.Prepare(context.Background()); err != nil || !s.HybridStatus().Ready {
		t.Fatalf("prepare: %+v %v", s.HybridStatus(), err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), chunk.Text) {
		t.Fatalf("cache exposed text: %v", err)
	}
	// Windows does not expose POSIX owner/group permission bits.
	if st, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0600) {
		t.Fatalf("cache permissions: %v %v", st, err)
	}
	reloaded := hybridStore(t, chunk)
	configureHybrid(t, reloaded, HybridConfig{CachePath: path, HTTPClient: client})
	if err := reloaded.Prepare(context.Background()); err != nil || calls.Load() != 1 || !reloaded.HybridStatus().Ready {
		t.Fatalf("cached document recomputed: calls=%d status=%+v err=%v", calls.Load(), reloaded.HybridStatus(), err)
	}
	if _, err := reloaded.Search(context.Background(), "dzhanibekov", "battery", "", 1); err != nil || calls.Load() != 2 {
		t.Fatalf("query did not use model: calls=%d err=%v", calls.Load(), err)
	}
	for _, mutate := range []string{"model", "corpus", "corruption", "input_hash", "nonunit_vector"} {
		t.Run(mutate, func(t *testing.T) {
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			c := chunk
			model := "test-embed:1"
			switch mutate {
			case "model":
				model = "test-embed:2"
			case "corpus":
				c.RequiresFacts = []string{"changed_permissions"}
			case "corruption":
				_ = os.WriteFile(path, []byte("{"), 0600)
			default:
				var cache embeddingCache
				_ = json.Unmarshal(data, &cache)
				if mutate == "input_hash" {
					cache.Entries[0].Hash = strings.Repeat("0", 64)
				} else {
					cache.Entries[0].Vectors[0] = []float32{99, 1}
				}
				_ = os.WriteFile(path, []byte(JSON(cache)), 0600)
			}
			fresh := hybridStore(t, c)
			configureHybrid(t, fresh, HybridConfig{Model: model, CachePath: path, HTTPClient: client})
			if st := fresh.HybridStatus(); st.Ready || st.Count != 0 || st.Code != "embedding_cache_invalid" {
				t.Fatalf("trusted changed/corrupt cache: %+v", st)
			}
		})
	}
}

func TestHybridMissingModelAndMalformedResponsesAreExplicitFallback(t *testing.T) {
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"missing", `{"error":"model missing PRIVATE-SERVER-TEXT"}`, "embedding_model_missing", 404},
		{"service", `{"error":"PRIVATE-SERVER-TEXT"}`, "embedding_service_unavailable", 500},
		{"zero", `{"embeddings":[[0,0]]}`, "embedding_response_invalid", 200},
		{"empty", `{"embeddings":[]}`, "embedding_response_invalid", 200},
		{"nan", `{"embeddings":[[NaN,1]]}`, "embedding_response_invalid", 200},
		{"overflow", `{"embeddings":[[1e9999,1]]}`, "embedding_response_invalid", 200},
		{"trailing", `{"embeddings":[[1,0]]}{}`, "embedding_response_invalid", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := hybridStore(t, hybridChunk("a", "battery maintenance"))
			client := &http.Client{Transport: embeddingTransport(func(*http.Request) (*http.Response, error) {
				return embeddingResponse(tc.status, tc.body), nil
			})}
			configureHybrid(t, s, HybridConfig{HTTPClient: client})
			r, err := s.Search(context.Background(), "dzhanibekov", "battery", "", 5)
			if err != nil || len(r.Hits) != 1 || r.Retrieval.Mode != "bm25_fallback" || r.Retrieval.DegradedReason != tc.code {
				t.Fatalf("dishonest fallback metadata: %+v %v", r, err)
			}
			status := s.HybridStatus()
			if status.Ready || status.Code != tc.code || strings.Contains(JSON(status), "PRIVATE-") || strings.Contains(JSON(r.Retrieval), "PRIVATE-") {
				t.Fatalf("unsafe/misleading status: %+v", status)
			}
		})
	}
}

func TestHybridQueryFailureDoesNotClaimHybridWithReadyDocumentVectors(t *testing.T) {
	s := hybridStore(t, hybridChunk("a", "battery maintenance"))
	var requests atomic.Int32
	client := &http.Client{Transport: embeddingTransport(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return embeddingResponse(200, `{"embeddings":[[1,0]]}`), nil
		}
		return embeddingResponse(200, `{"embeddings":[[1,0,0]]}`), nil
	})}
	configureHybrid(t, s, HybridConfig{HTTPClient: client})
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, err := s.Search(context.Background(), "dzhanibekov", "battery", "", 1)
	if err != nil || r.Retrieval.Mode != "bm25_fallback" || r.Retrieval.DegradedReason != "embedding_response_invalid" || s.HybridStatus().Ready {
		t.Fatalf("query dimension mismatch: %+v %+v %v", r, s.HybridStatus(), err)
	}
}

func TestHybridLongChunksKeepTailAndUseBoundedUTF8Parts(t *testing.T) {
	text := strings.Repeat("Космические наблюдения у окна. ", 180) + "TAIL-LANDING"
	c := hybridChunk("long", text)
	c.Source.Title = strings.Repeat("Я", 256)
	var docInputs []string
	client := embeddingClient(t, "embeddinggemma:latest", func(input string) []float64 {
		if strings.HasPrefix(input, "task:") || strings.Contains(input, "TAIL-LANDING") {
			return []float64{1, 0}
		}
		docInputs = append(docInputs, input)
		return []float64{0, 1}
	})
	s := hybridStore(t, c)
	configureHybrid(t, s, HybridConfig{Model: "embeddinggemma:latest", HTTPClient: client, BatchSize: 2})
	r, err := s.Search(context.Background(), "dzhanibekov", "splashdown", "", 1)
	if err != nil || len(r.Hits) != 1 || r.Hits[0].Text != text || r.Retrieval.Mode != "hybrid" || len(docInputs) < 3 {
		t.Fatalf("long chunk tail omitted: %+v %v (parts=%d)", r, err, len(docInputs))
	}
	parts := documentInputs("embeddinggemma:latest", c)
	reassembled := ""
	prefix := "title: " + c.Source.Title + " | text: "
	for _, part := range parts {
		if !utf8.ValidString(part) || len(part) > 1800 || !strings.HasPrefix(part, prefix) {
			t.Fatalf("unsafe context split: bytes=%d utf8=%v", len(part), utf8.ValidString(part))
		}
		reassembled += strings.TrimPrefix(part, prefix)
	}
	if reassembled != text {
		t.Fatal("split lost or duplicated characters")
	}
	query := strings.Repeat("слово ", 300)
	if got := strings.Join(splitBytes(query, 1400), ""); got != query {
		t.Fatal("query split lost tail")
	}
}

func TestHybridTimeoutAndCallerCancellation(t *testing.T) {
	for _, callerCancel := range []bool{false, true} {
		t.Run(fmt.Sprint(callerCancel), func(t *testing.T) {
			s := hybridStore(t, hybridChunk("a", "battery maintenance"))
			entered := make(chan struct{})
			client := &http.Client{Transport: embeddingTransport(func(r *http.Request) (*http.Response, error) {
				close(entered)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			configureHybrid(t, s, HybridConfig{HTTPClient: client, QueryTimeout: 30 * time.Millisecond})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if callerCancel {
				go func() { <-entered; cancel() }()
			}
			start := time.Now()
			r, err := s.Search(ctx, "dzhanibekov", "battery", "", 1)
			if callerCancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("caller cancellation swallowed: %+v %v", r, err)
				}
			} else if err != nil || r.Retrieval.Mode != "bm25_fallback" || r.Retrieval.DegradedReason != "embedding_timeout" || len(r.Hits) != 1 {
				t.Fatalf("bounded fallback: %+v %v", r, err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("unbounded lazy search")
			}
		})
	}
}

func TestHybridConcurrentScopeSearchPrepareAndCacheWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vectors.json")
	chunks := []Chunk{}
	for i := 0; i < 12; i++ {
		chunks = append(chunks, hybridChunk(fmt.Sprintf("c%d", i), "battery maintenance"))
	}
	client := embeddingClient(t, "test-embed:1", func(string) []float64 { return []float64{0.8, 0.6} })
	stores := []*Store{hybridStore(t, chunks...), hybridStore(t, chunks...)}
	for _, s := range stores {
		configureHybrid(t, s, HybridConfig{HTTPClient: client, CachePath: path, BatchSize: 2})
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := stores[i%2].Scope(nil)
			if i%3 == 0 {
				if err := s.Prepare(context.Background()); err != nil {
					t.Error(err)
				}
				return
			}
			r, err := s.Search(context.Background(), "dzhanibekov", "battery", "", 5)
			if err != nil || len(r.Hits) != 5 || r.Retrieval.Mode != "hybrid" {
				t.Errorf("concurrent search: %+v %v", r, err)
			}
			_ = s.HybridStatus()
		}(i)
	}
	wg.Wait()
	reloaded := hybridStore(t, chunks...)
	configureHybrid(t, reloaded, HybridConfig{HTTPClient: client, CachePath: path})
	if !reloaded.HybridStatus().Ready || reloaded.HybridStatus().Count != len(chunks) {
		t.Fatalf("concurrent cache corruption: %+v", reloaded.HybridStatus())
	}
}

func TestHybridRejectsRemoteAndInvalidConfiguration(t *testing.T) {
	s := hybridStore(t, hybridChunk("a", "battery"))
	for _, address := range []string{"https://example.com", "http://user:pass@localhost", "file:///tmp/embeddings", "http://localhost/api/embed", "http://localhost?x=y", "http://localhost:0", "http://localhost:99999", "http://localhost:", "http://localhost:http"} {
		if err := s.WithHybrid(HybridConfig{URL: address, Model: "embeddinggemma"}); err == nil {
			t.Errorf("accepted unsafe URL %q", address)
		}
	}
	for _, address := range []string{"http://localhost:11434", "http://127.0.0.1:11434", "http://[::1]:11434"} {
		if err := s.WithHybrid(HybridConfig{URL: address, Model: "embeddinggemma"}); err != nil {
			t.Errorf("rejected loopback URL %q: %v", address, err)
		}
	}
	for _, config := range []HybridConfig{{Model: ""}, {Model: "bad\nname"}, {Model: "ok", BatchSize: 33}, {Model: "ok", QueryTimeout: -time.Second}} {
		config.URL = "http://localhost:11434"
		if err := s.WithHybrid(config); err == nil {
			t.Errorf("accepted invalid config %+v", config)
		}
	}
}

func TestHybridCacheFailureDoesNotEraseWorkingSemanticSearch(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "file")
	if err := os.WriteFile(parentFile, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	s := hybridStore(t, hybridChunk("a", "battery maintenance"))
	client := embeddingClient(t, "test-embed:1", func(string) []float64 { return []float64{1, 0} })
	configureHybrid(t, s, HybridConfig{HTTPClient: client, CachePath: filepath.Join(parentFile, "cache.json")})
	r, err := s.Search(context.Background(), "dzhanibekov", "battery", "", 1)
	if err != nil || r.Retrieval.Mode != "hybrid" || s.HybridStatus().Code != "embedding_cache_unwritable" || !s.HybridStatus().Ready {
		t.Fatalf("cache persistence incorrectly disabled semantics: %+v %+v %v", r, s.HybridStatus(), err)
	}
}
