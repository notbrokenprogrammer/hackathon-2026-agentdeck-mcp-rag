// Package mcpapp runs the MCP child process. Stdout is JSON-RPC only.
package mcpapp

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"feeders/internal/game"
	"feeders/internal/knowledge"
	"feeders/internal/mcp"
	"feeders/internal/mission"
	"feeders/internal/rag"
	"feeders/internal/retrievalconfig"
)

func Main(args []string) {
	flags := flag.NewFlagSet("mcp-tools", flag.ExitOnError)
	defaultPath := os.Getenv("RAG_CHUNKS_FILE")
	if defaultPath == "" {
		defaultPath = "knowledge"
	}
	path := flags.String("chunks", defaultPath, "JSONL corpus path; missing file means empty corpus")
	check := flags.Bool("check", false, "validate corpus and print status without starting MCP")
	exportTools := flags.Bool("export-tools", false, "print tool definitions as JSON and exit")
	missionFile := flags.String("mission-file", "", "read-only authoritative mission.json from game host")
	playerID := flags.String("player-id", "", "trusted actor binding, required with -view-file or -mission-file")
	viewFile := flags.String("view-file", "", "read-only crew-view.json projection from the current game host")
	flags.Parse(args)
	s, err := rag.Load(*path)
	if err == nil {
		c := knowledge.Catalog{Store: s}
		endpoint, key := os.Getenv("MCP_WORLD_URL"), os.Getenv("MCP_WORLD_TOKEN")
		if endpoint != "" && key != "" && *playerID != "" {
			u, e := url.Parse(endpoint)
			if e != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
				fmt.Fprintln(os.Stderr, "MCP world endpoint must be loopback")
				os.Exit(1)
			}
			c.AllowExperiments = true
			c.StartExperiment = func(ctx context.Context, args json.RawMessage) (any, error) {
				var p struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(args, &p); err != nil {
					return nil, err
				}
				data, _ := json.Marshal(map[string]string{"player": *playerID, "type": p.Type})
				req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(endpoint, "/")+"/api/internal/experiment", bytes.NewReader(data))
				if e != nil {
					return nil, e
				}
				req.Header.Set("Authorization", "Bearer "+key)
				req.Header.Set("Content-Type", "application/json")
				transport := http.DefaultTransport.(*http.Transport).Clone()
				transport.Proxy = nil
				client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				res, e := client.Do(req)
				if e != nil {
					return nil, e
				}
				defer res.Body.Close()
				if res.StatusCode != 200 {
					return nil, fmt.Errorf("world tool HTTP %d", res.StatusCode)
				}
				var value map[string]any
				e = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&value)
				return value, e
			}
		}

		if *viewFile != "" {
			c.ScenarioOverride = game.CallCrewTool
		}
		if ((*missionFile == "" && *viewFile == "") != (*playerID == "")) || (*missionFile != "" && *viewFile != "") {
			fmt.Fprintln(os.Stderr, "use -player-id with exactly one of -view-file or -mission-file")
			os.Exit(1)
		}
		if *missionFile != "" {
			c.ViewProvider = func(context.Context) (mission.View, error) { return mission.ReadView(*missionFile, *playerID) }
		}
		if *viewFile != "" {
			c.ViewProvider = func(context.Context) (mission.View, error) { return game.ReadCrewView(*viewFile, *playerID) }
		}
		if *exportTools {
			fmt.Println(rag.JSON(map[string]any{"tools": c.List()}))
			return
		}
		if *check {
			var status any
			status, err = c.Call(context.Background(), "rag_status", nil)
			if data, ok := status.(map[string]any); ok {
				data["total_loaded_chunks"] = s.Inventory()
			}
			if err == nil {
				fmt.Println(rag.JSON(status))
			}
		} else {
			// Metadata/check commands above intentionally leave DATA_DIR absent.
			// Serving only configures/reads the cache; a search triggers bounded
			// lazy embeddings, with the same environment and cache as the host.
			if err = retrievalconfig.Configure(s, ""); err != nil {
				fmt.Fprintln(os.Stderr, "chronograd-rag:", err)
				os.Exit(1)
			}
			err = mcp.Serve(context.Background(), os.Stdin, os.Stdout, c)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "chronograd-rag:", err)
		os.Exit(1)
	}
}
