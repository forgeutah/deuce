import { defineConfig, configDefaults } from "vitest/config";
import path from "path";

// Vitest reads this file in preference to vite.config.ts, so the `@` alias the
// app relies on must be redeclared here. The dev server / proxy config in
// vite.config.ts is irrelevant to tests and intentionally not carried over.
//
// jsdom environment: the pure-logic suites (reducer, visibility, string utils)
// run fine under jsdom, and it's required for the component render tests
// (react-markdown safety behavior). See docs/plans — U1.
export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup-dom.ts"],
    // Stale checkouts under .worktrees/ carry duplicate *.test.ts copies that
    // aren't part of the live tree — keep vitest's defaults and add them.
    exclude: [...configDefaults.exclude, "**/.worktrees/**"],
  },
});
