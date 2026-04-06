import type {
  Team,
  Project,
  Session,
  Message,
  Agent,
  User,
  FileNode,
  ActivityItem,
} from "@/types";

// ── Users ────────────────────────────────────────────────────────

export const users: User[] = [
  {
    id: "current-user",
    name: "Clint Berry",
    email: "clint@forge.dev",
    avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Clint",
    status: "online",
  },
  {
    id: "user-2",
    name: "Sarah Chen",
    email: "sarah@forge.dev",
    avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Sarah",
    status: "online",
  },
  {
    id: "user-3",
    name: "Mike Rodriguez",
    email: "mike@forge.dev",
    avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Mike",
    status: "offline",
  },
  {
    id: "user-4",
    name: "Alex Park",
    email: "alex@acme.co",
    avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Alex",
    status: "online",
  },
  {
    id: "user-5",
    name: "Jordan Lee",
    email: "jordan@acme.co",
    avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Jordan",
    status: "offline",
  },
];

// ── Agents ───────────────────────────────────────────────────────

export const agentPresets: Agent[] = [
  {
    id: "agent-coder",
    name: "Coder",
    role: "coder",
    color: "#58a6ff",
    colorMuted: "#0c2d6b",
    status: "idle",
    provider: "Anthropic",
    model: "Claude Sonnet 4",
    description: "Writes and modifies code",
  },
  {
    id: "agent-reviewer",
    name: "Reviewer",
    role: "reviewer",
    color: "#BE8FFF",
    colorMuted: "#3c1e70",
    status: "idle",
    provider: "Anthropic",
    model: "Claude Sonnet 4",
    description: "Reviews code changes",
  },
  {
    id: "agent-planner",
    name: "Planner",
    role: "planner",
    color: "#3fb950",
    colorMuted: "#033a16",
    status: "idle",
    provider: "OpenAI",
    model: "GPT-4o",
    description: "Creates implementation plans",
  },
  {
    id: "agent-tester",
    name: "Tester",
    role: "tester",
    color: "#d29922",
    colorMuted: "#4b2900",
    status: "idle",
    provider: "Anthropic",
    model: "Claude Sonnet 4",
    description: "Writes and runs tests",
  },
  {
    id: "agent-designer",
    name: "Designer",
    role: "designer",
    color: "#f778ba",
    colorMuted: "#5e103e",
    status: "idle",
    provider: "OpenAI",
    model: "GPT-4o",
    description: "UI/UX suggestions",
  },
];

function getAgent(id: string): Agent {
  return { ...agentPresets.find((a) => a.id === id)! };
}

// ── Teams ────────────────────────────────────────────────────────

export const teams: Team[] = [
  {
    id: "team-1",
    name: "Forge Utah",
    slug: "forge-utah",
    members: [users[0], users[1], users[2]],
  },
  {
    id: "team-2",
    name: "Acme Corp",
    slug: "acme-corp",
    members: [users[3], users[4]],
  },
];

// ── Projects ─────────────────────────────────────────────────────

export const projects: Project[] = [
  {
    id: "proj-1",
    name: "forge-api",
    repoUrl: "https://github.com/forgeutah/forge-api",
    teamId: "team-1",
  },
  {
    id: "proj-2",
    name: "forge-web",
    repoUrl: "https://github.com/forgeutah/forge-web",
    teamId: "team-1",
  },
  {
    id: "proj-3",
    name: "acme-dashboard",
    repoUrl: "https://github.com/acmecorp/dashboard",
    teamId: "team-2",
  },
];

// ── Sessions ─────────────────────────────────────────────────────

const now = new Date();
const minutesAgo = (m: number) => new Date(now.getTime() - m * 60000).toISOString();
const hoursAgo = (h: number) => new Date(now.getTime() - h * 3600000).toISOString();
const daysAgo = (d: number) => new Date(now.getTime() - d * 86400000).toISOString();

