/* Deuce UI Kit — seed data (mirrors src/mocks/data/seed.ts). Plain globals. */
(function () {
  const now = Date.now();
  const mins = (m) => new Date(now - m * 60000).toISOString();
  const hrs = (h) => new Date(now - h * 3600000).toISOString();
  const days = (d) => new Date(now - d * 86400000).toISOString();

  const AGENTS = {
    coder:    { id: "agent-coder",    name: "Coder",    color: "#58a6ff", status: "idle", provider: "Anthropic", model: "Claude Sonnet 4", description: "Writes and modifies code" },
    reviewer: { id: "agent-reviewer", name: "Reviewer", color: "#BE8FFF", status: "idle", provider: "Anthropic", model: "Claude Sonnet 4", description: "Reviews code changes" },
    planner:  { id: "agent-planner",  name: "Planner",  color: "#3fb950", status: "idle", provider: "OpenAI",    model: "GPT-4o",         description: "Creates implementation plans" },
    tester:   { id: "agent-tester",   name: "Tester",   color: "#d29922", status: "idle", provider: "Anthropic", model: "Claude Sonnet 4", description: "Writes and runs tests" },
    designer: { id: "agent-designer", name: "Designer", color: "#f778ba", status: "idle", provider: "OpenAI",    model: "GPT-4o",         description: "UI/UX suggestions" },
  };
  const agent = (k) => ({ ...AGENTS[k] });

  const USERS = {
    clint: { id: "current-user", name: "Clint Berry",    email: "clint@forge.dev",  avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Clint",  status: "online" },
    sarah: { id: "user-2",       name: "Sarah Chen",     email: "sarah@forge.dev",  avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Sarah",  status: "online" },
    mike:  { id: "user-3",       name: "Mike Rodriguez", email: "mike@forge.dev",   avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Mike",   status: "offline" },
    alex:  { id: "user-4",       name: "Alex Park",      email: "alex@acme.co",     avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Alex",   status: "online" },
    jordan:{ id: "user-5",       name: "Jordan Lee",     email: "jordan@acme.co",   avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Jordan", status: "offline" },
  };

  const projects = [
    { id: "proj-1", name: "forge-api", teamId: "team-1" },
    { id: "proj-2", name: "forge-web", teamId: "team-1" },
    { id: "proj-3", name: "acme-dashboard", teamId: "team-2" },
  ];

  const sessions = [
    {
      id: "sess-1", name: "auth-module", description: "JWT validation and refresh-token flow for the v2 API",
      projectId: "proj-1", status: "active", workspaceStatus: "ready", unreadCount: 3,
      agents: [agent("coder"), agent("reviewer"), agent("tester")], members: [USERS.clint, USERS.sarah],
      lastActivityAt: mins(5),
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
- Token refresh flow works end-to-end`,
    },
    {
      id: "sess-2", name: "api-rate-limiting", description: "Token-bucket rate limiter via Redis, per-endpoint config",
      projectId: "proj-1", status: "active", workspaceStatus: "ready", unreadCount: 0,
      agents: [agent("coder"), agent("planner")], members: [USERS.clint, USERS.mike],
      lastActivityAt: hrs(2),
      planContent: `# Rate Limiting Plan

## Approach
- Token bucket algorithm
- Per-user rate limits via Redis
- Configurable limits per endpoint

## TODO
- [ ] Implement rate limiter middleware
- [ ] Add Redis integration
- [ ] Configure per-route limits`,
    },
    {
      id: "sess-3", name: "homepage-redesign", description: "Marketing homepage refresh with the new hero animation",
      projectId: "proj-2", status: "active", workspaceStatus: "ready", unreadCount: 1,
      agents: [agent("coder"), agent("designer")], members: [USERS.clint, USERS.sarah],
      lastActivityAt: mins(30), planContent: "",
    },
    {
      id: "sess-4", name: "ci-pipeline", description: "",
      projectId: "proj-1", status: "paused", workspaceStatus: "suspended", unreadCount: 0,
      agents: [agent("coder")], members: [USERS.mike], lastActivityAt: days(2), planContent: "",
    },
    {
      id: "sess-6", name: "dashboard-charts", description: "Recharts integration for the customer analytics dashboard",
      projectId: "proj-3", status: "active", workspaceStatus: "starting", unreadCount: 2,
      agents: [agent("coder"), agent("reviewer"), agent("tester")], members: [USERS.alex, USERS.jordan],
      lastActivityAt: mins(15), planContent: "",
    },
  ];

  const messages = {
    "sess-1": [
      { id: "m1", authorId: "current-user", authorType: "human", content: "Let's start working on the auth module. We need JWT validation with token expiration checking.", createdAt: hrs(4) },
      { id: "m2", authorId: "user-2", authorType: "human", content: "Agreed. I've updated the plan with acceptance criteria. @Planner can you break this down?", createdAt: hrs(3.5) },
      { id: "m3", authorId: "agent-coder", authorType: "agent", content: "I've set up the auth middleware and user model. The base structure is ready for JWT integration.", createdAt: hrs(3),
        expandable: [{ title: "changes", summary: "internal/auth/middleware.go (+45 -0)", lines: [
          ["add","+func Middleware(next http.Handler) http.Handler {"],
          ["add","+  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {"],
          ["add","+    token := extractToken(r)"],
          ["add","+    if token == \"\" {"],
          ["add","+      http.Error(w, \"unauthorized\", http.StatusUnauthorized)"],
          ["add","+      return"],
          ["add","+    }"],
          ["add","+    next.ServeHTTP(w, r)"],
          ["add","+  })"],
          ["add","+}"],
        ]}] },
      { id: "m4", authorId: "current-user", authorType: "human", content: "@Coder now add token expiration checking to the Validate function", createdAt: hrs(2) },
      { id: "m5", authorId: "agent-coder", authorType: "agent", content: "I've updated the Validate function with token expiration checking. The implementation parses JWT claims and checks the expiry timestamp.", createdAt: hrs(1.5),
        expandable: [{ title: "changes", summary: "internal/auth/validate.go (+12 -3)", lines: [
          ["ctx","@@ -42,8 +42,19 @@ func Validate(token string) error"],
          ["ctx","   }"],
          ["add","+  claims, err := ParseClaims(token)"],
          ["add","+  if err != nil {"],
          ["add","+    return fmt.Errorf(\"parse claims: %w\", err)"],
          ["add","+  }"],
          ["add","+  if claims.ExpiresAt.Before(time.Now()) {"],
          ["add","+    return ErrTokenExpired"],
          ["add","+  }"],
          ["ctx","   return nil"],
          ["ctx"," }"],
        ]}] },
      { id: "m6", authorId: "current-user", authorType: "human", content: "@Tester can you write tests for the new expiration logic?", createdAt: hrs(1) },
      { id: "m7", authorId: "agent-tester", authorType: "agent", content: "Tests are written and passing. I've covered valid tokens, expired tokens, invalid format, and empty input.", createdAt: mins(45),
        expandable: [{ title: "test results", summary: "4/4 passing", lines: [
          ["ctx","=== RUN   TestValidate"],
          ["add","--- PASS: TestValidate (0.00s)"],
          ["ctx","=== RUN   TestValidateExpired"],
          ["add","--- PASS: TestValidateExpired (0.00s)"],
          ["ctx","=== RUN   TestValidateInvalid"],
          ["add","--- PASS: TestValidateInvalid (0.00s)"],
          ["ctx","=== RUN   TestValidateEmpty"],
          ["add","--- PASS: TestValidateEmpty (0.00s)"],
          ["add","PASS"],
          ["ctx","ok  forge-api/internal/auth  0.003s"],
        ]}] },
      { id: "m8", authorId: "user-2", authorType: "human", content: "@Reviewer can you review all the auth changes?", createdAt: mins(20) },
      { id: "m9", authorId: "agent-reviewer", authorType: "agent", content: "Code looks good overall. Two suggestions:\n\n1. Consider using `errors.Is()` for error comparison in middleware\n2. Add context to the JWT parsing errors for better debugging\n\nThe token expiration logic is correctly implemented. Tests cover the main cases well.", createdAt: mins(10) },
    ],
    "sess-2": [
      { id: "m20", authorId: "current-user", authorType: "human", content: "We need rate limiting on the API endpoints. @Planner can you outline an approach?", createdAt: hrs(6) },
      { id: "m21", authorId: "agent-planner", authorType: "agent", content: "Here's my recommended approach for rate limiting:\n\n**Algorithm:** Token bucket (allows bursts while maintaining average rate)\n\n**Storage:** Redis for distributed rate limit state\n\n**Configuration:**\n- Default: 100 requests/minute per user\n- Auth endpoints: 10 requests/minute (stricter)\n- Public endpoints: 30 requests/minute per IP\n\n**Implementation phases:**\n1. Core rate limiter with in-memory store (for dev)\n2. Redis adapter\n3. Per-route configuration\n4. Rate limit headers in responses", createdAt: hrs(5.5) },
      { id: "m22", authorId: "user-3", authorType: "human", content: "Looks good. Make sure we add X-RateLimit headers in the response.", createdAt: hrs(5) },
    ],
    "sess-3": [
      { id: "m30", authorId: "current-user", authorType: "human", content: "We need to redesign the homepage. @Designer any ideas for improving the layout?", createdAt: hrs(8) },
      { id: "m31", authorId: "agent-designer", authorType: "agent", content: "Here are my recommendations for the homepage redesign:\n\n1. **Hero section:** Move the CTA above the fold with a clear value proposition\n2. **Social proof:** Add a customer logos bar below the hero\n3. **Features grid:** Replace the feature list with a 3-column card layout\n4. **Dark/light contrast:** Use alternating section backgrounds for visual rhythm\n\nThe current layout has too much text density. Let's prioritize visual hierarchy.", createdAt: hrs(7) },
    ],
    "sess-4": [],
    "sess-6": [
      { id: "m60", authorId: "user-4", authorType: "human", content: "Starting work on the dashboard charts. We need bar charts, line charts, and a pie chart for the overview.", createdAt: hrs(3) },
      { id: "m61", authorId: "user-5", authorType: "human", content: "@Coder can you set up Recharts and create a basic bar chart component?", createdAt: hrs(2.5) },
    ],
  };

  const activities = {
    "sess-1": [
      { id: "a1", type: "agent-action", description: "Reviewer completed code review", timestamp: mins(10) },
      { id: "a2", type: "test-run", description: "4/4 tests passing", timestamp: mins(45) },
      { id: "a3", type: "file-change", description: "validate.go", timestamp: hrs(1.5), add: "12", del: "3" },
      { id: "a4", type: "file-change", description: "middleware.go", timestamp: hrs(3), add: "45", del: "0" },
      { id: "a5", type: "commit", description: "a1b2c3d Add token expiration check", timestamp: hrs(1) },
    ],
    "sess-2": [
      { id: "a20", type: "agent-action", description: "Planner created implementation plan", timestamp: hrs(5.5) },
    ],
    "sess-3": [], "sess-4": [], "sess-6": [],
  };

  const files = {
    "sess-1": [
      { name: "internal", type: "dir", open: true, children: [
        { name: "auth", type: "dir", open: true, children: [
          { name: "middleware.go", type: "file", git: "M", path: "internal/auth/middleware.go" },
          { name: "validate.go", type: "file", git: "M", path: "internal/auth/validate.go" },
          { name: "validate_test.go", type: "file", git: "A", path: "internal/auth/validate_test.go" },
        ]},
        { name: "api", type: "dir", open: false, children: [
          { name: "router.go", type: "file", path: "internal/api/router.go" },
        ]},
      ]},
      { name: "cmd/server/main.go", type: "file", path: "cmd/server/main.go" },
      { name: "go.mod", type: "file", path: "go.mod" },
      { name: "README.md", type: "file", path: "README.md" },
    ],
  };

  const fileContents = {
    "internal/auth/validate.go": [
      ["kw","package"," auth"], [],
      ["kw","import"," ("],
      ["str","  \"errors\""], ["str","  \"fmt\""], ["str","  \"time\""],
      [")"], [],
      ["kw","var"," ("],
      ["  ErrInvalidFormat = ",["fn","errors.New"],["str","(\"invalid token format\")"]],
      ["  ErrTokenExpired  = ",["fn","errors.New"],["str","(\"token expired\")"]],
      [")"], [],
      ["kw","func ",["fn","Validate"],"(token ",["ty","string"],") ",["ty","error"]," {"],
      ["  ",["kw","if"]," token == ",["str","\"\""]," {"],
      ["    ",["kw","return"]," ErrInvalidFormat"],
      ["  }"], [],
      ["  ",["cm","// Check token expiration"]],
      ["  claims, err := ",["fn","ParseClaims"],"(token)"],
      ["  ",["kw","if"]," err != ",["kw","nil"]," {"],
      ["    ",["kw","return fmt"],".",["fn","Errorf"],"(",["str","\"parse claims: %w\""],", err)"],
      ["  }"], [],
      ["  ",["kw","if"]," claims.ExpiresAt.",["fn","Before"],"(time.",["fn","Now"],"()) {"],
      ["    ",["kw","return"]," ErrTokenExpired"],
      ["  }"], [],
      ["  ",["kw","return nil"]],
      ["}"],
    ],
  };

  const terminalLines = [
    ["prompt","vscode ➜ ","path","/workspaces/forge-api"," (auth-module) $ ","go test ./internal/auth/..."],
    ["out","ok  \tforge-api/internal/auth\t0.004s"],
    ["prompt","vscode ➜ ","path","/workspaces/forge-api"," (auth-module) $ ","git status -s"],
    ["out"," M internal/auth/middleware.go"],
    ["out"," M internal/auth/validate.go"],
    ["out","?? internal/auth/validate_test.go"],
    ["prompt","vscode ➜ ","path","/workspaces/forge-api"," (auth-module) $ "],
  ];

  const logLines = [
    ["info","[devpod] creating workspace 'auth-module'..."],
    ["out","[devpod] provider: docker"],
    ["out","[devpod] pulling image mcr.microsoft.com/devcontainers/go:1.22"],
    ["ok","[devpod] image ready"],
    ["out","[devpod] starting container..."],
    ["ok","[devpod] container started (id 9f3ac2)"],
    ["out","[devpod] running postCreateCommand: go mod download"],
    ["ok","[devpod] workspace ready in 18.2s"],
    ["info","[agent] Coder connected via devpod ssh"],
  ];

  window.DEUCE = { AGENTS, USERS, projects, sessions, messages, activities, files, fileContents, terminalLines, logLines };
})();
