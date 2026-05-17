import { get, writable } from 'svelte/store';

export type ThemeMode = 'system' | 'light' | 'dark';

const THEME_STORAGE_KEY = 'turnstile-appcheck-gateway.theme';

export const supportedThemeModes = Object.freeze(['system', 'light', 'dark'] as const);
export const themePreference = writable<ThemeMode>('system');
export const resolvedTheme = writable<'light' | 'dark'>('light');

let cleanupMediaQuery = () => {};

function normalizeThemePreference(value: string | null): ThemeMode {
  return supportedThemeModes.includes(value as ThemeMode) ? (value as ThemeMode) : 'system';
}

function resolveTheme(preference: ThemeMode): 'light' | 'dark' {
  if (preference === 'system') {
    if (typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches) return 'dark';
    return 'light';
  }
  return preference;
}

function applyResolvedTheme(theme: 'light' | 'dark') {
  if (typeof document === 'undefined') return;

  const root = document.documentElement;
  root.classList.toggle('dark', theme === 'dark');
  root.style.colorScheme = theme;
  resolvedTheme.set(theme);
}

function syncTheme() {
  applyResolvedTheme(resolveTheme(get(themePreference)));
}

function readStoredThemePreference(): ThemeMode {
  if (typeof window === 'undefined') return 'system';
  return normalizeThemePreference(window.localStorage.getItem(THEME_STORAGE_KEY));
}

function attachMediaQueryListener() {
  if (typeof window === 'undefined') return () => {};

  const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
  const handleChange = () => {
    if (get(themePreference) === 'system') syncTheme();
  };

  if (typeof mediaQuery.addEventListener === 'function') {
    mediaQuery.addEventListener('change', handleChange);
    return () => mediaQuery.removeEventListener('change', handleChange);
  }

  mediaQuery.addListener(handleChange);
  return () => mediaQuery.removeListener(handleChange);
}

export function setupTheme() {
  themePreference.set(readStoredThemePreference());
  cleanupMediaQuery();
  cleanupMediaQuery = attachMediaQueryListener();
  syncTheme();
  return cleanupTheme;
}

export function cleanupTheme() {
  cleanupMediaQuery();
  cleanupMediaQuery = () => {};
}

export function setThemeMode(nextTheme: ThemeMode) {
  const normalized = normalizeThemePreference(nextTheme);
  themePreference.set(normalized);
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(THEME_STORAGE_KEY, normalized);
  }
  syncTheme();
}
