#!/usr/bin/env python3
"""Тесты сопоставления сессии Claude Code с проектом SwarmViz.

Запуск: python3 -m unittest discover -s integrations/claude-code -p 'test_*.py'

Сеть не трогается: fetch_projects и call подменяются.
"""

import os
import subprocess
import sys
import tempfile
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import swarmviz_hook as hook  # noqa: E402


ROOT = {"id": "default", "name": "SwarmViz", "path": "/repos/SwarmViz", "basePath": ""}
DOTA = {"id": "dota", "name": "dota", "path": "/repos/dota", "basePath": "/p/dota"}
VAULT = {"id": "vault", "name": "vault", "path": "/home/pin/vault", "basePath": "/p/vault"}
PARENT = {"id": "repos", "name": "repos", "path": "/repos", "basePath": "/p/repos"}

ALL = [ROOT, DOTA, VAULT, PARENT]


class TestIsWithin(unittest.TestCase):
    def test_same_path(self):
        self.assertTrue(hook.is_within("/a/b", "/a/b"))

    def test_child(self):
        self.assertTrue(hook.is_within("/a/b/c", "/a/b"))

    def test_sibling_prefix_is_not_child(self):
        # /a/bc не внутри /a/b, хотя строкой начинается так же.
        self.assertFalse(hook.is_within("/a/bc", "/a/b"))

    def test_parent_is_not_child(self):
        self.assertFalse(hook.is_within("/a", "/a/b"))


class TestMatchProject(unittest.TestCase):
    def test_exact_match_wins(self):
        self.assertEqual(hook.match_project(ALL, ["/repos/dota"]), DOTA)

    def test_root_project_matches_on_empty_base_path(self):
        matched = hook.match_project(ALL, ["/repos/SwarmViz"])
        self.assertEqual(matched, ROOT)
        self.assertEqual(matched["basePath"], "")

    def test_deepest_ancestor_wins_over_parent(self):
        # Сессия в подкаталоге dota: и /repos, и /repos/dota подходят,
        # выиграть должен более глубокий.
        self.assertEqual(hook.match_project(ALL, ["/repos/dota/pkg/api"]), DOTA)

    def test_worktree_falls_back_to_main_repo(self):
        # Первый кандидат (сам worktree) не совпадает ни с чем точно, но лежит
        # внутри vault — значит достаётся vault.
        candidates = ["/home/pin/vault/.claude/worktrees/x", "/home/pin/vault"]
        self.assertEqual(hook.match_project(ALL, candidates), VAULT)

    def test_worktree_outside_repo_uses_second_candidate(self):
        candidates = ["/tmp/detached-worktree", "/home/pin/vault"]
        self.assertEqual(hook.match_project(ALL, candidates), VAULT)

    def test_unrelated_repo_matches_nothing(self):
        self.assertIsNone(hook.match_project(ALL, ["/somewhere/else"]))

    def test_earlier_candidate_exact_beats_later_candidate_exact(self):
        # Порядок кандидатов = порядок предпочтения.
        self.assertEqual(hook.match_project(ALL, ["/repos/dota", "/home/pin/vault"]), DOTA)