export const sessions: Session[] = [
  {
    id: "sess-1",
    name: "auth-module",
    projectId: "proj-1",
    status: "active",
    agents: [getAgent("agent-coder"), getAgent("agent-reviewer"), getAgent("agent-tester")],
    members: [users[0], users[1]],
    unreadCount: 3,
    createdAt: daysAgo(2),
    lastActivityAt: minutesAgo(5),
    workspaceStatus: "ready",
    planContent: `# Auth Module Plan

## Goals
- [ ] Implement JWT token validation
- [ ] Add token expiration checks
- [x] Set up auth middleware
- [x] Create user model

## Technical Notes
- Using \`golang-jwt/jwt/v5\` for JWT parsing
- Token expiry window: 24 hours
- Refresh tokens stored in Redis

## Acceptance Criteria
- All endpoints behind auth middleware return 401 without valid token
- Expired tokens are rejected with appropriate error message
- Token refresh flow works end-to-end
`,
  },
  {
    id: "sess-2",
    name: "api-rate-limiting",
    projectId: "proj-1",
    status: "active",
    agents: [getAgent("agent-coder"), getAgent("agent-planner")],
    members: [users[0], users[2]],
    unreadCount: 0,
    createdAt: daysAgo(1),
    lastActivityAt: hoursAgo(2),
    workspaceStatus: "ready",
    planContent: `# Rate Limiting Plan

## Approach
- Token bucket algorithm
- Per-user rate limits via Redis
- Configurable limits per endpoint

## TODO
- [ ] Implement rate limiter middleware
- [ ] Add Redis integration
- [ ] Configure per-route limits
`,
  },
  {
    id: "sess-3",
    name: "homepage-redesign",
    projectId: "proj-2",
    status: "active",
    agents: [getAgent("agent-coder"), getAgent("agent-designer")],
    members: [users[0], users[1]],
    unreadCount: 1,
    createdAt: daysAgo(3),
    lastActivityAt: minutesAgo(30),
    workspaceStatus: "ready",
    planContent: "",
  },
  {
    id: "sess-4",
    name: "ci-pipeline",
    projectId: "proj-1",
    status: "paused",
    agents: [getAgent("agent-coder")],
    members: [users[2]],
    unreadCount: 0,
    createdAt: daysAgo(5),
    lastActivityAt: daysAgo(2),
    workspaceStatus: "suspended",
    planContent: "",
  },
  {
    id: "sess-5",
    name: "onboarding-flow",
    projectId: "proj-2",
    status: "archived",
    agents: [getAgent("agent-coder"), getAgent("agent-planner")],
    members: [users[0], users[1]],
    unreadCount: 0,
    createdAt: daysAgo(14),
    lastActivityAt: daysAgo(7),
    workspaceStatus: "suspended",
    planContent: "",
  },
  {
    id: "sess-6",
    name: "dashboard-charts",
    projectId: "proj-3",
    status: "active",
    agents: [getAgent("agent-coder"), getAgent("agent-reviewer"), getAgent("agent-tester")],
    members: [users[3], users[4]],
    unreadCount: 2,
    createdAt: daysAgo(1),
    lastActivityAt: minutesAgo(15),
    workspaceStatus: "starting",
    planContent: "",
  },
];

// ── Messages ─────────────────────────────────────────────────────

