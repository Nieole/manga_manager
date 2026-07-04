/**
 * 业务说明：本文件是应用外壳的目录浏览 hook，为「新增/编辑资料库」弹窗的服务器端目录选择器
 * 提供当前目录、父目录、子目录列表与驱动器列表状态，以及打开/导航目录的请求逻辑。
 * 维护时应保持与 /api/browse-dirs 响应形状一致。
 */

import { useState } from 'react';
import { apiClient } from '../../api/client';
import type { BrowseDirEntry, BrowseDrive } from './types';

interface BrowseResponse {
  dirs?: BrowseDirEntry[];
  current: string;
  parent: string;
  drives?: BrowseDrive[];
}

export function useDirectoryBrowser() {
  const [browsing, setBrowsing] = useState(false);
  const [browseDirs, setBrowseDirs] = useState<BrowseDirEntry[]>([]);
  const [browseCurrent, setBrowseCurrent] = useState('');
  const [browseParent, setBrowseParent] = useState('');
  const [browseDrives, setBrowseDrives] = useState<BrowseDrive[]>([]);

  const applyBrowseResponse = (data: BrowseResponse) => {
    setBrowseDirs(data.dirs || []);
    setBrowseCurrent(data.current);
    setBrowseParent(data.parent);
    setBrowseDrives(data.drives || []);
  };

  const openDirectoryBrowser = () => {
    setBrowsing(true);
    apiClient.get<BrowseResponse>('/api/browse-dirs')
      .then((res) => applyBrowseResponse(res.data))
      .catch(() => { });
  };

  const navigateDirectoryBrowser = (path: string) => {
    apiClient.get<BrowseResponse>(`/api/browse-dirs?path=${encodeURIComponent(path)}`)
      .then((res) => applyBrowseResponse(res.data));
  };

  return {
    browsing,
    setBrowsing,
    browseDirs,
    browseCurrent,
    browseParent,
    browseDrives,
    openDirectoryBrowser,
    navigateDirectoryBrowser,
  };
}
