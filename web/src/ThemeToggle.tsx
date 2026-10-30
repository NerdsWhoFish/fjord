import { useState } from "react";

type Theme = "light" | "dark";

export function initializeTheme(): Theme {
  let theme: Theme = matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
  try {
    const saved = localStorage.getItem("fjord:theme");
    if (saved === "light" || saved === "dark") theme = saved;
  } catch {
    // Storage can be disabled; the toggle still works for this page.
  }
  document.documentElement.dataset.theme = theme;
  return theme;
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() =>
    document.documentElement.dataset.theme === "dark" ? "dark" : "light",
  );
  return (
    <button
      type="button"
      className="theme-toggle"
      aria-label="Dark mode"
      aria-pressed={theme === "dark"}
      onClick={() => {
        const next = theme === "dark" ? "light" : "dark";
        document.documentElement.dataset.theme = next;
        setTheme(next);
        try {
          localStorage.setItem("fjord:theme", next);
        } catch {
          // Preserve the choice in memory when storage is unavailable.
        }
      }}
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden="true"
      >
        <path
          d="M20.5 14A8.5 8.5 0 0 1 10 3.5 8.5 8.5 0 1 0 20.5 14Z"
          stroke="currentColor"
          strokeWidth="1.7"
          strokeLinejoin="round"
        />
      </svg>
      Dark mode{" "}
      <span aria-hidden="true">{theme === "dark" ? "on" : "off"}</span>
    </button>
  );
}