export const messagesBySession: Record<string, Message[]> = {
  "sess-1": [
    {
      id: "msg-1",
      sessionId: "sess-1",
      authorId: "current-user",
      authorType: "human",
      content: "Let's start working on the auth module. We need JWT validation with token expiration checking.",
      mentions: [],
      createdAt: hoursAgo(4),
      status: "sent",
    },
    {
      id: "msg-2",
      sessionId: "sess-1",
      authorId: "user-2",
      authorType: "human",
      content: "Agreed. I've updated the plan with acceptance criteria. @Planner can you break this down?",
      mentions: ["agent-planner"],
      createdAt: hoursAgo(3.5),
      status: "sent",
    },
    {
      id: "msg-3",
      sessionId: "sess-1",
      authorId: "agent-coder",
      authorType: "agent",
      content: "I've set up the auth middleware and user model. The base structure is ready for JWT integration.",
      expandableContent: [
        {
          type: "diff",
          title: "changes",
          summary: "internal/auth/middleware.go (+45 -0)",
          content: `+package auth
+
+import (
+  "net/http"
+  "strings"
+)
+
+func Middleware(next http.Handler) http.Handler {
+  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+    token := extractToken(r)
+    if token == "" {
+      http.Error(w, "unauthorized", http.StatusUnauthorized)
+      return
+    }
+
+    if err := Validate(token); err != nil {
+      http.Error(w, err.Error(), http.StatusUnauthorized)
+      return
+    }
+
+    next.ServeHTTP(w, r)
+  })
+}
+
+func extractToken(r *http.Request) string {
+  auth := r.Header.Get("Authorization")
+  if strings.HasPrefix(auth, "Bearer ") {
+    return strings.TrimPrefix(auth, "Bearer ")
+  }
+  return ""
+}`,
        },
      ],
      mentions: [],
      createdAt: hoursAgo(3),
      status: "sent",
    },
    {
      id: "msg-4",
      sessionId: "sess-1",
      authorId: "current-user",
      authorType: "human",
      content: "@Coder now add token expiration checking to the Validate function",
      mentions: ["agent-coder"],
      createdAt: hoursAgo(2),
      status: "sent",
    },
    {
      id: "msg-5",
      sessionId: "sess-1",
      authorId: "agent-coder",
      authorType: "agent",
      content: "I've updated the Validate function with token expiration checking. The implementation parses JWT claims and checks the expiry timestamp.",
      expandableContent: [
        {
          type: "diff",
          title: "changes",
          summary: "internal/auth/validate.go (+12 -3)",
          content: `@@ -42,8 +42,19 @@ func Validate(token string) error {
     return ErrInvalidFormat
   }

+  // Check token expiration
+  claims, err := ParseClaims(token)
+  if err != nil {
+    return fmt.Errorf("parse claims: %w", err)
+  }
+
+  if claims.ExpiresAt.Before(time.Now()) {
+    return ErrTokenExpired
+  }
+
   return nil
 }`,
        },
      ],
      mentions: [],
      createdAt: hoursAgo(1.5),
      status: "sent",
    },
    {
      id: "msg-6",
      sessionId: "sess-1",
      authorId: "current-user",
      authorType: "human",
      content: "@Tester can you write tests for the new expiration logic?",
      mentions: ["agent-tester"],
      createdAt: hoursAgo(1),
      status: "sent",
    },
    {
      id: "msg-7",
      sessionId: "sess-1",
      authorId: "agent-tester",
      authorType: "agent",
      content: "Tests are written and passing. I've covered valid tokens, expired tokens, invalid format, and empty input.",
      expandableContent: [
        {
          type: "test-results",
          title: "test results",
          summary: "4/4 passing",
          content: `=== RUN   TestValidate
--- PASS: TestValidate (0.00s)
=== RUN   TestValidateExpired
--- PASS: TestValidateExpired (0.00s)
=== RUN   TestValidateInvalid
--- PASS: TestValidateInvalid (0.00s)
=== RUN   TestValidateEmpty
--- PASS: TestValidateEmpty (0.00s)
PASS
ok  	forge-api/internal/auth	0.003s`,
        },
      ],
      mentions: [],
      createdAt: minutesAgo(45),
      status: "sent",
    },
    {
      id: "msg-8",
      sessionId: "sess-1",
      authorId: "user-2",
      authorType: "human",
      content: "@Reviewer can you review all the auth changes?",
      mentions: ["agent-reviewer"],
      createdAt: minutesAgo(20),
      status: "sent",
    },
    {
      id: "msg-9",
      sessionId: "sess-1",
      authorId: "agent-reviewer",
      authorType: "agent",
      content: "Code looks good overall. Two suggestions:\n\n1. Consider using `errors.Is()` for error comparison in middleware\n2. Add context to the JWT parsing errors for better debugging\n\nThe token expiration logic is correctly implemented. Tests cover the main cases well.",
      mentions: [],
      createdAt: minutesAgo(10),
      status: "sent",
    },
  ],
  "sess-2": [
    {
      id: "msg-20",
      sessionId: "sess-2",
      authorId: "current-user",
      authorType: "human",
      content: "We need rate limiting on the API endpoints. @Planner can you outline an approach?",
      mentions: ["agent-planner"],
      createdAt: hoursAgo(6),
      status: "sent",
    },
    {
      id: "msg-21",
      sessionId: "sess-2",
      authorId: "agent-planner",
      authorType: "agent",
      content: "Here's my recommended approach for rate limiting:\n\n**Algorithm:** Token bucket (allows bursts while maintaining average rate)\n\n**Storage:** Redis for distributed rate limit state\n\n**Configuration:**\n- Default: 100 requests/minute per user\n- Auth endpoints: 10 requests/minute (stricter)\n- Public endpoints: 30 requests/minute per IP\n\n**Implementation phases:**\n1. Core rate limiter with in-memory store (for dev)\n2. Redis adapter\n3. Per-route configuration\n4. Rate limit headers in responses",
      mentions: [],
      createdAt: hoursAgo(5.5),
      status: "sent",
    },
    {
      id: "msg-22",
      sessionId: "sess-2",
      authorId: "user-3",
      authorType: "human",
      content: "Looks good. Make sure we add X-RateLimit headers in the response.",
      mentions: [],
      createdAt: hoursAgo(5),
      status: "sent",
    },
  ],
  "sess-3": [
    {
      id: "msg-30",
      sessionId: "sess-3",
      authorId: "current-user",
      authorType: "human",
      content: "We need to redesign the homepage. @Designer any ideas for improving the layout?",
      mentions: ["agent-designer"],
      createdAt: hoursAgo(8),
      status: "sent",
    },
    {
      id: "msg-31",
      sessionId: "sess-3",
      authorId: "agent-designer",
      authorType: "agent",
      content: "Here are my recommendations for the homepage redesign:\n\n1. **Hero section:** Move the CTA above the fold with a clear value proposition\n2. **Social proof:** Add a customer logos bar below the hero\n3. **Features grid:** Replace the feature list with a 3-column card layout\n4. **Dark/light contrast:** Use alternating section backgrounds for visual rhythm\n\nThe current layout has too much text density. Let's prioritize visual hierarchy.",
      mentions: [],
      createdAt: hoursAgo(7),
      status: "sent",
    },
  ],
  "sess-6": [
    {
      id: "msg-60",
      sessionId: "sess-6",
      authorId: "user-4",
      authorType: "human",
      content: "Starting work on the dashboard charts. We need bar charts, line charts, and a pie chart for the overview.",
      mentions: [],
      createdAt: hoursAgo(3),
      status: "sent",
    },
    {
      id: "msg-61",
      sessionId: "sess-6",
      authorId: "user-5",
      authorType: "human",
      content: "@Coder can you set up Recharts and create a basic bar chart component?",
      mentions: ["agent-coder"],
      createdAt: hoursAgo(2.5),
      status: "sent",
    },
  ],
};

