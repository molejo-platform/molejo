import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, extname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourceRoot = join(root, "src");
const generatedRoot = join(sourceRoot, "shared", "api", "generated");
const extensions = [".ts", ".tsx"];

function sourceFiles(directory) {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    if (path.startsWith(generatedRoot)) return [];
    return statSync(path).isDirectory() ? sourceFiles(path) : extensions.includes(extname(path)) ? [path] : [];
  });
}

function resolveImport(importer, specifier) {
  if (!specifier.startsWith(".")) return undefined;
  const base = resolve(dirname(importer), specifier);
  const candidates = [
    base,
    ...extensions.map((extension) => `${base}${extension}`),
    ...extensions.map((extension) => join(base, `index${extension}`)),
  ];
  return candidates.find((candidate) => files.has(candidate));
}

function featureName(path) {
  const parts = relative(sourceRoot, path).split(sep);
  return parts[0] === "features" ? parts[1] : undefined;
}

const files = new Set(sourceFiles(sourceRoot));
const productionFiles = [...files].filter((path) => !/\.(unit|integration)\.test\.tsx?$/.test(path));
const graph = new Map();
const problems = [];
const importPattern = /(?:import|export)\s+(?:[^"']*?\s+from\s+)?["']([^"']+)["']|import\(["']([^"']+)["']\)/g;

for (const file of productionFiles) {
  const content = readFileSync(file, "utf8");
  const lineCount = content.split("\n").length;
  if (lineCount > 1_000)
    problems.push(`${relative(root, file)} has ${lineCount} lines; authored files must stay below 1,000`);

  const imports = [];
  for (const match of content.matchAll(importPattern)) {
    const imported = resolveImport(file, match[1] ?? match[2]);
    if (!imported) continue;
    imports.push(imported);

    const source = relative(sourceRoot, file).split(sep)[0];
    const target = relative(sourceRoot, imported).split(sep)[0];
    if (source === "shared" && (target === "features" || target === "app")) {
      problems.push(`${relative(root, file)} imports ${relative(root, imported)} across the shared boundary`);
    }

    const sourceFeature = featureName(file);
    const targetFeature = featureName(imported);
    if (sourceFeature && targetFeature && sourceFeature !== targetFeature && !imported.endsWith(`${sep}public.ts`)) {
      problems.push(`${relative(root, file)} deep-imports feature ${targetFeature}; use its public.ts contract`);
    }
  }
  graph.set(file, imports);
}

const visited = new Set();
const active = new Set();
const stack = [];

function visit(file) {
  if (active.has(file)) {
    const cycleStart = stack.indexOf(file);
    problems.push(
      `static import cycle: ${stack
        .slice(cycleStart)
        .concat(file)
        .map((item) => relative(root, item))
        .join(" -> ")}`,
    );
    return;
  }
  if (visited.has(file)) return;
  visited.add(file);
  active.add(file);
  stack.push(file);
  for (const imported of graph.get(file) ?? []) visit(imported);
  stack.pop();
  active.delete(file);
}

for (const file of productionFiles) visit(file);

if (problems.length > 0) {
  console.error(problems.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Console architecture is valid (${productionFiles.length} authored source files checked).`);
}
