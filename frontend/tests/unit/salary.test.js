import { describe, expect, it } from "vitest";
import { formatSalary } from "../../public/js/utils/salary.js";

describe("formatSalary", () => {
  it("formats ranges in supported currencies", () => {
    expect(formatSalary("limited", 37000, 83000, "EUR")).toBe("€37k-83k");
    expect(formatSalary("limited", 37000, 83000, "USD")).toBe("$37k-83k");
    expect(formatSalary("limited", 37000, 83000, "GBP")).toBe("£37k-83k");
  });

  it("formats single bounds", () => {
    expect(formatSalary("no_min", null, 83000, "USD")).toBe("Up to $83k");
    expect(formatSalary("no_max", 37000, null, "USD")).toBe("From $37k");
    expect(formatSalary("limited", 37000, 37000, "EUR")).toBe("€37k");
  });

  it("handles missing and small amounts", () => {
    expect(formatSalary("unknown", null, null)).toBe("Salary undisclosed");
    expect(formatSalary("limited", 900, 900, "EUR")).toBe("€900");
    expect(formatSalary("limited", 1500, 1500, "EUR")).toBe("€1.5k");
  });

  it("uses the euro symbol for unsupported currencies", () => {
    expect(formatSalary("limited", 37000, 83000, "CAD")).toBe("€37k-83k");
  });
});
