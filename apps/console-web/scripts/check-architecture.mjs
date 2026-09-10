import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, extname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourceRoot = join(root, "src");
const generatedRoot = join(sourceRoot, "shared", "api", "generated");
const extensions = [".ts", ".tsx"];
const authoredExtensions = [...extensions, ".css"];

function sourceFiles(directory) {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    if (path.startsWith(generatedRoot)) return [];
    return statSync(path).isDirectory() ? sourceFiles(path) : extensions.includes(extname(path)) ? [path] : [];
  });
}

function authoredFiles(directory) {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    if (path.startsWith(generatedRoot)) return [];
    return statSync(path).isDirectory()
      ? authoredFiles(path)
      : authoredExtensions.includes(extname(path))
        ? [path]
        : [];
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

for (const file of authoredFiles(sourceRoot)) {
  const content = readFileSync(file, "utf8");
  const lineCount = content.split("\n").length;
  const isTest = /\.(unit|integration)\.test\.tsx?$/.test(file);
  if (!isTest && lineCount > 400)
    problems.push(
      `${relative(root, file)} has ${lineCount} lines; production-authored files must stay at or below 400`,
    );

  if (extname(file) === ".css" && relative(sourceRoot, file).startsWith(`shared${sep}`)) {
    if (/@import\s+[^;]*(?:features|\/app\/)/.test(content)) {
      problems.push(`${relative(root, file)} imports application or feature CSS across the shared boundary`);
    }
  }

  const legacyLayoutContracts = ["form-row", "data-row", "constrained", "narrow"];
  for (const contract of legacyLayoutContracts) {
    if (content.includes(contract)) problems.push(`${relative(root, file)} uses legacy layout contract '${contract}'`);
  }
  if (content.includes("stack data-list-item")) {
    problems.push(`${relative(root, file)} combines stack and data-list-item instead of composing an owned item`);
  }
}

for (const file of productionFiles) {
  const content = readFileSync(file, "utf8");
  const sourcePath = relative(sourceRoot, file);

  const imports = [];
  for (const match of content.matchAll(importPattern)) {
    const specifier = match[1] ?? match[2];
    if (
      (specifier === "lucide-react" || specifier.startsWith("@base-ui/react")) &&
      !sourcePath.startsWith(`shared${sep}ui${sep}`)
    ) {
      problems.push(`${relative(root, file)} imports ${specifier} outside the shared/ui adapter boundary`);
    }
    const imported = resolveImport(file, specifier);
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
