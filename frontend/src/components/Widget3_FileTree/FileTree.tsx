import React, { useState } from 'react';
import { Search, Eye, Filter } from 'lucide-react';
import {
  useFileTree,
  useActiveClaims,
  useConflicts,
  useShowAllFiles,
  useSelectedFileFilter,
  useSwarmStore,
} from '../../store/useSwarmStore';
import { FileTreeNodeItem } from './FileTreeNodeItem';

export const FileTree: React.FC = () => {
  const fileTree = useFileTree();
  const activeClaims = useActiveClaims();
  const conflicts = useConflicts();
  const showAllFiles = useShowAllFiles();
  const selectedFile = useSelectedFileFilter();
  const { setShowAllFiles, setSelectedFileFilter } = useSwarmStore();

  const [searchQuery, setSearchQuery] = useState<string>('');

  return (
    <div className="flex flex-col h-full bg-[#12141c] border border-[#1e2230] rounded-lg p-3 overflow-hidden shadow-lg">
      {/* Header Bar */}
      <div className="flex items-center justify-between pb-2 border-b border-[#1e2230] mb-3 shrink-0">
        <h3 className="text-sm font-semibold text-slate-200 flex items-center gap-2">
          <Filter className="w-4 h-4 text-purple-400" />
          Workspace Tree
        </h3>

        {/* Show All Toggle */}
        <label className="flex items-center gap-1.5 text-xs text-slate-400 cursor-pointer hover:text-slate-200 transition-colors">
          <input
            type="checkbox"
            checked={showAllFiles}
            onChange={(e) => setShowAllFiles(e.target.checked)}
            className="rounded border-[#1e2230] bg-[#0a0b0e] text-purple-600 focus:ring-0 w-3.5 h-3.5"
          />
          <Eye className="w-3.5 h-3.5" />
          Show All
        </label>
      </div>

      {/* Fuzzy Search Bar */}
      <div className="relative mb-3 shrink-0">
        <Search className="w-3.5 h-3.5 absolute left-2.5 top-2.5 text-slate-500" />
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="Filter files..."
          className="w-full bg-[#0a0b0e] border border-[#1e2230] rounded pl-8 pr-3 py-1.5 text-xs text-slate-200 placeholder-slate-500 focus:outline-none focus:border-purple-500/50 transition-colors"
        />
        {searchQuery && (
          <button
            onClick={() => setSearchQuery('')}
            className="absolute right-2.5 top-2 text-xs text-slate-500 hover:text-slate-300"
          >
            ×
          </button>
        )}
      </div>

      {/* Selected File Filter Banner */}
      {selectedFile && (
        <div className="mb-2 px-2 py-1 bg-purple-950/40 border border-purple-500/40 rounded text-xs flex items-center justify-between text-purple-300 shrink-0">
          <span className="truncate">Filtering: <strong>{selectedFile}</strong></span>
          <button
            onClick={() => setSelectedFileFilter(null)}
            className="text-purple-400 hover:text-purple-200 ml-2 font-bold"
          >
            Clear
          </button>
        </div>
      )}

      {/* File Tree List */}
      <div className="flex-1 overflow-y-auto custom-scrollbar pr-1">
        {fileTree && fileTree.children && fileTree.children.length > 0 ? (
          fileTree.children.map((node) => (
            <FileTreeNodeItem
              key={node.path}
              node={node}
              activeClaims={activeClaims}
              conflicts={conflicts}
              showAllFiles={showAllFiles}
              searchQuery={searchQuery}
              onSelectFile={(path) => setSelectedFileFilter(path === selectedFile ? null : path)}
              selectedFile={selectedFile}
            />
          ))
        ) : (
          <div className="text-xs text-slate-500 italic p-4 text-center">
            No workspace files tracked yet
          </div>
        )}
      </div>
    </div>
  );
};
