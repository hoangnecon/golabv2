import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

const SIDEBAR_STORAGE_KEY = 'golab.sidebar';
const COLOR_SCHEME_KEY = 'color-scheme';
const DESKTOP_QUERY = '(min-width: 768px)';
const DARK_QUERY = '(prefers-color-scheme: dark)';

export type EffectiveTheme = 'light' | 'dark';

interface ShellApi {
  sidebarOpen: boolean;
  toggleSidebar: () => void;
  dismissOnCompact: () => void;
  theme: EffectiveTheme;
  toggleTheme: () => void;
}

const ShellContext = createContext<ShellApi | null>(null);

function initialSidebarOpen(): boolean {
  if (!window.matchMedia(DESKTOP_QUERY).matches) return false;
  return window.localStorage.getItem(SIDEBAR_STORAGE_KEY) !== 'closed';
}

function resolveEffectiveTheme(): EffectiveTheme {
  const stored = window.localStorage.getItem(COLOR_SCHEME_KEY);
  if (stored === 'dark' || stored === 'light') return stored;
  return window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light';
}

function syncColorSchemeMeta(nextContent: 'light' | 'dark' | 'light dark') {
  const meta = document.querySelector('meta[name="color-scheme"]');
  if (meta) meta.setAttribute('content', nextContent);
}

export function ShellProvider({ children }: { children: ReactNode }) {
  const [sidebarOpen, setSidebarOpen] = useState(initialSidebarOpen);
  const [theme, setTheme] = useState<EffectiveTheme>(resolveEffectiveTheme);

  const toggleSidebar = useCallback(() => {
    setSidebarOpen((open) => {
      window.localStorage.setItem(SIDEBAR_STORAGE_KEY, open ? 'closed' : 'open');
      return !open;
    });
  }, []);

  const dismissOnCompact = useCallback(() => {
    if (!window.matchMedia(DESKTOP_QUERY).matches) setSidebarOpen(false);
  }, []);

  const toggleTheme = useCallback(() => {
    setTheme((current) => {
      const next: EffectiveTheme = current === 'dark' ? 'light' : 'dark';
      const osDefault: EffectiveTheme = window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light';
      if (next === osDefault) {
        window.localStorage.removeItem(COLOR_SCHEME_KEY);
        syncColorSchemeMeta('light dark');
      } else {
        window.localStorage.setItem(COLOR_SCHEME_KEY, next);
        syncColorSchemeMeta(next);
      }
      return next;
    });
  }, []);

  useEffect(() => {
    const mql = window.matchMedia(DARK_QUERY);
    const onChange = () => {
      const stored = window.localStorage.getItem(COLOR_SCHEME_KEY);
      if (stored !== 'light' && stored !== 'dark') {
        setTheme(mql.matches ? 'dark' : 'light');
      }
    };
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'b') {
        event.preventDefault();
        toggleSidebar();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [toggleSidebar]);

  const value = useMemo(
    () => ({ sidebarOpen, toggleSidebar, dismissOnCompact, theme, toggleTheme }),
    [sidebarOpen, toggleSidebar, dismissOnCompact, theme, toggleTheme],
  );

  return <ShellContext.Provider value={value}>{children}</ShellContext.Provider>;
}

export function useShell(): ShellApi {
  const context = useContext(ShellContext);
  if (!context) throw new Error('useShell must be used inside ShellProvider');
  return context;
}
