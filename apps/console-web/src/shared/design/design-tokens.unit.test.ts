import { readdir, readFile } from "node:fs/promises";
import { extname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const sourceRoot = fileURLToPath(new URL("../..", import.meta.url));
const themePath = resolve(sourceRoot, "shared/design/theme-molejo.css");

async function collectCSS(directory: string): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true });
  const paths = await Promise.all(
    entries.map((entry) => {
      const path = resolve(directory, entry.name);
      return entry.isDirectory() ? collectCSS(path) : [path];
    }),
  );
  return paths.flat().filter((path) => extname(path) === ".css");
}

function declarations(source: string) {
  return new Map([...source.matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/gi)].map((match) => [match[1], match[2].trim()]));
}

function resolveColor(tokens: Map<string, string>, name: string, visited = new Set<string>()): string {
  if (visited.has(name)) throw new Error(`Circular token reference: ${name}`);
  visited.add(name);
  const value = tokens.get(name);
  if (!value) throw new Error(`Missing token: ${name}`);
  const reference = value.match(/^var\((--[a-z0-9-]+)\)$/i);
  return reference ? resolveColor(tokens, reference[1], visited) : value;
}

function relativeLuminance(hex: string) {
  const channels = [1, 3, 5].map((index) => Number.parseInt(hex.slice(index, index + 2), 16) / 255);
  const [red, green, blue] = channels.map((channel) =>
    channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4,
  );
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
}

function contrast(foreground: string, background: string) {
  const [lighter, darker] = [relativeLuminance(foreground), relativeLuminance(background)].sort(
    (left, right) => right - left,
  );
  return (lighter + 0.05) / (darker + 0.05);
}

describe("Molejo Console design tokens", () => {
  it("keeps authored colors inside the theme contract", async () => {
    const violations: string[] = [];
    for (const path of await collectCSS(sourceRoot)) {
      if (path === themePath) continue;
      const source = await readFile(path, "utf8");
      if (/(?:#[\da-f]{3,8}\b|\brgba?\()/i.test(source)) violations.push(relative(sourceRoot, path));
    }
    expect(violations).toEqual([]);
  });

  it("does not expose brand primitives to component or feature styles", async () => {
    const violations: string[] = [];
    for (const path of await collectCSS(sourceRoot)) {
      if (path === themePath) continue;
      const source = await readFile(path, "utf8");
      if (source.includes("--color-molejo-")) violations.push(relative(sourceRoot, path));
    }
    expect(violations).toEqual([]);
  });

  it("declares the minimum semantic theme contract", async () => {
    const tokens = declarations(await readFile(themePath, "utf8"));
    const required = [
      "--ui-canvas",
      "--ui-surface",
      "--ui-text",
      "--ui-text-muted",
      "--ui-heading",
      "--ui-border",
      "--ui-control-border",
      "--ui-action-primary",
      "--ui-on-action-primary",
      "--ui-focus-ring",
      "--ui-danger",
      "--ui-success",
      "--ui-warning",
      "--ui-info",
      "--font-family-heading",
      "--font-family-body",
      "--font-family-mono",
      "--control-min-size",
    ];
    expect(required.filter((token) => !tokens.has(token))).toEqual([]);
  });

  it("preserves accessible text and control contrast pairs", async () => {
    const tokens = declarations(await readFile(themePath, "utf8"));
    const pairs = [
      ["--ui-text", "--ui-canvas", 4.5],
      ["--ui-text", "--ui-surface", 4.5],
      ["--ui-on-action-primary", "--ui-action-primary", 4.5],
      ["--ui-danger", "--ui-danger-surface", 4.5],
      ["--ui-success", "--ui-success-surface", 4.5],
      ["--ui-warning", "--ui-warning-surface", 4.5],
      ["--ui-info", "--ui-info-surface", 4.5],
      ["--ui-control-border", "--ui-surface", 3],
      ["--ui-focus-ring", "--ui-surface", 3],
    ] as const;
    for (const [foreground, background, minimum] of pairs) {
      expect(
        contrast(resolveColor(tokens, foreground), resolveColor(tokens, background)),
        `${foreground} on ${background}`,
      ).toBeGreaterThanOrEqual(minimum);
    }
  });
});
