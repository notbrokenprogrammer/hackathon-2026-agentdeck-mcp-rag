# Изменения относительно New.zip

Все исходные файлы New.zip сохранены, кроме исполняемых файлов прежней сборки и данных аккаунтов. Бинарники пересобраны. Исходный архив не изменён.

Изменённые существующие файлы:

- `README.md`
- `docs/architecture.md`
- `scripts/build.ps1`
- `web/index.html`
- `web/css/style.css`
- `web/src/main.js`
- `web/src/scene.js`
- `internal/agents/agents.go`
- `internal/game/module.go`
- `internal/game/scenario.go`
- `cmd/game-server/main.go`
- `cmd/mcp-tools/main.go`

Добавленные модули: `internal/crew`, `internal/npc`, `internal/knowledge`, `internal/rag`, `internal/mission`, `internal/mcp`; адаптеры `internal/game/crew_bridge.go`, `internal/agents/crew_service.go`; сцена персонажей `web/src/crew/BridgeCrew.js`; `knowledge/`, схемы и примеры MCP.

Капитан — Савиных, инженер — Джанибеков. Рабочий движок ресурсов остаётся прежним ScenarioEngine. Полная инструкция и ограничения — README.md.
