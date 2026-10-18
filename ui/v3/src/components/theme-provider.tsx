import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useState,
  type PropsWithChildren,
} from "react";

type Theme = "light" | "dark" | "system";

interface ThemeContextValue {
  theme: Theme;
  setTheme: (t: Theme) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: PropsWithChildren) {
  const [theme, setThemeState] = useState<Theme>(() => {
    const stored = localStorage.getItem("theme");
    return stored === "light" || stored === "dark" ? stored : "system";
  });

  const applyTheme = useCallback((t: Theme) => {
    const root = document.documentElement;
    const dark =
      t === "dark" ||
      (t === "system" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    root.classList.toggle("dark", dark);
    const resolved = dark ? "dark" : "light";
    root.style.colorScheme = resolved;
    root.dataset.theme = resolved;

    // Opt-in document hints follow the resolved theme, including a saved
    // override. Leave the main app's configured browser color untouched.
    for (const meta of document.querySelectorAll<HTMLMetaElement>(
      'meta[name="theme-color"][data-app-theme]',
    )) {
      meta.content = dark ? "#0a0a0a" : "#ffffff";
    }
    document
      .querySelector('meta[name="color-scheme"]')
      ?.setAttribute("content", resolved);
  }, []);

  useLayoutEffect(() => {
    applyTheme(theme);
  }, [theme, applyTheme]);

  // Listen to system preference changes when theme is "system"
  useEffect(() => {
    if (theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => applyTheme("system");
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [theme, applyTheme]);

  const setTheme = useCallback((t: Theme) => {
    localStorage.setItem("theme", t);
    setThemeState(t);
  }, []);

  return (
    <ThemeContext.Provider value={{ theme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider");
  return ctx;
}
