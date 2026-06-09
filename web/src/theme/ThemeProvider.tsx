/**
 * 业务说明：本文件是业务实现，属于前端主题系统，负责集中定义颜色、间距、阴影和明暗模式下的视觉变量。
 * 它影响资料库、阅读器、关系图谱和设置页的整体观感与主题跟随能力。
 * 维护时应关注 CSS 变量命名、深浅色对比度、组件覆盖范围和持久化偏好。
 */

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { APP_THEMES, DARK_THEMES, LIGHT_THEMES, THEME_STORAGE_KEY, applyTheme, getStoredThemeId, getThemeById, type AppTheme, type AppThemeId } from './themes';

interface ThemeContextValue {
  themeId: AppThemeId;
  resolvedTheme: AppTheme;
  availableThemes: readonly AppTheme[];
  lightThemes: readonly AppTheme[];
  darkThemes: readonly AppTheme[];
  setTheme: (themeId: AppThemeId | string) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [themeId, setThemeId] = useState<AppThemeId>(() => getStoredThemeId());

  useEffect(() => {
    applyTheme(themeId);
    window.localStorage.setItem(THEME_STORAGE_KEY, themeId);
  }, [themeId]);

  const setTheme = useCallback((nextThemeId: AppThemeId | string) => {
    setThemeId(getThemeById(nextThemeId).id);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({
      themeId,
      resolvedTheme: getThemeById(themeId),
      availableThemes: APP_THEMES,
      lightThemes: LIGHT_THEMES,
      darkThemes: DARK_THEMES,
      setTheme,
    }),
    [themeId, setTheme],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within ThemeProvider');
  }
  return context;
}
