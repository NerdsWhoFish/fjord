import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { getThemeChoice, initializeTheme, setThemeChoice } from "./theme";

let media: EventTarget & { matches: boolean };
let storage: Map<string, string>;
let browser: EventTarget;

beforeEach(() => {
  media = Object.assign(new EventTarget(), { matches: false });
  storage = new Map();
  browser = Object.assign(new EventTarget(), { matchMedia: () => media });
  vi.stubGlobal("window", browser);
  vi.stubGlobal("document", { documentElement: { dataset: {} } });
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("theme preference lifecycle", () => {
  it.each(["light", "dark", "dracula"])(
    "restores the saved %s preference",
    (theme) => {
      storage.set("fjord:theme", theme);
      expect(initializeTheme()).toBe(theme);
      expect(document.documentElement.dataset.theme).toBe(theme);
    },
  );

  it("follows OS changes until the reviewer chooses a theme", () => {
    expect(initializeTheme()).toBe("light");
    media.matches = true;
    media.dispatchEvent(new Event("change"));
    expect(document.documentElement.dataset.theme).toBe("dark");
    setThemeChoice("dracula");
    expect(storage.get("fjord:theme")).toBe("dracula");
    media.matches = false;
    media.dispatchEvent(new Event("change"));
    expect(document.documentElement.dataset.theme).toBe("dracula");
    setThemeChoice("system");
    expect(storage.has("fjord:theme")).toBe(false);
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("keeps explicit choices usable when browser storage is denied", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    });
    media.matches = true;
    expect(initializeTheme()).toBe("dark");
    setThemeChoice("light");
    media.dispatchEvent(new Event("change"));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(getThemeChoice()).toBe("light");
  });

  it("updates an open tab when another tab changes its saved choice", () => {
    initializeTheme();
    browser.dispatchEvent(
      Object.assign(new Event("storage"), {
        key: "fjord:theme",
        newValue: "dracula",
      }),
    );
    expect(document.documentElement.dataset.theme).toBe("dracula");
    media.matches = true;
    browser.dispatchEvent(
      Object.assign(new Event("storage"), {
        key: "fjord:theme",
        newValue: null,
      }),
    );
    expect(getThemeChoice()).toBe("system");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("ignores obsolete or invalid stored values and uses the OS preference", () => {
    storage.set("fjord:theme", "unknown-theme");
    media.matches = true;
    expect(initializeTheme()).toBe("dark");
    expect(getThemeChoice()).toBe("system");
  });
});

function luminance(hex: string) {
  const channels = hex
    .slice(1)
    .match(/../g)!
    .map((value) => {
      const color = parseInt(value, 16) / 255;
      return color <= 0.04045
        ? color / 12.92
        : ((color + 0.055) / 1.055) ** 2.4;
    });
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
}
function contrast(a: string, b: string) {
  const values = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (values[0] + 0.05) / (values[1] + 0.05);
}

describe("theme readability", () => {
  const css = readFileSync(new URL("./theme.css", import.meta.url), "utf8");
  const blocks = [
    ...css.matchAll(
      /:root\[data-theme="(light|dark|dracula)"\]\s*\{([^}]+)\}/g,
    ),
  ];
  for (const [, theme, body] of blocks) {
    const colors = Object.fromEntries(
      [...body.matchAll(/--([\w-]+):\s*(#[\da-f]{6});/g)].map((match) => [
        match[1],
        match[2],
      ]),
    );
    it(`${theme} keeps text and syntax legible on source and diff backgrounds`, () => {
      for (const background of [
        "white",
        "diff-added",
        "diff-deleted",
        "target-background",
        "code-hover",
      ]) {
        for (const foreground of [
          "ink",
          "line-number",
          ...Object.keys(colors).filter((name) => name.startsWith("syntax-")),
        ]) {
          expect(
            contrast(colors[foreground], colors[background]),
            `${foreground} on ${background}`,
          ).toBeGreaterThanOrEqual(4.5);
        }
      }
      for (const [foreground, background] of [
        ["primary-text", "primary-background"],
        ["tree-text", "tree-background"],
        ["tree-muted", "tree-background"],
        ["tree-selected-text", "tree-selected"],
        ["warning-text", "warning-background"],
      ]) {
        expect(
          contrast(colors[foreground], colors[background]),
          `${foreground} on ${background}`,
        ).toBeGreaterThanOrEqual(4.5);
      }
    });
  }
});
