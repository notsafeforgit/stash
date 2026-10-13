// Stable public library identities for fixtures. Numeric database keys are not
// accepted by the native UI/API, including saved filters and selected entities.
export const libraryID = (value: number) =>
  `00000000-0000-4000-8000-${String(value).padStart(12, "0")}`;
