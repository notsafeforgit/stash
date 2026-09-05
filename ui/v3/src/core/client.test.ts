import { expect, it, vi } from "vitest";
const factory = vi.hoisted(() => vi.fn(() => ({ client: {}, wsClient: {} })));
vi.mock("./create-client", () => ({ createClient: factory }));
import { getClient, getWSClient } from "./client";

it("creates one lazy client pair for the app and imperative queries", () => {
  expect(factory).not.toHaveBeenCalled();
  expect(getClient()).toBe(getClient());
  expect(getWSClient()).toBe(getWSClient());
  expect(factory).toHaveBeenCalledTimes(1);
});
