import { createContext, useContext } from "react";

export type CardLayout = "grid" | "details" | "wall";

export const CardLayoutContext = createContext<CardLayout>("grid");

export function useCardLayout(): CardLayout {
  return useContext(CardLayoutContext);
}
