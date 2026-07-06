/**
 * 业务说明：本文件是业务实现，属于前端漫画系列关系图谱，负责把续作、前传、衍生、改编等关系渲染为可探索的节点网络。
 * 它依赖后端系列关系数据、封面缩略图和主题变量，帮助用户理解作品之间的方向性关联。
 * 维护时应关注力导布局稳定性、连线箭头方向、主题背景、节点可读性和大图交互性能。
 */

import React, { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { apiClient } from '../../api/client';
import {
  ReactFlow,
  MiniMap,
  Controls,
  Background,
  BaseEdge,
  useNodesState,
  useEdgesState,
  BackgroundVariant,
  getStraightPath,
  Position,
  useStore,
} from '@xyflow/react';
import type { Node, Edge, EdgeProps, InternalNode, ReactFlowState } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { ArrowLeft, RefreshCw } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { CustomNode } from './CustomNode';
import type { FranchiseRelation } from '../series-detail/types';

// 图谱节点承载的业务数据形状（与 CustomNode 的 data 契约一致）。
type FranchiseNodeData = {
  name: string;
  coverPath: string;
  isCurrent: boolean;
  degree?: number;
};

const GRAPH_NODE_WIDTH = 112;
const GRAPH_NODE_HEIGHT = 138;

const graphNodeSize = (data: FranchiseNodeData | undefined) => {
  return data?.isCurrent ? 74 : Math.min(70, 52 + (data?.degree ?? 0) * 4);
};

const getVisualNodeCenter = (
  node: InternalNode | undefined,
  fallback: { x: number; y: number },
) => {
  if (!node) return fallback;
  const position = node.internals?.positionAbsolute ?? node.position;
  // React Flow 的内部 store 无类型（data 为 Record<string, unknown>），在此边界处收窄为业务类型。
  const data = (node.internals?.userNode?.data ?? node.data) as FranchiseNodeData | undefined;
  if (!position) return fallback;

  return {
    x: position.x + GRAPH_NODE_WIDTH / 2,
    y: position.y + graphNodeSize(data) / 2,
  };
};

// 关系边承载的是“source 系列指向 target 系列”的业务语义，不能只依赖 React Flow 默认锚点。
// 这里使用节点视觉中心重算路径，并在边的中后段绘制小箭头，让续作、前传、改编等方向在图谱里可读。
function DirectedEdge({
  id,
  source,
  sourceX,
  sourceY,
  target,
  targetX,
  targetY,
  label,
  labelStyle,
  labelShowBg,
  labelBgStyle,
  labelBgPadding,
  labelBgBorderRadius,
  style,
  interactionWidth,
}: EdgeProps) {
  const sourceNode = useStore((state: ReactFlowState) => state.nodeLookup.get(source));
  const targetNode = useStore((state: ReactFlowState) => state.nodeLookup.get(target));
  const sourceCenter = getVisualNodeCenter(sourceNode, { x: sourceX, y: sourceY });
  const targetCenter = getVisualNodeCenter(targetNode, { x: targetX, y: targetY });
  const [edgePath, labelX, labelY] = getStraightPath({
    sourceX: sourceCenter.x,
    sourceY: sourceCenter.y,
    targetX: targetCenter.x,
    targetY: targetCenter.y,
  });
  const arrowX = sourceCenter.x + (targetCenter.x - sourceCenter.x) * 0.62;
  const arrowY = sourceCenter.y + (targetCenter.y - sourceCenter.y) * 0.62;
  const angle = Math.atan2(targetCenter.y - sourceCenter.y, targetCenter.x - sourceCenter.x) * (180 / Math.PI);

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        label={label}
        labelX={labelX}
        labelY={labelY}
        labelStyle={labelStyle}
        labelShowBg={labelShowBg}
        labelBgStyle={labelBgStyle}
        labelBgPadding={labelBgPadding}
        labelBgBorderRadius={labelBgBorderRadius}
        style={style}
        interactionWidth={interactionWidth}
      />
      <g
        className="franchise-graph-edge-arrow"
        transform={`translate(${arrowX}, ${arrowY}) rotate(${angle})`}
        pointerEvents="none"
      >
        <path d="M -4 -2.8 L 4 0 L -4 2.8 z" />
      </g>
    </>
  );
}

const nodeTypes = {
  custom: CustomNode,
};

const edgeTypes = {
  directed: DirectedEdge,
};

const seededUnit = (value: string) => {
  let hash = 2166136261;
  for (let i = 0; i < value.length; i += 1) {
    hash ^= value.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0) / 4294967295;
};

