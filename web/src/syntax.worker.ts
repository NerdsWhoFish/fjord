import { highlightRequest } from "./syntax";
import type { SyntaxRequest, SyntaxResult } from "./syntax-types";

self.onmessage = (event: MessageEvent<SyntaxRequest>) => {
  let result: SyntaxResult;
  try {
    result = highlightRequest(event.data);
  } catch {
    result = { lines: [], limited: true };
  }
  self.postMessage(result);
};
