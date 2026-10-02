import { useEffect, useState } from 'react';

export type Theme = 'system' | 'light' | 'dark';

const STORAGE_KEY = 'affinity.theme';

function readStored(): Theme {
  const value = localStorage.getItem(STORAGE_KEY);
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system';
}

/**
 * Persists the theme preference and keeps the `.dark` class in sync with either
 * the explicit choice or the operating system.
 */
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(readStored);

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const apply = () => {
      const effective = theme === 'system' ? (media.matches ? 'dark' : 'light') : theme;
      document.documentElement.classList.toggle('dark', effective === 'dark');
      document.documentElement.dataset.theme = effective;
    };
    apply();
    localStorage.setItem(STORAGE_KEY, theme);
    if (theme !== 'system') return;
    media.addEventListener('change', apply);
    return () => media.removeEventListener('change', apply);
  }, [theme]);

  return [theme, setTheme] as const;
}
