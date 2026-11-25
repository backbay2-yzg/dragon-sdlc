import { webDarkTheme, webLightTheme, type Theme } from "@fluentui/react-components";

export const dragonLightTheme: Theme = {
  ...webLightTheme,
  fontFamilyBase:
    '"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", -apple-system, BlinkMacSystemFont, sans-serif',
  fontFamilyMonospace: '"Cascadia Code", "JetBrains Mono", Consolas, monospace',
  colorNeutralBackground1: "rgba(255, 255, 255, 0.72)",
  colorNeutralBackground2: "rgba(248, 249, 252, 0.56)",
  colorNeutralBackground3: "rgba(240, 242, 247, 0.48)",
  colorNeutralStroke1: "rgba(15, 23, 42, 0.12)",
  colorNeutralStroke2: "rgba(15, 23, 42, 0.07)",
  colorBrandBackground: "#0F6CBD",
  colorBrandBackgroundHover: "#115EA3",
  colorBrandBackgroundPressed: "#0C3B5E",
};

export const dragonDarkTheme: Theme = {
  ...webDarkTheme,
  fontFamilyBase:
    '"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", -apple-system, BlinkMacSystemFont, sans-serif',
  fontFamilyMonospace: '"Cascadia Code", "JetBrains Mono", Consolas, monospace',
  colorNeutralBackground1: "rgba(38, 40, 46, 0.72)",
  colorNeutralBackground2: "rgba(28, 30, 35, 0.58)",
  colorNeutralBackground3: "rgba(20, 22, 26, 0.52)",
  colorNeutralStroke1: "rgba(255, 255, 255, 0.12)",
  colorNeutralStroke2: "rgba(255, 255, 255, 0.07)",
  colorBrandBackground: "#479EF5",
  colorBrandBackgroundHover: "#62ABF5",
  colorBrandBackgroundPressed: "#2886DE",
};

export function resolveEffectiveDark(themeMode: string, systemDark: boolean): boolean {
  if (themeMode === "dark") return true;
  if (themeMode === "light") return false;
  return systemDark;
}
