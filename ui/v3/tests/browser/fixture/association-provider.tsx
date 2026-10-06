import type { ReactNode } from "react";
import { ApolloClient, HttpLink, InMemoryCache } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { ConfigurationProvider } from "@/hooks/config";
import { playerConfiguration } from "./player-configuration";

const client = new ApolloClient({
  link: new HttpLink({ uri: "/graphql" }),
  cache: new InMemoryCache(),
});
export function AssociationFixtureProvider({
  children,
}: {
  children: ReactNode;
}) {
  return (
    <ApolloProvider client={client}>
      <ConfigurationProvider configuration={playerConfiguration}>
        {children}
      </ConfigurationProvider>
    </ApolloProvider>
  );
}
