import React, { useState } from 'react';
import { ChevronDown } from 'lucide-react';

interface DiffHunkViewerProps {
  hunk: string;
}

export const DiffHunkViewer: React.FC<DiffHunkViewerProps> = ({ hunk }) => {
  const [isExpanded, setIsExpanded] = useState(false);

  if (!hunk) return null;

  const lines = hunk.split('\n');
  const unchangedLines = lines.filter((l) => !l.startsWith('+') && !l.startsWith('-') && !l.startsWith('@'));
  const isLargeBlock = unchangedLines.length > 6;

  return (
    <div className="font-mono text-xs overflow-x-auto bg-[#0a0b0e] p-2 rounded border border-[#1e2230] space-y-0.5 custom-scrollbar">
      {lines.map((line, idx) => {
        const isAdd = line.startsWith('+') && !line.startsWith('+++');
        const isRemove = line.startsWith('-') && !line.startsWith('---');
        const isHeader = line.startsWith('@@');

        if (isHeader) {
          return (
            <div key={idx} className="text-purple-400/80 font-bold py-0.5 text-[11px]">
              {line}
            </div>
          );
        }

        if (!isAdd && !isRemove && isLargeBlock && !isExpanded && idx > 3 && idx < lines.length - 3) {
          if (idx === 4) {
            return (
              <button
                key={idx}
                onClick={() => setIsExpanded(true)}
                className="w-full py-1 text-[11px] text-slate-500 hover:text-slate-300 bg-slate-900/60 hover:bg-slate-800 flex items-center justify-center gap-1 rounded my-1 transition"
              >
                <ChevronDown className="w-3.5 h-3.5" /> ... [{unchangedLines.length} Unchanged Lines Skipped] ...
              </button>
            );
          }
          return null;
        }

        let bgStyle = 'text-slate-400';
        if (isAdd) bgStyle = 'bg-[rgba(34,197,94,0.15)] text-emerald-300 font-medium px-1 rounded-sm';
        if (isRemove) bgStyle = 'bg-[rgba(239,68,68,0.15)] text-rose-300 font-medium px-1 rounded-sm';

        return (
          <div key={idx} className={`whitespace-pre ${bgStyle}`}>
            {line}
          </div>
        );
      })}
    </div>
  );
};
