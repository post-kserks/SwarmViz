# SwarmViz v3.1 — Multi-Agent Swarm Visualizer

SwarmViz — это высокопроизводительный локальный инструмент визуализации работы мультиагентных систем в реальном времени.

Приложение позволяет визуализировать граф агентов, отслеживать точечные инкрементальные изменения (diff) в файлах, видеть привязку правок к конкретным агентам (Attribution Engine) и анализировать динамику изменения объема кода (LOC Delta Chart).

![Theme](https://img.shields.io/badge/Theme-Dark%20Cyberpunk-purple)
![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)
![React](https://img.shields.io/badge/React-18-61DAFB?logo=react)
![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript)

---

## ⚡ Особенности и архитектура

- **Single Binary Packaging**: Весь веб-интерфейс компилируется в статику и вшивается прямо в исполняемый Go-бинарник (`//go:embed`). Внешние зависимости для запуска не требуются!
- **Claim-then-Diff с инкрементальным теневым копированием**:
  1. Перед правкой файла агент или оркестратор объявляет claim (`ClaimFile`).
  2. Вся теневая копия репозитория хранится исключительно на диске в `/tmp/swarmviz_run_<pid>/` (OOM-защита).
  3. Изменения вычисляются по алгоритму Майерса (`sergi/go-diff`) с защитой от состояния гонок (per-file `sync.Mutex`).
- **4 Интерфейсных Виджета (Сетка 30% | 45% | 25%)**:
  1. **Subagent Map**: Иерархический граф агентов (`@xyflow/react` + `dagre`), пульсация статусов (`RUNNING`, `WAITING`, `DONE`, `ERROR`), просмотр логов и экспорт схемы в PNG.
  2. **Live Edit Stream**: Виртуализированная лента правок (`react-window`) с подсветкой добавленний/удалений и детекцией конфликтов правок.
  3. **Workspace File Tree**: Дерево файлов с счётчиком правок и индикаторами реального времени (🟢 — активный claim прямо сейчас, 🔵 — редактировался ранее).
  4. **LOC Delta Chart**: График накопленной дельт строк (`recharts`) со сглаживанием LTTB.

---

## 🚀 Быстрый старт

### Требования
- Инициализированный Git-репозиторий в целевой папке (`.git`).
- Установленный `git` в `$PATH`.

### Запуск готового бинарника
```bash
./swarmviz --path ./ --port 8942
```
После запуска откройте браузер по адресу: **http://127.0.0.1:8942**

### Одновременный запуск бэкенда и фронтенда (Dev Mode)
Для разработки бэкенда и фронтенда с горячей перезагрузкой (Vite Hot Reload) используйте скрипт `dev.sh`:
```bash
./dev.sh
```
Скрипт автоматически запустит:
- **Backend API / WS**: `http://127.0.0.1:8942`
- **Frontend Dev UI**: `http://localhost:3000`

### Завершение работы
Нажмите **`Ctrl + C`** в терминале. Приложение или скрипт выполнит Graceful Shutdown и аккуратно остановит все фоновые процессы.

---

## ⚙️ Флаги командной строки (CLI)

| Флаг | Короткий | По умолчанию | Описание |
|---|---|---|---|
| `--port` | `-p` | `8942` | Порт веб-интерфейса |
| `--path` | `-d` | `./` | Путь к отслеживаемому git-репозиторию |
| `--interval` | | `300ms` | Дебаунс серии событий по одному файлу |
| `--claim-ttl` | | `2s` | TTL claim'а агента на файл до автоснятия |
| `--buffer-size` | | `5000` | Размер кольцевого буфера WebSocket-событий |
| `--host` | | `127.0.0.1` | Адрес прослушивания |
| `--log-level` | | `info` | Уровень логирования (`debug \| info \| warn \| error`) |
| `--max-repo-size` | | `500MB` | Порог отсечения большого репозитория при старте |
| `--disk-check-interval` | | `30s` | Периодичность проверки размера `/tmp` в сессии |
| `--api-token` | | *(пусто)* | Требовать `Authorization: Bearer <токен>` для ingest API |

---

## 🛠 Сборка из исходного кода

### 1. Сборка фронтенда

Каталог `frontend/dist/` не хранится в git (в нём лежит только заглушка
`.gitkeep`, чтобы директива `//go:embed` находила путь). Поэтому **фронтенд
нужно собрать до сборки Go-бинарника** — иначе UI внутри бинарника будет пустым
и при старте вы увидите предупреждение. API и WebSocket при этом работают.

```bash
cd frontend
npm install
npm run build
cd ..
```

### 2. Сборка единого Go-бинарника со вшитым UI
```bash
go build -o swarmviz main.go
```

### 3. Глобальная установка в систему (запуск из любой директории)

Чтобы команда `swarmviz` была доступна из любого места вашего терминала:

#### Вариант A: Копирование в систему
```bash
sudo cp swarmviz /usr/local/bin/
```

#### Вариант B: Установка через Go (`go install`)
Убедитесь, что `$GOPATH/bin` (обычно `~/go/bin`) добавлен в ваш `$PATH`:
```bash
# Для zsh (macOS по умолчанию) добавьте в ~/.zshrc:
export PATH="$HOME/go/bin:$PATH"

# Выполните сборку и установку
cd frontend && npm run build && cd ..
go install .
```

После установки вы можете перейти в **любой git-репозиторий** на вашем компьютере и просто запустить:
```bash
cd /path/to/any/project
swarmviz
```
и открыть в браузере `http://127.0.0.1:8942`.

### 4. Запуск тестов
Для запуска полного набора unit/integration тестов с детектором гонок (`race detector`):
```bash
go test -v -race ./...
```

---

## 🔌 Интеграция с оркестраторами

### Вариант A: HTTP Ingest API (любой язык)

Запущенный бинарник принимает события агентов по HTTP, поэтому оркестратор на
Python, Node, Bash или чём угодно ещё может наполнять граф без единой строки Go.

| Метод | Эндпоинт | Тело | Ответ |
|---|---|---|---|
| `GET` | `/api/health` | — | `{"status":"ok"}` (без токена) |
| `GET` | `/api/state` | — | Полный снимок: агенты, рёбра, claim'ы, дерево файлов, LOC |
| `POST` | `/api/agents` | `{"agent_id","agent_type","parent_id","label"}` | `201 {"agent_id"}` |
| `POST` | `/api/agents/{id}/status` | `{"status"}` | `202` |
| `POST` | `/api/agents/{id}/terminate` | `{"reason"}` | `202` |
| `POST` | `/api/agents/{id}/log` | `{"level","message"}` | `202` |
| `POST` | `/api/edges` | `{"from_id","to_id","kind"}` | `201` |
| `POST` | `/api/claims` | `{"agent_id","file"}` | `201 {"claim_id"}` |
| `DELETE` | `/api/claims/{claim_id}` | — | `200` |
| `POST` | `/api/events` | `[{"type","data"}, ...]` | `202 {"applied"}` |

Допустимые значения: `agent_type` — `orchestrator \| teamwork \| challenger \| worker`;
`status` — `IDLE \| RUNNING \| WAITING \| DONE \| ERROR`;
`kind` — `TASK_DELEGATION \| DATA_PASS \| REVIEW_REQUEST`;
`level` — `debug \| info \| warn \| error`.

Неизвестные поля и значения отклоняются с `400`. Пакетный `/api/events`
применяется по принципу «всё или ничего»: одна ошибка отменяет весь пакет,
чтобы интерфейс не показывал наполовину применённую пачку событий.

Полный цикл «объявил claim → отредактировал файл → снял claim» — именно то, что
превращает правку из `external` в `claimed` и привязывает её к агенту:

```bash
# 1. Регистрируем агентов и связь между ними
curl -X POST localhost:8942/api/agents \
  -d '{"agent_id":"root","agent_type":"orchestrator","label":"Оркестратор"}'
curl -X POST localhost:8942/api/agents \
  -d '{"agent_id":"w1","agent_type":"worker","parent_id":"root","label":"Воркер 1"}'
curl -X POST localhost:8942/api/edges \
  -d '{"from_id":"root","to_id":"w1","kind":"TASK_DELEGATION"}'

# 2. Объявляем claim ПЕРЕД правкой файла
CLAIM=$(curl -s -X POST localhost:8942/api/claims \
  -d '{"agent_id":"w1","file":"main.go"}' | jq -r .claim_id)

# 3. Правим файл — diff будет атрибутирован агенту w1
echo '// правка' >> main.go

# 4. Снимаем claim (иначе он снимется сам по --claim-ttl)
curl -X DELETE localhost:8942/api/claims/$CLAIM
```

Готовый пример, поднимающий целый рой, — `examples/demo_swarm.py`:

```bash
./swarmviz --path /tmp/demo-repo --claim-ttl 10s &
python3 examples/demo_swarm.py --repo /tmp/demo-repo
```

**Безопасность.** По умолчанию сервер слушает `127.0.0.1`, и ingest API открыт.
Если вы меняете `--host`, закройте API общим секретом:

```bash
./swarmviz --host 0.0.0.0 --api-token "$(openssl rand -hex 16)"
# далее: curl -H "Authorization: Bearer <токен>" ...
```

### Вариант B: Go-интерфейс `AgentEventHub` (внутри процесса)

```go
type AgentEventHub interface {
    AgentCreated(id string, agentType AgentType, parentID string, label string)
    AgentStatusChanged(id string, status AgentStatus)
    AgentTerminated(id string, reason string)
    AgentEdge(fromID, toID string, kind EdgeKind)
    
    ClaimFile(agentID string, filePath string) (claimID string)
    ReleaseClaim(claimID string)
    
    AgentLog(agentID string, level string, message string)
    RegisterControlHandler(handler func(agentID string, action ControlAction) error)
}
```

---

## 📄 Лицензия

MIT License.
