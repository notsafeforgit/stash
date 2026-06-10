import { createContext, useContext } from "react";

export type CardAspect = "portrait" | "landscape" | "auto";

export const CardAspectContext = createContext<CardAspect>("auto");

export function useCardAspect(): CardAspect {
  return useContext(CardAspectContext);
}
