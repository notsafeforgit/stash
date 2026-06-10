import { type Dispatch, type SetStateAction, useEffect, useState } from "react";
import type { View } from "src/components/list/views";
import type { ConfigImageLightboxInput } from "src/core/generated-graphql";

interface InterfaceQueryConfig {
  filter: string;
  itemsPerPage: number;
  currentPage: number;
}

export interface ViewConfig {
  showSidebar?: boolean;
}

type QueryConfig = Record<string, InterfaceQueryConfig>;

interface InterfaceConfig {
  queryConfig: QueryConfig;
  imageLightbox: ConfigImageLightboxInput;
  // Partial is required because using View makes the key mandatory
  viewConfig: Partial<Record<View, ViewConfig>>;
}

export interface ChangelogConfig {
  versions: Record<string, boolean>;
}

function readStorage<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key);
    if (raw !== null) return JSON.parse(raw) as T;
  } catch {
    // ignore parse errors
  }
  return null;
}

function writeStorage<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // ignore storage errors
  }
}

export function useLocalForage<T extends object>(
  key: string,
  defaultValue: T = {} as T,
): [T, Dispatch<SetStateAction<T>>] {
  const [data, setData] = useState<T>(
    () => readStorage<T>(key) ?? defaultValue,
  );

  useEffect(() => {
    writeStorage(key, data);
  }, [key, data]);

  return [data, setData];
}

export function readInterfaceConfig(): InterfaceConfig {
  return readStorage<InterfaceConfig>("interface") ?? ({} as InterfaceConfig);
}

export const useInterfaceLocalForage = () =>
  useLocalForage<InterfaceConfig>("interface");

export const useChangelogStorage = () =>
  useLocalForage<ChangelogConfig>("changelog");
