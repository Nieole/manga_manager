/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

export interface Library {
  id: string;
  name: string;
  path: string;
  scan_mode?: string;
  koreader_sync_enabled?: boolean;
  scan_interval?: number;
  scan_formats?: string;
}

export interface SearchHit {
  id: string;
  score?: number;
  fields?: {
    id?: string;
    title?: string;
    series_name?: string;
    type?: string;
    cover_path?: string;
  };
}

export interface BrowseDirEntry {
  name: string;
  path: string;
}

export interface BrowseDrive {
  name: string;
  path: string;
}
