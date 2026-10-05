import { createContext } from "react";

// A save or preview must include the entry the user just selected, rather than
// racing the lookup that resolves its local ID to a native UUID.
export const PolicySelectionPending = createContext<(pending: boolean) => void>(
  () => {},
);
