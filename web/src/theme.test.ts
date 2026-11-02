import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import {
  getCodeStyle,
  getThemeChoice,
  initializeTheme,
  parseCodeStyle,
  setCodeStyle,
  setThemeChoice,
} from "./theme";

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
  it("migrates the replaced custom palette preferences", () => {
    expect(parseCodeStyle("vivid")).toBe("github");
    expect(parseCodeStyle("warm")).toBe("one");
  });

  it("keeps manual themes usable without system-theme detection", () => {
    vi.stubGlobal("window", new EventTarget());
    storage.set("fjord:theme", "dracula");
    expect(initializeTheme()).toBe("dracula");
    setThemeChoice("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    setThemeChoice("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    setThemeChoice("system");
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("follows system changes with legacy media-query listeners", () => {
    const legacyMedia = {
      matches: false,
      addListener: vi.fn(),
      removeListener: vi.fn(),
    };
    Object.assign(browser, { matchMedia: () => legacyMedia });
    expect(initializeTheme()).toBe("light");
    const changed = legacyMedia.addListener.mock.calls[0][0];
    legacyMedia.matches = true;
    changed();
    expect(document.documentElement.dataset.theme).toBe("dark");
    initializeTheme();
    expect(legacyMedia.removeListener).toHaveBeenCalledWith(changed);
  });

  it.each(["light", "dark", "dracula", "black"])(
    "restores the saved %s preference",
    (theme) => {
      storage.set("fjord:theme", theme);
      expect(initializeTheme()).toBe(theme);
      expect(document.documentElement.dataset.theme).toBe(theme);
    },
  );

  it.each(["github", "one", "mono"] as const)(
    "restores %s code colors independently of the theme",
    (style) => {
      storage.set("fjord:theme", "black");
      storage.set("fjord:code-style", style);
      expect(initializeTheme()).toBe("black");
      expect(getCodeStyle()).toBe(style);
      expect(document.documentElement.dataset.codeStyle).toBe(style);
      setThemeChoice("light");
      expect(document.documentElement.dataset.codeStyle).toBe(style);
      setCodeStyle("default");
      expect(storage.has("fjord:code-style")).toBe(false);
      expect(document.documentElement.dataset.codeStyle).toBe("default");
      expect(getThemeChoice()).toBe("light");
    },
  );

  it("syncs code colors across tabs and resets both preferences on storage clear", () => {
    initializeTheme();
    setThemeChoice("black");
    browser.dispatchEvent(
      Object.assign(new Event("storage"), {
        key: "fjord:code-style",
        newValue: "one",
      }),
    );
    expect(getCodeStyle()).toBe("one");
    expect(getThemeChoice()).toBe("black");
    browser.dispatchEvent(
      Object.assign(new Event("storage"), {
        key: null,
        newValue: null,
      }),
    );
    expect(getThemeChoice()).toBe("system");
    expect(getCodeStyle()).toBe("default");
  });

  it("falls back for invalid code colors and allows changes without storage", () => {
    storage.set("fjord:code-style", "unknown");
    initializeTheme();
    expect(getCodeStyle()).toBe("default");
    vi.stubGlobal("localStorage", {
      setItem: () => {
        throw new Error("denied");
      },
    });
    setCodeStyle("github");
    expect(document.documentElement.dataset.codeStyle).toBe("github");
  });

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
      /:root\[data-theme="(light|dark|dracula|black)"\]\s*\{([^}]+)\}/g,
    ),
  ];
  for (const [, theme, body] of blocks) {
    const colors = Object.fromEntries(
      [...body.matchAll(/--([\w-]+):\s*(#[\da-f]{6});/g)].map((match) => [
        match[1],
        match[2],
      ]),
    );
    // Published syntax palettes retain their original muted token colors.
    // Contrast requirements here apply to interface text and line numbers.
    it(`${theme} keeps interface text legible on source and diff backgrounds`, () => {
      for (const background of [
        "white",
        "diff-added",
        "diff-deleted",
        "target-background",
        "code-hover",
      ]) {
        for (const foreground of ["ink", "line-number"]) {
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
