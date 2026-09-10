import { describe, expect, it } from "vitest";

import { transitionRuntimeStream } from "./runtime-stream-state";

describe("runtime stream state", () => {
  it("maps transport events without depending on browser effects", () => {
    expect(transitionRuntimeStream("connected", "interrupt")).toBe("reconnecting");
    expect(transitionRuntimeStream("reconnecting", "open")).toBe("connected");
    expect(transitionRuntimeStream("connected", "pause")).toBe("paused");
    expect(transitionRuntimeStream("paused", "connect")).toBe("connecting");
  });
});
