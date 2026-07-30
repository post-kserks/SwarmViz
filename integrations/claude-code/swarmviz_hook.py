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

Настройка через окружение:
    SWARMVIZ_URL    базовый URL (по умолчанию http://127.0.0.1:8942)
    SWARMVIZ_TOKEN  bearer-токен, если сервер запущен с --api-token
    SWARMVIZ_REPO   корень наблюдаемого репозитория (по умолчанию — git-корень
                    от cwd сессии)
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
EDIT_TOOLS = {"Edit", "Write", "NotebookEdit", "MultiEdit"}
DELEGATION_TOOL = "Task"

BASE_URL = os.environ.get("SWARMVIZ_URL", "http://127.0.0.1:8942").rstrip("/")
TOKEN = os.environ.get("SWARMVIZ_TOKEN") or None
DEBUG = os.environ.get("SWARMVIZ_DEBUG") == "1"


def debug(msg):
    if DEBUG:
        print(f"[swarmviz-hook] {msg}", file=sys.stderr)


def call(method, path, payload=None):
    """Дёргает ingest API. Возвращает разобранный ответ или None при любой беде.

    SwarmViz — вспомогательная визуализация, а не критичная зависимость:
    если сервер не запущен, хук обязан молча пройти мимо.
    """
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(BASE_URL + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if TOKEN:
        req.add_header("Authorization", "Bearer " + TOKEN)
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            body = resp.read()
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as exc:
        debug(f"{method} {path} -> HTTP {exc.code}: {exc.read()[:200]}")
    except Exception as exc:  # соединение, таймаут, кривой JSON — всё равно
        debug(f"{method} {path} -> {exc}")
    return None


# --------------------------------------------------------------------------
# Состояние сессии
#
# Нужно ровно для одного: помнить активный claim по каждому файлу, чтобы снять
# предыдущий перед новым. Хаб схлопывает повторные claim одного агента по
# agent_id, так что для атрибуции это не критично, но иначе claim'ы копятся до
# истечения TTL и засоряют список активных.
# --------------------------------------------------------------------------

def state_path(session_id):
    safe = hashlib.sha1(session_id.encode()).hexdigest()[:16]
    return os.path.join(tempfile.gettempdir(), f"swarmviz_hook_{safe}.json")


def load_state(session_id):
    try:
        with open(state_path(session_id)) as fh:
            return json.load(fh)
    except Exception:
        return {"agent_created": False, "claims": {}, "workers": {}}


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


def session_root(payload):
    """Корень репозитория, в котором работает сама сессия Claude Code."""
    cwd = payload.get("cwd") or os.getcwd()
    try:
        out = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            cwd=cwd, capture_output=True, text=True, timeout=TIMEOUT,
        )
        if out.returncode == 0 and out.stdout.strip():
            return out.stdout.strip()
    except Exception as exc:
        debug(f"git rev-parse не отработал: {exc}")
    return cwd


def repo_root(payload):
    """Репозиторий, за которым следит SwarmViz."""
    return os.path.abspath(os.environ.get("SWARMVIZ_REPO") or session_root(payload))


def out_of_scope(payload):
    """True, если сессия работает не в том репозитории, что смотрит SwarmViz.

    Хуки обычно ставят глобально, в ~/.claude/settings.json, а инстанс
    SwarmViz следит ровно за одним репозиторием. Без этой проверки любая
    сессия на машине заводила бы агента, к которому визуализатор никогда не
    сможет привязать ни одной правки.
    """
    configured = os.environ.get("SWARMVIZ_REPO")
    if not configured:
        return False
    mismatch = os.path.abspath(configured) != os.path.abspath(session_root(payload))
    if mismatch:
        debug(f"сессия вне {configured}, ничего не отправляю")
    return mismatch


def relative_path(payload, file_path):
    """Путь относительно наблюдаемого репозитория, либо None если файл вне его.

    Хаб сравнивает claim с путём из diff, а тот всегда относительный от корня
    репозитория, так что абсолютный путь из tool_input сюда не годится.
    """
    if not file_path:
        return None
    root = repo_root(payload)
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

def ensure_agent(session_id, payload, state):
    """Создаёт агента сессии один раз за сессию."""
    if state.get("agent_created"):
        return
    label = os.path.basename(repo_root(payload)) or "claude"
    ok = call("POST", "/api/agents", {
        "agent_id": agent_id_for(session_id),
        "agent_type": "orchestrator",
        "parent_id": "",
        "label": f"Claude · {label}",
    })
    if ok is not None:
        state["agent_created"] = True


def release_previous_claim(state, rel):
    claim_id = state["claims"].pop(rel, None)
    if claim_id:
        call("DELETE", f"/api/claims/{claim_id}")


def cmd_session_start(payload, state):
    session_id = payload.get("session_id", "")
    ensure_agent(session_id, payload, state)
    call("POST", f"/api/agents/{agent_id_for(session_id)}/status", {"status": "RUNNING"})


def cmd_pre_tool(payload, state):
    session_id = payload.get("session_id", "")
    tool = payload.get("tool_name", "")
    tool_input = payload.get("tool_input") or {}
    agent = agent_id_for(session_id)

    ensure_agent(session_id, payload, state)

    if tool == DELEGATION_TOOL:
        worker = worker_id_for(session_id, tool_input)
        label = (tool_input.get("description")
                 or tool_input.get("subagent_type") or "subagent")
        call("POST", "/api/agents", {
            "agent_id": worker, "agent_type": "worker",
            "parent_id": agent, "label": label[:60],
        })
        call("POST", "/api/edges", {
            "from_id": agent, "to_id": worker, "kind": "TASK_DELEGATION",
        })
        call("POST", f"/api/agents/{worker}/status", {"status": "RUNNING"})
        state["workers"][worker] = label[:60]
        return

    if tool not in EDIT_TOOLS:
        return

    rel = relative_path(payload, tool_input.get("file_path"))
    if not rel:
        return

    # Снять свой предыдущий claim на этот файл, чтобы они не копились до TTL.
    release_previous_claim(state, rel)

    resp = call("POST", "/api/claims", {"agent_id": agent, "file": rel})
    if resp and resp.get("claim_id"):
        state["claims"][rel] = resp["claim_id"]
    call("POST", f"/api/agents/{agent}/status", {"status": "RUNNING"})


def cmd_post_tool(payload, state):
    session_id = payload.get("session_id", "")
    tool = payload.get("tool_name", "")
    tool_input = payload.get("tool_input") or {}
    agent = agent_id_for(session_id)

    if tool == DELEGATION_TOOL:
        worker = worker_id_for(session_id, tool_input)
        call("POST", f"/api/agents/{worker}/terminate", {"reason": "completed"})
        state["workers"].pop(worker, None)
        return

    if tool not in EDIT_TOOLS:
        return

    rel = relative_path(payload, tool_input.get("file_path"))
    if not rel:
        return

    # Claim здесь намеренно НЕ снимается: watcher дебаунсит событие (--interval,
    # по умолчанию 300 мс), и diff считается уже после того, как PostToolUse
    # отработал. Снимем claim сейчас — правка уедет в `external`. Он отвалится
    # сам по --claim-ttl либо будет снят перед следующей правкой этого файла.
    call("POST", f"/api/agents/{agent}/log", {
        "level": "info", "message": f"{tool}: {rel}",
    })


def cmd_stop(payload, state):
    session_id = payload.get("session_id", "")
    call("POST", f"/api/agents/{agent_id_for(session_id)}/status", {"status": "WAITING"})


def cmd_session_end(payload, state):
    session_id = payload.get("session_id", "")
    agent = agent_id_for(session_id)
    for claim_id in list(state.get("claims", {}).values()):
        call("DELETE", f"/api/claims/{claim_id}")
    for worker in list(state.get("workers", {})):
        call("POST", f"/api/agents/{worker}/terminate", {"reason": "session ended"})
    call("POST", f"/api/agents/{agent}/terminate", {"reason": "session ended"})
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

    if out_of_scope(payload):
        return 0

    session_id = payload.get("session_id", "")
    state = load_state(session_id)
    try:
        COMMANDS[command](payload, state)
    except Exception as exc:
        debug(f"{command} упал: {exc}")
    finally:
        if command != "session-end":
            save_state(session_id, state)
    return 0


if __name__ == "__main__":
    sys.exit(main())
