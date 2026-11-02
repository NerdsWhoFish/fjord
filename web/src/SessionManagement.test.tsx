import { describe, expect, it } from "vitest";
import { isSessionSummary, sessionAge } from "./SessionManagement";

describe("review session metadata", () => {
  const now = Date.parse("2026-11-02T12:00:00Z");
  it("shows useful sync ages without negative or invalid times", () => {
    expect(sessionAge("2026-11-02T12:01:00Z", now)).toBe("Just now");
    expect(sessionAge("2026-11-02T11:59:00Z", now)).toBe("1m ago");
    expect(sessionAge("2026-11-02T11:00:00Z", now)).toBe("1h ago");
    expect(sessionAge("2026-10-31T12:00:00Z", now)).toBe("2d ago");
    expect(sessionAge("invalid", now)).toBe("Unknown");
  });
  it("rejects malformed API metadata before rendering it", () => {
    const item = {
      id: "session",
      owner: "example",
      repo: "project",
      number: 3,
      title: "Review",
      headSHA: "revision",
      updatedAt: "2026-11-02T12:00:00Z",
      expiresAt: "2026-11-02T14:00:00Z",
      draftCommentCount: 2,
    };
    expect(isSessionSummary(item)).toBe(true);
    for (const bad of [
      null,
      [],
      { ...item, id: 1 },
      { ...item, number: 0 },
      { ...item, updatedAt: "invalid" },
      { ...item, draftCommentCount: -1 },
    ]) {
      expect(isSessionSummary(bad)).toBe(false);
    }
  });
});
