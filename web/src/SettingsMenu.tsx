import { useEffect, useRef } from "react";
import { ThemeToggle } from "./ThemeToggle";
import "./settings-menu.css";

export function SettingsMenu() {
  const menu = useRef<HTMLDetailsElement>(null);
  const trigger = useRef<HTMLElement>(null);
  useEffect(() => {
    const dismiss = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        menu.current &&
        !menu.current.contains(event.target)
      )
        menu.current.open = false;
    };
    document.addEventListener("pointerdown", dismiss);
    return () => document.removeEventListener("pointerdown", dismiss);
  }, []);
  return (
    <details
      className="settings-menu"
      ref={menu}
      onKeyDown={(event) => {
        if (event.key === "Escape" && menu.current?.open) {
          event.preventDefault();
          menu.current.open = false;
          trigger.current?.focus();
        }
      }}
      onBlur={(event) => {
        if (
          event.relatedTarget instanceof Node &&
          !event.currentTarget.contains(event.relatedTarget)
        )
          event.currentTarget.open = false;
      }}
    >
      <summary
        className="button settings-trigger"
        ref={trigger}
        aria-label="Settings menu"
        title="Settings"
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 20 20"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          aria-hidden="true"
        >
          <path d="M3 5h14M3 10h14M3 15h14" />
        </svg>
        <span>Settings</span>
      </summary>
      <div
        className="settings-panel"
        role="group"
        aria-label="Appearance settings"
      >
        <span className="eyebrow">APPEARANCE</span>
        <ThemeToggle />
      </div>
    </details>
  );
}
