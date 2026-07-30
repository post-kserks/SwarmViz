import React from 'react';
import { FolderGit2 } from 'lucide-react';
import { useSwarmStore, useProjects, useCurrentProjectId } from '../../store/useSwarmStore';

export const ProjectSwitcher: React.FC = () => {
  const projects = useProjects();
  const currentProjectId = useCurrentProjectId();
  const switchProject = useSwarmStore((s) => s.switchProject);

  if (projects.length <= 1) return null;

  return (
    <div className="flex items-center gap-1.5 text-xs font-mono">
      <FolderGit2 className="w-3.5 h-3.5 text-slate-500" />
      <select
        value={currentProjectId ?? ''}
        onChange={(e) => switchProject(e.target.value)}
        title="Switch watched project"
        className="bg-[#191c26] border border-[#1e2230] rounded px-1.5 py-0.5 text-slate-300 hover:border-purple-500/40 focus:outline-none focus:border-purple-500/60 max-w-[14rem]"
      >
        {projects.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
            {p.watching === false ? ' (idle)' : ''}
          </option>
        ))}
      </select>
    </div>
  );
};
