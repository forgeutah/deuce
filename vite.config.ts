import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";
import type { IncomingMessage } from "http";

// Local-dev forge-proxy header injector. Opt-in via VITE_DEV_FORGE_HEADERS=1
// so it never fires unless explicitly requested. When on, the Vite dev proxy
// stamps every /api and /ws request with the X-Forge-* set the backend
// expects in forge-proxy mode — useful for browser-testing the
// NotAuthorizedView path or running the SPA against a backend with
// DEUCE_AUTH_MODE=forge-proxy.
//
// Every header is configurable via env so you can flip the user's role,
// id, email, etc. without editing this file.
function buildForgeHeaders(env: Record<string, string>): Record<string, string> | null {
  if (env.VITE_DEV_FORGE_HEADERS !== "1") return null;
  return {
    "X-Forge-Proxy-Secret": env.VITE_DEV_FORGE_SECRET ?? "devsecret",
    "X-Forge-Contract-Version": env.VITE_DEV_FORGE_CONTRACT_VERSION ?? "1",
    "X-Forge-User-Id": env.VITE_DEV_FORGE_USER_ID ?? "42",
    "X-Forge-Email": env.VITE_DEV_FORGE_EMAIL ?? "alice@example.com",
    "X-Forge-Name": env.VITE_DEV_FORGE_NAME ?? "Alice",
    "X-Forge-Avatar": env.VITE_DEV_FORGE_AVATAR ?? "https://example.com/a.png",
    "X-Forge-Roles": env.VITE_DEV_FORGE_ROLES ?? "deuce",
  };
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const forgeHeaders = buildForgeHeaders(env);

  const configureProxy = forgeHeaders
    ? (proxy: import("http-proxy").Server) => {
        const stamp = (req: IncomingMessage) => {
          for (const [k, v] of Object.entries(forgeHeaders)) {
            req.headers[k.toLowerCase()] = v;
          }
        };
        // HTTP requests: rewrite the upstream request's headers before send.
        proxy.on("proxyReq", (proxyReq) => {
          for (const [k, v] of Object.entries(forgeHeaders)) {
            proxyReq.setHeader(k, v);
          }
        });
        // WebSocket upgrade: the upgrade request is a normal HTTP GET first.
        // proxyReqWs gives us the upstream request before it's sent.
        proxy.on("proxyReqWs", (proxyReq) => {
          for (const [k, v] of Object.entries(forgeHeaders)) {
            proxyReq.setHeader(k, v);
          }
        });
        // Suppress unused-param lint without changing the hook signature.
        void stamp;
      }
    : undefined;

  if (forgeHeaders) {
    console.log(
      "[vite] forge-proxy header injection ENABLED — every /api and /ws request will carry X-Forge-* headers (user_id=%s, roles=%s)",
      forgeHeaders["X-Forge-User-Id"],
      forgeHeaders["X-Forge-Roles"],
    );
  }

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: {
      host: true,
      port: 4000,
      proxy: {
        "/api": {
          target: "http://localhost:8080",
          ...(configureProxy && { configure: configureProxy }),
        },
        "/ws/terminal": {
          target: "ws://localhost:8080",
          ws: true,
          ...(configureProxy && { configure: configureProxy }),
        },
        "/ws": {
          target: "ws://localhost:8080",
          ws: true,
          ...(configureProxy && { configure: configureProxy }),
        },
      },
    },
  };
});
