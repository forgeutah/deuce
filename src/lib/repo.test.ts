import { describe, expect, it } from "vitest";
import { NO_REPO_LABEL, repoGroupKey, repoGroupLabel } from "./repo";

// NOTE: The frontend has no test runner wired up yet (see CLAUDE.md). These
// Vitest-style specs capture the intended behavior and run as soon as a runner
// is added. Until then, `npx tsc --noEmit` keeps them type-checked.
describe("repoGroupLabel", () => {
  it("derives owner/repo from an HTTPS URL", () => {
    expect(repoGroupLabel("https://github.com/acmecorp/dashboard")).toBe(
      "acmecorp/dashboard",
    );
  });

  it("strips a trailing .git suffix", () => {
    expect(repoGroupLabel("https://github.com/forgeutah/forge-api.git")).toBe(
      "forgeutah/forge-api",
    );
  });

  it("ignores a trailing slash", () => {
    expect(repoGroupLabel("https://github.com/forgeutah/forge-web/")).toBe(
      "forgeutah/forge-web",
    );
  });

  it("strips a trailing slash that follows .git", () => {
    expect(repoGroupLabel("https://github.com/forgeutah/forge-api.git/")).toBe(
      "forgeutah/forge-api",
    );
  });

  it("ignores a query string or fragment", () => {
    expect(
      repoGroupLabel("https://github.com/acmecorp/dashboard?tab=readme"),
    ).toBe("acmecorp/dashboard");
    expect(repoGroupLabel("https://github.com/acmecorp/dashboard#top")).toBe(
      "acmecorp/dashboard",
    );
  });

  it("parses the SSH clone form", () => {
    expect(repoGroupLabel("git@github.com:acmecorp/dashboard.git")).toBe(
      "acmecorp/dashboard",
    );
  });

  it("returns the no-repository label for empty input", () => {
    expect(repoGroupLabel("")).toBe(NO_REPO_LABEL);
    expect(repoGroupLabel("   ")).toBe(NO_REPO_LABEL);
  });

  it("returns a single unparseable segment unchanged", () => {
    expect(repoGroupLabel("dashboard")).toBe("dashboard");
  });
});

describe("repoGroupKey", () => {
  it("canonicalizes HTTPS and SSH forms of the same repo to one key", () => {
    const https = repoGroupKey("https://github.com/acmecorp/dashboard");
    const ssh = repoGroupKey("git@github.com:acmecorp/dashboard.git");
    expect(https).toBe("github.com/acmecorp/dashboard");
    expect(ssh).toBe(https);
  });

  it("is case-insensitive across host and owner/repo", () => {
    expect(repoGroupKey("https://GitHub.com/AcmeCorp/Dashboard")).toBe(
      "github.com/acmecorp/dashboard",
    );
  });

  it("keeps different hosts distinct", () => {
    expect(repoGroupKey("https://github.com/acme/app")).not.toBe(
      repoGroupKey("https://gitlab.com/acme/app"),
    );
  });

  it("returns the empty key for input with no repository", () => {
    expect(repoGroupKey("")).toBe("");
    expect(repoGroupKey("   ")).toBe("");
  });
});
