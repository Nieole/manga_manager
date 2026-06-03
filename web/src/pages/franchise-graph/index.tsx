import React, { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import axios from 'axios';
import {
  ReactFlow,
  MiniMap,
  Controls,
  Background,
  useNodesState,
  useEdgesState,
  MarkerType,
  BackgroundVariant,
  Position
} from '@xyflow/react';
import type { Node, Edge } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import dagre from 'dagre';
import { ArrowLeft, RefreshCw } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { CustomNode } from './CustomNode';
import type { FranchiseRelation } from '../series-detail/types';

const nodeTypes = {
  custom: CustomNode,
};

const dagreGraph = new dagre.graphlib.Graph();
dagreGraph.setDefaultEdgeLabel(() => ({}));

const getLayoutedElements = (nodes: Node[], edges: Edge[], direction = 'TB') => {
  const isHorizontal = direction === 'LR';
  dagreGraph.setGraph({ rankdir: direction, nodesep: 100, ranksep: 150 });

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: 144, height: 250 });
  });

  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target);
  });

  dagre.layout(dagreGraph);

  nodes.forEach((node) => {
    const nodeWithPosition = dagreGraph.node(node.id);
    node.targetPosition = isHorizontal ? Position.Left : Position.Top;
    node.sourcePosition = isHorizontal ? Position.Right : Position.Bottom;

    // We are shifting the dagre node position (anchor=center center) to the top left
    // so it matches the React Flow node anchor point (top left).
    node.position = {
      x: nodeWithPosition.x - 144 / 2,
      y: nodeWithPosition.y - 250 / 2,
    };

    return node;
  });

  return { nodes, edges };
};

export const FranchiseGraphPage: React.FC = () => {
  const { id, libId } = useParams<{ id?: string, libId?: string }>();
  const navigate = useNavigate();
  const { t } = useI18n();
  const seriesId = id ? parseInt(id, 10) : 0;
  const libraryId = libId ? parseInt(libId, 10) : 0;

  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [isLoading, setIsLoading] = useState(true);

  const fetchGraphData = useCallback(async () => {
    setIsLoading(true);
    try {
      const endpoint = libraryId 
        ? `/api/libraries/${libraryId}/franchise` 
        : `/api/series/${seriesId}/franchise`;
      const res = await axios.get<FranchiseRelation[]>(endpoint);
      const relations = res.data || [];

      const seriesMap = new Map<number, { id: number; name: string; cover_path: string; isCurrent: boolean }>();
      relations.forEach(rel => {
        if (!seriesMap.has(rel.source_series_id)) {
          seriesMap.set(rel.source_series_id, {
            id: rel.source_series_id,
            name: rel.source_series_name,
            cover_path: rel.source_cover_path,
            isCurrent: seriesId > 0 && rel.source_series_id === seriesId
          });
        }
        if (!seriesMap.has(rel.target_series_id)) {
          seriesMap.set(rel.target_series_id, {
            id: rel.target_series_id,
            name: rel.target_series_name,
            cover_path: rel.target_cover_path,
            isCurrent: seriesId > 0 && rel.target_series_id === seriesId
          });
        }
      });

      const initialNodes: Node[] = Array.from(seriesMap.values()).map(s => ({
        id: s.id.toString(),
        type: 'custom',
        position: { x: 0, y: 0 },
        data: { name: s.name, coverPath: s.cover_path, isCurrent: s.isCurrent },
      }));

      const initialEdges: Edge[] = relations.map(rel => ({
        id: `e${rel.source_series_id}-${rel.target_series_id}`,
        source: rel.source_series_id.toString(),
        target: rel.target_series_id.toString(),
        label: t(`series.relations.type.${rel.relation_type}`) || rel.relation_type,
        animated: true,
        style: { stroke: '#a855f7', strokeWidth: 2 },
        labelStyle: { fill: '#d1d5db', fontWeight: 600, fontSize: 12 },
        labelBgStyle: { fill: '#1f2937', color: '#fff', fillOpacity: 0.8 },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 20,
          height: 20,
          color: '#a855f7',
        },
      }));

      const { nodes: layoutedNodes, edges: layoutedEdges } = getLayoutedElements(initialNodes, initialEdges);
      
      setNodes([...layoutedNodes]);
      setEdges([...layoutedEdges]);
    } catch (error) {
      console.error(error);
    } finally {
      setIsLoading(false);
    }
  }, [seriesId, libraryId, t, setNodes, setEdges]);

  useEffect(() => {
    fetchGraphData();
  }, [fetchGraphData]);

  const onNodeClick = useCallback((_: any, node: Node) => {
    navigate(`/series/${node.id}`);
  }, [navigate]);

  return (
    <div className="flex flex-col h-screen w-full bg-gray-950 text-white overflow-hidden">
      <header className="flex h-16 shrink-0 items-center gap-4 border-b border-white/10 px-6 backdrop-blur-md bg-gray-950/80 z-10 relative">
        <button
          onClick={() => navigate(-1)}
          className="flex h-9 w-9 items-center justify-center rounded-full hover:bg-white/10 transition-colors"
          title={t('common.back') || 'Back'}
        >
          <ArrowLeft className="h-5 w-5 text-gray-400 hover:text-white" />
        </button>
        <div>
          <h1 className="text-lg font-bold">
            {libraryId ? (t('library.franchise.title') || 'Library Relationships Graph') : (t('series.franchise.title') || 'Franchise Universe')}
          </h1>
          <p className="text-xs text-gray-400">
            {libraryId ? (t('library.franchise.description') || 'Visual graph of all connected series in this library') : (t('series.franchise.description') || 'Visual relationship graph')}
          </p>
        </div>
      </header>

      <div className="flex-1 relative w-full h-full">
        {isLoading ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <RefreshCw className="h-8 w-8 animate-spin text-komgaPrimary" />
          </div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={onNodeClick}
            nodeTypes={nodeTypes}
            fitView
            fitViewOptions={{ padding: 0.2 }}
            minZoom={0.2}
            maxZoom={2}
            proOptions={{ hideAttribution: true }}
          >
            <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#374151" />
            <Controls className="!bg-gray-900 !border-white/10 !fill-gray-400 hover:!fill-white" />
            <MiniMap 
              className="!bg-gray-900 !border-white/10"
              maskColor="rgba(0, 0, 0, 0.7)"
              nodeColor={(n: any) => n.data.isCurrent ? '#a855f7' : '#4b5563'} 
            />
          </ReactFlow>
        )}
      </div>
    </div>
  );
};

export default FranchiseGraphPage;
