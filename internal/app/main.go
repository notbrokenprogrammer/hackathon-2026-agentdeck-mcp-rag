// Package app provides the shared source and release launcher.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"feeders/internal/mcpapp"
	"feeders/internal/share"
)

const usage = `LastCrew FlightDeck

Run from the project directory:
  go run .                    Build and start the game
  go build -o lastcrew .       Build once
  ./lastcrew                  Start the compiled game
  go run . --install-tunnel    Install the HTTPS connector on this host once
  go run . --share             Start the game and a browser-only HTTPS invite

For a normal launch open http://localhost:8080; with --share use the printed HTTPS URL.
Stop with Ctrl+C.
world/ and knowledge/ must remain in the working directory.
Ollama runs separately; the default model is qwen3:4b-instruct-2507-q4_K_M.
Articles use BM25 + local embeddings (default embeddinggemma); casual chat uses no retrieval.
Prepare/check articles: go run ./cmd/check-agents -prepare
Check a real conversation: go run ./cmd/check-agents -mode casual -agent savinykh -question "Привет!"

Environment:
  HOST_ADDR        Listen address (default 0.0.0.0:8080)
  DATA_DIR         Saved games and accounts (default data)
  OLLAMA_ENABLED   Set false to use reference answers without the model
  OLLAMA_MODEL     Local Ollama model name
  OLLAMA_URL       Local Ollama endpoint (default http://127.0.0.1:11434)
  RAG_HYBRID_ENABLED   Enable vector + BM25 retrieval (default true)
  RAG_EMBEDDING_MODEL  Local embedding model (default embeddinggemma)
  RAG_EMBEDDING_URL    Embedding origin (default OLLAMA_URL)
  RAG_EMBEDDING_CACHE  Vector cache (default DATA_DIR/rag-vectors.json)
  DEV_WEB_DIR      Optional frontend directory instead of embedded files
  MCP_BINARY      Optional standalone MCP executable instead of this binary
  TLS_CERT_FILE   Certificate chain for direct HTTPS (with TLS_KEY_FILE)
  TLS_KEY_FILE    Private key for direct HTTPS
  PUBLIC_URL      Fixed HTTPS origin for invitations / a trusted local proxy
  TRUST_PROXY     true only for a loopback HTTPS proxy with PUBLIC_URL set
  CLOUDFLARED_BINARY  Optional path to the host's cloudflared executable

Tools:
  --mcp [flags]   Run the embedded MCP stdio server (internal use)
  --help          Show this help
`

func Main() {
	shared := false
	// MCP stdout must contain only JSON-RPC, with no host logs or database startup.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--mcp":
			mcpapp.Main(os.Args[2:])
			return
		case "--help", "-h":
			fmt.Print(usage)
			return
		case "--install-tunnel":
			if len(os.Args) != 2 {
				fmt.Fprintln(os.Stderr, "Use: go run . --install-tunnel")
				os.Exit(2)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			path, err := share.Install(ctx, "bin", runtime.GOOS, runtime.GOARCH)
			if err != nil {
				fmt.Fprintln(os.Stderr, "HTTPS connector:", err)
				os.Exit(1)
			}
			fmt.Println("HTTPS connector installed:", path)
			fmt.Println("Start the game: go run . --share. Guests need only a browser.")
			return
		case "--share":
			if len(os.Args) != 2 {
				fmt.Fprintln(os.Stderr, "Use: go run . --share")
				os.Exit(2)
			}
			shared = true
		default:
			fmt.Fprintf(os.Stderr, "Unknown argument: %s\nUse --help for launch instructions.\n", os.Args[1])
			os.Exit(2)
		}
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log, shared); err != nil {
		log.Error("host stopped", "error", err)
		os.Exit(1)
	}
}
