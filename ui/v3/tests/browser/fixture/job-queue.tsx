import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ApolloProvider } from "@apollo/client/react";
import { IntlProvider } from "react-intl";
import { JobTable } from "@/components/settings/tasks/job-table";
import { getClient } from "@/core/client";
import "@/styles/globals.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing fixture root");
createRoot(root).render(
  <StrictMode>
    <ApolloProvider client={getClient()}>
      <IntlProvider locale="en-GB" defaultLocale="en-GB">
        <JobTable />
      </IntlProvider>
    </ApolloProvider>
  </StrictMode>,
);
