import { MockedProvider } from "@apollo/client/testing/react";
import type { MockedResponse } from "@apollo/client/testing";
import * as GQL from "@/core/generated-graphql";
import { ConfigurationProvider } from "@/hooks/config";
import { PackageManager } from "@/components/settings/package-manager";
import { playerConfiguration } from "./player-configuration";

const source = "https://packages.test/index.yml";
const configuration: GQL.ConfigDataFragment = {
  ...playerConfiguration,
  general: {
    ...playerConfiguration.general,
    scraperPackageSources: [
      {
        __typename: "PackageSource",
        name: "Fixture packages",
        url: source,
        local_path: null,
      },
    ],
  },
};

declare global {
  interface Window {
    packageIndexRequests: number;
  }
}
window.packageIndexRequests = 0;

const mocks: MockedResponse[] = [
  {
    request: { query: GQL.ConfigurationDocument },
    result: { data: { configuration } },
    maxUsageCount: Infinity,
  },
  {
    request: { query: GQL.InstalledScraperPackagesDocument },
    result: { data: { installedPackages: [] } },
    maxUsageCount: Infinity,
  },
  {
    request: { query: GQL.JobsLifecycleSubscribeDocument },
    result: { data: { jobsLifecycleSubscribe: null } },
  },
  {
    request: { query: GQL.JobsProgressSubscribeDocument },
    result: { data: { jobsProgressSubscribe: null } },
  },
  {
    request: {
      query: GQL.AvailableScraperPackagesDocument,
      variables: { source },
    },
    result: () => {
      window.packageIndexRequests += 1;
      return {
        data: {
          availablePackages: [
            {
              __typename: "Package",
              package_id: "network",
              name: "Network scraper",
              version: "1",
              date: null,
              sourceURL: source,
              metadata: {
                scrapes: ["Example Studio"],
                scene_urls: ["example.test/scenes/"],
              },
              requires: [],
            },
            {
              __typename: "Package",
              package_id: "older",
              name: "Old scraper",
              version: "1",
              date: null,
              sourceURL: source,
              metadata: {},
              requires: [],
            },
          ],
        },
      };
    },
  },
];

export function PackageSearchFixture() {
  return (
    <MockedProvider mocks={mocks}>
      <ConfigurationProvider configuration={configuration}>
        <div className="p-4">
          <PackageManager type="scraper" />
        </div>
      </ConfigurationProvider>
    </MockedProvider>
  );
}
