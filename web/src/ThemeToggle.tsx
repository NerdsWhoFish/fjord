import { useSyncExternalStore } from "react";
import { getThemeChoice, setThemeChoice, subscribeTheme } from "./theme";
import type { ThemeChoice } from "./theme";

export { initializeTheme } from "./theme";

export function ThemeToggle() {
  const choice = useSyncExternalStore(
    subscribeTheme,
    getThemeChoice,
    () => "system",
  );
  return (
    <label className="theme-toggle">
      <span>Theme</span>
      <select
        aria-label="Color theme"
        value={choice}
        onChange={(event) => setThemeChoice(event.target.value as ThemeChoice)}
      >
        <option value="system">System</option>
        <option value="light">Light</option>
        <option value="dark">Dark</option>
        <option value="dracula">Dracula</option>
      </select>
    </label>
  );
}
