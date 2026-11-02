import { writable } from "svelte/store";

export type ThemeName = "light" | "dark" | "eye-care";

/** 点击循环的顺序：亮色 → 暗色 → 护眼 → 亮色。 */
const THEME_ORDER: readonly ThemeName[] = ["light", "dark", "eye-care"];

/** 主题的中文名，页面上只留一个循环按钮，靠它写 title 与 aria-label。 */
export function themeLabel(name: ThemeName): string {
  switch (name) {
    case "light":
      return "亮色";
    case "dark":
      return "暗色";
    case "eye-care":
      return "护眼";
  }
}

/** nextTheme 返回循环里的下一个主题。 */
export function nextTheme(name: ThemeName): ThemeName {
  const index = THEME_ORDER.indexOf(name);
  return THEME_ORDER[(index + 1) % THEME_ORDER.length];
}

const STORAGE_KEY = "note.theme";

export function resolveTheme(
  stored: string | null,
  systemPrefersDark: boolean,
): ThemeName {
  if (stored === "light" || stored === "dark" || stored === "eye-care") {
    return stored;
  }
  return systemPrefersDark ? "dark" : "light";
}

function detectTheme(): ThemeName {
  const stored =
    typeof localStorage === "undefined"
      ? null
      : localStorage.getItem(STORAGE_KEY);
  const prefersDark =
    typeof matchMedia === "undefined"
      ? false
      : matchMedia("(prefers-color-scheme: dark)").matches;
  return resolveTheme(stored, prefersDark);
}

export const theme = writable<ThemeName>(detectTheme());

theme.subscribe((name) => {
  if (typeof document !== "undefined") {
    document.documentElement.dataset.theme = name;
  }
});

export function setTheme(name: ThemeName): void {
  if (typeof localStorage !== "undefined") {
    localStorage.setItem(STORAGE_KEY, name);
  }
  theme.set(name);
}
