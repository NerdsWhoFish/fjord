import { useSyncExternalStore } from "react";
import {
  codeStyleOptions,
  getCodeStyle,
  getThemeChoice,
  setCodeStyle,
  setThemeChoice,
  subscribeTheme,
  themeOptions,
} from "./theme";

export { initializeTheme } from "./theme";

export function ThemeToggle() {
  const choice = useSyncExternalStore(
    subscribeTheme,
    getThemeChoice,
    () => "system",
  );
  const codeStyle = useSyncExternalStore(
    subscribeTheme,
    getCodeStyle,
    () => "default",
  );
  return (
    <>
      <div className="theme-toggle">
        <span>Theme</span>
        <div className="theme-options" role="group" aria-label="Color theme">
          {themeOptions.map(([theme, label]) => (
            <button
              key={theme}
              type="button"
              aria-pressed={choice === theme}
              onClick={() => setThemeChoice(theme)}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <div className="theme-toggle">
        <span>Code colors</span>
        <div className="theme-options" role="group" aria-label="Code colors">
          {codeStyleOptions.map(([style, label]) => (
            <button
              key={style}
              type="button"
              aria-pressed={codeStyle === style}
              onClick={() => setCodeStyle(style)}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
    </>
  );
}
