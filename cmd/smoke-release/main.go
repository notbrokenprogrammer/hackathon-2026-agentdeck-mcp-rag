// smoke-release exercises the compiled host, rather than an httptest handler.
package main

import (
	"bytes"
	"encoding/json"
	"feeders/pkg/protocol"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	if len(os.Args) != 2 {
		panic("usage: smoke-release path/to/compiled-host")
	}
	binary, e := filepath.Abs(os.Args[1])
	check(e)
	dir, e := os.MkdirTemp("", "last-crew-release-")
	check(e)
	defer os.RemoveAll(dir)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	check(e)
	addr := listener.Addr().String()
	listener.Close()
	base := "http://" + addr
	var process *exec.Cmd
	start := func(model bool) {
		process = exec.Command(binary)
		process.Env = append(os.Environ(), "HOST_ADDR="+addr, "DATA_DIR="+dir, "DEV_WEB_DIR=", "OLLAMA_ENABLED="+fmt.Sprint(model), "OLLAMA_URL=http://127.0.0.1:1")
		process.Stdout = io.Discard
		process.Stderr = io.Discard
		check(process.Start())
		for i := 0; i < 100; i++ {
			r, e := http.Get(base + "/healthz")
			if e == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		panic("compiled host did not start")
	}
	stop := func() {
		if process != nil && process.Process != nil {
			_ = process.Process.Kill()
			_ = process.Wait()
			process = nil
		}
	}
	defer stop()
	start(false)
	for _, asset := range []string{"index.html", "src/main.js", "css/style.css", "css/universe.css", "src/scene.js", "src/net/NetworkManager.js", "src/ui/UniversePanel.js", "src/ui/FlightDeck.js", "src/world/LivingWorld.js", "src/crew/BridgeCrew.js", "src/crew/CrewLabels.js", "src/interior/ShipInterior.js", "src/interior/ReferenceArchitecture.js", "src/interior/ReferenceEquipment.js", "src/interior/SurfaceMaterials.js", "src/render/FrameBudget.js", "src/render/TextureQuality.js", "src/render/SoftwareRenderer.js", "src/world/StreamingWorld.js"} {
		response, err := http.Get(base + "/" + asset)
		check(err)
		actual, err := io.ReadAll(response.Body)
		response.Body.Close()
		check(err)
		expected, err := os.ReadFile(filepath.Join("web", asset))
		check(err)
		if response.StatusCode != 200 || !bytes.Equal(actual, expected) {
			panic("embedded frontend differs from source: " + asset)
		}
	}
	clients := []*http.Client{}
	sockets := []*websocket.Conn{}
	call := func(c *websocket.Conn, kind, id string, p any) protocol.Envelope {
		packet, e := protocol.Message(kind, id, p)
		check(e)
		check(c.WriteJSON(packet))
		check(c.SetReadDeadline(time.Now().Add(12 * time.Second)))
		for {
			var got protocol.Envelope
			check(c.ReadJSON(&got))
			if got.RequestID == id {
				if got.Type == "error" {
					panic(kind + ": " + string(got.Payload))
				}
				return got
			}
		}
	}
	connect := func(client *http.Client) *websocket.Conn {
		req, _ := http.NewRequest("GET", base, nil)
		header := http.Header{"Origin": []string{base}}
		for _, cookie := range client.Jar.Cookies(req.URL) {
			header.Add("Cookie", cookie.String())
		}
		c, _, e := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", header)
		check(e)
		call(c, "player.init", "init", map[string]any{})
		return c
	}
	for _, name := range []string{"release_captain", "release_engineer"} {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
		body, _ := json.Marshal(map[string]string{"username": name, "password": "disposable-release-test"})
		r, e := client.Post(base+"/api/auth/register", "application/json", bytes.NewReader(body))
		check(e)
		r.Body.Close()
		if r.StatusCode != 201 {
			panic("register failed")
		}
		clients = append(clients, client)
		sockets = append(sockets, connect(client))
	}
	a, b := sockets[0], sockets[1]
	reply := call(a, "room.create", "room", map[string]string{"name": "Release smoke"})
	var room map[string]any
	check(json.Unmarshal(reply.Payload, &room))
	code := room["code"].(string)
	call(b, "room.join", "join", map[string]string{"code": code})
	for i, c := range sockets {
		role := "captain"
		if i == 1 {
			role = "engineer"
		}
		call(c, "room.role", "role", map[string]string{"role": role})
		call(c, "room.ready", "ready", map[string]bool{"ready": true})
	}
	call(a, "room.start", "start", map[string]any{})
	// Sunday features through the actual compiled host, with two authenticated clients.
	readUniverse := func(c *websocket.Conn, id string) map[string]any {
		packet := call(c, "universe.get", id, map[string]any{})
		var world map[string]any
		check(json.Unmarshal(packet.Payload, &world))
		return world
	}
	u := readUniverse(a, "universe-initial")
	initialObjects := len(u["objects"].([]any))
	call(a, "universe.admin", "universe-settings", map[string]any{"expected_version": u["version"], "settings": map[string]any{"npc_count": 96, "ambient_color": "#ff8855"}})
	u = readUniverse(b, "universe-peer")
	if u["settings"].(map[string]any)["npc_count"] != float64(96) {
		panic("universe settings not shared")
	}
	call(a, "universe.interact", "crate-pickup", map[string]any{"expected_version": u["version"], "object_id": "crate-cryo", "action": "pickup"})
	u = readUniverse(b, "carried-crate")
	if u["objects"].([]any)[1].(map[string]any)["held_by"] == "" {
		panic("pickup not shared")
	}
	call(a, "universe.interact", "crate-drop", map[string]any{"expected_version": u["version"], "object_id": "crate-cryo", "action": "drop"})
	call(b, "agent.talk", "plant-world", map[string]string{"agent_id": "ryazansky", "text": "Создай сад"})
	u = readUniverse(a, "generated-world")
	if len(u["objects"].([]any)) != initialObjects+4 {
		panic("agent did not create shared environment")
	}
	call(b, "telemetry.report", "client-metrics", map[string]any{"fps": 60, "ping_ms": 12, "frame_ms": 16.7, "draw_calls": 128, "triangles": 25000, "renderer": "webgl", "hidden": false})
	metricsReply := call(a, "telemetry.get", "admin-metrics", map[string]any{})
	var metrics struct {
		Clients []map[string]any `json:"clients"`
	}
	check(json.Unmarshal(metricsReply.Payload, &metrics))
	if len(metrics.Clients) != 2 {
		panic("missing client telemetry rows")
	}
	foundMetrics := false
	for _, c := range metrics.Clients {
		if c["fps"] == float64(60) && c["ping_ms"] == float64(12) {
			foundMetrics = true
		}
	}
	if !foundMetrics {
		panic("client metrics not delivered")
	}
	preview := call(a, "game.preview", "preview", map[string]any{"action": "repair_cooling", "expected_version": 1})
	var forecast struct {
		Allowed bool `json:"allowed"`
		After   struct {
			Energy int `json:"energy"`
		} `json:"after"`
	}
	check(json.Unmarshal(preview.Payload, &forecast))
	if !forecast.Allowed || forecast.After.Energy != 25 {
		panic("server forecast does not match actual costs")
	}
	call(b, "game.action", "repair-id", map[string]any{"action": "repair_cooling", "expected_version": 1})
	call(b, "game.action", "repair-id", map[string]any{"action": "repair_cooling", "expected_version": 1})
	reply = call(a, "game.action", "state", map[string]string{"action": "get_state"})
	var state map[string]any
	check(json.Unmarshal(reply.Payload, &state))
	if state["energy"] != float64(25) || state["version"] != float64(2) {
		panic("state did not synchronize")
	}
	reply = call(a, "agent.talk", "talk", map[string]string{"agent_id": "dzhanibekov", "text": "Как восстановить охлаждение реактора?"})
	var agent protocol.AgentReply
	check(json.Unmarshal(reply.Payload, &agent))
	if agent.ToolTransport != "MCP stdio" {
		panic("actual host did not use stdio MCP: " + agent.ToolTransport)
	}
	call(a, "experiment.start", "lab", map[string]any{})
	version := 1
	experiment := func(c *websocket.Conn, action string, p map[string]any) {
		p["action"] = action
		p["expected_version"] = version
		call(c, "experiment.command", fmt.Sprint("exp-", version), p)
		version++
	}
	experiment(a, "join", map[string]any{"role": "observer"})
	experiment(b, "join", map[string]any{"role": "operator"})
	experiment(a, "predict", map[string]any{"prediction": "same"})
	experiment(b, "predict", map[string]any{"prediction": "different"})
	experiment(b, "speed", map[string]any{"speed": .6})
	experiment(a, "run", map[string]any{})
	reply = call(a, "room.get", "lab-result", map[string]any{})
	var result struct {
		Experiment struct {
			Gamma float64 `json:"gamma"`
		} `json:"experiment"`
	}
	check(json.Unmarshal(reply.Payload, &result))
	if result.Experiment.Gamma != 1.25 {
		panic("wrong experiment result")
	}
	r, e := clients[0].Get(base + "/api/world/interest")
	check(e)
	var plan struct {
		World     int `json:"world_bytes"`
		Budget    int `json:"budget"`
		Resources []struct {
			Size int `json:"size"`
		} `json:"resources"`
	}
	check(json.NewDecoder(r.Body).Decode(&plan))
	r.Body.Close()
	cached := 0
	for _, item := range plan.Resources {
		cached += item.Size
	}
	if plan.World == 0 || plan.Budget <= 0 || cached > plan.Budget {
		panic("cache exceeds budget")
	}
	for _, view := range []string{"bridge", "reactor", "bio", "habitat"} {
		response, err := clients[0].Get(base + "/api/world/interest?view=" + view)
		check(err)
		var focused struct {
			Resources []struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
			} `json:"resources"`
		}
		check(json.NewDecoder(response.Body).Decode(&focused))
		response.Body.Close()
		if response.StatusCode != 200 || len(focused.Resources) == 0 {
			panic("overview plan unavailable")
		}
		for _, resource := range focused.Resources {
			if resource.Kind != "object" {
				continue
			}
			asset, err := clients[0].Get(base + "/api/world/resource/" + resource.ID + "?view=" + view)
			check(err)
			asset.Body.Close()
			if asset.StatusCode != 200 {
				panic("overview resource authorization differs from plan")
			}
		}
	}
	r, e = clients[0].Get(base + "/__qa/enter")
	check(e)
	if len(r.Cookies()) != 0 {
		panic("QA entry accidentally shipped in release")
	}
	r.Body.Close()
	for _, c := range sockets {
		c.Close()
	}
	stop()
	start(true)
	r, e = clients[0].Get(base + "/api/auth/me")
	check(e)
	r.Body.Close()
	if r.StatusCode != 200 {
		panic("session lost after process restart")
	}
	a = connect(clients[0])
	defer a.Close()
	reply = call(a, "room.get", "saved-room", map[string]any{})
	check(json.Unmarshal(reply.Payload, &room))
	if room["code"] != code {
		panic("room lost after restart")
	}
	reply = call(a, "game.action", "saved-state", map[string]string{"action": "get_state"})
	check(json.Unmarshal(reply.Payload, &state))
	if state["version"] != float64(2) {
		panic("game progress lost")
	}
	u = readUniverse(a, "restored-universe")
	if u["settings"].(map[string]any)["npc_count"] != float64(96) || len(u["objects"].([]any)) != initialObjects+4 {
		panic("universe lost after restart")
	}
	reply = call(a, "agent.talk", "fallback", map[string]string{"agent_id": "dzhanibekov", "text": "Объясни проще"})
	check(json.Unmarshal(reply.Payload, &agent))
	if agent.Mode == "model" || !strings.Contains(agent.Text, "охлажд") {
		panic("missing-model fallback or persistent topic failed")
	}
	output := map[string]any{"compiled_host": "passed", "shared_universe": "passed", "interactive_pickup_drop": "passed", "agent_world_command": "passed", "client_telemetry": "passed", "universe_restore": "passed", "embedded_frontend_matches_source": "passed", "operation_forecast": "passed", "two_websocket_clients": "passed", "registration": "passed", "synchronized_resources": "passed", "idempotency": "passed", "sqlite_crash_restore": "passed", "session_restore": "passed", "actual_stdio_mcp": "passed", "shared_experiment_gamma": 1.25, "world_cache_ratio": float64(cached) / float64(plan.World), "unavailable_llm_fallback": "passed", "qa_fixture_excluded": "passed"}
	check(json.NewEncoder(os.Stdout).Encode(output))
}
