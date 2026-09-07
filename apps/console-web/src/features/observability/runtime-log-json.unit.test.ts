import { describe, expect, it } from "vitest";

import { parseRuntimeJsonBody } from "./runtime-log-json";

describe("runtime log JSON", () => {
  it("classifies properties, values, and nested delimiters without losing content", () => {
    const body = '{"request":{"status":200,"cached":true,"items":[null,"ok"]}}';
    const parsed = parseRuntimeJsonBody(body);

    expect(parsed?.compact).toBe(body);
    expect(parsed?.compactTokens.filter((token) => token.kind === "key").map((token) => token.text)).toEqual([
      '"request"',
      '"status"',
      '"cached"',
      '"items"',
    ]);
    expect(
      parsed?.compactTokens.filter((token) => token.kind === "bracket").map(({ text, depth }) => ({ text, depth })),
    ).toEqual([
      { text: "{", depth: 0 },
      { text: "{", depth: 1 },
      { text: "[", depth: 2 },
      { text: "]", depth: 2 },
      { text: "}", depth: 1 },
      { text: "}", depth: 0 },
    ]);
    expect(parsed?.formatted).toContain('\n  "request": {\n    "status": 200');
  });

  it("leaves plain, primitive, and oversized bodies untouched", () => {
    expect(parseRuntimeJsonBody("server ready")).toBeUndefined();
    expect(parseRuntimeJsonBody('"server ready"')).toBeUndefined();
    expect(parseRuntimeJsonBody(`{"message":"${"x".repeat(32_768)}"}`)).toBeUndefined();
    expect(parseRuntimeJsonBody(`${"[".repeat(21)}0${"]".repeat(21)}`)).toBeUndefined();
  });
});
