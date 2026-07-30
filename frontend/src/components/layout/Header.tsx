import React from 'react';
import { Activity, AlertTriangle } from 'lucide-react';
import { useSwarmStore } from '../../store/useSwarmStore';

export const Header: React.FC = () => {
  const { connectionStatus, serverWarning, isDegraded, clearServerWarning } = useSwarmStore((state) => ({
    connectionStatus: state.connectionStatus,
    serverWarning: state.serverWarning,
    isDegraded: state.isDegraded,
    clearServerWarning: state.clearServerWarning,
  }));

  const statusColors = {
    connected: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/30',
    connecting: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/30 animate-pulse',
    reconnecting: 'bg-orange-500/10 text-orange-400 border-orange-500/30 animate-pulse',
    disconnected: 'bg-rose-500/10 text-rose-400 border-rose-500/30',
  };

  return (
    <header className="h-12 bg-[#12141c] border-b border-[#1e2230] px-4 flex items-center justify-between z-20 select-none shrink-0">
      <div className="flex items-center gap-3">
        <div className="flex items-center gap-2">
          <Activity className="w-5 h-5 text-purple-400" />
          <span className="font-bold text-sm tracking-wider bg-gradient-to-r from-purple-400 via-blue-400 to-emerald-400 bg-clip-text text-transparent">
            SWARMVIZ <span className="text-xs text-slate-500 font-mono">v3.1</span>
          </span>
        </div>

        {isDegraded && (
          <span className="text-xs px-2 py-0.5 rounded bg-amber-500/10 border border-amber-500/30 text-amber-400 flex items-center gap-1 font-mono">
            <AlertTriangle className="w-3.5 h-3.5" /> DEGRADED MODE
          </span>
        )}
      </div>

      {serverWarning && (
        <div className="flex items-center gap-2 bg-amber-950/40 border border-amber-500/30 text-amber-300 text-xs px-3 py-1 rounded max-w-xl truncate">
          <AlertTriangle className="w-4 h-4 shrink-0 text-amber-400" />
          <span className="truncate">{serverWarning}</span>
          <button
            onClick={() => clearServerWarning()}
            className="ml-2 hover:text-white font-mono"
          >
            ✕
          </button>
        </div>
      )}

      <div className="flex items-center gap-3">
        <div className={`text-xs px-2.5 py-1 rounded-full border flex items-center gap-1.5 font-mono ${statusColors[connectionStatus] || statusColors.disconnected}`}>
          <span className="w-1.5 h-1.5 rounded-full bg-current" />
          {connectionStatus.toUpperCase()}
        </div>
      </div>
    </header>
  );
};
