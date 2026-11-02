import { useEffect, useRef, useState } from "react";
import type { ReviewDraft } from "./ReviewPanel";
import type { PullRequest, Selection } from "./types";
import {
  startReviewSession,
  bindSessionLifecycle,
  type ActionContext,
  type SessionAction,
  type SessionState,
} from "./session-client";
import { captureError } from "./telemetry";

export function useReviewSession({
  pr,
  demo,
  draft,
  getDraft,
  selection,
  onAction,
}: {
  pr?: PullRequest;
  demo: boolean;
  draft: ReviewDraft;
  getDraft?: () => ReviewDraft;
  selection?: Selection;
  onAction: (action: SessionAction, context: ActionContext) => Promise<void>;
}) {
  const key = pr
    ? `${pr.owner}/${pr.repo}/${pr.number}/${pr.headSHA}/${pr.baseSHA}`
    : "";
  const latest = useRef({ key, draft, getDraft, selection, onAction });
  latest.current = { key, draft, getDraft, selection, onAction };
  const runner = useRef<ReturnType<typeof startReviewSession>>(undefined);
  const [state, setState] = useState<SessionState>({ status: "disabled" });
  useEffect(() => {
    if (!pr || demo) {
      setState({ status: "disabled" });
      return;
    }
    const session = bindSessionLifecycle(window, () =>
      startReviewSession({
        pr,
        snapshot: () => {
          if (latest.current.key !== key)
            throw new Error("The pull request changed.");
          return {
            draft: latest.current.getDraft?.() || latest.current.draft,
            selection: latest.current.selection
              ? {
                  path: latest.current.selection.path,
                  sha: latest.current.selection.sha,
                  line: latest.current.selection.line,
                  character: latest.current.selection.character,
                }
              : undefined,
          };
        },
        onAction: (action, context) => {
          const isCurrent = () =>
            context.isCurrent() && latest.current.key === key;
          if (!isCurrent())
            return Promise.reject(new Error("The pull request changed."));
          return latest.current.onAction(action, { ...context, isCurrent });
        },
        onState: (next) => {
          if (latest.current.key === key) setState(next);
        },
        onError: (error) => captureError(error, "mcp.session"),
      }),
    );
    runner.current = session;
    return () => {
      session.stop();
      if (runner.current === session) runner.current = undefined;
    };
  }, [key, demo]);
  return { ...state, retry: () => runner.current?.retry() };
}
