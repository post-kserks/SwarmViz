"""Файл-заглушка для проверки SwarmViz.

Никакой логики здесь нет — файл существует только чтобы его редактировали,
пока SwarmViz следит за репозиторием. Каждая правка должна проехать по
пайплайну watcher -> diff.Engine -> BroadcastEvent -> WebSocket и появиться
в Live Edit Stream, дереве файлов и графике LOC.

Проверка:
    swarmviz --path <этот репозиторий> --port 8942 --claim-ttl 10s &
    # застолбить файл за агентом, потом писать в него:
    curl -sX POST localhost:8942/api/claims \
        -H 'Content-Type: application/json' \
        -d '{"agent_id":"worker-1","file":"src/stub.py"}'
    echo "# правка" >> src/stub.py
"""

STUB_VERSION = 1

# Строки ниже дописываются/удаляются во время проверки, чтобы дельта LOC
# была и положительной, и отрицательной.
FILLER_A = "aaa"
FILLER_B = "bbb"
FILLER_C = "ccc"


def noop():
    """Ничего не делает: заглушка, а не рабочий код."""
    return None


# правка от Claude: должна появиться в Live Edit Stream
CHECK_1 = 1
CHECK_2 = 2

# финальная проверка
FIN = 1
HOOK_TEST = 1
HOOK_TEST_2 = 2
TMUX_HOOK_TEST = 3
