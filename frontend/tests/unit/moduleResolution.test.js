import { describe, expect, it } from "vitest";
import path from "node:path";
import { resolveLocalImport } from "../../scripts/moduleResolution.js";

describe("local module resolution", () => {
  const testFile = path.resolve("tests/unit/moduleResolution.test.js");

  it("resolves dynamic imports with query suffixes", () => {
    const appModule = path.resolve("public/js/app.js");

    expect(resolveLocalImport(testFile, "../../public/js/app.js?restore-filters", new Set([appModule])))
      .toBe(appModule);
  });

  it("resolves demo modules excluded from quality scanning", () => {
    const demoStore = path.resolve("demo/demoStore.js");

    expect(resolveLocalImport(testFile, "../../demo/demoStore.js", new Set([demoStore])))
      .toBe(demoStore);
  });
});
