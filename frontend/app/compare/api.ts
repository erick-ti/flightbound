// Types and request helper for POST /api/trip-comparisons. The API validates
// every field and does all arithmetic. Amounts are decimal strings in the
// chosen currency, and null means an estimate is unknown.

export type DestinationInput = {
  label: string;
  flight_estimate: string | null;
  nightly_stay_estimate: string | null;
};

export type ComparisonRequest = {
  check_in: string;
  check_out: string;
  travelers: number;
  currency: string;
  destinations: DestinationInput[];
};

export type DestinationEstimate = DestinationInput & {
  stay_estimate: string | null;
  flight_and_stay_estimate: string | null;
  complete: boolean;
  lowest_estimate: boolean;
};

export type Comparison = {
  check_in: string;
  check_out: string;
  nights: number;
  travelers: number;
  currency: string;
  all_complete: boolean;
  destinations: DestinationEstimate[];
};

export type FieldError = {
  field: string;
  message: string;
};

export type ComparisonOutcome =
  | { kind: "ok"; comparison: Comparison }
  | { kind: "invalid"; message: string; fieldErrors: FieldError[] }
  | { kind: "failed"; message: string };

const unavailable = "The comparison service is unavailable right now. Try again in a moment.";
const rejected = "The comparison request could not be processed. Reload the page and try again.";

export async function requestComparison(body: ComparisonRequest): Promise<ComparisonOutcome> {
  let response: Response;
  try {
    response = await fetch("/api/trip-comparisons", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    return { kind: "failed", message: unavailable };
  }

  const data: unknown = await response.json().catch(() => null);
  if (response.ok && isComparison(data)) {
    return { kind: "ok", comparison: data };
  }
  if (response.status === 422 && isFieldErrorBody(data)) {
    return { kind: "invalid", message: data.message, fieldErrors: data.field_errors };
  }
  if (response.status === 400 || response.status === 413) {
    return { kind: "failed", message: rejected };
  }
  return { kind: "failed", message: unavailable };
}

function isComparison(data: unknown): data is Comparison {
  return typeof data === "object" && data !== null && Array.isArray((data as Comparison).destinations);
}

function isFieldErrorBody(data: unknown): data is { message: string; field_errors: FieldError[] } {
  if (typeof data !== "object" || data === null) {
    return false;
  }
  const body = data as { message?: unknown; field_errors?: unknown };
  return typeof body.message === "string" && Array.isArray(body.field_errors);
}
