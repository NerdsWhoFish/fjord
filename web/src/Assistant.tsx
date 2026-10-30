import { useState } from "react";
import type { Selection } from "./types";

interface Props {
  selection?: Selection;
  enabled: boolean;
  demo: boolean;
  onAsk: (question: string) => Promise<string>;
}

export function Assistant({ selection, enabled, demo, onAsk }: Props) {
  const [question, setQuestion] = useState("");
  const [answer, setAnswer] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <div className="assistant-panel">
      <span className="eyebrow">A SECOND PAIR OF EYES</span>
      <h2>Ask about the code.</h2>
      <p className="muted small">
        Investigate a concern or unpack a change. You make the review decision.
      </p>
      {selection ? (
        <div className="selection-card">
          <span className="eyebrow">SELECTED CONTEXT</span>
          <code>
            {selection.path}:{selection.line}
          </code>
          <pre>{selection.code}</pre>
        </div>
      ) : (
        <p className="empty-message">
          Select a symbol in the diff to give your question context.
        </p>
      )}
      {!enabled && !demo && (
        <p className="notice">
          AI assistance is not configured on this Fjord server. Code navigation
          and GitHub reviews work independently.
        </p>
      )}
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          if (!selection || !question.trim()) return;
          setBusy(true);
          setError("");
          setAnswer("");
          try {
            setAnswer(await onAsk(question.trim()));
          } catch (error) {
            setError(
              error instanceof Error ? error.message : "AI request failed.",
            );
          } finally {
            setBusy(false);
          }
        }}
      >
        <label htmlFor="ai-question">Your question</label>
        <textarea
          id="ai-question"
          rows={5}
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder="What happens if this operation is cancelled?"
        />
        <p className="small muted">
          Sending shares your question and the selected code line with the
          configured AI provider. No code is sent automatically.
        </p>
        <button
          className="primary wide"
          disabled={
            busy || (!enabled && !demo) || !selection || !question.trim()
          }
        >
          {busy
            ? "Investigating..."
            : demo
              ? "Show demo answer"
              : "Send question"}
        </button>
      </form>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {answer && (
        <div className="answer" aria-live="polite">
          <span className="eyebrow">
            {demo ? "ILLUSTRATIVE DEMO ANSWER" : "AI RESPONSE"}
          </span>
          <div>{answer}</div>
          <p className="small muted">
            Check this against the code before acting on it.
          </p>
        </div>
      )}
    </div>
  );
}
