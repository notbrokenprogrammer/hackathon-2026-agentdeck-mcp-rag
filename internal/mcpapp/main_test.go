package mcpapp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMCPRuntimeConfigurationChild(t *testing.T) {
	if os.Getenv("LASTCREW_MCP_CONFIG_CHILD") != "true" {
		return
	}
	args := []string{}
	if os.Getenv("LASTCREW_MCP_CHECK_ONLY") == "true" {
		args = []string{"-check"}
	}
	Main(args)
	os.Exit(0)
}

func TestMCPRuntimeUsesSharedHybridConfigurationWithoutEagerWrites(t *testing.T) {
	root := t.TempDir()
	data, corpus := filepath.Join(root, "unused-data"), filepath.Join(root, "articles.jsonl")
	if err := os.WriteFile(corpus, []byte(`{"id":"one","text":"Питание корабля.","agents":["savinykh"],"source":{"id":"manual","title":"Руководство"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"LASTCREW_MCP_CONFIG_CHILD": "true", "DATA_DIR": data, "RAG_CHUNKS_FILE": corpus, "RAG_HYBRID_ENABLED": "true", "RAG_EMBEDDING_MODEL": "embeddinggemma", "RAG_EMBEDDING_URL": "http://127.0.0.1:11434", "RAG_EMBEDDING_CACHE": "", "MCP_WORLD_URL": "", "MCP_WORLD_TOKEN": "", "LASTCREW_MCP_CHECK_ONLY": "false"} {
		t.Setenv(key, value)
	}
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"rag_status","arguments":{}}}
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPRuntimeConfigurationChild$")
	command.Stdin = strings.NewReader(input)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatal("MCP stdout contains nonprotocol output", string(out))
	}
	var response struct {
		Result struct {
			Structured struct {
				Retrieval struct {
					Configured bool   `json:"configured"`
					Model      string `json:"model"`
				} `json:"retrieval"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &response); err != nil || !response.Result.Structured.Retrieval.Configured || response.Result.Structured.Retrieval.Model != "embeddinggemma" {
		t.Fatal("MCP serving did not configure shared hybrid index", string(out), err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("metadata-only serving indexed or created DATA_DIR", err)
	}
	// Check mode must not validate/use unrelated embedding configuration.
	t.Setenv("LASTCREW_MCP_CHECK_ONLY", "true")
	t.Setenv("RAG_EMBEDDING_URL", "not-an-origin")
	command = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPRuntimeConfigurationChild$")
	out, err = command.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "total_loaded_chunks") {
		t.Fatal("corpus check activated embedding configuration", string(out), err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("check created DATA_DIR", err)
	}
}
