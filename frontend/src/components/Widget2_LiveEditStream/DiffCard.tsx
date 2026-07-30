import React from 'react';
import { EditEvent } from '../../types/swarm';
import { DiffHunkViewer } from './DiffHunkViewer';
import { AlertTriangle, FileCode, HardDrive } from 'lucide-react';

interface DiffCardProps {
  event: EditEvent;
  hasConflict?: boolean;
}

export const DiffCard: React.FC<DiffCardProps> = ({ event, hasConflict }) => {
  const formattedTime = event.ts ? new Date(event.ts).toLocaleTimeString() : '';

  return (
    <div
      className={`p-3 bg-[#12141c] border rounded-lg transition-all space-y-2 ${
        hasConflict || event.attribution === 'conflict'
          ? 'border-yellow-500/80 border-dashed bg-amber-950/10 shadow-[0_0_10px_rgba(234,179,8,0.2)]'
          : 'border-[#1e2230] hover:border-slate-700'
      }`}
    >
      {/* Header Info */}
      <div className="flex items-center justify-between text-xs font-mono">
        <div className="flex items-center gap-2 truncate">
          <span className="text-purple-400 font-semibold">{event.agentId || 'external'}</span>
          <span className="text-slate-600">|</span>
          <span className="text-slate-200 truncate font-medium flex items-center gap-1">
            <FileCode className="w-3.5 h-3.5 text-blue-400 shrink-0" />
            {event.file}
          </span>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className="text-emerald-400">+{event.added}</span>
          <span className="text-rose-400">-{event.removed}</span>
          {formattedTime && <span className="text-slate-500 text-[10px]">{formattedTime}</span>}
        </div>
      </div>

      {/* Conflict Badge */}
      {(hasConflict || event.attribution === 'conflict') && (
        <div className="flex items-center gap-1.5 text-[11px] text-yellow-400 bg-yellow-500/10 px-2 py-1 rounded border border-yellow-500/30 font-mono">
          <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
          <span>⚠️ Multiple agents editing this file</span>
        </div>
      )}

      {/* Card Content depending on Flags */}
      {event.skipped ? (
        <div className="text-amber-400 text-xs font-mono p-2 bg-amber-950/20 border border-amber-500/30 rounded flex items-center gap-2">
          <HardDrive className="w-4 h-4 text-amber-400" />
          <span>⚠️ File too large — tracking skipped for this session</span>
        </div>
      ) : event.binary ? (
        <div className="text-slate-400 text-xs font-mono p-2 bg-slate-900 rounded border border-slate-800 flex items-center gap-2">
          <FileCode className="w-4 h-4 text-slate-500" />
          <span>📄 Binary file changed</span>
        </div>
      ) : event.hunk ? (
        <DiffHunkViewer hunk={event.hunk} />
      ) : null}

      {/* Truncated Notice */}
      {event.truncated && (
        <div className="text-[10px] text-slate-500 italic font-mono text-right">
          Diff truncated — line limit exceeded
        </div>
      )}
    </div>
  );
};
