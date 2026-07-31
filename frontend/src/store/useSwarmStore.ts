import { create } from 'zustand';
import {
  ConnectionStatus,
  AgentNode,
  AgentEdge,
  FileTreeNode,
  EditEvent,
  LocPoint,
  ActiveClaim,
  ToastMessage,
  ProjectSummary,
  WSEventEnvelope,
} from '../types/swarm';
import { lttbDownsample } from '../utils/lttb';

const MAX_EDIT_EVENTS = 1000;
const MAX_LOC_HISTORY = 500;

// Fields that hold data scoped to whichever project is currently selected.
// Reused both for the store's initial state and to reset on project switch,
// so the two can't drift out of sync.
const initialProjectState = {
  agents: {} as Record<string, AgentNode>,
  edges: [] as AgentEdge[],
  fileTree: null as FileTreeNode | null,
  editStream: [] as EditEvent[],
  locHistory: [] as LocPoint[],
  activeClaims: {} as Record<string, ActiveClaim>,
  conflicts: {} as Record<string, string[]>,
  lastSeq: 0,
  serverWarning: null as string | null,
  isDegraded: false,
  disabledControls: {} as Record<string, boolean>,
  selectedAgentPopover: null as string | null,
  selectedFileFilter: null as string | null,
  toastNotifications: [] as ToastMessage[],
};

function updateTreeNode(node: FileTreeNode, targetPath: string, editCount: number, state?: string): FileTreeNode {
  const isDir = node.isDir || node.isDirectory;
  if (node.path === targetPath) {
    return { ...node, editCount, state: state || node.state };
  }
  if (isDir && node.children) {
    return {
      ...node,
      // Descend on a path-segment boundary: plain startsWith would send
      // "pkgfoo/x.go" into the unrelated "pkg" directory.
      children: node.children.map((child) =>
        child.path === targetPath || targetPath.startsWith(`${child.path}/`)
          ? updateTreeNode(child, targetPath, editCount, state)
          : child
      ),
    };
  }
  return node;
}

export interface SwarmVizStore {
  connectionStatus: ConnectionStatus;
  agents: Record<string, AgentNode>;
  edges: AgentEdge[];
  fileTree: FileTreeNode | null;
  editStream: EditEvent[];
  locHistory: LocPoint[];
  activeClaims: Record<string, ActiveClaim>;
  conflicts: Record<string, string[]>;
  lastSeq: number;
  serverWarning: string | null;
  isDegraded: boolean;
  disabledControls: Record<string, boolean>;
  selectedAgentPopover: string | null;
  selectedFileFilter: string | null;
  showAllFiles: boolean;
  toastNotifications: ToastMessage[];
  projects: ProjectSummary[];
  currentProjectId: string | null;

  // Action methods
  setConnectionStatus: (status: ConnectionStatus) => void;
  setProjects: (projects: ProjectSummary[]) => void;
  switchProject: (projectId: string) => void;
  setSelectedAgentPopover: (agentId: string | null) => void;
  clearServerWarning: () => void;
  setControlDisabled: (agentId: string, disabled: boolean) => void;
  handleWSEvent: (envelope: WSEventEnvelope) => void;
  handleInitState: (data: any) => void;
  addAgent: (data: any) => void;
  updateAgentStatus: (agentId: string, status: any) => void;
  addEdge: (edgeData: any) => void;
  terminateAgent: (agentId: string, reason: string) => void;
  addClaim: (agentId: string, file: string, claimId: string, ts: string) => void;
  releaseClaim: (claimId: string, file: string) => void;
  addEditEvent: (event: EditEvent) => void;
  setConflict: (file: string, agentIds: string[]) => void;
  resolveConflict: (file: string) => void;
  updateFileTreeCount: (file: string, editCount: number, state?: string) => void;
  appendLocDelta: (ts: string, added: number, removed: number) => void;
  addToast: (type: 'warning' | 'error' | 'info', message: string) => void;
  removeToast: (id: string) => void;
  setSelectedFileFilter: (file: string | null) => void;
  setShowAllFiles: (show: boolean) => void;
  setLastSeq: (seq: number) => void;

  actions: {
    setConnectionStatus: (status: ConnectionStatus) => void;
    setSelectedAgentPopover: (agentId: string | null) => void;
    clearServerWarning: () => void;
    setControlDisabled: (agentId: string, disabled: boolean) => void;
    handleWSEvent: (envelope: WSEventEnvelope) => void;
  };
}

