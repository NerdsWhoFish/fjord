export type Theme = "light" | "dark" | "dracula";
export type ThemeChoice = Theme | "system";

const storageKey = "fjord:theme";
const listeners = new Set<() => void>();
let choice: ThemeChoice = "system";
let media: MediaQueryList | undefined;
let removeListeners: (() => void) | undefined;

export function parseThemeChoice(value: string | null): ThemeChoice {
  return value === "light" || value === "dark" || value === "dracula"
    ? value
    : "system";
}

export function resolvedTheme(
  selected: ThemeChoice,
  systemDark: boolean,
): Theme {
  return selected === "system" ? (systemDark ? "dark" : "light") : selected;
}

function applyTheme(): Theme {
  const theme = resolvedTheme(choice, media?.matches ?? false);
  document.documentElement.dataset.theme = theme;
  listeners.forEach((listener) => listener());
  return theme;
}

export function initializeTheme(): Theme {
  removeListeners?.();
  media = window.matchMedia("(prefers-color-scheme: dark)");
  choice = "system";
  try {
    choice = parseThemeChoice(localStorage.getItem(storageKey));
  } catch {
    // Private browsing can deny storage; OS preference still works.
  }
  const systemChanged = () => {
    if (choice === "system") applyTheme();
  };
  const storageChanged = (event: StorageEvent) => {
    if (event.key === storageKey || event.key === null) {
      choice = parseThemeChoice(event.newValue);
      applyTheme();
    }
  };
  const currentMedia = media;
  currentMedia.addEventListener("change", systemChanged);
  window.addEventListener("storage", storageChanged);
  removeListeners = () => {
    currentMedia.removeEventListener("change", systemChanged);
    window.removeEventListener("storage", storageChanged);
  };
  return applyTheme();
}

export function setThemeChoice(next: ThemeChoice) {
  choice = parseThemeChoice(next);
  try {
    if (choice === "system") localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, choice);
  } catch {
    // Keep the explicit choice for this page even when it cannot be persisted.
  }
  applyTheme();
}

export function getThemeChoice() {
  return choice;
}
export function subscribeTheme(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
