package app

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"feeders/internal/agents"
	"feeders/internal/auth"
	"feeders/internal/config"
	"feeders/internal/game"
	"feeders/internal/knowledge"
	"feeders/internal/npc"
	"feeders/internal/rag"
	"feeders/internal/realtime"
	"feeders/internal/retrievalconfig"
	"feeders/internal/server"
	"feeders/internal/share"
	"feeders/internal/storage"
	"feeders/web"
)

func run(log *slog.Logger, shared bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var connector string
	if shared {
		if cfg.PublicURL != "" || cfg.TLSCertFile != "" {
			return fmt.Errorf("--share creates its own HTTPS URL; unset PUBLIC_URL, TLS_CERT_FILE and TLS_KEY_FILE")
		}
		connector, err = share.FindBinary()
		if err != nil {
			return err
		}
		cfg.TrustProxy = true
		_, port, _ := net.SplitHostPort(cfg.Addr)
		cfg.Addr = net.JoinHostPort("127.0.0.1", port)
	}
	if cfg.PublicURL != "" && cfg.TLSCertFile == "" && !cfg.TrustProxy {
		return fmt.Errorf("PUBLIC_URL requires TLS_CERT_FILE/TLS_KEY_FILE or TRUST_PROXY=true for a local HTTPS proxy")
	}
	access, err := server.NewPublicAccess(cfg.PublicURL, cfg.TrustProxy)
	if err != nil {
		return err
	}
	var certificate tls.Certificate
	if cfg.TLSCertFile != "" {
		certificate, err = tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("HTTPS certificate: %w", err)
		}
	}
	repo, err := storage.NewSQLiteStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer repo.Close()
	authHandler, err := auth.New(repo, cfg.SessionTTL, cfg.CookieSecure, log)
	if err != nil {
		return err
	}
	hub := realtime.New(authHandler, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Этап 2 & 3 (Разработчик 2): 20Hz Tick Loop, валидация перемещений,
	// рассылка world.snapshot и периодическое автосохранение позиций игроков
	serverLoop := game.NewServerLoop(repo, hub, log)
	serverLoop.Start(ctx)
	defer serverLoop.Stop()

	persistence := game.NewPersistenceManager(repo, serverLoop, 2*time.Second, log)
	persistence.Start(ctx)
	defer persistence.Stop()

	// Этап 4 (Разработчик 2): AI Orchestrator с очередью запросов,
	// локальным клиентом Ollama и базой знаний первоисточников
	aiOrchestrator, err := agents.NewMentor(cfg.DataDir)
	if err != nil {
		return err
	}

	if err := aiOrchestrator.WithStore(repo); err != nil {
		return err
	}
	gameModule := game.New(repo, serverLoop, nil, log).WithCrewProjection(filepath.Join(cfg.DataDir, "crew-view.json"))
	if err := gameModule.WithPersistence(repo); err != nil {
		return err
	}
	serverLoop.ConfigureWorld(gameModule.RoomOf, gameModule.Walkable)
	if err := gameModule.WithWorld("world", authHandler); err != nil {
		return err
	}
	corpus, err := rag.Load(envOr("RAG_CHUNKS_FILE", "knowledge"))
	if err != nil {
		return err
	}
	if err := retrievalconfig.Configure(corpus, cfg.DataDir); err != nil {
		return err
	}
	if corpus.HybridStatus().Configured {
		prepared := retrievalconfig.PrepareAsync(ctx, corpus)
		go func() {
			prepareErr := <-prepared
			if ctx.Err() != nil {
				return
			}
			status := retrievalconfig.Status(corpus)
			if prepareErr != nil {
				log.Warn("article semantic index unavailable; BM25 remains available", "code", status.Code, "model", status.Model)
			} else {
				log.Info("article semantic index prepared", "code", status.Code, "model", status.Model)
			}
		}()
	}
	local, err := npc.NewLocal(knowledge.Catalog{Store: corpus, ScenarioOverride: game.CallCrewTool}, envOr("OLLAMA_URL", "http://127.0.0.1:11434"), envOr("OLLAMA_MODEL", "qwen3:4b-instruct-2507-q4_K_M"))
	if err != nil {
		return err
	}
	local.WithMission(gameModule)
	if err := local.WithMemory(repo); err != nil {
		return err
	}
	mcpBinary := os.Getenv("MCP_BINARY")
	var mcpArgs []string
	if mcpBinary == "" {
		mcpBinary, err = os.Executable()
		if err != nil {
			return err
		}
		mcpArgs = []string{"--mcp"}
	}
	local.WithMCP(mcpBinary, filepath.Join(cfg.DataDir, "crew-view.json"), envOr("RAG_CHUNKS_FILE", "knowledge"), mcpArgs...)
	if os.Getenv("OLLAMA_ENABLED") == "false" {
		local.WithoutModel()
	}
	log.Info("local crew model configured", "enabled", local.ModelEnabled(), "model", envOr("OLLAMA_MODEL", "qwen3:4b-instruct-2507-q4_K_M"), "endpoint", envOr("OLLAMA_URL", "http://127.0.0.1:11434"))
	gameModule.WithAgents(agents.NewCrewService(local, aiOrchestrator).WithMemory(repo))
	gameModule.Register(hub)

	var files fs.FS = web.Files
	if dir := os.Getenv("DEV_WEB_DIR"); dir != "" {
		files = os.DirFS(dir)
	}
	var capability [32]byte
	if _, err := rand.Read(capability[:]); err != nil {
		return err
	}
	worldKey := hex.EncodeToString(capability[:])
	gameModule.EnableWorldTools(worldKey)
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           access.Wrap(qaHandler(server.New(files, authHandler, hub, gameModule, &agentStatus{auth: authHandler, model: local, retrieval: corpus}), authHandler)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	if cfg.TLSCertFile != "" {
		httpServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}
	defer httpServer.Close()

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}

	defer listener.Close()
	toolListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	toolServer := &http.Server{Handler: gameModule.WorldToolsHandler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10}
	defer toolServer.Close()
	local.WithWorldTools("http://"+toolListener.Addr().String(), worldKey)
	errs := make(chan error, 2)
	go func() { errs <- toolServer.Serve(toolListener) }()
	go func() {
		if cfg.TLSCertFile != "" {
			errs <- httpServer.ServeTLS(listener, "", "")
		} else {
			errs <- httpServer.Serve(listener)
		}
	}()
	log.Info("host ready", "address", listener.Addr(), "data_dir", cfg.DataDir, "protocol_version", 1, "world_enabled", true, "simulation_hz", 20, "ai_enabled", true)
	var tunnel *share.Tunnel
	var tunnelDone <-chan error
	if shared {
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		tunnel, err = share.Start(ctx, connector, "http://127.0.0.1:"+port, func(address string) {
			if e := access.SetPublicURL(address); e != nil {
				log.Error("invalid HTTPS link", "error", e)
				return
			}
			log.Info("HTTPS invitation ready", "url", address)
			fmt.Printf("\nОткройте %s — создайте экспедицию и нажмите «Пригласить».\nДругому игроку нужен только браузер. Остановка: Ctrl+C.\n\n", address)
		}, os.Stderr)
		if err != nil {
			return err
		}
		defer tunnel.Stop()
		tunnelDone = tunnel.Done
		log.Info("waiting for public HTTPS invitation")
	} else if cfg.PublicURL != "" {
		log.Info("public invitation configured", "url", cfg.PublicURL)
	} else {
		scheme := "http"
		if cfg.TLSCertFile != "" {
			scheme = "https"
		}
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		log.Info("open game in browser", "url", scheme+"://localhost:"+port)
	}

	select {
	case <-ctx.Done():
	case err = <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	case tunnelErr := <-tunnelDone:
		if ctx.Err() == nil {
			err = fmt.Errorf("HTTPS connector stopped: %v; restart --share to get a new link", tunnelErr)
		}
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Фиксация состояний игроков перед завершением
	persistence.Flush(shutdownCtx)

	// Закрытие WebSocket-соединений
	if closeErr := hub.Close(shutdownCtx); closeErr != nil {
		log.Error("websocket shutdown", "error", closeErr)
	}

	if closeErr := httpServer.Shutdown(shutdownCtx); closeErr != nil {
		_ = httpServer.Close()
		return closeErr
	}

	return err
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
