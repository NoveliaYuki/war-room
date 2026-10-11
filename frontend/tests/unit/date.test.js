import { describe, expect, it } from "vitest";
import { formatCalendarDate } from "../../public/js/utils/date.js";

describe("calendar date formatting", () => {
  it("formats valid calendar dates without shifting the day", () => {
    expect(formatCalendarDate("2026-10-13", { weekday: true })).toMatch(/Tuesday, Oct 13, 2026/);
  });

  it("rejects missing and impossible calendar dates", () => {
    expect(formatCalendarDate("")).toBe("Date unavailable");
    expect(formatCalendarDate("2026-02-30")).toBe("Date unavailable");
  });
});
