import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "@babel/parser";

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const roots = ["public/js", "tests", "scripts", "../scripts"].map((directory) => path.join(frontendRoot, directory));
const root = frontendRoot;
const MAX_COMPLEXITY = 10;
const FUNCTION_TYPES = new Set([
  "FunctionDeclaration",
  "FunctionExpression",
  "ArrowFunctionExpression",
  "ObjectMethod",
  "ClassMethod",
  "ClassPrivateMethod",
]);
const DECISION_TYPES = new Set([
  "IfStatement",
  "ForStatement",
  "ForInStatement",
  "ForOfStatement",
  "WhileStatement",
  "DoWhileStatement",
  "CatchClause",
  "ConditionalExpression",
  "LogicalExpression",
]);

/**
 * Recursively lists JavaScript source files below a directory.
 * @param {string} directory - Directory to inspect.
 * @returns {Promise<string[]>} Sorted source file paths.
 */
async function collectJavaScriptFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(entries.map(async (entry) => {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) return collectJavaScriptFiles(entryPath);
    return entry.isFile() && /\.(?:m?js)$/.test(entry.name) ? [entryPath] : [];
  }));
  return nested.flat().sort();
}

/**
 * Visits an AST subtree without entering nested functions.
 * @param {object} node - AST node to visit.
 * @param {(current: object) => void} visitor - Node callback.
 * @param {boolean} [skipNestedFunctions=false] - Whether to omit nested functions.
 * @param {boolean} [isRoot=true] - Whether the current node is the initial node.
 */
function visitNode(node, visitor, skipNestedFunctions = false, isRoot = true) {
  if (shouldSkipNode(node, skipNestedFunctions, isRoot)) return;
  if (typeof node.type === "string") visitor(node);
  visitChildren(node, visitor, skipNestedFunctions);
}

function shouldSkipNode(node, skipNestedFunctions, isRoot) {
  if (!node || typeof node !== "object") return true;
  return skipNestedFunctions && !isRoot && FUNCTION_TYPES.has(node.type);
}

function visitChildren(node, visitor, skipNestedFunctions) {
  for (const [key, value] of Object.entries(node)) {
    if (isAstMetadata(key)) continue;
    visitNodeValue(value, visitor, skipNestedFunctions);
  }
}

function isAstMetadata(key) {
  return ["loc", "start", "end", "leadingComments", "trailingComments"].includes(key);
}

function visitNodeValue(value, visitor, skipNestedFunctions) {
  if (Array.isArray(value)) {
    value.forEach((child) => visitNode(child, visitor, skipNestedFunctions, false));
    return;
  }
  if (value && typeof value === "object") visitNode(value, visitor, skipNestedFunctions, false);
}

/**
 * Computes McCabe cyclomatic complexity for one function body.
 * @param {object} fn - Function AST node.
 * @returns {number} Complexity score, starting at one.
 */
function calculateComplexity(fn) {
  let complexity = 1;
  visitNode(fn.body, (node) => {
    if (DECISION_TYPES.has(node.type)) complexity += 1;
    if (node.type === "SwitchCase" && node.test !== null) complexity += 1;
  }, true);
  return complexity;
}

/**
 * Returns whether an AST node has an attached JSDoc block.
 * @param {object} node - AST node to inspect.
 * @returns {boolean} True when a JSDoc block is attached.
 */
function hasJsDoc(node) {
  return (node.leadingComments || []).some((comment) => comment.type === "CommentBlock" && comment.value.startsWith("*"));
}

/**
 * Finds exported declarations whose public API lacks JSDoc.
 * @param {object} ast - Parsed JavaScript AST.
 * @returns {string[]} Names of undocumented exported APIs.
 */
function findUndocumentedExports(ast) {
  const declarations = collectTopLevelDeclarations(ast);
  const undocumented = [];
  for (const statement of ast.program.body) {
    if (statement.type === "ExportDefaultDeclaration") recordDefaultExport(statement, undocumented);
    if (statement.type === "ExportNamedDeclaration") recordNamedExports(statement, declarations, undocumented);
  }
  return undocumented;
}

function collectTopLevelDeclarations(ast) {
  const declarations = new Map();
  for (const statement of ast.program.body) {
    const declaration = statement.type.startsWith("Export") ? statement.declaration : statement;
    if (declaration) registerDeclaration(declarations, declaration);
  }
  return declarations;
}

function registerDeclaration(declarations, declaration) {
  if (declaration.type === "FunctionDeclaration" || declaration.type === "ClassDeclaration") {
    if (declaration.id) declarations.set(declaration.id.name, declaration);
    return;
  }
  if (declaration.type === "VariableDeclaration") {
    for (const item of declaration.declarations) {
      if (item.id.type === "Identifier") declarations.set(item.id.name, item);
    }
  }
}

function recordDefaultExport(statement, undocumented) {
  if (!isDocumented(statement, statement.declaration)) undocumented.push("default export");
}

function recordNamedExports(statement, declarations, undocumented) {
  if (statement.declaration) recordExportDeclaration(statement, statement.declaration, undocumented);
  if (statement.specifiers.length) recordExportSpecifiers(statement, declarations, undocumented);
}

