import React from 'react';
import { Handle, Position } from '@xyflow/react';

interface CustomNodeProps {
  data: {
    name: string;
    coverPath: string;
    isCurrent: boolean;
  };
}

export const CustomNode: React.FC<CustomNodeProps> = ({ data }) => {
  return (
    <div
      className={`relative flex w-36 flex-col gap-2 rounded-xl p-2 bg-gray-950/80 shadow-lg backdrop-blur-md transition-all ${
        data.isCurrent ? 'ring-2 ring-komgaPrimary bg-komgaPrimary/10' : 'border border-white/10 hover:border-white/30'
      }`}
    >
      <Handle type="target" position={Position.Top} className="!bg-gray-500 !w-3 !h-3 !border-gray-900" />
      <div className="aspect-[2/3] w-full overflow-hidden rounded-lg bg-gray-900 shadow-sm">
        {data.coverPath ? (
          <img
            src={`/api/thumbnails/${data.coverPath}`}
            alt={data.name}
            className="h-full w-full object-cover"
            loading="lazy"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-gray-700">
            <span className="text-xs">No Cover</span>
          </div>
        )}
      </div>
      <div className="flex flex-col text-center">
        <span
          className={`line-clamp-2 text-xs font-medium leading-tight ${
            data.isCurrent ? 'text-komgaPrimary' : 'text-gray-200'
          }`}
          title={data.name}
        >
          {data.name}
        </span>
      </div>
      <Handle type="source" position={Position.Bottom} className="!bg-komgaPrimary !w-3 !h-3 !border-gray-900" />
    </div>
  );
};
