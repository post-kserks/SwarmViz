import { useEffect } from 'react';
import { useSwarmStore } from '../store/useSwarmStore';
import { ProjectSummary } from '../types/swarm';

const STORAGE_KEY = 'swarmviz.currentProjectId';

// Fetches the project list once on mount and selects an initial project
// (the last one persisted in localStorage, else whatever the backend lists
// first — always the root project in single-project mode). Persists the
// current selection so a reload keeps whichever project was being watched.
export function useProjectDiscovery() {
  const setProjects = useSwarmStore((s) => s.setProjects);
  const switchProject = useSwarmStore((s) => s.switchProject);
  const currentProjectId = useSwarmStore((s) => s.currentProjectId);

  useEffect(() => {
    let cancelled = false;

    fetch('/api/projects')
      .then((res) => (res.ok ? (res.json() as Promise<ProjectSummary[]>) : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then((projects) => {
        if (cancelled || !projects.length) return;
        setProjects(projects);
        const preferred = window.localStorage.getItem(STORAGE_KEY);
        const initial = projects.find((p) => p.id === preferred) || projects[0];
        switchProject(initial.id);
      })
      .catch(() => {
        // No /api/projects (single-project build/deployment without
        // --projects-root): the root project is always reachable directly.
        if (cancelled) return;
        setProjects([{ id: 'default', name: 'SwarmViz', path: '', basePath: '' }]);
        switchProject('default');
      });

    return () => {
      cancelled = true;
    };
    // Runs once: project discovery happens on mount, not on every store change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (currentProjectId) {
      window.localStorage.setItem(STORAGE_KEY, currentProjectId);
    }
  }, [currentProjectId]);
}
