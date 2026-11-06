import { authenticatedFetch } from "./auth";

export async function api<T>(
  endpoint: string,
  body: unknown = {},
  signal?: AbortSignal,
): Promise<T> {
  const mutation = endpoint === "review" || endpoint === "reply";
  const unknownOutcome = () =>
    new Error(
      "Outcome unknown. Check GitHub before retrying; your review or reply may already have been sent.",
    );
  let response: Response;
  try {
    response = await authenticatedFetch(`/api/${endpoint}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal,
    });
  } catch (error) {
    if (mutation) throw unknownOutcome();
    throw error;
  }
  const data = await response.json().catch(() => null);
  if (
    mutation &&
    response.ok &&
    (typeof data?.url !== "string" ||
      !data.url.startsWith("https://github.com/"))
  )
    throw unknownOutcome();
  if (mutation && response.status >= 500 && typeof data?.error !== "string")
    throw unknownOutcome();
  if (!response.ok)
    throw new Error(
      typeof data?.error === "string"
        ? data.error
        : `Request failed (${response.status}). Try again.`,
    );
  if (data === null)
    throw new Error("The server returned an invalid response. Try again.");
  return data as T;
}
