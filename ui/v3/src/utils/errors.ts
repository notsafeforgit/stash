import { CombinedGraphQLErrors } from "@apollo/client";

export const apolloError = (error: unknown) =>
  error instanceof CombinedGraphQLErrors ? error.message : "";

export function errorToString(error: unknown) {
  let message: string | undefined;
  if (error instanceof Error) {
    message = error.message;
  }
  if (!message) {
    message = String(error);
  }
  if (!message) {
    message = "Unknown error";
  }

  return message;
}
