// Pierre's stylesheets use `light-dark()`, which needs Safari 17.5 / Chromium
// 123. The generator lowers it (scripts/generate-pierre-viewer.mjs) to a pair of
// custom properties that exactly one of the two states below turns on, so the
// document follows the app theme on the older WebViews the app still supports.
const RULES = "[data-mewla-scheme=light]{--lightningcss-light:initial;--lightningcss-dark: }[data-mewla-scheme=dark]{--lightningcss-light: ;--lightningcss-dark:initial}";

export function applyColorScheme(container, dark) {
  if (!container.querySelector(":scope > style[data-mewla-scheme-rules]")) {
    const rules = document.createElement("style");
    rules.setAttribute("data-mewla-scheme-rules", "");
    rules.textContent = RULES;
    container.prepend(rules);
  }
  container.setAttribute("data-mewla-scheme", dark ? "dark" : "light");
  container.style.colorScheme = dark ? "dark" : "light";
}
