package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feeders/internal/knowledge"
	"feeders/internal/rag"
)

const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

func run(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	store, _ := rag.Read(strings.NewReader(""))
	var out bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")), &out, knowledge.Catalog{Store: store}); err != nil {
		t.Fatal(err)
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, r)
	}
	return responses
}
func TestHandshakeToolsAndEmptyCorpus(t *testing.T) {
	r := run(t, initialize, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dzhanibekov_search","arguments":{"query":"питание"}}}`)
	if len(r) != 3 {
		t.Fatal(r)
	}
	if r[0]["result"].(map[string]any)["protocolVersion"] != Version {
		t.Fatal(r[0])
	}
	if len(r[1]["result"].(map[string]any)["tools"].([]any)) != 40 {
		t.Fatal(r[1])
	}
	result := r[2]["result"].(map[string]any)
	if result["isError"] != false || result["structuredContent"].(map[string]any)["status"] != "empty_corpus" {
		t.Fatal(result)
	}
}
func TestProtocolErrorsAndNotifications(t *testing.T) {
	r := run(t, `{`, `[]`, `{"jsonrpc":"2.0","id":true,"method":"ping"}`, `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`, `{"jsonrpc":"2.0","method":"unknown_notification"}`, initialize, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":2,"method":"unknown"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"unknown"}}`, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"dzhanibekov_search","arguments":{"query":"x","limit":0}}}`)
	for i, code := range []float64{-32700, -32600, -32600, -32600} {
		if r[i]["error"].(map[string]any)["code"] != code {
			t.Fatal(r[i])
		}
	}
	if len(r) != 8 {
		t.Fatal(r)
	}
	if r[5]["error"].(map[string]any)["code"] != float64(-32601) || r[6]["error"].(map[string]any)["code"] != float64(-32602) {
		t.Fatal(r)
	}
	if r[7]["result"].(map[string]any)["isError"] != true {
		t.Fatal(r[7])
	}
}
