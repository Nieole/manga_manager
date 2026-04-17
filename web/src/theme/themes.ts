export const THEME_STORAGE_KEY = 'manga_manager_theme';

export const APP_THEMES = [
  {
    id: 'midnight',
    name: 'Midnight',
    description: '默认深色主题，冷调霓虹感，延续当前系统气质。',
    colorScheme: 'dark' as const,
    swatches: ['#8b5cf6', '#2dd4bf', '#181c27', '#d9e2f4'],
  },
  {
    id: 'paper',
    name: 'Paper',
    description: '浅色纸张风格，适合长时间管理与浏览。',
    colorScheme: 'light' as const,
    swatches: ['#d8b15d', '#f5efe3', '#53483c', '#93826f'],
  },
  {
    id: 'forest',
    name: 'Forest',
    description: '墨绿夜色主题，低饱和、长时间使用更稳定。',
    colorScheme: 'dark' as const,
    swatches: ['#4ade80', '#1e3a2b', '#16231d', '#d7e6d6'],
  },
  {
    id: 'amber',
    name: 'Amber',
    description: '暖色书房主题，强调胶片感与木质氛围。',
    colorScheme: 'dark' as const,
    swatches: ['#f59e0b', '#3d2415', '#1b1410', '#f3e2c7'],
  },
  {
    id: 'graphite',
    name: 'Graphite',
    description: '中性工业风主题，偏专业工具与运维面板气质。',
    colorScheme: 'dark' as const,
    swatches: ['#38bdf8', '#1a222d', '#111821', '#d9e0e8'],
  },
] as const;

export type AppTheme = (typeof APP_THEMES)[number];
export type AppThemeId = AppTheme['id'];

export const DEFAULT_THEME_ID: AppThemeId = 'midnight';

export function getThemeById(themeId?: string | null): AppTheme {
  return APP_THEMES.find((theme) => theme.id === themeId) ?? APP_THEMES[0];
}

export function getStoredThemeId(): AppThemeId {
  if (typeof window === 'undefined') {
    return DEFAULT_THEME_ID;
  }

  const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
  return getThemeById(stored).id;
}

export function applyTheme(themeId: string) {
  if (typeof document === 'undefined') {
    return;
  }

  const theme = getThemeById(themeId);
  document.documentElement.dataset.theme = theme.id;
  document.documentElement.style.colorScheme = theme.colorScheme;
}

export function initializeTheme() {
  applyTheme(getStoredThemeId());
}
