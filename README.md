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

---

## 🛠 Сборка из исходного кода

### 1. Сборка фронтенда
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

## 🔌 Интеграция с оркестраторами (AgentEventHub)

Внутри процесса Go оркестраторы подключаются через Go-интерфейс `AgentEventHub`:

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
