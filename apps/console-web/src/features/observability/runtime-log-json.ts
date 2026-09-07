const MAX_HIGHLIGHT_LENGTH = 32_768;
const MAX_FORMATTED_LENGTH = 131_072;
const MAX_TOKENS = 8_000;
const MAX_DEPTH = 20;
const MAX_CONTAINERS = 2_000;

export type RuntimeJsonToken = {
  kind: "key" | "string" | "number" | "boolean" | "null" | "bracket" | "punctuation" | "text";
  text: string;
  depth?: number;
};

export type ParsedRuntimeJson = {
  compact: string;
  formatted: string;
  compactTokens: readonly RuntimeJsonToken[];
  formattedTokens: readonly RuntimeJsonToken[];
};

export function parseRuntimeJsonBody(body: string): ParsedRuntimeJson | undefined {
  if (body.length > MAX_HIGHLIGHT_LENGTH) return undefined;

  let value: unknown;
  try {
    value = JSON.parse(body);
  } catch {
    return undefined;
  }
  if (value === null || typeof value !== "object") return undefined;
  if (!hasSupportedShape(value)) return undefined;

  const compact = JSON.stringify(value);
  const formatted = JSON.stringify(value, null, 2);
  if (formatted.length > MAX_FORMATTED_LENGTH) return undefined;

  const compactTokens = tokenizeJson(compact);
  const formattedTokens = tokenizeJson(formatted);
  if (compactTokens.length > MAX_TOKENS || formattedTokens.length > MAX_TOKENS) return undefined;

  return { compact, formatted, compactTokens, formattedTokens };
}

function hasSupportedShape(root: object): boolean {
  const pending: Array<{ value: object; depth: number }> = [{ value: root, depth: 1 }];
  let containers = 0;

  while (pending.length > 0) {
    const current = pending.pop();
    if (!current || current.depth > MAX_DEPTH || ++containers > MAX_CONTAINERS) return false;
    for (const value of Object.values(current.value)) {
      if (value !== null && typeof value === "object") pending.push({ value, depth: current.depth + 1 });
    }
  }

  return true;
}

function tokenizeJson(json: string): RuntimeJsonToken[] {
  const tokens: RuntimeJsonToken[] = [];
  let depth = 0;
  let index = 0;

  while (index < json.length) {
    const character = json[index];
    if (/\s/.test(character)) {
      const end = consumeWhile(json, index, (value) => /\s/.test(value));
      tokens.push({ kind: "text", text: json.slice(index, end) });
      index = end;
      continue;
    }
    if (character === '"') {
      const end = consumeString(json, index);
      let next = end;
      while (next < json.length && /\s/.test(json[next])) next += 1;
      tokens.push({ kind: json[next] === ":" ? "key" : "string", text: json.slice(index, end) });
      index = end;
      continue;
    }
    if (character === "{" || character === "[") {
      tokens.push({ kind: "bracket", text: character, depth });
      depth += 1;
      index += 1;
      continue;
    }
    if (character === "}" || character === "]") {
      depth = Math.max(0, depth - 1);
      tokens.push({ kind: "bracket", text: character, depth });
      index += 1;
      continue;
    }
    if (character === ":" || character === ",") {
      tokens.push({ kind: "punctuation", text: character });
      index += 1;
      continue;
    }

    const literal = json.slice(index).match(/^(?:-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null)/)?.[0];
    if (literal) {
      const kind = literal === "true" || literal === "false" ? "boolean" : literal === "null" ? "null" : "number";
      tokens.push({ kind, text: literal });
      index += literal.length;
      continue;
    }

    tokens.push({ kind: "text", text: character });
    index += 1;
  }

  return tokens;
}

function consumeWhile(value: string, start: number, predicate: (character: string) => boolean): number {
  let index = start;
  while (index < value.length && predicate(value[index])) index += 1;
  return index;
}

function consumeString(value: string, start: number): number {
  let escaped = false;
  for (let index = start + 1; index < value.length; index += 1) {
    if (escaped) {
      escaped = false;
    } else if (value[index] === "\\") {
      escaped = true;
    } else if (value[index] === '"') {
      return index + 1;
    }
  }
  return value.length;
}
