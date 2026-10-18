// A classic, parser-blocking script: the public CSP permits same-origin assets,
// not inline scripts. Resolve the theme before CSS paints the fixed body; Safari
// can retain the first color it samples from a viewport-sized fixed element.
(() => {
  let theme = "system";
  try {
    theme = localStorage.getItem("theme") || "system";
  } catch {
    // Storage can be unavailable in a restricted recipient browser.
  }
  const dark =
    theme === "dark" ||
    (theme !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
  const root = document.documentElement;
  const resolved = dark ? "dark" : "light";
  root.classList.toggle("dark", dark);
  root.dataset.theme = resolved;
  root.style.colorScheme = resolved;
  document.querySelector('meta[name="color-scheme"]').content = resolved;
  document.querySelector('meta[name="theme-color"]').content = dark
    ? "#0a0a0a"
    : "#ffffff";
})();
