# Протокол v1

Оболочка: `{protocol_version:1,type,request_id,payload}`. `request_id` уникален в пределах актёра и сеанса клиента; UI добавляет случайный nonce. Спонтанные события сервера имеют пустой ID. Схемы — `contracts/client-message.schema.json`, `contracts/protocol.ts`, Go-типы — `pkg/protocol`.

HTTP-регистрация/вход возвращают HttpOnly cookie. `/ws` требует сессию и same-origin. Анонимный сокет получает 401, чужой Origin — 403. Пароли хешируются, в SQLite хранятся хеши токенов сессий.

| Запрос | Ответ / назначение |
|---|---|
| `player.init {}` | `player.ready {user,state,capabilities}`; ID берётся из сессии |
| `room.get/create/join/leave/role/ready/start/reset` | `room.state`; один игрок закреплён за одной комнатой |
| `player.input {seq,x,y,z,yaw}` | Авторитетная проверка; 20 Гц `world.snapshot` только своей комнаты |
| `player.teleport {target}` | `player.position`; только фиксированные точки NPC/отсеков |
| `game.action {action:"get_state"}` | Текущее `game.state` своей комнаты, без расхода |
| `game.preview {action,expected_version}` | Точный прогноз `before/after`, не меняет состояние |
| `game.action {action,expected_version}` | `game.state` или `decision.pending`; роль, цена и версия проверяются сервером |
| `investigation.solve {clues,expected_version}` | `investigation.result` и общее обновление состояния |
| `decision.vote {approve,expected_version}` | Согласие на предложенный финал; повторный голос не выполняет его заново |
| `chat.send {text}` | Чат только комнаты; длина и роль проверяются хостом |
| `agent.talk {agent_id,text}` | Асинхронный `agent.reply`, ping и движение продолжаются |
| `dialogue.history {agent_id}` | Личная история для игрока, NPC и поколения миссии |
| `experiment.start {}` | Идемпотентное создание световых часов |
| `experiment.command {action,expected_version,...}` | Роли join, predict, speed, run; версия опыта отдельна от версии ресурсов |
| `experiment.preview {speed}` | Расчёт без совместного зачёта |
| `ping {timestamp}` | `pong`, исходный request_id |

Ресурсные операции, расследование и опыт требуют ожидаемой версии. Повтор ресурсоизменяющего запроса с тем же ID и тем же payload возвращает сохранённый результат. Тот же ID с другим payload отвергается. При таймауте нельзя автоматически создавать новую ресурсную операцию: сначала запросите состояние.

`agent.reply` содержит `mode`, `evidence`, `citations`, `analysis`, `tool_transport`. `mode=model` — успешный ответ локальной модели; `knowledge_base` — документы; `rules` — серверные проверки. `tool_transport` отличает MCP stdio от внутреннего резервного поиска. Сам факт корректных ID ссылок не доказывает истинность каждого предложения модели.

Мировые HTTP API требуют авторизации: `/api/world/meta`, `/api/world/interest`, `/api/world/resource/{id}`. Только выбранные сервером зависимости разрешены к загрузке, ответ `no-store`. Внутренний `/api/internal/experiment` ограничен loopback и capability-токеном хоста; это не публичный игровой API.


## Интерьер и модель · ноябрь 2026

`universe.state.objects` сохраняет совместимое поле `open` (целевое состояние двери) и добавляет `motion_at` (Unix ms), `motion_from` (раскрытие 0–1), необязательное `locked` (причина блокировки). Клиент и сервер вычисляют раскрытие за 900 мс от этих полей; размер зазора определяет коллизию. `layout_version` обеспечивает однократную миграцию существующих экспедиций. `door_moving`, `door_occupied`, `door_locked` и `fixed_architecture` дают понятные причины отказа.

`agent.reply` дополняется `model` и необязательным `fallback_reason`. Поле `mode=model` по-прежнему означает только принятый ответ модели; наличие настроенного имени модели само по себе не подтверждает генерацию. Кнопки обзора клиента больше не вызывают `player.teleport`; прежний endpoint оставлен для совместимости.
