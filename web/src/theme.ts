export type Theme = "light" | "dark" | "dracula" | "black";
export type ThemeChoice = Theme | "system";
export type CodeStyle = "default" | "github" | "one" | "mono";

export const themeOptions = [
  ["system", "System"],
  ["light", "Light"],
  ["dark", "Dark"],
  ["dracula", "Dracula"],
  ["black", "True Black"],
] as const;
export const codeStyleOptions = [
  ["default", "Theme default"],
  ["github", "GitHub"],
  ["one", "Atom One"],
  ["mono", "Monochrome"],
] as const;

const storageKey = "fjord:theme";
const codeStyleStorageKey = "fjord:code-style";
const listeners = new Set<() => void>();
let choice: ThemeChoice = "system";
let codeStyle: CodeStyle = "default";
let media: MediaQueryList | undefined;
let removeListeners: (() => void) | undefined;

export function parseThemeChoice(value: string | null): ThemeChoice {
  return value === "light" ||
    value === "dark" ||
    value === "dracula" ||
    value === "black"
    ? value
    : "system";
}

export function parseCodeStyle(value: string | null): CodeStyle {
  if (value === "vivid") return "github";
  if (value === "warm") return "one";
  return value === "github" || value === "one" || value === "mono"
    ? value
    : "default";
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
  document.documentElement.dataset.codeStyle = codeStyle;
  listeners.forEach((listener) => listener());
  return theme;
}

export function initializeTheme(): Theme {
  removeListeners?.();
  media = window.matchMedia?.("(prefers-color-scheme: dark)");
  choice = "system";
  codeStyle = "default";
  try {
    choice = parseThemeChoice(localStorage.getItem(storageKey));
    codeStyle = parseCodeStyle(localStorage.getItem(codeStyleStorageKey));
  } catch {
    // Private browsing can deny storage; OS preference still works.
  }
  const systemChanged = () => {
    if (choice === "system") applyTheme();
  };
  const storageChanged = (event: StorageEvent) => {
    if (event.key === storageKey || event.key === null) {
      choice = parseThemeChoice(event.newValue);
    }
    if (event.key === codeStyleStorageKey || event.key === null) {
      codeStyle = parseCodeStyle(event.newValue);
    }
    if (
      event.key === storageKey ||
      event.key === codeStyleStorageKey ||
      event.key === null
    ) {
      applyTheme();
    }
  };
  const currentMedia = media;
  if (currentMedia?.addEventListener) {
    currentMedia.addEventListener("change", systemChanged);
  } else {
    currentMedia?.addListener(systemChanged);
  }
  window.addEventListener("storage", storageChanged);
  removeListeners = () => {
    if (currentMedia?.removeEventListener) {
      currentMedia.removeEventListener("change", systemChanged);
    } else {
      currentMedia?.removeListener(systemChanged);
    }
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
export function setCodeStyle(next: CodeStyle) {
  codeStyle = parseCodeStyle(next);
  try {
    if (codeStyle === "default") localStorage.removeItem(codeStyleStorageKey);
    else localStorage.setItem(codeStyleStorageKey, codeStyle);
  } catch {
    // Keep the current page usable when storage is unavailable.
  }
  applyTheme();
}
export function getCodeStyle() {
  return codeStyle;
}
export function subscribeTheme(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
