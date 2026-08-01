#!/usr/bin/env python3
"""Drive SwarmViz with a simulated agent swarm.

Creates an orchestrator with a few workers and a challenger, then loops:
each worker claims a file, edits it in the watched repository, and releases the
claim. Because the claim is taken *before* the write, the diff engine attributes
every edit to the agent that made it, which is what fills the Subagent Map, the
Live Edit Stream and the LOC chart.

Usage:
    ./swarmviz --path /tmp/demo-repo --claim-ttl 10s &
    python3 examples/demo_swarm.py --repo /tmp/demo-repo

Only the standard library is required.
"""

import argparse
import json
import os
import random
import subprocess
import sys
import time
import urllib.error
import urllib.request

WORKER_FILES = [
    "src/parser.py",
    "src/planner.py",
    "src/executor.py",
    "docs/notes.md",
]


class SwarmVizClient:
    def __init__(self, base_url, token=None):
        self.base_url = base_url.rstrip("/")
        self.token = token

    def _request(self, method, path, payload=None):
        data = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request(self.base_url + path, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if self.token:
            req.add_header("Authorization", "Bearer " + self.token)
        try:
            with urllib.request.urlopen(req, timeout=5) as resp:
                body = resp.read()
                return json.loads(body) if body else {}
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode(errors="replace")
            raise SystemExit(f"{method} {path} failed: {exc.code} {detail}") from exc
        except urllib.error.URLError as exc:
            raise SystemExit(
                f"cannot reach SwarmViz at {self.base_url}: {exc.reason}\n"
                "Is the binary running with --port matching --url?"
            ) from exc

    def health(self):
        return self._request("GET", "/api/health")

    def create_agent(self, agent_id, agent_type, label, parent_id="", task=""):
        return self._request("POST", "/api/agents", {
            "agent_id": agent_id,
            "agent_type": agent_type,
            "parent_id": parent_id,
            "label": label,
            "task": task,
        })

    def task(self, agent_id, task):
        """What this agent is working on right now; "" while it is idle."""
        return self._request("POST", f"/api/agents/{agent_id}/task", {"task": task})

    def edge(self, from_id, to_id, kind="TASK_DELEGATION"):
        return self._request("POST", "/api/edges", {
            "from_id": from_id, "to_id": to_id, "kind": kind,
        })

    def status(self, agent_id, status):
        return self._request("POST", f"/api/agents/{agent_id}/status", {"status": status})

    def log(self, agent_id, message, level="info"):
        return self._request("POST", f"/api/agents/{agent_id}/log", {
            "level": level, "message": message,
        })

    def claim(self, agent_id, file_path):
        return self._request("POST", "/api/claims", {
            "agent_id": agent_id, "file": file_path,
        })["claim_id"]

    def release(self, claim_id):
        return self._request("DELETE", f"/api/claims/{claim_id}")

    def terminate(self, agent_id, reason):
        return self._request("POST", f"/api/agents/{agent_id}/terminate", {"reason": reason})

    def batch(self, events):
        return self._request("POST", "/api/events", events)


def ensure_repo(repo):
    """Create the watched git repository if it does not exist yet."""
    os.makedirs(repo, exist_ok=True)
    if not os.path.isdir(os.path.join(repo, ".git")):
        subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
        subprocess.run(["git", "config", "user.email", "demo@example.com"], cwd=repo, check=True)
        subprocess.run(["git", "config", "user.name", "demo"], cwd=repo, check=True)

    for rel in WORKER_FILES:
        path = os.path.join(repo, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        if not os.path.exists(path):
            with open(path, "w") as fh:
                fh.write(f"# {os.path.basename(rel)}\n")

    subprocess.run(["git", "add", "-A"], cwd=repo, check=True)
    subprocess.run(
        ["git", "commit", "-qm", "demo baseline"],
        cwd=repo, check=False,  # nothing to commit on a rerun is fine
    )


def edit_file(repo, rel, agent_id, iteration):
    path = os.path.join(repo, rel)
    with open(path, "a") as fh:
        fh.write(f"\n# edit {iteration} by {agent_id}\n")
        for i in range(random.randint(1, 4)):
            fh.write(f"line_{iteration}_{i} = {random.randint(0, 999)}\n")

    # Occasionally shrink a file so the LOC chart shows a negative delta too.
    if random.random() < 0.25:
        with open(path) as fh:
            lines = fh.readlines()
        if len(lines) > 6:
            with open(path, "w") as fh:
                fh.writelines(lines[: len(lines) - 3])


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--repo", required=True, help="Path to the repository SwarmViz watches")
    parser.add_argument("--url", default="http://127.0.0.1:8942", help="SwarmViz base URL")
    parser.add_argument("--token", default=os.environ.get("SWARMVIZ_TOKEN"),
                        help="Bearer token, if the server was started with --api-token")
    parser.add_argument("--workers", type=int, default=3, help="Number of worker agents")
    parser.add_argument("--rounds", type=int, default=12, help="Edit rounds (0 = run forever)")
    parser.add_argument("--delay", type=float, default=1.5, help="Seconds between edits")
    args = parser.parse_args()

    client = SwarmVizClient(args.url, args.token)
    client.health()
    print(f"connected to SwarmViz at {args.url}")

    ensure_repo(args.repo)
    print(f"watching repository {args.repo}")

    client.create_agent("orchestrator", "orchestrator", "Orchestrator",
                        task="Раздать правки воркерам и дождаться ревью")
    workers = [f"worker-{i + 1}" for i in range(args.workers)]

    setup = []
    for worker in workers:
        setup.append({"type": "AGENT_CREATED", "data": {
            "agent_id": worker, "agent_type": "worker",
            "parent_id": "orchestrator", "label": worker.replace("-", " ").title(),
        }})
        setup.append({"type": "AGENT_EDGE", "data": {
            "from_id": "orchestrator", "to_id": worker, "kind": "TASK_DELEGATION",
        }})
    setup.append({"type": "AGENT_CREATED", "data": {
        "agent_id": "challenger", "agent_type": "challenger",
        "parent_id": "orchestrator", "label": "Reviewer",
        "task": "Ждёт очередной раунд правок",
    }})
    setup.append({"type": "AGENT_EDGE", "data": {
        "from_id": "orchestrator", "to_id": "challenger", "kind": "REVIEW_REQUEST",
    }})
    client.batch(setup)
    print(f"registered {len(workers)} workers + 1 challenger")

    round_no = 0
    try:
        while args.rounds == 0 or round_no < args.rounds:
            round_no += 1
            worker = random.choice(workers)
            rel = random.choice(WORKER_FILES)

            client.status(worker, "RUNNING")
            client.task(worker, f"Раунд {round_no}: правит {rel}")
            client.log(worker, f"editing {rel}")

            # Claim first: this is what makes the diff 'claimed' rather than
            # 'external'.
            claim_id = client.claim(worker, rel)
            edit_file(args.repo, rel, worker, round_no)
            time.sleep(args.delay)
            client.release(claim_id)
            client.status(worker, "WAITING")
            # Пустая задача = агент простаивает; иначе на узле висела бы правка,
            # которую он уже закончил.
            client.task(worker, "")

            print(f"round {round_no}: {worker} edited {rel}")

            if round_no % 4 == 0:
                client.status("challenger", "RUNNING")
                client.task("challenger", f"Ревью раунда {round_no}: {rel}")
                client.log("challenger", f"reviewing round {round_no}", level="warn")
                client.edge("challenger", worker, "REVIEW_REQUEST")
                time.sleep(args.delay / 2)
                client.status("challenger", "WAITING")
                client.task("challenger", "Ждёт очередной раунд правок")

    except KeyboardInterrupt:
        print("\ninterrupted")

    for worker in workers:
        client.terminate(worker, "completed")
    client.terminate("challenger", "completed")
    client.terminate("orchestrator", "completed")
    print("swarm finished; agents marked DONE")


if __name__ == "__main__":
    sys.exit(main())
