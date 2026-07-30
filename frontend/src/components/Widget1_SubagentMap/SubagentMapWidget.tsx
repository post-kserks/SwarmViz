import React, { useMemo, useRef } from 'react';
import { ReactFlow, Background, Controls, Node, Edge, NodeTypes } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Camera, Layers } from 'lucide-react';
import { toPng } from 'html-to-image';
import { useSwarmStore } from '../../store/useSwarmStore';
import { AgentCustomNode } from './AgentCustomNode';
import { AgentPopover } from './AgentPopover';
import { getLayoutedElements } from './graphLayout';

const nodeTypes: NodeTypes = {
  agentNode: AgentCustomNode as any,
};

const PERF_DEGRADE_THRESHOLD = 50;

export const SubagentMapWidget: React.FC = () => {
  const graphRef = useRef<HTMLDivElement>(null);
  const { agents, edges, activeClaims, selectedAgentPopover, setSelectedAgentPopover } = useSwarmStore((state) => ({
    agents: state.agents,
    edges: state.edges,
    activeClaims: state.activeClaims,
    selectedAgentPopover: state.selectedAgentPopover,
    setSelectedAgentPopover: state.setSelectedAgentPopover,
  }));

  const nodeCount = Object.keys(agents).length;
  const isHighPerfMode = nodeCount > PERF_DEGRADE_THRESHOLD;

  const { nodes, edges: layoutedEdges } = useMemo(() => {
    const rawNodes: Node[] = Object.values(agents).map((ag) => {
      const claimCount = Object.values(activeClaims).filter(
        (c) => c.agentId === ag.id || (c as any).agent_id === ag.id
      ).length;
      return {
        id: ag.id,
        type: 'agentNode',
        data: {
          label: ag.label || ag.id,
          type: ag.type,
          status: ag.status,
          activeClaimCount: claimCount,
          isHighPerfMode,
          onNodeClick: (id: string) => setSelectedAgentPopover(id),
        },
        position: { x: 0, y: 0 },
      };
    });

    const rawEdges: Edge[] = edges.map((e) => ({
      id: e.id || `${e.fromId}->${e.toId}`,
      source: e.fromId,
      target: e.toId,
      animated: !isHighPerfMode,
      style: { stroke: '#475569', strokeWidth: 1.5 },
    }));

    return getLayoutedElements(rawNodes, rawEdges);
  }, [agents, edges, activeClaims, isHighPerfMode, setSelectedAgentPopover]);

  const handleExportPNG = async () => {
    if (!graphRef.current) return;
    try {
      const dataUrl = await toPng(graphRef.current, { backgroundColor: '#0a0b0e' });
      const link = document.createElement('a');
      link.download = `swarmviz-graph-${Date.now()}.png`;
      link.href = dataUrl;
      link.click();
    } catch (err) {
      console.error('Failed to export graph image:', err);
    }
  };

  return (
    <div ref={graphRef} className="w-full h-full relative bg-[#0a0b0e] select-none">
      {/* Widget Header Controls */}
      <div className="absolute top-3 left-3 z-10 flex items-center gap-2 bg-[#12141c]/80 backdrop-blur border border-[#1e2230] p-1.5 rounded-lg text-xs font-mono">
        <div className="flex items-center gap-1.5 px-2 text-slate-300">
          <Layers className="w-4 h-4 text-purple-400" />
          <span>SUBAGENT MAP ({nodeCount})</span>
        </div>
        {isHighPerfMode && (
          <span className="text-[10px] text-amber-400 bg-amber-500/10 px-1.5 py-0.5 rounded border border-amber-500/30">
            PERF MODE (&gt;50 NODES)
          </span>
        )}
        <button
          onClick={handleExportPNG}
          title="Export Graph PNG"
          className="p-1.5 hover:bg-slate-800 text-slate-400 hover:text-slate-200 rounded transition"
        >
          <Camera className="w-4 h-4" />
        </button>
      </div>

      <ReactFlow nodes={nodes} edges={layoutedEdges} nodeTypes={nodeTypes} fitView>
        <Background color="#1e2230" gap={20} />
        <Controls />
      </ReactFlow>

      {selectedAgentPopover && (
        <AgentPopover agentId={selectedAgentPopover} onClose={() => setSelectedAgentPopover(null)} />
      )}
    </div>
  );
};
