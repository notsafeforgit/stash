import { afterEach } from "vitest";
import { clearStaleFocus, installPopoverStubs } from "./popover-jsdom";

// Node-environment tests have no DOM to patch; jsdom tests opt in per file.
if (typeof window !== "undefined") {
  installPopoverStubs();
  afterEach(clearStaleFocus);
}
