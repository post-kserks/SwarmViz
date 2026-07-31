import { useCallback } from 'react';
import { useSwarmStore } from '../store/useSwarmStore';
import { isEmbedded, revealInEditor } from '../utils/host';

// Opens a repository-relative file in the host editor, resolved against the
// currently selected project — with several projects on one backend, the same
// relative path exists in more than one repository.
//
// Returns null in a plain browser tab, so callers can skip rendering an
// affordance that would do nothing there.
export function useRevealInEditor(): ((file: string, line?: number) => void) | null {
  const projectPath = useSwarmStore(
    (s) => s.projects.find((p) => p.id === s.currentProjectId)?.path ?? ''
  );

  const reveal = useCallback(
    (file: string, line?: number) => {
      revealInEditor({ file, projectPath, line });
    },
    [projectPath]
  );

  return isEmbedded() ? reveal : null;
}
