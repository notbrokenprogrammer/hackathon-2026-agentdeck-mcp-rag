package npc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"feeders/internal/rag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WithMCP accepts optional arguments before the MCP flags, e.g. --mcp for the host itself.
func (s *Local) WithMCP(binary, view, corpus string, prefixArgs ...string) *Local {
	s.mcpBinary = binary
	s.mcpArgs = append([]string(nil), prefixArgs...)
	s.mcpView = view
	s.mcpCorpus = corpus
	return s
}

// Each bounded child has a trusted player binding. The model cannot supply it.
func (s *Local) search(ctx context.Context, r Request, query string) (rag.SearchResult, string, error) {
	args := json.RawMessage(rag.JSON(map[string]any{"query": query, "limit": 3}))
	// Explicit articles use the host's prepared shared hybrid index. Spawning a
	// fresh MCP process per turn would repeatedly warm the embedding cache and
	// could degrade a healthy host index to lexical-only results on a timeout.
	if r.DialogueMode == DialogueArticles {
		value, err := s.catalog.Call(ctx, r.AgentID+"_search", args)
		if err != nil {
			return rag.SearchResult{}, "", err
		}
		return value.(rag.SearchResult), "Локальный поиск", nil
	}
	if s.mcpBinary != "" {
		if result, err := s.searchMCP(ctx, r, args); err == nil {
			return result, "MCP stdio", nil
		}
	}
	value, err := s.catalog.Call(ctx, r.AgentID+"_search", args)
	if err != nil {
		return rag.SearchResult{}, "", err
	}
	transport := "Локальный поиск"
	if s.mcpBinary != "" {
		transport = "Резервный поиск · MCP недоступен"
	}
	return value.(rag.SearchResult), transport, nil
}
func (s *Local) WithWorldTools(url, token string) *Local {
	s.mcpURL = url
	s.mcpToken = token
	s.catalog.AllowExperiments = true
	return s
}
func (s *Local) executeTool(ctx context.Context, r Request, name string, args json.RawMessage) (any, error) {
	if strings.HasSuffix(name, "_start_experiment") {
		raw, err := s.callMCP(ctx, r, name, args)
		if err != nil {
			return nil, err
		}
		var v map[string]any
		err = json.Unmarshal(raw, &v)
		return v, err
	}
	return s.catalog.Call(ctx, name, args)
}
func (s *Local) searchMCP(ctx context.Context, r Request, args json.RawMessage) (rag.SearchResult, error) {
	var found rag.SearchResult
	raw, err := s.callMCP(ctx, r, r.AgentID+"_search", args)
	if err != nil {
		return found, err
	}
	err = json.Unmarshal(raw, &found)
	return found, err
}
func (s *Local) callMCP(ctx context.Context, r Request, name string, args json.RawMessage) (json.RawMessage, error) {
	childCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	binary, err := filepath.Abs(s.mcpBinary)
	if err != nil {
		return nil, err
	}
	commandArgs := append([]string(nil), s.mcpArgs...)
	commandArgs = append(commandArgs, "-chunks", s.mcpCorpus, "-view-file", s.mcpView, "-player-id", r.PlayerID)
	command := exec.CommandContext(childCtx, binary, commandArgs...)
	command.Env = append(os.Environ(), "MCP_WORLD_URL="+s.mcpURL, "MCP_WORLD_TOKEN="+s.mcpToken)
	input := bytes.Buffer{}
	enc := json.NewEncoder(&input)
	enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "last-crew-host", "version": "2.0"}}})
	enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	command.Stdin = &input
	out, err := command.Output()
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var response struct {
			ID     int `json:"id"`
			Result struct {
				Structured json.RawMessage `json:"structuredContent"`
				IsError    bool            `json:"isError"`
			} `json:"result"`
			Error any `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			return nil, err
		}
		if response.ID == 2 {
			if response.Error != nil || response.Result.IsError {
				return nil, errors.New("MCP rejected the tool request")
			}
			return response.Result.Structured, nil
		}
	}
	return nil, errors.New("MCP did not return the requested result")
}
