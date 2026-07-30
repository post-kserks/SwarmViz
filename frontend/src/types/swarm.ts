export type ConnectionStatus = 'connecting' | 'connected' | 'reconnecting' | 'disconnected';

// One entry from GET /api/projects. basePath is prepended to /ws and /api/...
// to reach this project's backend runtime ("" for the always-on root project).
export interface ProjectSummary {
  id: string;
  name: string;
  path: string;
  basePath: string;
  watching?: boolean;
}

export type AgentType = 'orchestrator' | 'teamwork' | 'challenger' | 'worker';
export type AgentStatus = 'IDLE' | 'RUNNING' | 'WAITING' | 'DONE' | 'ERROR';
export type EdgeKind = 'TASK_DELEGATION' | 'DATA_PASS' | 'REVIEW_REQUEST';
export type ControlAction = 'pause' | 'resume';

export interface AgentLogEntry {
  ts: string;
  level: string;
  message: string;
}

export interface AgentNode {
  id: string;
  type: AgentType;
  parentId?: string | null;
  label: string;
  status: AgentStatus;
  logs: AgentLogEntry[];
  terminatedReason?: string;
}

export type AgentNodeData = AgentNode;

export interface AgentEdge {
  id?: string;
  fromId: string;
  toId: string;
  kind: EdgeKind;
}

export type AgentEdgeData = AgentEdge;

export interface FileClaim {
  claimId: string;
  agentId: string;
  filePath: string;
  file?: string;
  since: string;
}

export type ActiveClaim = FileClaim;

export interface FileTreeNode {
  name: string;
  path: string;
  isDir: boolean;
  isDirectory?: boolean;
  editCount: number;
  state?: string;
  children?: FileTreeNode[];
}

export interface EditEvent {
  id?: string;
  seq: number;
  ts: string;
  agentId: string;
  file: string;
  status: string;
  attribution: 'claimed' | 'external' | 'conflict';
  hunk?: string;
  added: number;
  removed: number;
  binary: boolean;
  truncated: boolean;
  skipped: boolean;
  reason?: string;
}

export interface LocPoint {
  ts: string;
  added: number;
  removed: number;
  netDelta: number;
}

export interface ToastMessage {
  id: string;
  type: 'warning' | 'error' | 'info';
  message: string;
  ts: number;
}

export type WSEventType =
  | 'INIT_STATE'
  | 'AGENT_CREATED'
  | 'AGENT_STATUS_CHANGED'
  | 'AGENT_EDGE'
  | 'AGENT_TERMINATED'
  | 'AGENT_EDIT_CLAIM'
  | 'AGENT_CLAIM_RELEASED'
  | 'LIVE_DIFF_STREAM'
  | 'CONFLICT_DETECTED'
  | 'CONFLICT_RESOLVED'
  | 'FILE_TREE_UPDATE'
  | 'LOC_DELTA_UPDATE'
  | 'SERVER_WARNING'
  | 'ERROR'
  | 'PING'
  | 'PONG';

export interface WSEnvelope<T = any> {
  v: number;
  seq: number;
  ts: string;
  type: WSEventType;
  data: T;
}

export type WSEventEnvelope = WSEnvelope;

export interface InitStateData {
  agents?: any;
  edges?: any[];
  fileTree?: FileTreeNode;
  file_tree?: FileTreeNode;
  locHistory?: { ts: string; added: number; removed: number }[];
  recentEvents?: EditEvent[];
  recent_edits?: EditEvent[];
  activeClaims?: any;
  active_claims?: any[];
  conflicts?: Record<string, string[]>;
  lastSeq?: number;
}

export interface AgentCreatedData {
  agent_id: string;
  agent_type: AgentType;
  parent_id?: string;
  label: string;
}

export interface AgentStatusChangedData {
  agent_id: string;
  status: AgentStatus;
}

export interface AgentEdgeDataPayload {
  from_id: string;
  to_id: string;
  kind: EdgeKind;
}

export interface AgentTerminatedData {
  agent_id: string;
  reason: string;
}

export interface AgentEditClaimData {
  agent_id: string;
  file: string;
  claim_id: string;
}

export interface AgentClaimReleasedData {
  claim_id: string;
  file: string;
  reason?: 'released' | 'ttl_expired';
}

export interface LiveDiffStreamData {
  agent_id: string;
  file: string;
  status: string;
  attribution: 'claimed' | 'external' | 'conflict';
  hunk?: string;
  added: number;
  removed: number;
  binary: boolean;
  truncated: boolean;
  skipped: boolean;
  reason?: string;
}

export interface ConflictDetectedData {
  file: string;
  agent_ids: string[];
}

export interface ConflictResolvedData {
  file: string;
}

export interface FileTreeUpdateData {
  file: string;
  edit_count: number;
  state?: string;
}

export interface LocDeltaUpdateData {
  total_added: number;
  total_removed: number;
  timestamp?: string;
}

export interface ServerWarningData {
  message: string;
  code?: string;
}

export interface ServerErrorData {
  message: string;
  recoverable?: boolean;
}
