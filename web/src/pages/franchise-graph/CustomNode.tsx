/**
 * 业务说明：本文件是业务实现，属于前端漫画系列关系图谱，负责把续作、前传、衍生、改编等关系渲染为可探索的节点网络。
 * 它依赖后端系列关系数据、封面缩略图和主题变量，帮助用户理解作品之间的方向性关联。
 * 维护时应关注力导布局稳定性、连线箭头方向、主题背景、节点可读性和大图交互性能。
 */

import React from 'react';
import { Handle, Position } from '@xyflow/react';

interface CustomNodeProps {
  data: {
    name: string;
    coverPath: string;
    isCurrent: boolean;
    degree?: number;
  };
}

export const CustomNode: React.FC<CustomNodeProps> = ({ data }) => {
  const nodeSize = data.isCurrent ? 74 : Math.min(70, 52 + (data.degree ?? 0) * 4);

  return (
    <div
      className={`group relative flex w-28 cursor-pointer flex-col items-center gap-2 transition-all duration-200 hover:-translate-y-1 ${
        data.isCurrent ? 'z-10' : ''
      }`}
    >
      <Handle
        type="target"
        position={Position.Top}
        className="!left-1/2 !top-1/2 !h-2 !w-2 !-translate-x-1/2 !-translate-y-1/2 !border-0 !bg-transparent !opacity-0"
      />
      <div
        className={`relative overflow-hidden rounded-full bg-komgaSurface shadow-[0_12px_40px_rgb(var(--theme-shadow)/0.28)] ring-1 ring-white/15 transition-all group-hover:ring-komgaSecondary/70 ${
          data.isCurrent ? 'ring-2 ring-komgaSecondary shadow-[0_0_32px_rgb(var(--rgb-komga-secondary)/0.22)]' : ''
        }`}
        style={{ width: nodeSize, height: nodeSize }}
      >
        <span className="pointer-events-none absolute inset-0 rounded-full bg-linear-to-br from-white/18 via-transparent to-black/25" />
        {data.coverPath ? (
          <img
            src={`/api/thumbnails/${data.coverPath}`}
            alt={data.name}
            className="h-full w-full object-cover"
            loading="lazy"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-komgaSurface text-gray-500">
            <span className="text-[10px] font-semibold uppercase">No Cover</span>
          </div>
        )}
      </div>
      <div className="flex max-w-28 flex-col items-center text-center">
        <span
          className={`line-clamp-2 rounded-full border px-2 py-1 text-[11px] font-semibold leading-tight shadow-lg backdrop-blur-md transition-colors ${
            data.isCurrent
              ? 'border-komgaSecondary/50 bg-komgaSecondary/12 text-komgaSecondary'
              : 'border-white/10 bg-komgaBackground/70 text-gray-200 group-hover:border-komgaSecondary/35 group-hover:text-white'
          }`}
          title={data.name}
        >
          {data.name}
        </span>
      </div>
      <Handle
        type="source"
        position={Position.Bottom}
        className="!left-1/2 !top-1/2 !h-2 !w-2 !-translate-x-1/2 !-translate-y-1/2 !border-0 !bg-transparent !opacity-0"
      />
    </div>
  );
};
