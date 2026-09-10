import type { RuntimeRange } from "./api";

export function createRuntimeRange(hours: number): RuntimeRange {
  const to = new Date();
  return { from: new Date(to.getTime() - hours * 60 * 60 * 1_000).toISOString(), to: to.toISOString() };
}

export function boundRuntimeRange(range: RuntimeRange, maximumHours: number): RuntimeRange {
  const earliest = Date.parse(range.to) - maximumHours * 60 * 60 * 1_000;
  return { from: new Date(Math.max(Date.parse(range.from), earliest)).toISOString(), to: range.to };
}
