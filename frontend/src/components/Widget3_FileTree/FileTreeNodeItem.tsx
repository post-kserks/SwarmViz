import React, { useState } from 'react';
import { Folder, FolderOpen, FileText, ChevronRight, ChevronDown } from 'lucide-react';
import { FileTreeNode, FileClaim } from '../../types/swarm';

interface FileTreeNodeItemProps {
  node: FileTreeNode;
  activeClaims: Record<string, FileClaim>;
  conflicts: Record<string, string[]>;
  showAllFiles: boolean;
  searchQuery: string;
  onSelectFile: (path: string) => void;
  selectedFile: string | null;
}

export const FileTreeNodeItem: React.FC<FileTreeNodeItemProps> = ({
  node,
  activeClaims,
  conflicts,
  showAllFiles,
  searchQuery,
  onSelectFile,
  selectedFile,
}) => {
  const [isOpen, setIsOpen] = useState<boolean>(true);

  const isDir = node.isDir || node.isDirectory;
  const hasClaim = !!activeClaims[node.path];
  const hasEdits = node.editCount > 0;
  const isConflict = !!conflicts[node.path];

  // Visibility filtering
  if (!isDir) {
    const matchesSearch = searchQuery === '' || node.path.toLowerCase().includes(searchQuery.toLowerCase());
    const matchesFilter = showAllFiles || hasEdits || hasClaim;
    if (!matchesSearch || !matchesFilter) return null;
  } else {
    // Directory node: check if any children are visible
    if (!showAllFiles && searchQuery === '') {
      const hasVisibleChildren = (children?: FileTreeNode[]): boolean => {
        if (!children) return false;
        return children.some((c) => {
          const childIsDir = c.isDir || c.isDirectory;
          return childIsDir ? hasVisibleChildren(c.children) : (c.editCount > 0 || !!activeClaims[c.path]);
        });
      };
      if (!hasVisibleChildren(node.children)) return null;
    }
  }

  const isSelected = selectedFile === node.path;

  return (
    <div className="select-none font-sans text-xs">
      <div
        onClick={() => {
          if (isDir) setIsOpen(!isOpen);
          else onSelectFile(node.path);
        }}
        className={`flex items-center gap-1.5 py-1 px-2 rounded cursor-pointer transition-colors ${
          isSelected
            ? 'bg-purple-900/40 text-purple-200 border border-purple-500/30'
            : 'hover:bg-[#1e2230]/50 text-slate-300'
        }`}
      >
        {/* Toggle chevron for directories */}
        {isDir ? (
          <span className="text-slate-400">
            {isOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          </span>
        ) : (
          <span className="w-3.5 h-3.5" />
        )}

        {/* Directory or File Icon */}
        {isDir ? (
          isOpen ? <FolderOpen className="w-4 h-4 text-sky-400" /> : <Folder className="w-4 h-4 text-sky-400" />
        ) : (
          <FileText className="w-4 h-4 text-slate-400" />
        )}

        {/* Name */}
        <span className={`truncate flex-1 ${isSelected ? 'font-semibold text-white' : ''}`}>
          {node.name}
        </span>

        {/* Real-time Indicator Dots */}
        {!isDir && (
          <div className="flex items-center gap-1.5 ml-2">
            {isConflict && (
              <span className="w-2 h-2 rounded-full bg-amber-400 animate-ping" title="Attribution Conflict" />
            )}
            {hasClaim ? (
              <span className="w-2 h-2 rounded-full bg-emerald-400 shadow-[0_0_8px_#34d399]" title="Active Agent Claim (🟢)" />
            ) : hasEdits ? (
              <span className="w-2 h-2 rounded-full bg-blue-400" title="Edited in session (🔵)" />
            ) : null}

            {/* Edit Count Monospace Badge */}
            {node.editCount > 0 && (
              <span className="font-mono text-[10px] bg-[#1e2230] text-slate-400 px-1.5 py-0.5 rounded border border-[#2d3348]">
                {node.editCount} {node.editCount === 1 ? 'edit' : 'edits'}
              </span>
            )}
          </div>
        )}
      </div>

      {/* Recursive Children */}
      {isDir && isOpen && node.children && (
        <div className="pl-3.5 border-l border-[#1e2230]/60 ml-2">
          {node.children.map((child) => (
            <FileTreeNodeItem
              key={child.path}
              node={child}
              activeClaims={activeClaims}
              conflicts={conflicts}
              showAllFiles={showAllFiles}
              searchQuery={searchQuery}
              onSelectFile={onSelectFile}
              selectedFile={selectedFile}
            />
          ))}
        </div>
      )}
    </div>
  );
};