// 关系图谱采用轻量力导布局：节点间互斥让画面松散，边的弹簧力维持关联距离，中心拉力避免整体漂移过远。
// 算法只在数据加载时执行一次，输出稳定坐标，保证刷新后同一批关系不会因为随机数导致用户失去空间记忆。
const getForceLayoutedElements = (nodes: Node[], edges: Edge[]) => {
  if (nodes.length === 0) return { nodes, edges };

  const degree = new Map<string, number>();
  nodes.forEach((node) => degree.set(node.id, 0));
  edges.forEach((edge) => {
    degree.set(edge.source, (degree.get(edge.source) ?? 0) + 1);
    degree.set(edge.target, (degree.get(edge.target) ?? 0) + 1);
  });

  const radius = Math.max(420, nodes.length * 54);
  const centerPull = nodes.length > 30 ? 0.005 : 0.007;
  const linkDistance = Math.min(360, Math.max(190, 120 + nodes.length * 7));
  const repulsion = Math.min(70000, Math.max(26000, nodes.length * 2600));
  const positions = new Map<string, { x: number; y: number; vx: number; vy: number }>();

  nodes.forEach((node, index) => {
    const angle = (Math.PI * 2 * index) / nodes.length + seededUnit(node.id) * 0.8;
    const orbit = radius * (0.55 + seededUnit(`${node.id}:orbit`) * 0.45);
    positions.set(node.id, {
      x: Math.cos(angle) * orbit,
      y: Math.sin(angle) * orbit,
      vx: 0,
      vy: 0,
    });
  });

  for (let tick = 0; tick < 320; tick += 1) {
    const cooling = 1 - tick / 360;
    for (let i = 0; i < nodes.length; i += 1) {
      const a = positions.get(nodes[i].id);
      if (!a) continue;
      for (let j = i + 1; j < nodes.length; j += 1) {
        const b = positions.get(nodes[j].id);
        if (!b) continue;
        const dx = a.x - b.x;
        const dy = a.y - b.y;
        const distanceSq = Math.max(dx * dx + dy * dy, 900);
        const distance = Math.sqrt(distanceSq);
        const force = (repulsion / distanceSq) * cooling;
        const fx = (dx / distance) * force;
        const fy = (dy / distance) * force;
        a.vx += fx;
        a.vy += fy;
        b.vx -= fx;
        b.vy -= fy;
      }
    }

    edges.forEach((edge) => {
      const source = positions.get(edge.source);
      const target = positions.get(edge.target);
      if (!source || !target) return;
      const dx = target.x - source.x;
      const dy = target.y - source.y;
      const distance = Math.max(Math.sqrt(dx * dx + dy * dy), 1);
      const force = (distance - linkDistance) * 0.012 * cooling;
      const fx = (dx / distance) * force;
      const fy = (dy / distance) * force;
      source.vx += fx;
      source.vy += fy;
      target.vx -= fx;
      target.vy -= fy;
    });

    positions.forEach((point) => {
      point.vx += -point.x * centerPull * cooling;
      point.vy += -point.y * centerPull * cooling;
      point.vx *= 0.82;
      point.vy *= 0.82;
      point.x += point.vx;
      point.y += point.vy;
    });
  }

  const layoutedNodes = nodes.map((node) => {
    const point = positions.get(node.id) ?? { x: 0, y: 0 };
    const nodeDegree = degree.get(node.id) ?? 0;
    return {
      ...node,
      targetPosition: Position.Top,
      sourcePosition: Position.Bottom,
      position: {
        x: point.x - GRAPH_NODE_WIDTH / 2,
        y: point.y - GRAPH_NODE_HEIGHT / 2,
      },
      data: {
        ...node.data,
        degree: nodeDegree,
      },
    };
  });

  return { nodes: layoutedNodes, edges };
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
  // 图谱按节点级上限截断时，省略的系列数（>0 显示提示条）。
  const [truncatedCount, setTruncatedCount] = useState(0);

  // 后端返回的是关系边列表，前端先聚合出唯一系列节点，再根据关系类型生成有向边。
  // 单系列入口会突出当前系列；全库入口则展示库内所有关系，不强制指定中心节点。
  const fetchGraphData = useCallback(async () => {
    setIsLoading(true);
    try {
      const endpoint = libraryId 
        ? `/api/libraries/${libraryId}/franchise` 
        : `/api/series/${seriesId}/franchise`;
      const res = await apiClient.get<FranchiseRelation[]>(endpoint);
      const relations = res.data || [];

      // 节点级上限：>N 个节点的力导向图既不可读，其 O(N^2)×320 tick 布局又会冻结主线程。
      // 超限时按度数（连接数）保留最相关的 top-N 系列（单系列入口的当前系列始终保留），并提示已省略数量。
      const MAX_GRAPH_NODES = 200;

      const seriesMap = new Map<number, { id: number; name: string; cover_path: string; isCurrent: boolean }>();
      const degreeCount = new Map<number, number>();
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
        degreeCount.set(rel.source_series_id, (degreeCount.get(rel.source_series_id) ?? 0) + 1);
        degreeCount.set(rel.target_series_id, (degreeCount.get(rel.target_series_id) ?? 0) + 1);
      });

      let keptIds: Set<number> | null = null;
      if (seriesMap.size > MAX_GRAPH_NODES) {
        const ranked = Array.from(seriesMap.keys()).sort((a, b) => {
          const da = seriesId > 0 && a === seriesId ? Infinity : (degreeCount.get(a) ?? 0);
          const db = seriesId > 0 && b === seriesId ? Infinity : (degreeCount.get(b) ?? 0);
          return db - da;
        });
        keptIds = new Set(ranked.slice(0, MAX_GRAPH_NODES));
      }
      setTruncatedCount(keptIds ? seriesMap.size - keptIds.size : 0);

      const initialNodes: Node[] = Array.from(seriesMap.values())
        .filter(s => !keptIds || keptIds.has(s.id))
        .map(s => ({
          id: s.id.toString(),
          type: 'custom',
          position: { x: 0, y: 0 },
          data: { name: s.name, coverPath: s.cover_path, isCurrent: s.isCurrent },
        }));

      // 只保留两端节点都在图中的关系边，避免悬挂边。
      const visibleRelations = keptIds
        ? relations.filter(rel => keptIds.has(rel.source_series_id) && keptIds.has(rel.target_series_id))
        : relations;

      const showEdgeLabels = visibleRelations.length <= 40;
      const initialEdges: Edge[] = visibleRelations.map(rel => ({
        id: `e${rel.source_series_id}-${rel.target_series_id}`,
        source: rel.source_series_id.toString(),
        target: rel.target_series_id.toString(),
        type: 'directed',
        label: showEdgeLabels ? t(`series.relations.type.${rel.relation_type}`, undefined, rel.relation_type) : undefined,
        style: { stroke: 'rgb(var(--rgb-gray-500) / 0.5)', strokeWidth: 1.4 },
        labelStyle: { fill: 'rgb(var(--rgb-gray-700))', fontWeight: 600, fontSize: 11 },
        labelBgStyle: { fill: 'rgb(var(--rgb-komga-surface))', color: 'rgb(var(--rgb-white))', fillOpacity: 0.82 },
        labelBgPadding: [6, 3],
        labelBgBorderRadius: 999,
      }));

      const { nodes: layoutedNodes, edges: layoutedEdges } = getForceLayoutedElements(initialNodes, initialEdges);

      setNodes([...layoutedNodes]);
      setEdges([...layoutedEdges]);
    } catch (error) {
      console.error(error);
    } finally {
      setIsLoading(false);
    }
  }, [seriesId, libraryId, t, setNodes, setEdges, setTruncatedCount]);

  useEffect(() => {
    fetchGraphData();
  }, [fetchGraphData]);

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    navigate(`/series/${node.id}`);
  }, [navigate]);

  return (
    <div className="franchise-graph-page flex h-screen w-full flex-col overflow-hidden text-white">
      <header className="franchise-graph-header relative z-10 flex h-16 shrink-0 items-center gap-4 border-b px-6 backdrop-blur-md">
        <button
          onClick={() => navigate(-1)}
          className="flex h-9 w-9 items-center justify-center rounded-full hover:bg-white/10 transition-colors"
          title={t('common.back')}
        >
          <ArrowLeft className="h-5 w-5 text-gray-400 hover:text-white" />
        </button>
        <div>
          <h1 className="text-lg font-bold">
            {libraryId ? (t('library.franchise.title')) : (t('series.franchise.title'))}
          </h1>
          <p className="text-xs text-gray-400">
            {libraryId ? (t('library.franchise.description')) : (t('series.franchise.description'))}
          </p>
        </div>
      </header>

      <div className="flex-1 relative w-full h-full">
        <div className="franchise-graph-aura pointer-events-none absolute inset-0" />
        {!isLoading && truncatedCount > 0 && (
          <div className="absolute left-1/2 top-3 z-20 -translate-x-1/2 rounded-full border border-amber-400/30 bg-amber-500/15 px-4 py-1.5 text-xs font-medium text-amber-200 backdrop-blur">
            {t('franchise.graph.truncated', { count: truncatedCount })}
          </div>
        )}
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
            edgeTypes={edgeTypes}
            fitView
            fitViewOptions={{ padding: 0.32 }}
            minZoom={0.12}
            maxZoom={2.2}
            proOptions={{ hideAttribution: true }}
            className="franchise-graph-flow"
            defaultEdgeOptions={{
              focusable: false,
            }}
          >
            <Background variant={BackgroundVariant.Dots} gap={28} size={1.2} color="rgb(var(--rgb-gray-500) / 0.28)" />
            <Controls className="franchise-graph-controls" />
            <MiniMap
              className="franchise-graph-minimap"
              maskColor="rgb(var(--rgb-komga-background) / 0.72)"
              nodeColor={(n: Node) => (n.data as FranchiseNodeData).isCurrent ? 'rgb(var(--rgb-komga-secondary))' : 'rgb(var(--rgb-gray-500))'}
            />
          </ReactFlow>
        )}
      </div>
    </div>
  );
};

export default FranchiseGraphPage;
