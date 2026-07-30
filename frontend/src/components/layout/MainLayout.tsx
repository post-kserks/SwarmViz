import React from 'react';
import { Header } from './Header';
import { SubagentMapWidget } from '../Widget1_SubagentMap/SubagentMapWidget';
import { LiveEditStreamWidget } from '../Widget2_LiveEditStream/LiveEditStreamWidget';
import { FileTree } from '../Widget3_FileTree/FileTree';
import { LocChart } from '../Widget4_LocChart/LocChart';
import { useToastNotifications, useSwarmStore } from '../../store/useSwarmStore';
import { AlertCircle, AlertTriangle, Info, X } from 'lucide-react';

export const MainLayout: React.FC = () => {
  const toastNotifications = useToastNotifications();
  const removeToast = useSwarmStore((s) => s.removeToast);

  const toastIcons = {
    warning: <AlertTriangle className="w-4 h-4 text-amber-400 shrink-0" />,
    error: <AlertCircle className="w-4 h-4 text-rose-400 shrink-0" />,
    info: <Info className="w-4 h-4 text-sky-400 shrink-0" />,
  };

  const toastStyles = {
    warning: 'bg-amber-950/80 border-amber-500/40 text-amber-200',
    error: 'bg-rose-950/80 border-rose-500/40 text-rose-200',
    info: 'bg-sky-950/80 border-sky-500/40 text-sky-200',
  };

  return (
    <div className="h-screen w-screen bg-[#0a0b0e] text-[#e2e8f0] flex flex-col overflow-hidden select-none">
      <Header />

      {/* Main Grid: 30% | 45% | 25% */}
      <main className="flex-1 grid grid-cols-[30%_45%_25%] overflow-hidden">
        {/* Left Column (30%) */}
        <div className="flex flex-col border-r border-[#1e2230] overflow-hidden">
          {/* Subagent Map Widget (70%) */}
          <div className="h-[70%] border-b border-[#1e2230] relative overflow-hidden">
            <SubagentMapWidget />
          </div>

          {/* Lines of Code Chart Widget (30%) */}
          <div className="h-[30%] bg-[#12141c] p-2 overflow-hidden">
            <LocChart />
          </div>
        </div>

        {/* Center Column (45%) */}
        <div className="flex flex-col border-r border-[#1e2230] overflow-hidden bg-[#0a0b0e]">
          <LiveEditStreamWidget />
        </div>

        {/* Right Column (25%) */}
        <div className="flex flex-col overflow-hidden bg-[#12141c] p-2 text-xs font-mono">
          <FileTree />
        </div>
      </main>

      {/* Toast Overlay */}
      {toastNotifications.length > 0 && (
        <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-sm">
          {toastNotifications.slice(-3).map((toast) => (
            <div
              key={toast.id}
              className={`p-3 rounded-lg border shadow-xl text-xs flex items-center justify-between gap-3 font-mono ${
                toastStyles[toast.type] || toastStyles.info
              }`}
            >
              <div className="flex items-center gap-2">
                {toastIcons[toast.type]}
                <span>{toast.message}</span>
              </div>
              <button
                onClick={() => removeToast(toast.id)}
                className="hover:text-white"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
