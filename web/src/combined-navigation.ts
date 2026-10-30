import type { Method, NavigationResult } from "./types";

export async function combinedNavigation(
  methods: Method[],
  query: (method: "definition" | "references") => Promise<NavigationResult>,
): Promise<NavigationResult> {
  const requested = (["definition", "references"] as const).filter((method) =>
    methods.includes(method),
  );
  if (!requested.length)
    throw new Error("This server does not support definitions or references.");
  const responses = await Promise.allSettled(
    requested.map((method) => query(method)),
  );
  const result: NavigationResult = {
    locations: [],
    hover: "",
    definitions: [],
    references: [],
  };
  const messages: string[] = [];
  let succeeded = false;
  responses.forEach((response, index) => {
    const method = requested[index];
    const label = method === "definition" ? "Definitions" : "References";
    if (response.status === "fulfilled") {
      succeeded = true;
      result[method === "definition" ? "definitions" : "references"] =
        response.value.locations || [];
      if (response.value.message)
        messages.push(`${label}: ${response.value.message}`);
    } else {
      messages.push(
        `${label}: ${response.reason instanceof Error ? response.reason.message : "Lookup failed."}`,
      );
    }
  });
  if (!succeeded) throw new Error(messages.join(" "));
  for (const method of ["definition", "references"] as const)
    if (!methods.includes(method))
      messages.push(
        `${method === "definition" ? "Definitions" : "References"} are not supported by this server.`,
      );
  if (messages.length) result.message = messages.join(" ");
  return result;
}
