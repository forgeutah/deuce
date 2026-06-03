import { describe, expect, it } from "vitest";
import { NO_REPO_LABEL, repoGroupLabel } from "./repo";

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
