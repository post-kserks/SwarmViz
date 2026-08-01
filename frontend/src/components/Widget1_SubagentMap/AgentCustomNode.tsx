import React from 'react';
import { Handle, Position, NodeProps } from '@xyflow/react';
import { AlertTriangle, Bot, Cpu, Shield, UserCheck } from 'lucide-react';
import { AgentType, AgentStatus } from '../../types/swarm';

export interface AgentCustomNodeData {
  label: string;
  task?: string;
  type: AgentType;
  status: AgentStatus;
  activeClaimCount: number;
  isHighPerfMode?: boolean;
  onNodeClick: (id: string) => void;
}

export const AgentCustomNode: React.FC<NodeProps<any>> = ({ id, data }) => {
  const { label, task, type, status, activeClaimCount, isHighPerfMode, onNodeClick } = data;

  const typeIcons: Record<string, React.ReactNode> = {
    orchestrator: <Shield className="w-4 h-4 text-purple-400" />,
    teamwork: <Bot className="w-4 h-4 text-blue-400" />,
    challenger: <Cpu className="w-4 h-4 text-rose-400" />,
    worker: <UserCheck className="w-4 h-4 text-amber-400" />,
  };

  const typeBorders: Record<string, string> = {
    orchestrator: 'border-purple-500/60 bg-purple-950/30',
    teamwork: 'border-blue-500/60 bg-blue-950/30',
    challenger: 'border-rose-500/60 bg-rose-950/30',
    worker: 'border-amber-500/60 bg-amber-950/30',
  };

  let statusStyle = '';
  if (status === 'RUNNING' && !isHighPerfMode) {
    if (type === 'orchestrator') statusStyle = 'animate-pulse-glow-orchestrator';
    else if (type === 'teamwork') statusStyle = 'animate-pulse-glow-teamwork';
    else if (type === 'challenger') statusStyle = 'animate-pulse-glow-challenger';
    else statusStyle = 'animate-pulse-glow-worker';
  } else if (status === 'WAITING') {
    statusStyle = 'border-dashed border-2 border-slate-400 opacity-90';
  } else if (status === 'DONE') {
    statusStyle = 'opacity-65 grayscale-[30%]';
  } else if (status === 'ERROR') {
    statusStyle = 'border-2 border-rose-600 bg-rose-950/60';
  }

  return (
    <div
      onClick={() => onNodeClick(id)}
      className={`w-44 rounded-lg p-2.5 border transition-all cursor-pointer select-none relative ${typeBorders[type] || 'border-slate-700 bg-slate-900'} ${statusStyle}`}
    >
      <Handle type="target" position={Position.Top} className="!bg-slate-500 !w-2 !h-2" />

      <div className="flex items-center justify-between mb-1.5">
        <div className="flex items-center gap-1.5 truncate">
          {typeIcons[type] || <Bot className="w-4 h-4 text-slate-400" />}
          <span className="font-mono text-xs font-semibold text-slate-200 truncate">{label}</span>
        </div>
        {status === 'ERROR' && <AlertTriangle className="w-3.5 h-3.5 text-rose-500 animate-bounce" />}
      </div>

      {/* What the agent is doing. Two lines max: the node is fixed-width and
          dagre lays the graph out from a fixed height, so an unbounded prompt
          would overlap its neighbours. The full text is one hover (or click,
          in the popover) away. */}
      {task ? (
        <div
          title={task}
          className="text-[10px] leading-snug text-slate-300/90 mb-1 line-clamp-2 break-words"
        >
          {task}
        </div>
      ) : (
        <div className="text-[10px] leading-snug text-slate-600 italic mb-1">no task reported</div>
      )}

      <div className="flex items-center justify-between text-[10px] font-mono mt-1">
        <span className="text-slate-400 capitalize">{status ? status.toLowerCase() : 'idle'}</span>
        {activeClaimCount > 0 && (
          <span className="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-400 border border-emerald-500/40">
            🟢 {activeClaimCount} claimed
          </span>
        )}
      </div>

      <Handle type="source" position={Position.Bottom} className="!bg-slate-500 !w-2 !h-2" />
    </div>
  );
};
