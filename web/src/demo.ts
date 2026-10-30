import type { PullRequest, ThreadComment } from "./types";

export const demoPR: PullRequest = {
  owner: "fjord-demo",
  repo: "harbor",
  number: 42,
  title: "Keep connections healthy with bounded retries",
  body: "Retry temporary connection failures with a bounded backoff. Permanent errors return immediately, and cancellation stops the retry loop.",
  url: "",
  author: "maya",
  state: "open",
  baseSHA: "a".repeat(40),
  headSHA: "b".repeat(40),
  baseRef: "main",
  headRef: "fix/connection-retries",
  files: [
    {
      path: "internal/harbor/connect.go",
      status: "modified",
      additions: 12,
      deletions: 2,
      patch:
        '@@ -7,9 +7,19 @@ type Connector interface {\n }\n \n func Connect(ctx context.Context, c Connector) error {\n-    return c.Open(ctx)\n+    for attempt := 0; attempt < 3; attempt++ {\n+        err := c.Open(ctx)\n+        if err == nil || !isTemporary(err) {\n+            return err\n+        }\n+        select {\n+        case <-ctx.Done():\n+            return ctx.Err()\n+        case <-time.After(backoff(attempt)):\n+        }\n+    }\n+    return ErrUnavailable\n }\n \n-var ErrUnavailable = errors.New("connection failed")\n+var ErrUnavailable = errors.New("retries exhausted")',
    },
    {
      path: "internal/harbor/backoff.go",
      status: "added",
      additions: 9,
      deletions: 0,
      patch:
        '@@ -0,0 +1,9 @@\n+package harbor\n+\n+import "time"\n+\n+// Bound the delay so a failing connection remains responsive.\n+func backoff(attempt int) time.Duration {\n+    delay := time.Duration(1<<attempt) * 100 * time.Millisecond\n+    return min(delay, time.Second)\n+}',
    },
    {
      path: "README.md",
      status: "modified",
      additions: 2,
      deletions: 1,
      patch:
        "@@ -1,3 +1,4 @@\n # Harbor\n \n-A small connection manager.\n+A small connection manager with bounded retries.\n+Cancellation ends the current attempt immediately.",
    },
  ],
};

export const demoSource: Record<string, string> = {
  "internal/harbor/connect.go":
    'package harbor\n\nimport ("context"; "errors"; "time")\n\ntype Connector interface {\n    Open(context.Context) error\n}\n\nfunc Connect(ctx context.Context, c Connector) error {\n    for attempt := 0; attempt < 3; attempt++ {\n        err := c.Open(ctx)\n        if err == nil || !isTemporary(err) {\n            return err\n        }\n        select {\n        case <-ctx.Done():\n            return ctx.Err()\n        case <-time.After(backoff(attempt)):\n        }\n    }\n    return ErrUnavailable\n}\n\nvar ErrUnavailable = errors.New("retries exhausted")',
  "internal/harbor/backoff.go":
    'package harbor\n\nimport "time"\n\n// Bound the delay so a failing connection remains responsive.\nfunc backoff(attempt int) time.Duration {\n    delay := time.Duration(1<<attempt) * 100 * time.Millisecond\n    return min(delay, time.Second)\n}',
  "README.md":
    "# Harbor\n\nA small connection manager with bounded retries.\nCancellation ends the current attempt immediately.",
};

export const demoThreads: ThreadComment[] = [
  {
    id: 1,
    author: "alex",
    body: "Can we check what happens when cancellation arrives during the backoff?",
    path: "internal/harbor/connect.go",
    line: 15,
    side: "RIGHT",
    url: "",
  },
];

export const demoBaseSource: Record<string, string> = {
  "internal/harbor/connect.go":
    'package harbor\n\nimport ("context"; "errors")\n\ntype Connector interface {\n    Open(context.Context) error\n}\n\nfunc Connect(ctx context.Context, c Connector) error {\n    return c.Open(ctx)\n}\n\nvar ErrUnavailable = errors.New("connection failed")',
  "README.md": "# Harbor\n\nA small connection manager.",
};
