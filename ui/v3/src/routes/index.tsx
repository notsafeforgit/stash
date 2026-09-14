import { createFileRoute } from "@tanstack/react-router";
import { FrontPage } from "@/components/frontpage/front-page";
import { preloadFrontPage } from "@/components/frontpage/preload-front-page";
import { getClient } from "@/core/client";
import { getFrontPageContent } from "@/core/config";
import { ConfigurationDocument } from "@/core/generated-graphql";

export const Route = createFileRoute("/")({
  // Both options share FrontPage; keep their import in the same lazy chunk.
  codeSplitGroupings: [["component", "pendingComponent"]],
  pendingMs: 600,
  pendingMinMs: 0,
  pendingComponent: FrontPage,
  loader: () => {
    const client = getClient();
    const configuration = client.readQuery({ query: ConfigurationDocument });
    return preloadFrontPage(
      client,
      getFrontPageContent(configuration?.configuration.ui),
    );
  },
  component: FrontPage,
});
