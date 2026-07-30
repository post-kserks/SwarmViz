import React, { useState, useRef, useMemo, useEffect } from 'react';
import { VariableSizeList as List } from 'react-window';
import { ArrowDown, Filter, Search, X } from 'lucide-react';
import { useSwarmStore } from '../../store/useSwarmStore';
import { DiffCard } from './DiffCard';
import { EditEvent } from '../../types/swarm';

export const LiveEditStreamWidget: React.FC = () => {
  const containerRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<List>(null);
  const [containerHeight, setContainerHeight] = useState<number>(600);
  const [filterFileOnly, setFilterFileOnly] = useState(false);
  const [selectedAgent, setSelectedAgent] = useState<string>('ALL');
  const [searchQuery, setSearchQuery] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const [unreadCount, setUnreadCount] = useState(0);

  const { editStream, agents, conflicts, selectedFileFilter, setSelectedFileFilter } = useSwarmStore((state) => ({
    editStream: state.editStream,
    agents: state.agents,
    conflicts: state.conflicts,
    selectedFileFilter: state.selectedFileFilter,
    setSelectedFileFilter: state.setSelectedFileFilter,
  }));

  // Auto-resize list container
  useEffect(() => {
    const updateHeight = () => {
      if (containerRef.current) {
        setContainerHeight(containerRef.current.clientHeight);
      }
    };
    updateHeight();
    window.addEventListener('resize', updateHeight);
    return () => window.removeEventListener('resize', updateHeight);
  }, []);

  // Filtering Logic
  const filteredEvents = useMemo(() => {
    return editStream.filter((event) => {
      if (selectedFileFilter && event.file !== selectedFileFilter) return false;
      if (filterFileOnly && event.added === 0 && event.removed === 0) return false;
      if (selectedAgent !== 'ALL' && event.agentId !== selectedAgent) return false;
      if (searchQuery.trim() !== '') {
        const q = searchQuery.toLowerCase();
        if (!event.file.toLowerCase().includes(q) && !event.agentId.toLowerCase().includes(q)) return false;
      }
      return true;
    });
  }, [editStream, filterFileOnly, selectedAgent, searchQuery, selectedFileFilter]);

  // Track new unread events when autoScroll is paused
  const prevCountRef = useRef(filteredEvents.length);
  useEffect(() => {
    if (!autoScroll) {
      const delta = filteredEvents.length - prevCountRef.current;
      if (delta > 0) setUnreadCount((c) => c + delta);
    } else {
      setUnreadCount(0);
      if (listRef.current && filteredEvents.length > 0) {
        listRef.current.scrollToItem(filteredEvents.length - 1, 'end');
      }
    }
    prevCountRef.current = filteredEvents.length;
  }, [filteredEvents.length, autoScroll]);

  // Handle Scroll Position for Auto-Scroll logic
  const handleScroll = ({ scrollDirection }: any) => {
    if (scrollDirection === 'backward') {
      setAutoScroll(false);
    }
  };

  const scrollToBottom = () => {
    if (listRef.current && filteredEvents.length > 0) {
      listRef.current.scrollToItem(filteredEvents.length - 1, 'end');
      setAutoScroll(true);
      setUnreadCount(0);
    }
  };

  // Estimated height calculation for diff cards
  const getItemSize = (index: number) => {
    const event = filteredEvents[index];
    if (!event) return 100;
    if (event.skipped || event.binary) return 80;
    const lineCount = event.hunk ? event.hunk.split('\n').length : 3;
    return Math.min(Math.max(lineCount * 20 + 65, 90), 350);
  };

  return (
    <div className="flex flex-col h-full w-full bg-[#0a0b0e] select-none relative">
      {/* Stream Filter Header */}
      <div className="p-3 bg-[#12141c] border-b border-[#1e2230] flex items-center justify-between gap-3 text-xs font-mono shrink-0">
        <div className="flex items-center gap-2 flex-1">
          <div className="relative flex-1 max-w-xs">
            <Search className="w-3.5 h-3.5 text-slate-500 absolute left-2.5 top-2.5" />
            <input
              type="text"
              placeholder="Search file or agent..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full bg-[#0a0b0e] border border-[#1e2230] rounded pl-8 pr-3 py-1.5 text-slate-200 placeholder-slate-600 focus:outline-none focus:border-purple-500"
            />
          </div>

          <select
            value={selectedAgent}
            onChange={(e) => setSelectedAgent(e.target.value)}
            className="bg-[#0a0b0e] border border-[#1e2230] rounded px-2.5 py-1.5 text-slate-300 focus:outline-none focus:border-purple-500"
          >
            <option value="ALL">All Agents</option>
            {Object.values(agents).map((ag) => (
              <option key={ag.id} value={ag.id}>
                {ag.label} ({ag.id.slice(0, 6)})
              </option>
            ))}
          </select>
        </div>

        <button
          onClick={() => setFilterFileOnly(!filterFileOnly)}
          className={`px-2.5 py-1.5 rounded border flex items-center gap-1.5 transition ${
            filterFileOnly
              ? 'bg-purple-950/60 border-purple-500/50 text-purple-300'
              : 'bg-[#0a0b0e] border-[#1e2230] text-slate-400 hover:text-slate-200'
          }`}
        >
          <Filter className="w-3.5 h-3.5" /> Edits Only
        </button>
      </div>

      {/* Selected File Banner */}
      {selectedFileFilter && (
        <div className="px-3 py-1.5 bg-purple-950/40 border-b border-purple-500/30 text-xs font-mono flex items-center justify-between text-purple-300 shrink-0">
          <span>Filtering stream by tree selection: <strong>{selectedFileFilter}</strong></span>
          <button
            onClick={() => setSelectedFileFilter(null)}
            className="hover:text-white p-0.5"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Stream Card Virtualized Area */}
      <div ref={containerRef} className="flex-1 p-3 overflow-hidden relative">
        {filteredEvents.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-slate-600 font-mono text-xs">
            <span>No live diff events matching filters</span>
          </div>
        ) : (
          <List
            ref={listRef}
            height={containerHeight > 0 ? containerHeight - 24 : 600}
            width="100%"
            itemCount={filteredEvents.length}
            itemSize={getItemSize}
            onScroll={handleScroll}
          >
            {({ index, style }) => (
              <div style={style} className="pb-3 px-1">
                <DiffCard
                  event={filteredEvents[index]}
                  hasConflict={!!conflicts[filteredEvents[index].file]}
                />
              </div>
            )}
          </List>
        )}
      </div>

      {/* Floating Auto-Scroll Button */}
      {!autoScroll && (
        <button
          onClick={scrollToBottom}
          className="absolute bottom-4 left-1/2 -translate-x-1/2 bg-purple-600 hover:bg-purple-500 text-white font-mono text-xs px-3 py-1.5 rounded-full shadow-lg border border-purple-400/40 flex items-center gap-1.5 transition z-30"
        >
          <ArrowDown className="w-4 h-4 animate-bounce" />
          <span>New events ({unreadCount})</span>
        </button>
      )}
    </div>
  );
};
