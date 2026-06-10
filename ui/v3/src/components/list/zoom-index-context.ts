import { createContext, useContext } from "react";

export const ZoomIndexContext = createContext<number>(1);

export function useZoomIndex(): number {
  return useContext(ZoomIndexContext);
}
