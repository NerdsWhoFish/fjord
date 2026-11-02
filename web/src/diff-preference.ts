import { useEffect, useState } from "react";

export type DiffMode = "unified" | "split";
const key = "fjord:diff-mode";

export function parseDiffMode(value: string | null): DiffMode {
  return value === "split" ? "split" : "unified";
}

export function useDiffMode() {
  const [mode, setMode] = useState<DiffMode>(() => {
    try {
      return parseDiffMode(localStorage.getItem(key));
    } catch {
      return "unified";
    }
  });
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === key || event.key === null)
        setMode(parseDiffMode(event.newValue));
    };
    window.addEventListener("storage", changed);
    return () => window.removeEventListener("storage", changed);
  }, []);
  function update(next: DiffMode) {
    setMode(next);
    try {
      localStorage.setItem(key, next);
    } catch {
      // The current page still honors the preference when storage is unavailable.
    }
  }
  return [mode, update] as const;
}
