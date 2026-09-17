import { useEffect } from "react";
import { useApolloClient } from "@apollo/client/react";
import { activeLibraryQueries } from "@/core/mutation-invalidation";

/** A restored document retains its Apollo cache, including data that changed
 * while it was suspended. Revalidate visible library queries in place; normal
 * SPA Back and initial loads keep their existing cache-first behavior. */
export function useLibraryRestore() {
  const client = useApolloClient();
  useEffect(() => {
    function restore(event: PageTransitionEvent) {
      if (!event.persisted) return;
      // Including DocumentNodes in refetchQueries also selects skipped
      // instances of the same operation (e.g. closed scene-picker dialogs).
      // Refetch the active observables themselves instead.
      void Promise.all(
        activeLibraryQueries(client).map((query) => query.refetch()),
      ).catch((error: unknown) =>
        console.error("Could not refresh restored library views", error),
      );
    }
    window.addEventListener("pageshow", restore);
    return () => window.removeEventListener("pageshow", restore);
  }, [client]);
}
