import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { APP_THEMES, THEME_STORAGE_KEY, applyTheme, getStoredThemeId, getThemeById, type AppTheme, type AppThemeId } from './themes';

interface ThemeContextValue {
  themeId: AppThemeId;
  resolvedTheme: AppTheme;
  availableThemes: readonly AppTheme[];
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