export const useSwarmStore = create<SwarmVizStore>((set, get) => {
  const storeActions = {
    setConnectionStatus: (status: ConnectionStatus) => set({ connectionStatus: status }),
    setSelectedAgentPopover: (agentId: string | null) => set({ selectedAgentPopover: agentId }),
    clearServerWarning: () => set({ serverWarning: null }),
    setControlDisabled: (agentId: string, disabled: boolean) =>
      set((state) => ({
        disabledControls: { ...state.disabledControls, [agentId]: disabled },
      })),
    handleWSEvent: (envelope: WSEventEnvelope) => {
      const { seq, type, data } = envelope;
      const state = get();
      const nextSeq = Math.max(state.lastSeq, seq || 0);

      switch (type) {
        case 'INIT_STATE':
          state.handleInitState(data);
          break;
        case 'AGENT_CREATED':
          state.addAgent(data);
          break;
        case 'AGENT_STATUS_CHANGED':
          state.updateAgentStatus(data.agent_id, data.status);
          break;
        case 'AGENT_EDGE':
          state.addEdge(data);
          break;
        case 'AGENT_TERMINATED':
          state.terminateAgent(data.agent_id, data.reason);
          break;
        case 'AGENT_EDIT_CLAIM':
          state.addClaim(data.agent_id, data.file, data.claim_id, envelope.ts);
          break;
        case 'AGENT_CLAIM_RELEASED':
          state.releaseClaim(data.claim_id, data.file);
          break;
        case 'LIVE_DIFF_STREAM': {
          const editEv: EditEvent = {
            id: `${envelope.seq}-${data.file}`,
            seq: envelope.seq,
            ts: envelope.ts,
            agentId: data.agent_id,
            file: data.file,
            status: data.status,
            attribution: data.attribution,
            hunk: data.hunk,
            added: data.added || 0,
            removed: data.removed || 0,
            binary: data.binary || false,
            truncated: data.truncated || false,
            skipped: data.skipped || false,
            reason: data.reason,
          };
          state.addEditEvent(editEv);
          break;
        }
        case 'CONFLICT_DETECTED':
          state.setConflict(data.file, data.agent_ids);
          break;
        case 'CONFLICT_RESOLVED':
          state.resolveConflict(data.file);
          break;
        case 'FILE_TREE_UPDATE':
          state.updateFileTreeCount(data.file, data.edit_count, data.state);
          break;
        case 'LOC_DELTA_UPDATE':
          state.appendLocDelta(envelope.ts, data.total_added, data.total_removed);
          break;
        case 'SERVER_WARNING':
          set({
            lastSeq: nextSeq,
            serverWarning: data.message,
            isDegraded: data.code === 'DEGRADED_MODE' || state.isDegraded,
          });
          state.addToast('warning', data.message || 'Server warning received');
          break;
        case 'ERROR':
          if (data.message?.includes('Not implemented by orchestrator')) {
            const targetAgent = state.selectedAgentPopover;
            if (targetAgent) {
              set((s) => ({
                disabledControls: { ...s.disabledControls, [targetAgent]: true },
              }));
            }
          }
          state.addToast('error', data.message || 'Server error occurred');
          break;
        default:
          set({ lastSeq: nextSeq });
          break;
      }
    },
  };

  return {
    connectionStatus: 'connecting',
    ...initialProjectState,
    showAllFiles: false,
    projects: [],
    currentProjectId: null,

    actions: storeActions,

    setConnectionStatus: storeActions.setConnectionStatus,
    setSelectedAgentPopover: storeActions.setSelectedAgentPopover,
    clearServerWarning: storeActions.clearServerWarning,
    setControlDisabled: storeActions.setControlDisabled,
    handleWSEvent: storeActions.handleWSEvent,

    setLastSeq: (lastSeq: number) => set({ lastSeq }),

    setProjects: (projects: ProjectSummary[]) => set({ projects }),

    // Switching projects resets everything scoped to "the currently viewed
    // project" in one atomic update, then reconnects (useSwarmSocket watches
    // currentProjectId) so INIT_STATE repopulates it for the new project.
    switchProject: (projectId: string) =>
      set({ ...initialProjectState, currentProjectId: projectId }),

    handleInitState: (data: any) => {
      // The backend serialises agents with snake_case keys (agent_id,
      // agent_type), both as a list and as an id-keyed map, so normalise
      // either shape rather than trusting the map values as-is.
      const normalizeAgent = (a: any): AgentNode => ({
        id: a.agent_id || a.id,
        type: a.agent_type || a.type,
        parentId: a.parent_id || a.parentId || null,
        label: a.label || a.agent_id || a.id,
        status: a.status || 'IDLE',
        logs: a.logs || [],
        terminatedReason: a.terminated_reason || a.terminatedReason,
      });

      const agentsMap: Record<string, AgentNode> = {};
      if (data.agents) {
        const list: any[] = Array.isArray(data.agents) ? data.agents : Object.values(data.agents);
        list.forEach((a: any) => {
          const agent = normalizeAgent(a);
          if (agent.id) agentsMap[agent.id] = agent;
        });
      }

      let parsedEdges: AgentEdge[] = [];
      if (data.edges) {
        parsedEdges = data.edges.map((e: any) => ({
          id: e.id || `${e.from_id || e.fromId}->${e.to_id || e.toId}`,
          fromId: e.from_id || e.fromId,
          toId: e.to_id || e.toId,
          kind: e.kind,
        }));
      }

      let parsedClaims: Record<string, ActiveClaim> = {};
      if (data.active_claims && Array.isArray(data.active_claims)) {
        data.active_claims.forEach((c: any) => {
          parsedClaims[c.file] = {
            claimId: c.claim_id || c.claimId,
            agentId: c.agent_id || c.agentId,
            filePath: c.file || c.filePath,
            file: c.file || c.filePath,
            since: c.since || new Date().toISOString(),
          };
        });
      } else if (data.activeClaims) {
        parsedClaims = data.activeClaims;
      }

      const rawLocHistory: LocPoint[] = (data.locHistory || data.loc_history || []).map((p: any) => ({
        ts: p.ts || p.timestamp,
        added: p.added,
        removed: p.removed,
        netDelta: p.added - p.removed,
      }));

      const downsampledLoc =
        rawLocHistory.length > MAX_LOC_HISTORY
          ? lttbDownsample(rawLocHistory, MAX_LOC_HISTORY)
          : rawLocHistory;

      // recent_events replays raw WS envelopes; only the diff frames belong in
      // the edit stream, and they need unwrapping into EditEvent shape.
      const envelopeToEdit = (env: any): EditEvent | null => {
        const d = env?.data;
        if (!d?.file) return null;
        return {
          id: `${env.seq}-${d.file}`,
          seq: env.seq || 0,
          ts: env.ts,
          agentId: d.agent_id,
          file: d.file,
          status: d.status,
          attribution: d.attribution,
          hunk: d.hunk,
          added: d.added || 0,
          removed: d.removed || 0,
          binary: d.binary || false,
          truncated: d.truncated || false,
          skipped: d.skipped || false,
          reason: d.reason,
        };
      };

      const replayedEdits: EditEvent[] = (data.recent_events || [])
        .filter((env: any) => env?.type === 'LIVE_DIFF_STREAM')
        .map(envelopeToEdit)
        .filter((e: EditEvent | null): e is EditEvent => e !== null);

      const initialEdits: EditEvent[] =
        data.recent_edits || data.recentEvents || replayedEdits;

      set({
        agents: agentsMap,
        edges: parsedEdges,
        fileTree: data.file_tree || data.fileTree || null,
        editStream: initialEdits.slice(-MAX_EDIT_EVENTS),
        locHistory: downsampledLoc,
        activeClaims: parsedClaims,
        conflicts: data.conflicts || {},
        lastSeq: data.lastSeq || data.last_seq || 0,
      });
    },

    addAgent: (data: any) =>
      set((state) => ({
        agents: {
          ...state.agents,
          [data.agent_id || data.id]: {
            id: data.agent_id || data.id,
            type: data.agent_type || data.type,
            parentId: data.parent_id || data.parentId || null,
            label: data.label || data.agent_id || data.id,
            status: data.status || 'RUNNING',
            logs: data.logs || [],
          },
        },
      })),

    updateAgentStatus: (agentId: string, status: any) =>
      set((state) => {
        const agent = state.agents[agentId];
        if (!agent) return state;
        return {
          agents: {
            ...state.agents,
            [agentId]: { ...agent, status },
          },
        };
      }),

    addEdge: (edgeData: any) =>
      set((state) => {
        const fromId = edgeData.from_id || edgeData.fromId;
        const toId = edgeData.to_id || edgeData.toId;
        const kind = edgeData.kind;
        const id = `${fromId}->${toId}`;
        const newEdge: AgentEdge = { id, fromId, toId, kind };
        return {
          edges: [...state.edges.filter((e) => e.id !== id), newEdge],
        };
      }),

    terminateAgent: (agentId: string, reason: string) =>
      set((state) => {
        const agent = state.agents[agentId];
        if (!agent) return state;
        return {
          agents: {
            ...state.agents,
            [agentId]: { ...agent, status: 'DONE', terminatedReason: reason },
          },
        };
      }),

    addClaim: (agentId: string, file: string, claimId: string, ts: string) =>
      set((state) => ({
        activeClaims: {
          ...state.activeClaims,
          [file]: { claimId, agentId, filePath: file, file, since: ts },
        },
      })),

    releaseClaim: (claimId: string, file: string) =>
      set((state) => {
        const nextClaims = { ...state.activeClaims };
        if (file && nextClaims[file]) {
          delete nextClaims[file];
        } else {
          for (const [f, claim] of Object.entries(nextClaims)) {
            if (claim.claimId === claimId) {
              delete nextClaims[f];
              break;
            }
          }
        }
        return { activeClaims: nextClaims };
      }),

    addEditEvent: (event: EditEvent) =>
      set((state) => {
        const newStream = [...state.editStream, event].slice(-MAX_EDIT_EVENTS);
        return {
          editStream: newStream,
          lastSeq: Math.max(state.lastSeq, event.seq || 0),
        };
      }),

    setConflict: (file: string, agentIds: string[]) =>
      set((state) => ({
        conflicts: {
          ...state.conflicts,
          [file]: agentIds,
        },
      })),

    resolveConflict: (file: string) =>
      set((state) => {
        const nextConflicts = { ...state.conflicts };
        delete nextConflicts[file];
        return { conflicts: nextConflicts };
      }),

    updateFileTreeCount: (file: string, editCount: number, state?: string) =>
      set((prevState) => {
        if (!prevState.fileTree) return prevState;
        return {
          fileTree: updateTreeNode(prevState.fileTree, file, editCount, state),
        };
      }),

    appendLocDelta: (ts: string, added: number, removed: number) =>
      set((prevState) => {
        const newPoint: LocPoint = {
          ts,
          added,
          removed,
          netDelta: added - removed,
        };
        let updatedHistory = [...prevState.locHistory, newPoint];
        if (updatedHistory.length > MAX_LOC_HISTORY) {
          updatedHistory = lttbDownsample(updatedHistory, MAX_LOC_HISTORY);
        }
        return { locHistory: updatedHistory };
      }),

    addToast: (type: 'warning' | 'error' | 'info', message: string) =>
      set((state) => ({
        toastNotifications: [
          ...state.toastNotifications,
          { id: `${Date.now()}-${Math.random()}`, type, message, ts: Date.now() },
        ],
      })),

    removeToast: (id: string) =>
      set((state) => ({
        toastNotifications: state.toastNotifications.filter((t) => t.id !== id),
      })),

    setSelectedFileFilter: (file: string | null) => set({ selectedFileFilter: file }),
    setShowAllFiles: (showAllFiles: boolean) => set({ showAllFiles }),
  };
});

