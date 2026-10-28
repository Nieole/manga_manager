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
