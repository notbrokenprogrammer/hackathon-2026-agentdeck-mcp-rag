package app

import (
	"context"
	"net/http"
	"sync"
	"time"

	"feeders/internal/auth"
	"feeders/internal/npc"
	"feeders/internal/retrievalconfig"
	"feeders/internal/server"
)

type statusSource interface {
	Status(context.Context) npc.ModelStatus
}
type agentStatus struct {
	auth      *auth.Handler
	model     statusSource
	retrieval retrievalconfig.StatusSource
	mu        sync.Mutex
	checked   time.Time
	cached    npc.ModelStatus
}

func (a *agentStatus) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/status", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.auth.Resolve(r); !ok {
			server.JSON(w, 401, map[string]string{"code": "unauthorized", "message": "Войдите в игру"})
			return
		}
		a.mu.Lock()
		if time.Since(a.checked) > 3*time.Second {
			a.cached = a.model.Status(r.Context())
			a.checked = time.Now()
		}
		status := a.cached
		a.mu.Unlock()
		// Index state can change during background preparation; read it live
		// without re-probing Ollama or exposing the global document inventory.
		server.JSON(w, 200, struct {
			npc.ModelStatus
			Retrieval retrievalconfig.PublicStatus `json:"retrieval"`
		}{status, retrievalconfig.Status(a.retrieval)})
	})
}
