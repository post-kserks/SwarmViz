#!/usr/bin/env python3
"""Мост между хуками Claude Code и ingest API SwarmViz.

Claude Code сам ничего не сообщает SwarmViz, поэтому его правки попадают в
diff как `external`. Этот скрипт вызывается из хуков, регистрирует сессию как
агента и столбит файл перед записью — тем самым выполняя контракт
claim-then-diff, на котором держится вся атрибуция.

Вызывается так (см. README.md и settings.example.json):

    swarmviz_hook.py session-start|pre-tool|post-tool|stop|session-end

Полезная нагрузка хука читается из stdin как JSON. Скрипт никогда не должен
ломать работу Claude Code: любая ошибка гасится, код возврата всегда 0.

Хуки ставятся глобально, в ~/.claude/settings.json, и действуют на все проекты
сразу. Поэтому скрипт сам выясняет, в каком репозитории идёт сессия, спрашивает
у сервера список проектов (`GET /api/projects`) и шлёт события на префикс того
проекта, который этому репозиторию соответствует: корневой проект живёт на
`/api/...`, остальные — на `/p/{id}/api/...`. Если подходящего проекта нет, хук
молча проходит мимо.

Настройка через окружение:
    SWARMVIZ_URL    базовый URL (по умолчанию http://127.0.0.1:8942)
    SWARMVIZ_TOKEN  bearer-токен, если сервер запущен с --api-token
    SWARMVIZ_REPO   жёстко закрепить сессию за одним репозиторием: события
                    уходят, только если сессия запущена именно в нём. Обычно
                    не нужно — проект определяется автоматически.
    SWARMVIZ_DEBUG  1 — писать диагностику в stderr
"""

import hashlib
import json
import os
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request

TIMEOUT = 1.5
# Первое обращение к проекту поднимает его рантайм (shadow-копия репозитория),
# а это заметно дольше обычного запроса. Резолв проекта и создание агента ждут
# дольше — иначе на холодном старте сессия не зарегистрируется вообще.
COLD_START_TIMEOUT = 8.0
EDIT_TOOLS = {"Edit", "Write", "NotebookEdit", "MultiEdit"}
DELEGATION_TOOL = "Task"

BASE_URL = os.environ.get("SWARMVIZ_URL", "http://127.0.0.1:8942").rstrip("/")
TOKEN = os.environ.get("SWARMVIZ_TOKEN") or None
DEBUG = os.environ.get("SWARMVIZ_DEBUG") == "1"


def debug(msg):
    if DEBUG:
        print(f"[swarmviz-hook] {msg}", file=sys.stderr)


