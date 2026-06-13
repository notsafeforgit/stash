import { ApolloProvider } from "@apollo/client/react";
import { Toaster } from "src/components/ui/sonner";
import { createClient } from "src/core/create-client";
import { ThemeProvider } from "@/components/theme-provider";
import { ConfigLoader } from "@/components/config-loader";
import { SystemStatusGate } from "@/components/migration-gate";
import { PluginLoader } from "@/components/plugin-loader";
import { TooltipProvider } from "@/components/ui/tooltip";
import { SaveIndicatorProvider } from "@/hooks/save-indicator";
import { ShortcutProvider } from "@/components/shortcut-provider";

const { client } = createClient();

export function App() {
  return (
    <ApolloProvider client={client}>
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