class TestResolveTarget(unittest.TestCase):
    def setUp(self):
        self._fetch = hook.fetch_projects
        self._session_root = hook.session_root
        self._main_repo_root = hook.main_repo_root
        self._env = os.environ.pop("SWARMVIZ_REPO", None)

    def tearDown(self):
        hook.fetch_projects = self._fetch
        hook.session_root = self._session_root
        hook.main_repo_root = self._main_repo_root
        os.environ.pop("SWARMVIZ_REPO", None)
        if self._env is not None:
            os.environ["SWARMVIZ_REPO"] = self._env

    def stub(self, root, projects, main_repo=None):
        hook.session_root = lambda payload: root
        hook.main_repo_root = lambda payload: main_repo
        hook.fetch_projects = lambda: projects

    def test_routes_to_sub_project_base_path(self):
        self.stub("/repos/dota", ALL)
        state = {}
        target = hook.resolve_target({}, state)
        self.assertEqual(target["base"], "/p/dota")
        self.assertEqual(target["path"], "/repos/dota")
        self.assertEqual(state["project"], target, "успешный резолв должен кэшироваться")

    def test_routes_root_project_to_empty_base(self):
        self.stub("/repos/SwarmViz", ALL)
        self.assertEqual(hook.resolve_target({}, {})["base"], "")

    def test_unknown_repo_returns_none_and_is_not_cached(self):
        self.stub("/somewhere/else", ALL)
        state = {}
        self.assertIsNone(hook.resolve_target({}, state))
        self.assertIsNone(state.get("project"))

    def test_single_project_mode_falls_back_to_root_paths(self):
        # /api/projects отсутствует (404) — работаем как раньше.
        self.stub("/repos/anything", None)
        target = hook.resolve_target({}, {})
        self.assertEqual(target["base"], "")
        self.assertEqual(target["path"], "/repos/anything")

    def test_cached_project_short_circuits(self):
        def explode():
            raise AssertionError("fetch_projects не должен вызываться при кэше")

        hook.fetch_projects = explode
        cached = {"base": "/p/dota", "path": "/repos/dota", "id": "dota", "name": "dota"}
        self.assertEqual(hook.resolve_target({}, {"project": cached}), cached)

    def test_pinned_repo_skips_foreign_session(self):
        os.environ["SWARMVIZ_REPO"] = "/repos/SwarmViz"
        self.stub("/home/pin/vault", ALL)
        self.assertIsNone(hook.resolve_target({}, {}))

    def test_pinned_repo_accepts_session_inside_it(self):
        os.environ["SWARMVIZ_REPO"] = "/repos/SwarmViz"
        self.stub("/repos/SwarmViz/pkg", ALL)
        self.assertEqual(hook.resolve_target({}, {})["id"], "default")


class TestRelativePath(unittest.TestCase):
    def test_path_inside_repo(self):
        target = {"path": "/repos/dota"}
        self.assertEqual(hook.relative_path(target, "/repos/dota/pkg/api.go"), "pkg/api.go")

    def test_path_outside_repo_is_rejected(self):
        target = {"path": "/repos/dota"}
        self.assertIsNone(hook.relative_path(target, "/etc/passwd"))

    def test_missing_path(self):
        self.assertIsNone(hook.relative_path({"path": "/repos/dota"}, None))


class TestSessionRootUsesGit(unittest.TestCase):
    def test_non_repo_cwd_falls_back_to_cwd(self):
        with tempfile.TemporaryDirectory() as tmp:
            # Вне git корнем считается сам cwd — событие всё равно уйдёт, а
            # match_project затем решит, относится ли оно к какому-то проекту.
            self.assertEqual(hook.session_root({"cwd": tmp}), tmp)


class TestTmuxContext(unittest.TestCase):
    """Метка панели tmux. Сам tmux не запускается — subprocess подменяется."""

    def setUp(self):
        self._run = hook.subprocess.run
        self._env = {k: os.environ.pop(k, None) for k in ("TMUX", "TMUX_PANE")}

    def tearDown(self):
        hook.subprocess.run = self._run
        for key, value in self._env.items():
            os.environ.pop(key, None)
            if value is not None:
                os.environ[key] = value

    def stub_tmux(self, stdout="", returncode=0, boom=None):
        def fake_run(cmd, **kwargs):
            if boom is not None:
                raise boom
            return subprocess.CompletedProcess(cmd, returncode, stdout, "")

        hook.subprocess.run = fake_run

    def in_pane(self, pane="%1"):
        os.environ["TMUX"] = "/tmp/tmux-1000/default,42,0"
        os.environ["TMUX_PANE"] = pane

    def test_outside_tmux_returns_none(self):
        self.assertIsNone(hook.tmux_context())

    def test_pane_without_tmux_var_is_ignored(self):
        # TMUX_PANE мог остаться в окружении от родителя вне живой сессии.
        os.environ["TMUX_PANE"] = "%1"
        self.assertIsNone(hook.tmux_context())

    def test_human_readable_name_from_tmux(self):
        self.in_pane()
        self.stub_tmux(stdout="claude:0.1\n")
        self.assertEqual(hook.tmux_context(), "claude:0.1")

    def test_falls_back_to_pane_id_when_tmux_fails(self):
        self.in_pane("%3")
        self.stub_tmux(returncode=1)
        self.assertEqual(hook.tmux_context(), "%3")

    def test_falls_back_to_pane_id_when_tmux_missing(self):
        self.in_pane("%3")
        self.stub_tmux(boom=FileNotFoundError("tmux"))
        self.assertEqual(hook.tmux_context(), "%3")

    def test_label_without_tmux_is_unchanged(self):
        self.assertEqual(hook.agent_label({"name": "SwarmViz"}), "Claude · SwarmViz")

    def test_label_carries_pane(self):
        self.in_pane()
        self.stub_tmux(stdout="claude:0.1\n")
        # Панель вытесняет имя проекта: узел на карте режет подпись по ширине,
        # а различает сессии именно панель.
        self.assertEqual(hook.agent_label({"name": "SwarmViz"}), "Claude [claude:0.1]")


