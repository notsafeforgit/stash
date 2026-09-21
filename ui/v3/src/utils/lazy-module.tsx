import type { ReactNode } from "react";
import { lazyComponent } from "./lazy-component";

/** A render prop preserves generic exports (for example EntityDataTable<T>)
 * across React.lazy without erasing their type parameters or casting props. */
export function lazyModule<Module>(load: () => Promise<Module>) {
  return lazyComponent(async () => {
    const module = await load();
    return {
      default: ({ children }: { children: (module: Module) => ReactNode }) =>
        children(module),
    };
  });
}
