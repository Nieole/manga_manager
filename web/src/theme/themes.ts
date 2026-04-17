export const THEME_STORAGE_KEY = 'manga_manager_theme';

export interface AppThemeDefinition {
  id: string;
  name: string;
  description: string;
  colorScheme: 'light' | 'dark';
  swatches: readonly string[];
}

export const DARK_THEMES = [
  {
    id: 'midnight',
    name: 'Midnight',
    description: '默认深色主题，冷调霓虹感，延续当前系统气质。',
    colorScheme: 'dark' as const,
    swatches: ['#8b5cf6', '#2dd4bf', '#181c27', '#d9e2f4'],
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
  {
    id: 'ocean',
    name: 'Ocean',
    description: '深海蓝调主题，强调清晰对比和冷色层次。',
    colorScheme: 'dark' as const,
    swatches: ['#22d3ee', '#15314d', '#0a1626', '#d8edf7'],
  },
  {
    id: 'plum',
    name: 'Plum',
    description: '深李紫主题，偏创作工具感和低亮度氛围。',
    colorScheme: 'dark' as const,
    swatches: ['#c084fc', '#341b46', '#160d23', '#efe3ff'],
  },
  {
    id: 'rosewood',
    name: 'Rosewood',
    description: '酒红木质主题，复古但保持管理界面的清晰度。',
    colorScheme: 'dark' as const,
    swatches: ['#fb7185', '#41202a', '#190d13', '#f7d9de'],
  },
] as const satisfies readonly AppThemeDefinition[];

export const LIGHT_THEMES = [
  {
    id: 'paper',
    name: 'Paper',
    description: '浅色纸张风格，适合长时间管理与浏览。',
    colorScheme: 'light' as const,
    swatches: ['#d8b15d', '#f5efe3', '#53483c', '#93826f'],
  },
  {
    id: 'ivory',
    name: 'Ivory',
    description: '暖白书页主题，层次柔和，适合阅读型操作。',
    colorScheme: 'light' as const,
    swatches: ['#c38f58', '#fff8ee', '#5f4c3d', '#d2c0aa'],
  },
  {
    id: 'sage',
    name: 'Sage',
    description: '浅灰绿主题，办公纸感更强，适合长时间整理书库。',
    colorScheme: 'light' as const,
    swatches: ['#6aa37d', '#eff5ef', '#435448', '#c0cec2'],
  },
  {
    id: 'sky',
    name: 'Sky',
    description: '冷白浅蓝主题，信息密集时也能保持轻盈。',
    colorScheme: 'light' as const,
    swatches: ['#5ea8ff', '#f3f8ff', '#36506b', '#bfd7f2'],
  },
  {
    id: 'slate',
    name: 'Slate',
    description: '中性浅灰主题，偏专业工具面板风格。',
    colorScheme: 'light' as const,
    swatches: ['#64748b', '#f6f7f9', '#394150', '#c7ced8'],
  },
  {
    id: 'sunrise',
    name: 'Sunrise',
    description: '暖光米橙主题，界面更有晨光和纸面温度。',
    colorScheme: 'light' as const,
    swatches: ['#f97316', '#fff4eb', '#6d4634', '#f0cab0'],
  },
  {
    id: 'sakura',
    name: 'Sakura',
    description: '粉白柔和主题，适合轻量浏览和视觉减压。',
    colorScheme: 'light' as const,
    swatches: ['#ec4899', '#fff2f8', '#6f4258', '#f3c7dc'],
  },
] as const satisfies readonly AppThemeDefinition[];

export const APP_THEMES = [...DARK_THEMES, ...LIGHT_THEMES] as const;

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
