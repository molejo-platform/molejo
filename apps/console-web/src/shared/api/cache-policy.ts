export const cachePolicy = {
  session: 60_000,
  hierarchy: 45_000,
  capability: 30_000,
  availability: 20_000,
  history: 30_000,
  activeOperation: 1_000,
  terminalOperation: 45_000,
} as const;