// ── Activities ───────────────────────────────────────────────────

export const activitiesBySession: Record<string, ActivityItem[]> = {
  "sess-1": [
    {
      id: "act-1",
      sessionId: "sess-1",
      type: "agent-action",
      description: "Reviewer completed code review",
      timestamp: minutesAgo(10),
      agentId: "agent-reviewer",
    },
    {
      id: "act-2",
      sessionId: "sess-1",
      type: "test-run",
      description: "4/4 tests passing",
      timestamp: minutesAgo(45),
      agentId: "agent-tester",
    },
    {
      id: "act-3",
      sessionId: "sess-1",
      type: "file-change",
      description: "validate.go",
      timestamp: hoursAgo(1.5),
      agentId: "agent-coder",
      metadata: { additions: "12", deletions: "3" },
    },
    {
      id: "act-4",
      sessionId: "sess-1",
      type: "file-change",
      description: "middleware.go",
      timestamp: hoursAgo(3),
      agentId: "agent-coder",
      metadata: { additions: "45", deletions: "0" },
    },
    {
      id: "act-5",
      sessionId: "sess-1",
      type: "commit",
      description: "a1b2c3d Add token expiration check",
      timestamp: hoursAgo(1),
    },
  ],
  "sess-2": [
    {
      id: "act-20",
      sessionId: "sess-2",
      type: "agent-action",
      description: "Planner created implementation plan",
      timestamp: hoursAgo(5.5),
      agentId: "agent-planner",
    },
  ],
};

// ── File Trees ───────────────────────────────────────────────────

