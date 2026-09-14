import { createContext, useContext, useMemo } from "react";
import { useApolloClient } from "@apollo/client/react";

export interface FrontPageRowState {
  randomSeed: number;
  mounted: boolean;
  scrollLeft: number;
  /** Preserve the carousel's height while nearby cards remount on Back. */
  carouselHeight?: number;
}

interface FrontPageState {
  key: string;
  rows: FrontPageRowState[];
  scrollTop: number;
}

// One current Home configuration per client. Retain only seeds and positions;
// Apollo owns the data and departing pages release all cards and media DOM.
const visits = new WeakMap<object, FrontPageState>();

function getVisit(client: object, key: string, count: number): FrontPageState {
  const previous = visits.get(client);
  if (previous?.key === key) return previous;
  const next: FrontPageState = {
    key,
    scrollTop: 0,
    rows: Array.from({ length: count }, () => ({
      randomSeed: Math.floor(Math.random() * 1e8),
      mounted: false,
      scrollLeft: 0,
    })),
  };
  visits.set(client, next);
  return next;
}

export function useFrontPageState(key: string, count: number) {
  const client = useApolloClient();
  return useMemo(() => getVisit(client, key, count), [client, key, count]);
}

export const FrontPageRowContext = createContext<FrontPageRowState | undefined>(
  undefined,
);
export const useFrontPageRowState = () => useContext(FrontPageRowContext);
