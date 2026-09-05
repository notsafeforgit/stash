import { ApolloProvider } from "@apollo/client/react";
import { Toaster } from "src/components/ui/sonner";
import { getClient } from "src/core/client";
import { ThemeProvider } from "@/components/theme-provider";
import { ConfigLoader } from "@/components/config-loader";
import { SystemStatusGate } from "@/components/migration-gate";
import { PluginLoader } from "@/components/plugin-loader";
import { TooltipProvider } from "@/components/ui/tooltip";
import { SaveIndicatorProvider } from "@/hooks/save-indicator";
import { ShortcutProvider } from "@/components/shortcut-provider";

export function App() {
  return (
    <ApolloProvider client={getClient()}>
      <ThemeProvider>
        <TooltipProvider>
          <ShortcutProvider>
            <SystemStatusGate>
              {/* SaveIndicatorProvider wraps ConfigLoader so the mutating
                hooks (useConfigureUISetting / useConfigureInterface) that
                fire from anywhere inside the router can register their
                promises with the floating save indicator. */}
              <SaveIndicatorProvider>
                <ConfigLoader>
                  <PluginLoader />
                  <Toaster />
                </ConfigLoader>
              </SaveIndicatorProvider>
            </SystemStatusGate>
          </ShortcutProvider>
        </TooltipProvider>
      </ThemeProvider>
    </ApolloProvider>
  );
}
