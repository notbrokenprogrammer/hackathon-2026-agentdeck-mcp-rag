package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"feeders/internal/auth"
	"feeders/internal/npc"
	"feeders/internal/rag"
	"feeders/internal/storage"
)

type statusTestSource struct {
	calls atomic.Int32
	check func(context.Context) npc.ModelStatus
}

type indexStatusFixture struct{ value rag.HybridStatus }

func (s *indexStatusFixture) HybridStatus() rag.HybridStatus { return s.value }

func TestAgentStatusRetrievalRefreshesWithoutDisclosingGlobalInventory(t *testing.T) {
	model := &statusTestSource{check: func(context.Context) npc.ModelStatus {
		return npc.ModelStatus{Enabled: true, Ready: true, Model: "chat-model", Code: "ready"}
	}}
	handler, mux, cookie := newStatusTest(t, model)
	index := &indexStatusFixture{value: rag.HybridStatus{Configured: true, Model: "embeddinggemma", Code: "embedding_index_not_ready", Count: 1, Total: 91, Message: "/private/cache/path"}}
	handler.retrieval = index
	first := requestModelStatus(mux, cookie)
	if first.Code != 200 || !strings.Contains(first.Body.String(), "embedding_index_not_ready") {
		t.Fatal(first.Body.String())
	}
	index.value.Ready, index.value.Code = true, "ready"
	second := requestModelStatus(mux, cookie)
	var result struct {
		Retrieval struct {
			Ready bool   `json:"ready"`
			Model string `json:"model"`
		} `json:"retrieval"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &result); err != nil || !result.Retrieval.Ready || result.Retrieval.Model != "embeddinggemma" {
		t.Fatal(second.Body.String(), err)
	}
	if model.calls.Load() != 1 {
		t.Fatal("index progress should not re-probe chat model")
	}
	for _, private := range []string{"count", "total", "private/cache", "91"} {
		if strings.Contains(second.Body.String(), private) {
			t.Fatal("hidden inventory leaked", second.Body.String())
		}
	}
}

func (s *statusTestSource) Status(ctx context.Context) npc.ModelStatus {
	s.calls.Add(1)
	return s.check(ctx)
}

// Exercises actual authentication and the registered HTTP handler without
// opening a listening port or sending a request to the installed Ollama.
func newStatusTest(t *testing.T, source statusSource) (*agentStatus, *http.ServeMux, *http.Cookie) {
	t.Helper()
	accounts, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(accounts, time.Hour, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	status := &agentStatus{auth: a, model: source}
	mux := http.NewServeMux()
	a.RegisterRoutes(mux)
	status.RegisterRoutes(mux)
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(`{"username":"status-tester","password":"status-test-password"}`))
	request.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == auth.CookieName {
			return status, mux, cookie
		}
	}
	t.Fatal("registration did not issue the real session cookie")
	return nil, nil, nil
}

func requestModelStatus(mux *http.ServeMux, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", "/api/agents/status", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func decodeModelStatus(t *testing.T, response *httptest.ResponseRecorder) npc.ModelStatus {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status route failed: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
		t.Fatal("status response must remain JSON")
	}
	var status npc.ModelStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestAgentStatusRequiresActiveSessionEvenWithCachedResult(t *testing.T) {
	source := &statusTestSource{check: func(context.Context) npc.ModelStatus {
		return npc.ModelStatus{Enabled: true, Model: "host-model", Ready: true, Code: "ready", Message: "Модель установлена"}
	}}
	_, mux, cookie := newStatusTest(t, source)
	for _, credential := range []*http.Cookie{nil, {Name: auth.CookieName, Value: "forged"}} {
		response := requestModelStatus(mux, credential)
		if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "host-model") {
			t.Fatalf("unauthorized model information disclosed: %d %s", response.Code, response.Body.String())
		}
	}
	if source.calls.Load() != 0 {
		t.Fatal("unauthorized request probed local Ollama")
	}
	if got := decodeModelStatus(t, requestModelStatus(mux, cookie)); !got.Ready || got.Model != "host-model" {
		t.Fatal(got)
	}
	logout := httptest.NewRequest("POST", "/api/auth/logout", nil)
	logout.AddCookie(cookie)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, logout)
	if response.Code != http.StatusOK {
		t.Fatal("logout failed", response.Code)
	}
	if response := requestModelStatus(mux, cookie); response.Code != http.StatusUnauthorized {
		t.Fatal("revoked cookie read the cached model status", response.Code)
	}
	if source.calls.Load() != 1 {
		t.Fatal("unauthorized or logged-out requests triggered a probe", source.calls.Load())
	}
}

func TestAgentStatusCachesReadyAndOfflineThenRefreshes(t *testing.T) {
	next := npc.ModelStatus{Enabled: true, Model: "host-model", Ready: true, Code: "ready", Message: "Доступна"}
	source := &statusTestSource{check: func(context.Context) npc.ModelStatus { return next }}
	handler, mux, cookie := newStatusTest(t, source)
	get := func() npc.ModelStatus { return decodeModelStatus(t, requestModelStatus(mux, cookie)) }
	expire := func() {
		handler.mu.Lock()
		handler.checked = time.Now().Add(-4 * time.Second)
		handler.mu.Unlock()
	}
	if got := get(); got != next || source.calls.Load() != 1 {
		t.Fatal("first request did not check service", got, source.calls.Load())
	}
	next = npc.ModelStatus{Enabled: true, Model: "host-model", Code: "unavailable", Message: "Запустите Ollama"}
	if got := get(); !got.Ready || source.calls.Load() != 1 {
		t.Fatal("ready cache not reused", got, source.calls.Load())
	}
	expire()
	if got := get(); got != next || source.calls.Load() != 2 {
		t.Fatal("offline state not observed after expiry", got, source.calls.Load())
	}
	next = npc.ModelStatus{Enabled: true, Model: "host-model", Ready: true, Code: "ready", Message: "Снова доступна"}
	if got := get(); got.Ready || got.Code != "unavailable" || source.calls.Load() != 2 {
		t.Fatal("offline cache not reused", got, source.calls.Load())
	}
	expire()
	if got := get(); got != next || source.calls.Load() != 3 {
		t.Fatal("service recovery not observed after expiry", got, source.calls.Load())
	}
}

func TestAgentStatusConcurrentRequestsShareSingleProbe(t *testing.T) {
	source := &statusTestSource{check: func(context.Context) npc.ModelStatus {
		return npc.ModelStatus{Enabled: true, Model: "host-model", Ready: true, Code: "ready"}
	}}
	_, mux, cookie := newStatusTest(t, source)
	const requests = 12
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, requests)
	var group sync.WaitGroup
	for i := 0; i < requests; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- requestModelStatus(mux, cookie)
		}()
	}
	close(start)
	group.Wait()
	close(results)
	for response := range results {
		if got := decodeModelStatus(t, response); !got.Ready {
			t.Fatal(got)
		}
	}
	if source.calls.Load() != 1 {
		t.Fatal("concurrent status requests overwhelmed service", source.calls.Load())
	}
}
