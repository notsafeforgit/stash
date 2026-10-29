import React from "react";
import { createRoot } from "react-dom/client";
import { ThemeProvider, useTheme } from "@/components/theme-provider";
import { Button } from "@/components/ui/button";
import "./style.css";

function ThemeControls() {
  const { theme, setTheme } = useTheme();
  return (
    <main>
      <p>Current theme: {theme}</p>
      {(["light", "dark", "system"] as const).map((value) => (
        <Button key={value} onClick={() => setTheme(value)}>
          {value}
        </Button>
      ))}
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider>
      <ThemeControls />
    </ThemeProvider>
  </React.StrictMode>,
);