// Specialized Selectors
export const useConnectionStatus = () => useSwarmStore((s) => s.connectionStatus);
export const useAgents = () => useSwarmStore((s) => s.agents);
export const useAgentEdges = () => useSwarmStore((s) => s.edges);
export const useFileTree = () => useSwarmStore((s) => s.fileTree);
export const useActiveClaims = () => useSwarmStore((s) => s.activeClaims);
export const useConflicts = () => useSwarmStore((s) => s.conflicts);
export const useEditStream = () => useSwarmStore((s) => s.editStream);
export const useLocHistory = () => useSwarmStore((s) => s.locHistory);
export const useToastNotifications = () => useSwarmStore((s) => s.toastNotifications);
export const useSelectedFileFilter = () => useSwarmStore((s) => s.selectedFileFilter);
export const useShowAllFiles = () => useSwarmStore((s) => s.showAllFiles);
export const useProjects = () => useSwarmStore((s) => s.projects);
export const useCurrentProjectId = () => useSwarmStore((s) => s.currentProjectId);
// The active project's routing prefix for useSwarmSocket ("" for root, "/p/{id}"
// otherwise, null until discovery has picked an initial project).
export const useCurrentBasePath = () =>
  useSwarmStore((s) => s.projects.find((p) => p.id === s.currentProjectId)?.basePath ?? null);
