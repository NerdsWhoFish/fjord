import { describe, expect, it } from "vitest";
import { parsePRRoute, prPath } from "./pr-route";

const parse = (value: string) => parsePRRoute(new URL(value, "https://fjord.stout.zone"));

describe("pull request routes", () => {
  it("parses direct links and canonicalizes trailing slashes and leading zeros", () => {
    expect(parse("/NerdsWhoFish/fjord/007/")).toEqual({
      kind: "pr",
      target: { owner: "NerdsWhoFish", repo: "fjord", number: 7 },
      githubURL: "https://github.com/NerdsWhoFish/fjord/pull/7",
      path: "/NerdsWhoFish/fjord/7",
    });
    expect(prPath({ owner: "org", repo: "repo", number: 1 })).toBe("/org/repo/1");
    expect(parse("/")).toEqual({ kind: "home" });
  });

  it("supports explicit query targets without treating arbitrary URLs as repositories", () => {
    for (const value of [
      "/?owner=spacelift-solutions&repo=demo&number=229",
      "/?pr=https%3A%2F%2Fgithub.com%2Fspacelift-solutions%2Fdemo%2Fpull%2F229",
      "/?pr=https://github.com/spacelift-solutions/demo/pull/229/changes",
      "/?pr=https://github.com/spacelift-solutions/demo/pull/229/files",
    ]) {
      expect(parse(value)).toMatchObject({ kind: "pr", path: "/spacelift-solutions/demo/229" });
    }
  });

  it.each([
    "/org/repo/0", "/org/repo/-1", "/org/repo/1.5", "/org/repo/9007199254740992",
    "/org/repo/1/extra", "/org/repo/1?owner=other", "/org/repo/1#secret",
    "/api/navigate/1", "/assets/chunk/1", "/mcp/session/1",
    "/org/re%2Fpo/1", "/org/re%20po/1", "/-org/repo/1",
    "/?owner=org&repo=repo", "/?owner=org&repo=repo&number=0",
    "/?owner=org&repo=repo&number=1&number=2", "/?secret=value",
    "/?pr=https://github.com/org/repo/pull/1&owner=org",
    "/?pr=https://evil.example/org/repo/pull/1",
    "/?pr=https://github.com.evil.example/org/repo/pull/1",
    "/?pr=https://user@github.com/org/repo/pull/1",
    "/?pr=http://github.com/org/repo/pull/1",
    "/?pr=https://github.com/org/repo/pull/1%3Fsecret=x",
    "/?pr=https://github.com/org/repo/pull/1%23fragment",
  ])("rejects malformed or ambiguous route %s", (value) => {
    expect(parse(value).kind).toBe("error");
  });
});
