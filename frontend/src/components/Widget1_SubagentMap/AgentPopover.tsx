import React from 'react';
import { X, Play, Pause, AlertCircle, FileCode, ListTodo, Terminal } from 'lucide-react';
import { useSwarmStore, useCurrentBasePath } from '../../store/useSwarmStore';
import { useSwarmSocket } from '../../hooks/useSwarmSocket';

interface AgentPopoverProps {
  agentId: string;
  onClose: () => void;
}

export const AgentPopover: React.FC<AgentPopoverProps> = ({ agentId, onClose }) => {
  const { agent, activeClaims, isControlDisabled } = useSwarmStore((state) => {
    const ag = state.agents[agentId];
    const claims = Object.values(state.activeClaims).filter(
      (c) => c.agentId === agentId || (c as any).agent_id === agentId
    );
    const disabled = state.disabledControls[agentId] || false;
    return { agent: ag, activeClaims: claims, isControlDisabled: disabled };
  });

  const { sendControl } = useSwarmSocket(useCurrentBasePath());

  if (!agent) return null;

  const handleAction = (action: 'pause' | 'resume') => {
    sendControl(agentId, action);
  };

  return (
    <div className="absolute top-4 right-4 w-80 bg-[#12141c] border border-[#1e2230] rounded-lg shadow-2xl p-4 z-50 text-xs font-sans">
      <div className="flex items-center justify-between border-b border-[#1e2230] pb-2.5 mb-3">
        <div className="flex items-center gap-2">
          <span className="font-mono font-bold text-sm text-purple-300">{agent.label}</span>
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-400 font-mono uppercase">
            {agent.type}
          </span>
        </div>
        <button onClick={onClose} className="text-slate-400 hover:text-white p-1">
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Current task, in full — the node itself only has room for two lines */}
      <div className="mb-3">
        <div className="text-slate-400 font-mono text-[10px] mb-1 flex items-center gap-1">
          <ListTodo className="w-3 h-3" /> CURRENT TASK
        </div>
        {agent.task ? (
          <div className="text-[11px] text-slate-200 bg-slate-900/60 px-2 py-1.5 rounded border border-slate-800 max-h-24 overflow-y-auto whitespace-pre-wrap break-words">
            {agent.task}
          </div>
        ) : (
          <div className="text-slate-600 italic text-[11px]">No task reported by orchestrator</div>
        )}
      </div>

      {/* Control Buttons */}
      <div className="flex gap-2 mb-3">
        <button
          disabled={isControlDisabled || agent.status === 'DONE'}
          onClick={() => handleAction('pause')}
          className="flex-1 py-1.5 rounded bg-slate-800 hover:bg-slate-700 disabled:opacity-40 disabled:cursor-not-allowed border border-slate-700 flex items-center justify-center gap-1.5 text-slate-200 font-medium"
        >
          <Pause className="w-3.5 h-3.5" /> Pause
        </button>
        <button
          disabled={isControlDisabled || agent.status === 'DONE'}
          onClick={() => handleAction('resume')}
          className="flex-1 py-1.5 rounded bg-purple-900/40 hover:bg-purple-800/60 disabled:opacity-40 disabled:cursor-not-allowed border border-purple-500/40 flex items-center justify-center gap-1.5 text-purple-200 font-medium"
        >
          <Play className="w-3.5 h-3.5" /> Resume
        </button>
      </div>

      {isControlDisabled && (
        <div className="mb-3 text-[10px] text-amber-400 bg-amber-500/10 p-2 rounded border border-amber-500/20 flex items-center gap-1.5">
          <AlertCircle className="w-3.5 h-3.5 shrink-0" />
          <span>Control not implemented by orchestrator</span>
        </div>
      )}

      {/* Active Claims */}
      <div className="mb-3">
        <div className="text-slate-400 font-mono text-[10px] mb-1 flex items-center gap-1">
          <FileCode className="w-3 h-3" /> ACTIVE CLAIMS ({activeClaims.length})
        </div>
        {activeClaims.length === 0 ? (
          <div className="text-slate-600 italic text-[11px]">No active file claims</div>
        ) : (
          <div className="space-y-1 max-h-24 overflow-y-auto">
            {activeClaims.map((c) => (
              <div
                key={c.claimId || (c as any).claim_id}
                className="font-mono text-[11px] text-emerald-400 bg-emerald-950/20 px-2 py-1 rounded border border-emerald-500/20 truncate"
              >
                🟢 {c.filePath || c.file}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Log History */}
      <div>
        <div className="text-slate-400 font-mono text-[10px] mb-1 flex items-center gap-1">
          <Terminal className="w-3 h-3" /> RECENT LOGS
        </div>
        {!agent.logs || agent.logs.length === 0 ? (
          <div className="text-slate-600 italic text-[11px] p-2 bg-slate-900/50 rounded border border-slate-800">
            No logs reported by orchestrator
          </div>
        ) : (
          <div className="space-y-1 font-mono text-[10px] max-h-32 overflow-y-auto bg-slate-950 p-2 rounded border border-slate-800">
            {agent.logs.slice(-5).map((log, idx) => (
              <div key={idx} className="text-slate-300">
                <span className="text-slate-500">[{log.ts?.includes('T') ? log.ts.split('T')[1]?.slice(0, 8) : log.ts}]</span> {log.message}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
