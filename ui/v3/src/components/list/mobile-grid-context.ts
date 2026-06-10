import { createContext, useContext } from "react";

export const MobileGridColsContext = createContext<1 | 2>(2);

export function useMobileGridCols(): 1 | 2 {
  return useContext(MobileGridColsContext);
}
