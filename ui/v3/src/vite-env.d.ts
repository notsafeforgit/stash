/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_APP_PLATFORM_URL: string;
  readonly VITE_APP_PLATFORM_PORT: string;
  readonly VITE_APP_DATE: string;
  readonly VITE_APP_GITHASH: string;
  readonly VITE_APP_STASH_VERSION: string;
  readonly VITE_APP_SOURCEMAPS: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

declare module "apollo-upload-client/UploadHttpLink.mjs" {
  import { ApolloLink } from "@apollo/client";
  export default class UploadHttpLink extends ApolloLink {
    constructor(options?: {
      uri?: string;
      headers?: Record<string, string>;
      credentials?: string;
      fetchOptions?: Record<string, unknown>;
    });
  }
}
