package game

import (
	"context"
	"encoding/json"
	"feeders/internal/agents"
	"feeders/internal/knowledge"
	"feeders/internal/npc"
	"feeders/internal/rag"
	"feeders/pkg/protocol"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"time"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

func expCmd(t *testing.T, m *Module, s *fakeSession, action string, extra map[string]any) protocol.Envelope {
	t.Helper()
	r := m.rooms[m.RoomOf(s.user.ID)]
	p := map[string]any{"action": action, "expected_version": r.Experiment.Version}
	for k, v := range extra {
		p[k] = v
	}
	e, _ := protocol.Message("experiment.command", action, p)
	m.experimentCommand(context.Background(), s, e)
	return s.messages[len(s.messages)-1]
}
func TestCooperativeExperimentAndIndividualLearning(t *testing.T) {
	for _, speed := range []float64{0, .6, .8} {
		t.Run(string(rune('a'+int(speed*10))), func(t *testing.T) {
			m, store := roomFixture(t, t.TempDir())
			defer store.Close()
			a := attach(t, m, "alice")
			b := attach(t, m, "boris")
			roomCmd(t, m, a, "room.create", map[string]any{})
			roomCmd(t, m, b, "room.join", map[string]string{"code": m.RoomOf("alice")})
			for _, s := range []*fakeSession{a, b} {
				role := "captain"
				if s == b {
					role = "engineer"
				}
				roomCmd(t, m, s, "room.role", map[string]string{"role": role})
				roomCmd(t, m, s, "room.ready", map[string]bool{"ready": true})
			}
			roomCmd(t, m, a, "room.start", map[string]any{})
			if _, err := m.startExperiment("alice"); err != nil {
				t.Fatal(err)
			}
			expCmd(t, m, a, "join", map[string]any{"role": "observer"})
			expCmd(t, m, a, "predict", map[string]any{"prediction": "same"})
			if got := expCmd(t, m, a, "run", nil); got.Type != "error" {
				t.Fatal("one account completed cooperation")
			}
			expCmd(t, m, b, "join", map[string]any{"role": "operator"})
			expCmd(t, m, b, "predict", map[string]any{"prediction": "different"})
			expCmd(t, m, b, "speed", map[string]any{"speed": speed})
			if got := expCmd(t, m, a, "run", nil); got.Type != "experiment.accepted" {
				t.Fatal(got.Type, string(got.Payload))
			}
			room := m.rooms[m.RoomOf("alice")]
			if math.Abs(room.Experiment.Gamma-1/math.Sqrt(1-speed*speed)) > 1e-9 {
				t.Fatal(room.Experiment)
			}
			if room.Learning["alice"].Correct != (speed == 0) || room.Learning["boris"].Correct != (speed != 0) {
				t.Fatal(room.Learning)
			}
			v, err := m.View("alice")
			if err != nil || v.Learning == nil {
				t.Fatal("lesson not passed to agent", v, err)
			}
			if state := m.engineFor("alice").GetState(); state.Version != 1 || state.Energy != 35 {
				t.Fatal("experiment changed resources")
			}
			if got := expCmd(t, m, a, "run", nil); got.Type != "error" {
				t.Fatal("experiment completed twice")
			}
		})
	}
}
func TestRealMCPProcessStartsWorldFromModelToolCall(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	a := attach(t, m, "alice")
	roomCmd(t, m, a, "room.create", map[string]any{})
	roomCmd(t, m, a, "room.role", map[string]string{"role": "commander"})
	roomCmd(t, m, a, "room.ready", map[string]bool{"ready": true})
	roomCmd(t, m, a, "room.start", map[string]any{})
	viewFile := filepath.Join(t.TempDir(), "view.json")
	m.WithCrewProjection(viewFile)
	m.EnableWorldTools("test-only-capability")
	mux := http.NewServeMux()
	m.registerExperimentHTTP(mux)
	world := httptest.NewServer(mux)
	defer world.Close()
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		var messages []struct {
			Role string `json:"role"`
		}
		json.Unmarshal(body["messages"], &messages)
		if messages[len(messages)-1].Role != "tool" {
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"function": map[string]any{"name": "dzhanibekov_start_experiment", "arguments": map[string]string{"type": "light_clock"}}}}}})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": "Общая установка световых часов позволяет сравнить интервалы в вагоне и на платформе. [ship-lab-light-clock]"}})
		}
	}))
	defer model.Close()
	corpus, err := rag.Load(filepath.Join("..", "..", "knowledge"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := npc.NewLocal(knowledge.Catalog{Store: corpus, ScenarioOverride: CallCrewTool}, model.URL, "fake-model-for-protocol-test")
	if err != nil {
		t.Fatal(err)
	}
	// Build the real MCP process from this checkout; release binaries are not
	// present on a clean checkout and may be stale on a developer machine.
	mcpName := "mcp-tools"
	if runtime.GOOS == "windows" {
		mcpName += ".exe"
	}
	mcpBinary := filepath.Join(t.TempDir(), mcpName)
	buildCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", mcpBinary, "./cmd/mcp-tools")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build MCP test process: %v\n%s", err, output)
	}
	local.WithMission(m).WithMCP(mcpBinary, viewFile, filepath.Join("..", "..", "knowledge")).WithWorldTools(world.URL, "test-only-capability")
	service := agents.NewCrewService(local, nil)
	reply, err := service.Talk(context.Background(), agents.Request{PlayerID: "alice", Scope: m.memoryScope("alice"), AgentID: "dzhanibekov", Text: "Покажи опыт со световыми часами"})
	if err != nil || reply.Mode != "model" || modelCalls.Load() < 2 {
		t.Fatal(reply, err, modelCalls.Load())
	}
	if m.rooms[m.RoomOf("alice")].Experiment == nil {
		t.Fatal("model tool call did not create the world experiment")
	}
}