def call(method, path, payload=None, base="", timeout=TIMEOUT):
    """Дёргает ingest API. Возвращает разобранный ответ или None при любой беде.

    `base` — префикс проекта из /api/projects: пустая строка для корневого,
    `/p/{id}` для остальных.

    SwarmViz — вспомогательная визуализация, а не критичная зависимость:
    если сервер не запущен, хук обязан молча пройти мимо.
    """
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(BASE_URL + base + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if TOKEN:
        req.add_header("Authorization", "Bearer " + TOKEN)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read()
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as exc:
        debug(f"{method} {base}{path} -> HTTP {exc.code}: {exc.read()[:200]}")
    except Exception as exc:  # соединение, таймаут, кривой JSON — всё равно
        debug(f"{method} {base}{path} -> {exc}")
    return None


# --------------------------------------------------------------------------
# Состояние сессии
#
# Нужно для двух вещей: помнить активный claim по каждому файлу, чтобы снять
# предыдущий перед новым (иначе claim'ы копятся до истечения TTL и засоряют
# список активных), и кэшировать разрешённый проект, чтобы не дёргать
# /api/projects на каждый вызов хука.
# --------------------------------------------------------------------------

def state_path(session_id):
    safe = hashlib.sha1(session_id.encode()).hexdigest()[:16]
    return os.path.join(tempfile.gettempdir(), f"swarmviz_hook_{safe}.json")


def load_state(session_id):
    try:
        with open(state_path(session_id)) as fh:
            return json.load(fh)
    except Exception:
        return {"agent_created": False, "claims": {}, "workers": {}, "project": None}


def save_state(session_id, state):
    path = state_path(session_id)
    try:
        tmp = path + ".tmp"
        with open(tmp, "w") as fh:
            json.dump(state, fh)
        os.replace(tmp, path)
    except Exception as exc:
        debug(f"не смог сохранить состояние: {exc}")


def drop_state(session_id):
    try:
        os.unlink(state_path(session_id))
    except Exception:
        pass


# --------------------------------------------------------------------------
# Идентификаторы и пути
# --------------------------------------------------------------------------

def agent_id_for(session_id):
    return "claude-" + (session_id or "unknown")[:8]


def worker_id_for(session_id, tool_input):
    """Стабильный id сабагента: PreToolUse и PostToolUse должны сойтись."""
    seed = "{}|{}|{}".format(
        session_id,
        tool_input.get("subagent_type", ""),
        tool_input.get("description", ""),
    )
    return "sub-" + hashlib.sha1(seed.encode()).hexdigest()[:8]


def git_output(cwd, *args):
    try:
        out = subprocess.run(
            ["git"] + list(args),
            cwd=cwd, capture_output=True, text=True, timeout=TIMEOUT,
        )
        if out.returncode == 0 and out.stdout.strip():
            return out.stdout.strip()
    except Exception as exc:
        debug(f"git {' '.join(args)} не отработал: {exc}")
    return None


def session_root(payload):
    """Корень репозитория, в котором работает сама сессия Claude Code."""
    cwd = payload.get("cwd") or os.getcwd()
    return git_output(cwd, "rev-parse", "--show-toplevel") or cwd


def main_repo_root(payload):
    """Корень основного репозитория, если сессия идёт в git-worktree.

    Claude Code часто работает в worktree (.claude/worktrees/...), а SwarmViz
    следит за основным checkout'ом. `--git-common-dir` указывает на .git
    основного репозитория, его родитель и есть искомый корень.
    """
    cwd = payload.get("cwd") or os.getcwd()
    common = git_output(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
    if not common:
        return None
    return os.path.dirname(os.path.abspath(common)) or None


def is_within(child, parent):
    """True, если child — сам parent либо лежит внутри него."""
    child = os.path.abspath(child)
    parent = os.path.abspath(parent)
    if child == parent:
        return True
    return child.startswith(parent.rstrip(os.sep) + os.sep)


def fetch_projects():
    """Список проектов сервера, либо None если multi-project режим выключен.

    В одиночном режиме эндпоинта нет — сервер отвечает 404, и call() вернёт
    None. Это не ошибка, а сигнал работать по-старому: единственный проект на
    корневых путях.
    """
    projects = call("GET", "/api/projects", timeout=COLD_START_TIMEOUT)
    if isinstance(projects, list) and projects:
        return projects
    return None


def match_project(projects, candidates):
    """Проект, которому принадлежит репозиторий сессии.

    Порядок: точное совпадение пути (кандидаты идут по убыванию
    предпочтительности — сначала корень самой сессии, потом основной
    репозиторий worktree), затем самый глубокий проект, внутри которого сессия
    лежит. Глубина важна: если заданы и /home/pin/projects/foo, и
    /home/pin/projects, сессия из foo должна достаться foo, а не родителю.
    """
    for candidate in candidates:
        for p in projects:
            if os.path.abspath(p.get("path", "")) == os.path.abspath(candidate):
                return p

    for candidate in candidates:
        best = None
        for p in projects:
            path = p.get("path", "")
            if not path or not is_within(candidate, path):
                continue
            if best is None or len(os.path.abspath(path)) > len(os.path.abspath(best["path"])):
                best = p
        if best is not None:
            return best
    return None


def resolve_target(payload, state):
    """Куда слать события: {base, path, id, name}, либо None если репозиторий чужой.

    Успешный результат кэшируется в состоянии сессии. Неуспешный — нет: если
    сервер лежал в момент SessionStart, следующий хук попробует снова.
    """
    cached = state.get("project")
    if cached:
        return cached

    root = session_root(payload)
    pinned = os.environ.get("SWARMVIZ_REPO")
    if pinned:
        # Явное закрепление: сессия обязана идти именно в этом репозитории.
        pinned_abs = os.path.abspath(pinned)
        if not is_within(root, pinned_abs):
            debug(f"сессия вне {pinned_abs}, ничего не отправляю")
            return None
        candidates = [pinned_abs]
    else:
        candidates = [root]
        worktree_main = main_repo_root(payload)
        if worktree_main and os.path.abspath(worktree_main) != os.path.abspath(root):
            candidates.append(worktree_main)

    projects = fetch_projects()
    if projects is None:
        # Одиночный режим: единственный проект на корневых путях. Проверить,
        # тот ли это репозиторий, нечем, поэтому доверяем кандидату — так же
        # вело себя предыдущее поколение хука.
        target = {"base": "", "path": os.path.abspath(candidates[0]),
                  "id": "default",
                  "name": os.path.basename(candidates[0]) or "claude"}
        state["project"] = target
        return target

    matched = match_project(projects, candidates)
    if matched is None:
        debug(f"{root} не соответствует ни одному проекту SwarmViz, ничего не отправляю")
        return None

    target = {
        "base": matched.get("basePath", "") or "",
        "path": os.path.abspath(matched.get("path") or candidates[0]),
        "id": matched.get("id", "default"),
        "name": matched.get("name") or os.path.basename(candidates[0]) or "claude",
    }
    debug(f"{root} -> проект {target['id']} на {target['base'] or '/'}")
    state["project"] = target
    return target


def relative_path(target, file_path):
    """Путь относительно наблюдаемого репозитория, либо None если файл вне его.

    Хаб сравнивает claim с путём из diff, а тот всегда относительный от корня
    репозитория, так что абсолютный путь из tool_input сюда не годится.
    """
    if not file_path:
        return None
    root = target["path"]
    try:
        rel = os.path.relpath(os.path.abspath(file_path), root)
    except ValueError:
        return None
    if rel.startswith("..") or os.path.isabs(rel):
        debug(f"{file_path} вне наблюдаемого репозитория {root}")
        return None
    return rel


# --------------------------------------------------------------------------
# Действия
# --------------------------------------------------------------------------

def ensure_agent(session_id, target, state):
    """Создаёт агента сессии один раз за сессию."""
    if state.get("agent_created"):
        return
    ok = call("POST", "/api/agents", {
        "agent_id": agent_id_for(session_id),
        "agent_type": "orchestrator",
        "parent_id": "",
        "label": f"Claude · {target['name']}",
    }, base=target["base"], timeout=COLD_START_TIMEOUT)
    if ok is not None:
        state["agent_created"] = True


def release_previous_claim(target, state, rel):
    claim_id = state["claims"].pop(rel, None)
    if claim_id:
        call("DELETE", f"/api/claims/{claim_id}", base=target["base"])


def cmd_session_start(payload, target, state):
    session_id = payload.get("session_id", "")
    ensure_agent(session_id, target, state)
    call("POST", f"/api/agents/{agent_id_for(session_id)}/status",
         {"status": "RUNNING"}, base=target["base"])


def cmd_pre_tool(payload, target, state):
    session_id = payload.get("session_id", "")
    tool = payload.get("tool_name", "")
    tool_input = payload.get("tool_input") or {}
    agent = agent_id_for(session_id)
    base = target["base"]

    ensure_agent(session_id, target, state)

    if tool == DELEGATION_TOOL:
        worker = worker_id_for(session_id, tool_input)
        label = (tool_input.get("description")
                 or tool_input.get("subagent_type") or "subagent")
        call("POST", "/api/agents", {
            "agent_id": worker, "agent_type": "worker",
            "parent_id": agent, "label": label[:60],
        }, base=base)
        call("POST", "/api/edges", {
            "from_id": agent, "to_id": worker, "kind": "TASK_DELEGATION",
        }, base=base)
        call("POST", f"/api/agents/{worker}/status", {"status": "RUNNING"}, base=base)
        state["workers"][worker] = label[:60]
        return

    if tool not in EDIT_TOOLS:
        return

    rel = relative_path(target, tool_input.get("file_path"))
    if not rel:
        return

    # Снять свой предыдущий claim на этот файл, чтобы они не копились до TTL.
    release_previous_claim(target, state, rel)

    resp = call("POST", "/api/claims", {"agent_id": agent, "file": rel}, base=base)
    if resp and resp.get("claim_id"):
        state["claims"][rel] = resp["claim_id"]
    call("POST", f"/api/agents/{agent}/status", {"status": "RUNNING"}, base=base)


def cmd_post_tool(payload, target, state):
    session_id = payload.get("session_id", "")
    tool = payload.get("tool_name", "")
    tool_input = payload.get("tool_input") or {}
    agent = agent_id_for(session_id)
    base = target["base"]

    if tool == DELEGATION_TOOL:
        worker = worker_id_for(session_id, tool_input)
        call("POST", f"/api/agents/{worker}/terminate", {"reason": "completed"}, base=base)
        state["workers"].pop(worker, None)
        return

    if tool not in EDIT_TOOLS:
        return

    rel = relative_path(target, tool_input.get("file_path"))
    if not rel:
        return

    # Claim здесь намеренно НЕ снимается: watcher дебаунсит событие (--interval,
    # по умолчанию 300 мс), и diff считается уже после того, как PostToolUse
    # отработал. Снимем claim сейчас — правка уедет в `external`. Он отвалится
    # сам по --claim-ttl либо будет снят перед следующей правкой этого файла.
    call("POST", f"/api/agents/{agent}/log", {
        "level": "info", "message": f"{tool}: {rel}",
    }, base=base)


def cmd_stop(payload, target, state):
    session_id = payload.get("session_id", "")
    call("POST", f"/api/agents/{agent_id_for(session_id)}/status",
         {"status": "WAITING"}, base=target["base"])


def cmd_session_end(payload, target, state):
    session_id = payload.get("session_id", "")
    agent = agent_id_for(session_id)
    base = target["base"]
    for claim_id in list(state.get("claims", {}).values()):
        call("DELETE", f"/api/claims/{claim_id}", base=base)
    for worker in list(state.get("workers", {})):
        call("POST", f"/api/agents/{worker}/terminate", {"reason": "session ended"}, base=base)
    call("POST", f"/api/agents/{agent}/terminate", {"reason": "session ended"}, base=base)
    drop_state(session_id)


COMMANDS = {
    "session-start": cmd_session_start,
    "pre-tool": cmd_pre_tool,
    "post-tool": cmd_post_tool,
    "stop": cmd_stop,
    "session-end": cmd_session_end,
}


def main():
    if len(sys.argv) < 2 or sys.argv[1] not in COMMANDS:
        debug(f"использование: swarmviz_hook.py {'|'.join(COMMANDS)}")
        return 0

    command = sys.argv[1]
    try:
        raw = sys.stdin.read()
        payload = json.loads(raw) if raw.strip() else {}
    except Exception as exc:
        debug(f"не разобрал stdin: {exc}")
        payload = {}

    session_id = payload.get("session_id", "")
    state = load_state(session_id)

    try:
        target = resolve_target(payload, state)
        if target is None:
            return 0
        COMMANDS[command](payload, target, state)
    except Exception as exc:
        debug(f"{command} упал: {exc}")
    finally:
        if command != "session-end":
            save_state(session_id, state)
    return 0


if __name__ == "__main__":
    sys.exit(main())
