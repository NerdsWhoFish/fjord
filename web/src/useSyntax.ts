import { useEffect, useState } from "react";
import type { SyntaxRequest, SyntaxResult } from "./syntax-types";
import { captureError } from "./telemetry";

const plain: SyntaxResult = { lines: [] };

export function useSyntax(request?: SyntaxRequest): SyntaxResult {
  const [state, setState] = useState<{
    request: SyntaxRequest;
    result: SyntaxResult;
  }>();
  useEffect(() => {
    if (!request) return;
    let finished = false;
    let worker: Worker;
    try {
      worker = new Worker(new URL("./syntax.worker.ts", import.meta.url), {
        type: "module",
      });
    } catch {
      captureError(
        new Error("Syntax worker could not start"),
        "syntax.highlight",
      );
      setState({ request, result: { lines: [], limited: true } });
      return;
    }
    const finish = (result: SyntaxResult) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      worker.terminate();
      setState({ request, result });
    };
    const failed = () => {
      if (finished) return;
      captureError(
        new Error("Syntax worker failed or timed out"),
        "syntax.highlight",
      );
      finish({ lines: [], limited: true });
    };
    const timer = setTimeout(failed, 3000);
    worker.onmessage = (event: MessageEvent<SyntaxResult>) =>
      finish(event.data);
    worker.onerror = failed;
    worker.postMessage(request);
    return () => {
      finished = true;
      clearTimeout(timer);
      worker.terminate();
    };
  }, [request]);
  return state && state.request === request ? state.result : plain;
}