export const fileTreesBySession: Record<string, FileNode[]> = {
  "sess-1": [
    {
      id: "f-1",
      name: "cmd",
      path: "cmd",
      type: "directory",
      children: [
        {
          id: "f-2",
          name: "server",
          path: "cmd/server",
          type: "directory",
          children: [
            {
              id: "f-3",
              name: "main.go",
              path: "cmd/server/main.go",
              type: "file",
              language: "go",
              content: `package main

import (
	"log"
	"net/http"

	"forge-api/internal/api"
	"forge-api/internal/auth"
)

func main() {
	mux := api.NewRouter()
	handler := auth.Middleware(mux)

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}`,
            },
          ],
        },
      ],
    },
    {
      id: "f-10",
      name: "internal",
      path: "internal",
      type: "directory",
      children: [
        {
          id: "f-11",
          name: "auth",
          path: "internal/auth",
          type: "directory",
          children: [
            {
              id: "f-12",
              name: "middleware.go",
              path: "internal/auth/middleware.go",
              type: "file",
              language: "go",
              modifiedBy: "agent-coder",
              content: `package auth

import (
	"net/http"
	"strings"
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if err := Validate(token); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}`,
            },
            {
              id: "f-13",
              name: "validate.go",
              path: "internal/auth/validate.go",
              type: "file",
              language: "go",
              modifiedBy: "agent-coder",
              content: `package auth

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidFormat = errors.New("invalid token format")
	ErrTokenExpired  = errors.New("token expired")
)

func Validate(token string) error {
	if token == "" {
		return ErrInvalidFormat
	}

	claims, err := ParseClaims(token)
	if err != nil {
		return fmt.Errorf("parse claims: %w", err)
	}

	if claims.ExpiresAt.Before(time.Now()) {
		return ErrTokenExpired
	}

	return nil
}`,
            },
            {
              id: "f-14",
              name: "validate_test.go",
              path: "internal/auth/validate_test.go",
              type: "file",
              language: "go",
              modifiedBy: "agent-tester",
              content: `package auth_test

import (
	"testing"
	"forge-api/internal/auth"
)

func TestValidate(t *testing.T) {
	token := createValidToken(t)
	if err := auth.Validate(token); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateExpired(t *testing.T) {
	token := createExpiredToken(t)
	err := auth.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateInvalid(t *testing.T) {
	err := auth.Validate("not-a-real-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestValidateEmpty(t *testing.T) {
	err := auth.Validate("")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}`,
            },
          ],
        },
        {
          id: "f-20",
          name: "api",
          path: "internal/api",
          type: "directory",
          children: [
            {
              id: "f-21",
              name: "router.go",
              path: "internal/api/router.go",
              type: "file",
              language: "go",
              content: `package api

import "net/http"

func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /api/users", usersHandler)
	return mux
}`,
            },
          ],
        },
      ],
    },
    {
      id: "f-30",
      name: "go.mod",
      path: "go.mod",
      type: "file",
      language: "go",
      content: `module forge-api

go 1.22

require (
	github.com/golang-jwt/jwt/v5 v5.2.1
)`,
    },
    {
      id: "f-31",
      name: "README.md",
      path: "README.md",
      type: "file",
      language: "markdown",
      content: `# forge-api

Backend API for Forge platform.

## Getting Started

\`\`\`bash
go run cmd/server/main.go
\`\`\``,
    },
  ],
};

// ── Initialize Store ─────────────────────────────────────────────

export function seedStore(store: {
  setTeams: (t: Team[]) => void;
  setProjects: (p: Project[]) => void;
  setSessions: (s: Session[]) => void;
  setMessages: (id: string, m: Message[]) => void;
  setActivities: (id: string, a: ActivityItem[]) => void;
  setFileTrees: (id: string, f: FileNode[]) => void;
  setActiveSession: (id: string) => void;
}) {
  store.setTeams(teams);
  store.setProjects(projects);
  store.setSessions(sessions);

  for (const [sessionId, msgs] of Object.entries(messagesBySession)) {
    store.setMessages(sessionId, msgs);
  }

  for (const [sessionId, acts] of Object.entries(activitiesBySession)) {
    store.setActivities(sessionId, acts);
  }

  for (const [sessionId, files] of Object.entries(fileTreesBySession)) {
    store.setFileTrees(sessionId, files);
  }

  // Default to first active session
  store.setActiveSession("sess-1");
}
