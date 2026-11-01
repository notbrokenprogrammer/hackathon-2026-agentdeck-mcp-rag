// Package knowledge owns the same read-only tools for MCP clients and game NPCs.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"feeders/internal/crew"
	"feeders/internal/mission"
	"feeders/internal/rag"
	"feeders/internal/retrievalconfig"
	"feeders/pkg/protocol"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
}
type Catalog struct {
	AllowExperiments bool
	StartExperiment  func(context.Context, json.RawMessage) (any, error)
	Store            *rag.Store
	// Optional bridge to the host's existing authoritative scenario implementation.
	ScenarioOverride func(context.Context, string, json.RawMessage) (any, bool, error)
	// Optional trusted actor binding, supplied by the host or MCP process flags.
	ViewProvider func(context.Context) (mission.View, error)
}

var ErrUnknownTool = errors.New("unknown tool")

func schema(properties map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func str(max int) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
}
func tool(name, description string, input map[string]any) Tool {
	return Tool{name, description, input, map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}
}

// ForAgent exposes only one persona's tools to the model. MCP's trusted local
// client can list all personas; tool names are scoping, not user authentication.
func (c Catalog) ForAgent(id string) []Tool {
	p, ok := crew.Find(id)
	if !ok {
		return nil
	}
	out := []Tool{
		tool(id+"_search", p.Name+": поиск подтверждений в RAG. "+p.Focus+". Возвращает текст и источники; empty_corpus/no_matches означает отсутствие подтверждений.", schema(map[string]any{"query": str(2048), "topic": str(128), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 10, "default": 5}}, "query")),
		tool(id+"_read_chunk", "Прочитать чанк, доступный персонажу "+p.Name+", по ID из результатов поиска.", schema(map[string]any{"chunk_id": str(128)}, "chunk_id")),
		tool(id+"_list_sources", "Список загруженных источников персонажа "+p.Name+". Используйте offset/limit для страниц.", schema(map[string]any{"offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}})),
	}
	if c.AllowExperiments {
		t := tool(id+"_start_experiment", "Создать общие световые часы в лаборатории корабля. Используй, когда игрок просит показать опыт. Нужны два разных участника; ресурсы не тратятся.", schema(map[string]any{"type": map[string]any{"type": "string", "enum": []string{"light_clock"}}}, "type"))
		t.Annotations["readOnlyHint"] = false
		out = append(out, t)
	}
	return append(out, c.scenarioTools(id)...)
}
func (c Catalog) List() []Tool {
	out := []Tool{tool("agents_list", "Четыре учебных персонажа и их специализации.", schema(map[string]any{})), tool("rag_status", "Состояние локального корпуса, количество чанков по персонажам.", schema(map[string]any{}))}
	for _, p := range crew.All() {
		out = append(out, c.ForAgent(p.ID)...)
	}
	return out
}
func decode(args json.RawMessage, out any) error {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(args)), "{") {
		return errors.New("arguments must be an object")
	}
	// Null values are not valid optional parameters either.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return err
	}
	for _, v := range fields {
		if string(v) == "null" {
			return errors.New("null argument")
		}
	}
	return protocol.DecodePayload(args, out)
}
func (c Catalog) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, "_start_experiment") && c.AllowExperiments {
		known := false
		for _, p := range crew.All() {
			if name == p.ID+"_start_experiment" {
				known = true
			}
		}
		if !known || c.StartExperiment == nil {
			return nil, ErrUnknownTool
		}
		var p struct {
			Type string `json:"type"`
		}
		if decode(args, &p) != nil || p.Type != "light_clock" {
			return nil, errors.New("unsupported experiment")
		}
		return c.StartExperiment(ctx, args)
	}
	var bindErr error
	ctx, bindErr = c.Bind(ctx)
	if bindErr != nil {
		return nil, bindErr
	}
	if v, ok := mission.FromContext(ctx); ok {
		c.Store = c.Store.Scope(v.FactIDs())
	} else {
		c.Store = c.Store.Scope(nil)
	}
	if c.ScenarioOverride != nil {
		known := false
		for _, t := range c.List() {
			if t.Name == name {
				known = true
				break
			}
		}
		if !known {
			return nil, ErrUnknownTool
		}
		if value, handled, err := c.ScenarioOverride(ctx, name, args); handled {
			return value, err
		}
	}
	if value, handled, err := c.callScenario(ctx, name, args); handled {
		return value, err
	}
	if name == "agents_list" || name == "rag_status" {
		if err := decode(args, &struct{}{}); err != nil {
			return nil, err
		}
		if name == "agents_list" {
			return map[string]any{"agents": crew.All()}, nil
		}
		counts := map[string]int{}
		for _, p := range crew.All() {
			counts[p.ID] = c.Store.Count(p.ID)
		}
		status := "ready"
		if c.Store.Total() == 0 {
			status = "empty_corpus"
		}
		return map[string]any{"status": status, "visible_chunks": c.Store.Total(), "chunks": c.Store.Total(), "agents": counts, "retrieval": retrievalconfig.Status(c.Store), "reload": "restart process after replacing JSONL"}, nil
	}
	for _, p := range crew.All() {
		switch name {
		case p.ID + "_search":
			var a struct {
				Query string `json:"query"`
				Topic string `json:"topic"`
				Limit *int   `json:"limit"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			limit := 5
			if a.Limit != nil {
				limit = *a.Limit
				if limit < 1 || limit > 10 {
					return nil, errors.New("limit must be 1..10")
				}
			}
			return c.Store.Search(ctx, p.ID, a.Query, a.Topic, limit)
		case p.ID + "_read_chunk":
			var a struct {
				ID string `json:"chunk_id"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.ID) == "" || len(a.ID) > 128 {
				return nil, errors.New("chunk_id required, <=128 bytes")
			}
			return c.Store.Get(p.ID, a.ID)
		case p.ID + "_list_sources":
			var a struct {
				Offset int  `json:"offset"`
				Limit  *int `json:"limit"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			limit := 50
			if a.Limit != nil {
				limit = *a.Limit
			}
			if a.Offset < 0 || limit < 1 || limit > 100 {
				return nil, errors.New("offset >= 0; limit 1..100")
			}
			all := c.Store.Sources(p.ID)
			start := a.Offset
			if start > len(all) {
				start = len(all)
			}
			end := start + limit
			if end > len(all) {
				end = len(all)
			}
			return map[string]any{"sources": all[start:end], "total": len(all), "next_offset": end, "has_more": end < len(all)}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownTool, name)
}