function recordExportDeclaration(statement, declaration, undocumented) {
  if (declaration.type === "VariableDeclaration") {
    recordVariableExports(statement, declaration, undocumented);
  } else if (!isDocumented(statement, declaration)) {
    undocumented.push(declaration.id?.name || "exported declaration");
  }
}

function recordVariableExports(statement, declaration, undocumented) {
  for (const item of declaration.declarations) {
    const publicName = item.id.type === "Identifier" ? item.id.name : "exported value";
    if (!isDocumented(statement, declaration, item)) undocumented.push(publicName);
  }
}

function recordExportSpecifiers(statement, declarations, undocumented) {
  for (const specifier of statement.specifiers) {
    const localName = specifier.local.name;
    const declaration = declarations.get(localName);
    if (declaration && !isDocumented(declaration, statement)) {
      undocumented.push(specifier.exported.name || localName);
    }
  }
}

function isDocumented(...nodes) {
  return nodes.some((node) => node && hasJsDoc(node));
}

/** Returns relative ES module import paths from one parsed source file. */
function collectLocalImports(ast) {
  const imports = [];
  for (const statement of ast.program.body) {
    if (statement.type === "ImportDeclaration" && statement.source.value.startsWith(".")) {
      imports.push(statement.source.value);
    }
    if ((statement.type === "ExportNamedDeclaration" || statement.type === "ExportAllDeclaration")
      && statement.source?.value.startsWith(".")) {
      imports.push(statement.source.value);
    }
  }
  visitNode(ast, (node) => {
    if (node.type === "ImportExpression" && node.source.type === "StringLiteral" && node.source.value.startsWith(".")) {
      imports.push(node.source.value);
    }
  });
  return imports;
}

/** Resolves a local import to one of the frontend's JavaScript modules. */
function resolveLocalImport(sourceFile, specifier, sourceFiles) {
  const target = path.resolve(path.dirname(sourceFile), specifier);
  const candidates = path.extname(target) ? [target] : [ `${target}.js`, path.join(target, "index.js") ];
  return candidates.find((candidate) => sourceFiles.has(candidate));
}

/** Reports cycles in the frontend's directed module dependency graph. */
function findImportCycles(graph) {
  const states = new Map();
  const stack = [];
  const cycles = new Set();

  function visit(file) {
    if (states.get(file) === 2) return;
    if (states.get(file) === 1) {
      const cycleStart = stack.indexOf(file);
      cycles.add([...stack.slice(cycleStart), file].map((entry) => path.relative(root, entry)).join(" -> "));
      return;
    }
    states.set(file, 1);
    stack.push(file);
    for (const dependency of graph.get(file) || []) visit(dependency);
    stack.pop();
    states.set(file, 2);
  }

  for (const file of graph.keys()) visit(file);
  return [...cycles];
}

/**
 * Parses source files and enforces syntax, complexity, and exported API docs.
 */
async function main() {
  const failures = [];
  const files = (await Promise.all(roots.map(collectJavaScriptFiles))).flat();
  files.push(path.join(frontendRoot, "vitest.config.js"));
  files.sort();
  const sourceFiles = new Set(files);
  const graph = new Map();
  for (const file of files) {
    const relativePath = path.relative(root, file);
    const source = await readFile(file, "utf8");
    let ast;
    try {
      ast = parse(source, { sourceType: "module", sourceFilename: relativePath });
    } catch (error) {
      failures.push(`${relativePath}:${error.loc?.line || 1}:${error.loc?.column || 0} syntax: ${error.message}`);
      continue;
    }

    for (const undocumented of findUndocumentedExports(ast)) {
      failures.push(`${relativePath}: exported API "${undocumented}" is missing JSDoc`);
    }
    const dependencies = [];
    for (const specifier of collectLocalImports(ast)) {
      const dependency = resolveLocalImport(file, specifier, sourceFiles);
      if (!dependency) {
        failures.push(`${relativePath}: local import "${specifier}" does not resolve to a JavaScript module`);
      } else {
        dependencies.push(dependency);
      }
    }
    graph.set(file, dependencies);
    visitNode(ast, (node) => {
      if (!FUNCTION_TYPES.has(node.type)) return;
      const complexity = calculateComplexity(node);
      if (complexity > MAX_COMPLEXITY) {
        const line = node.loc?.start.line || 1;
        const name = node.id?.name || node.key?.name || "anonymous function";
        failures.push(`${relativePath}:${line}: ${name} complexity ${complexity} exceeds ${MAX_COMPLEXITY}`);
      }
    });
  }

  for (const cycle of findImportCycles(graph)) failures.push(`cyclic frontend imports: ${cycle}`);

  if (failures.length) {
    process.stderr.write(`${failures.join("\n")}\n`);
    process.exitCode = 1;
    return;
  }
  process.stdout.write(`Frontend quality checks passed for ${files.length} JavaScript files; complexity limit ${MAX_COMPLEXITY}.\n`);
}

await main();
