import path from "node:path";

/**
 * Resolves a relative module specifier to a JavaScript source file.
 * Query and fragment suffixes identify module variants but do not change the file path.
 * @param {string} sourceFile - Importing JavaScript file.
 * @param {string} specifier - Relative module specifier.
 * @param {Set<string>} sourceFiles - Absolute source file paths.
 * @returns {string|undefined} Matching source file path.
 */
export function resolveLocalImport(sourceFile, specifier, sourceFiles) {
  const modulePath = specifier.split(/[?#]/, 1)[0];
  const target = path.resolve(path.dirname(sourceFile), modulePath);
  const candidates = path.extname(target) ? [target] : [ `${target}.js`, path.join(target, "index.js") ];
  return candidates.find((candidate) => sourceFiles.has(candidate));
}
