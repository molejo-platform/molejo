export type RuntimeStreamState = "connecting" | "connected" | "reconnecting" | "paused" | "unavailable";

export type RuntimeStreamEvent = "connect" | "open" | "interrupt" | "pause" | "unavailable";

export function transitionRuntimeStream(_current: RuntimeStreamState, event: RuntimeStreamEvent): RuntimeStreamState {
  const transitions: Record<RuntimeStreamEvent, RuntimeStreamState> = {
    connect: "connecting",
    open: "connected",
    interrupt: "reconnecting",
    pause: "paused",
    unavailable: "unavailable",
  };
  return transitions[event];
}
