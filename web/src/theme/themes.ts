export const THEME_STORAGE_KEY = 'manga_manager_theme';

export interface AppThemeDefinition {
  id: string;
  nameKey: string;
  descriptionKey: string;
  colorScheme: 'light' | 'dark';
  swatches: readonly string[];
}

export const DARK_THEMES = [
  {
    id: 'midnight',
    nameKey: 'theme.midnight.name',
    descriptionKey: 'theme.midnight.description',
    colorScheme: 'dark' as const,
    swatches: ['#8b5cf6', '#2dd4bf', '#181c27', '#d9e2f4'],
  },
  {
    id: 'forest',
    nameKey: 'theme.forest.name',
    descriptionKey: 'theme.forest.description',
    colorScheme: 'dark' as const,
    swatches: ['#4ade80', '#1e3a2b', '#16231d', '#d7e6d6'],
  },
  {
    id: 'amber',
    nameKey: 'theme.amber.name',
    descriptionKey: 'theme.amber.description',
    colorScheme: 'dark' as const,
    swatches: ['#f59e0b', '#3d2415', '#1b1410', '#f3e2c7'],
  },
  {
    id: 'graphite',
    nameKey: 'theme.graphite.name',
    descriptionKey: 'theme.graphite.description',
    colorScheme: 'dark' as const,
    swatches: ['#38bdf8', '#1a222d', '#111821', '#d9e0e8'],
  },
  {
    id: 'ocean',
    nameKey: 'theme.ocean.name',
    descriptionKey: 'theme.ocean.description',
    colorScheme: 'dark' as const,
    swatches: ['#22d3ee', '#15314d', '#0a1626', '#d8edf7'],
  },
  {
    id: 'plum',
    nameKey: 'theme.plum.name',
    descriptionKey: 'theme.plum.description',
    colorScheme: 'dark' as const,
    swatches: ['#c084fc', '#341b46', '#160d23', '#efe3ff'],
  },
  {
    id: 'rosewood',
    nameKey: 'theme.rosewood.name',
    descriptionKey: 'theme.rosewood.description',
    colorScheme: 'dark' as const,
    swatches: ['#fb7185', '#41202a', '#190d13', '#f7d9de'],
  },
] as const satisfies readonly AppThemeDefinition[];

export const LIGHT_THEMES = [
  {
    id: 'paper',
    nameKey: 'theme.paper.name',
    descriptionKey: 'theme.paper.description',
    colorScheme: 'light' as const,
    swatches: ['#d8b15d', '#f5efe3', '#53483c', '#93826f'],
  },
  {
    id: 'ivory',
    nameKey: 'theme.ivory.name',
    descriptionKey: 'theme.ivory.description',
    colorScheme: 'light' as const,
    swatches: ['#c38f58', '#fff8ee', '#5f4c3d', '#d2c0aa'],
  },
  {
    id: 'sage',
    nameKey: 'theme.sage.name',
    descriptionKey: 'theme.sage.description',
    colorScheme: 'light' as const,
    swatches: ['#6aa37d', '#eff5ef', '#435448', '#c0cec2'],
  },
  {
    id: 'sky',
    nameKey: 'theme.sky.name',
    descriptionKey: 'theme.sky.description',
    colorScheme: 'light' as const,
    swatches: ['#5ea8ff', '#f3f8ff', '#36506b', '#bfd7f2'],
  },
  {
    id: 'slate',
    nameKey: 'theme.slate.name',
    descriptionKey: 'theme.slate.description',
    colorScheme: 'light' as const,
    swatches: ['#64748b', '#f6f7f9', '#394150', '#c7ced8'],
  },
  {
    id: 'sunrise',
    nameKey: 'theme.sunrise.name',
    descriptionKey: 'theme.sunrise.description',
    colorScheme: 'light' as const,
    swatches: ['#f97316', '#fff4eb', '#6d4634', '#f0cab0'],
  },
  {
    id: 'sakura',
    nameKey: 'theme.sakura.name',
    descriptionKey: 'theme.sakura.description',
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
  if (theme.colorScheme === 'dark') {
    document.documentElement.classList.add('dark');
  } else {
    document.documentElement.classList.remove('dark');
  }
}

export function initializeTheme() {
  applyTheme(getStoredThemeId());
}