class TestEnsureAgent(unittest.TestCase):
    """Подтверждение узла сессии переживает рестарт сервера."""

    TARGET = {"base": "/p/dota", "path": "/repos/dota", "id": "dota", "name": "dota"}

    def setUp(self):
        self._call = hook.call
        self.posts = []
        hook.call = lambda method, path, payload=None, base="", timeout=None: (
            self.posts.append((method, path, payload, base)) or self.response
        )
        self.response = {}

    def tearDown(self):
        hook.call = self._call

    def test_creates_agent_when_state_is_fresh(self):
        state = {}
        hook.ensure_agent("sess", self.TARGET, state)
        self.assertEqual([p[1] for p in self.posts], ["/api/agents"])
        self.assertIsInstance(state["agent_seen_at"], float)

    def test_skips_while_recently_confirmed(self):
        hook.ensure_agent("sess", self.TARGET, {"agent_seen_at": time.time()})
        self.assertEqual(self.posts, [])

    def test_reasserts_after_interval(self):
        # Сервер мог перезапуститься и забыть агента — узел нужно завести снова.
        stale = time.time() - hook.AGENT_REASSERT_SEC - 1
        hook.ensure_agent("sess", self.TARGET, {"agent_seen_at": stale})
        self.assertEqual(len(self.posts), 1)

    def test_clock_jump_backwards_forces_reassert(self):
        hook.ensure_agent("sess", self.TARGET, {"agent_seen_at": time.time() + 3600})
        self.assertEqual(len(self.posts), 1)

    def test_failed_post_is_not_remembered(self):
        self.response = None
        state = {}
        hook.ensure_agent("sess", self.TARGET, state)
        self.assertIsNone(state.get("agent_seen_at"),
                          "неудачную попытку нельзя считать подтверждением")

    def test_reassert_carries_current_task(self):
        # Сервер после рестарта задачу не помнит, помнит только состояние сессии.
        hook.ensure_agent("sess", self.TARGET, {"task": "почини сборку"})
        self.assertEqual(self.posts[0][2]["task"], "почини сборку")


class TestShortenTask(unittest.TestCase):
    def test_collapses_whitespace(self):
        self.assertEqual(hook.shorten_task("почини\n  сборку\tи тесты"),
                         "почини сборку и тесты")

    def test_truncates_long_prompt(self):
        out = hook.shorten_task("а" * (hook.MAX_TASK_CHARS + 50))
        self.assertEqual(len(out), hook.MAX_TASK_CHARS + 1)
        self.assertTrue(out.endswith("…"))

    def test_empty_prompt(self):
        self.assertEqual(hook.shorten_task(""), "")
        self.assertEqual(hook.shorten_task(None), "")


class TestUserPrompt(unittest.TestCase):
    """Промпт пользователя становится задачей на узле сессии."""

    TARGET = {"base": "", "path": "/repos/SwarmViz", "id": "default", "name": "SwarmViz"}

    def setUp(self):
        self._call = hook.call
        self.posts = []
        hook.call = lambda method, path, payload=None, base="", timeout=None: (
            self.posts.append((method, path, payload, base)) or {}
        )

    def tearDown(self):
        hook.call = self._call

    def test_prompt_is_sent_as_task(self):
        state = {}
        hook.cmd_user_prompt({"session_id": "abc12345", "prompt": "добавь задачу на узел"},
                             self.TARGET, state)
        paths = [p[1] for p in self.posts]
        self.assertIn("/api/agents/claude-abc12345/task", paths)
        task_post = next(p for p in self.posts if p[1].endswith("/task"))
        self.assertEqual(task_post[2], {"task": "добавь задачу на узел"})
        self.assertEqual(state["task"], "добавь задачу на узел",
                         "задачу надо запомнить, иначе рестарт сервера её потеряет")

    def test_empty_prompt_is_ignored(self):
        state = {}
        hook.cmd_user_prompt({"session_id": "abc12345", "prompt": "   "}, self.TARGET, state)
        self.assertEqual(self.posts, [])
        self.assertNotIn("task", state)


if __name__ == "__main__":
    unittest.main()
