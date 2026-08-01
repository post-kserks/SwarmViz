import dagre from 'dagre';
import { Node, Edge } from '@xyflow/react';

export const getLayoutedElements = (nodes: Node[], edges: Edge[], direction = 'TB') => {
  const dagreGraph = new dagre.graphlib.Graph();
  dagreGraph.setDefaultEdgeLabel(() => ({}));
  dagreGraph.setGraph({ rankdir: direction, nodesep: 40, ranksep: 60 });

  // Height covers the tallest node: header, two clamped lines of task text and
  // the status row. dagre only knows the box it is told about, so understating
  // it would let a node with a task overlap the rank below.
  const NODE_WIDTH = 180;
  const NODE_HEIGHT = 104;

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  });

  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target);
  });

  dagre.layout(dagreGraph);

  const layoutedNodes = nodes.map((node) => {
    const nodeWithPosition = dagreGraph.node(node.id);
    return {
      ...node,
      position: {
        // dagre reports the node's centre; React Flow positions by top-left.
        x: nodeWithPosition ? nodeWithPosition.x - NODE_WIDTH / 2 : 0,
        y: nodeWithPosition ? nodeWithPosition.y - NODE_HEIGHT / 2 : 0,
      },
    };
  });

  return { nodes: layoutedNodes, edges };
};
