"use client";

import { useState, type ReactNode, type SubmitEvent } from "react";
import {
  requestComparison,
  type Comparison,
  type ComparisonRequest,
  type DestinationEstimate,
} from "./api";
import styles from "./TripComparison.module.css";

// Currency codes the API accepts. The API validates the choice and owns each
// currency's number of decimal places.
const CURRENCIES = [
  "AUD", "BRL", "CAD", "CHF", "CNY", "CZK", "DKK", "EUR", "GBP", "HKD",
  "HUF", "IDR", "ILS", "INR", "ISK", "JPY", "KRW", "MXN", "MYR", "NOK",
  "NZD", "PHP", "PLN", "RON", "SEK", "SGD", "THB", "TRY", "USD", "ZAR",
];
const MAX_TRAVELERS = 20;
const MIN_DESTINATIONS = 2;
const MAX_DESTINATIONS = 3;

type DestinationDraft = {
  label: string;
  flight: string;
  nightly: string;
};

type Draft = {
  checkIn: string;
  checkOut: string;
  travelers: number;
  currency: string;
  destinations: DestinationDraft[];
};

type Outcome =
  | { kind: "none" }
  | { kind: "result"; comparison: Comparison; submitted: Draft }
  | { kind: "invalid"; message: string; fieldErrors: Record<string, string> }
  | { kind: "failed"; message: string };

const blankDestination: DestinationDraft = { label: "", flight: "", nightly: "" };

const initialDraft: Draft = {
  checkIn: "",
  checkOut: "",
  travelers: 2,
  currency: "USD",
  destinations: [blankDestination, blankDestination],
};

export default function TripComparison() {
  const [draft, setDraft] = useState(initialDraft);
  const [pending, setPending] = useState(false);
  const [outcome, setOutcome] = useState<Outcome>({ kind: "none" });

  const errors: Record<string, string> = outcome.kind === "invalid" ? outcome.fieldErrors : {};
  const stale = outcome.kind === "result" && !sameDraft(outcome.submitted, draft);

  function update(patch: Partial<Draft>) {
    setDraft((current) => ({ ...current, ...patch }));
  }

  function updateDestination(index: number, patch: Partial<DestinationDraft>) {
    setDraft((current) => ({
      ...current,
      destinations: current.destinations.map((d, i) => (i === index ? { ...d, ...patch } : d)),
    }));
  }

  function setDestinations(destinations: DestinationDraft[]) {
    update({ destinations });
    // Field errors name destination positions, which just changed.
    if (outcome.kind === "invalid") {
      setOutcome({ kind: "none" });
    }
  }

  async function handleSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    const submitted = draft;
    setPending(true);
    const result = await requestComparison(toRequest(submitted));
    setPending(false);
    if (result.kind === "ok") {
      setOutcome({ kind: "result", comparison: result.comparison, submitted });
    } else if (result.kind === "invalid") {
      const fieldErrors = Object.fromEntries(result.fieldErrors.map((e) => [e.field, e.message]));
      setOutcome({ kind: "invalid", message: result.message, fieldErrors });
    } else {
      setOutcome({ kind: "failed", message: result.message });
    }
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1>Compare trips</h1>
        <p>
          Compare flight and stay estimates for two or three destinations on the same dates. Enter your
          own estimates for the whole party, and leave one blank if you do not know it yet.
        </p>
      </header>

      <form className={styles.form} onSubmit={handleSubmit} noValidate>
        <fieldset className={styles.bare} disabled={pending}>
          <fieldset className={styles.group}>
            <legend>Trip</legend>
            <div className={styles.grid}>
              <Field path="check_in" label="Check-in date" error={errors.check_in}>
                <input
                  {...controlProps("check_in", errors.check_in)}
                  type="date"
                  value={draft.checkIn}
                  onChange={(e) => update({ checkIn: e.target.value })}
                />
              </Field>
              <Field path="check_out" label="Check-out date" error={errors.check_out}>
                <input
                  {...controlProps("check_out", errors.check_out)}
                  type="date"
                  value={draft.checkOut}
                  onChange={(e) => update({ checkOut: e.target.value })}
                />
              </Field>
              <Field path="travelers" label="Travelers" error={errors.travelers}>
                <select
                  {...controlProps("travelers", errors.travelers)}
                  value={draft.travelers}
                  onChange={(e) => update({ travelers: Number(e.target.value) })}
                >
                  {Array.from({ length: MAX_TRAVELERS }, (_, i) => i + 1).map((n) => (
                    <option key={n} value={n}>
                      {plural(n, "traveler")}
                    </option>
                  ))}
                </select>
              </Field>
              <Field
                path="currency"
                label="Currency"
                hint="Every amount uses this currency. Changing it does not convert amounts."
                error={errors.currency}
              >
                <select
                  {...controlProps("currency", errors.currency, true)}
                  value={draft.currency}
                  onChange={(e) => update({ currency: e.target.value })}
                >
                  {CURRENCIES.map((code) => (
                    <option key={code} value={code}>
                      {code}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
          </fieldset>

          {draft.destinations.map((destination, i) => {
            const prefix = `destinations[${i}].`;
            const labelPath = `${prefix}label`;
            const flightPath = `${prefix}flight_estimate`;
            const nightlyPath = `${prefix}nightly_stay_estimate`;
            return (
              <fieldset key={i} className={styles.group}>
                <legend>Destination {i + 1}</legend>
                <div className={styles.grid}>
                  <Field path={labelPath} label="Name" error={errors[labelPath]}>
                    <input
                      {...controlProps(labelPath, errors[labelPath])}
                      type="text"
                      autoComplete="off"
                      value={destination.label}
                      onChange={(e) => updateDestination(i, { label: e.target.value })}
                    />
                  </Field>
                  <Field
                    path={flightPath}
                    label="Round-trip flights for the whole party"
                    hint="Optional. Leave blank if unknown."
                    error={errors[flightPath]}
                  >
                    <input
                      {...controlProps(flightPath, errors[flightPath], true)}
                      type="text"
                      autoComplete="off"
                      value={destination.flight}
                      onChange={(e) => updateDestination(i, { flight: e.target.value })}
                    />
                  </Field>
                  <Field
                    path={nightlyPath}
                    label="Accommodation per night for the whole party"
                    hint="Optional. Leave blank if unknown."
                    error={errors[nightlyPath]}
                  >
                    <input
                      {...controlProps(nightlyPath, errors[nightlyPath], true)}
                      type="text"
                      autoComplete="off"
                      value={destination.nightly}
                      onChange={(e) => updateDestination(i, { nightly: e.target.value })}
                    />
                  </Field>
                </div>
                {draft.destinations.length > MIN_DESTINATIONS && (
                  <button
                    type="button"
                    className={`${styles.button} ${styles.secondary}`}
                    onClick={() => setDestinations(draft.destinations.filter((_, j) => j !== i))}
                  >
                    Remove destination {i + 1}
                  </button>
                )}
              </fieldset>
            );
          })}
          {errors.destinations && <p className={styles.error}>{errors.destinations}</p>}

          <div className={styles.actions}>
            {draft.destinations.length < MAX_DESTINATIONS && (
              <button
                type="button"
                className={`${styles.button} ${styles.secondary}`}
                onClick={() => setDestinations([...draft.destinations, blankDestination])}
              >
                Add a third destination
              </button>
            )}
            <button type="submit" className={`${styles.button} ${styles.primary}`}>
              {pending ? "Comparing…" : "Compare estimates"}
            </button>
          </div>
        </fieldset>
      </form>

      {(outcome.kind === "invalid" || outcome.kind === "failed") && (
        <p role="alert" className={styles.alert}>
          {outcome.message}
        </p>
      )}
      {outcome.kind === "result" && <Results comparison={outcome.comparison} stale={stale} />}
    </div>
  );
}

function Results({ comparison, stale }: { comparison: Comparison; stale: boolean }) {
  const { currency, nights, travelers } = comparison;
  const jointLowest = comparison.destinations.filter((d) => d.lowest_estimate).length > 1;
  return (
    <section className={styles.results} aria-labelledby="results-heading">
      <h2 id="results-heading">Results</h2>
      <p className={styles.summary} role="status">
        {plural(nights, "night")} · {plural(travelers, "traveler")} · {currency} · amounts are for the whole
        party
      </p>
      {stale && (
        <p className={styles.notice} role="status">
          You changed the trip after this comparison. Select Compare estimates to update it.
        </p>
      )}
      <div className={stale ? `${styles.cards} ${styles.staleCards}` : styles.cards}>
        {comparison.destinations.map((destination, i) => (
          <DestinationCard
            key={i}
            destination={destination}
            currency={currency}
            nights={nights}
            jointLowest={jointLowest}
          />
        ))}
      </div>
      {!comparison.all_complete && (
        <p className={styles.note}>No lowest estimate is marked until every destination has both estimates.</p>
      )}
      <p className={styles.note}>
        These are your own estimates, not live prices or supplier offers. They cover flights and
        accommodation only, not a full trip budget.
      </p>
    </section>
  );
}

function DestinationCard({
  destination,
  currency,
  nights,
  jointLowest,
}: {
  destination: DestinationEstimate;
  currency: string;
  nights: number;
  jointLowest: boolean;
}) {
  const nightly =
    destination.nightly_stay_estimate === null ? "unknown" : `${currency} ${destination.nightly_stay_estimate}`;
  return (
    <article className={styles.card}>
      <h3>{destination.label}</h3>
      {destination.lowest_estimate && (
        <p className={styles.badge}>{jointLowest ? "Joint lowest estimate" : "Lowest estimate"}</p>
      )}
      <dl className={styles.amounts}>
        <div>
          <dt>Round-trip flights</dt>
          <dd>{amount(currency, destination.flight_estimate)}</dd>
        </div>
        <div>
          <dt>
            Stay: {plural(nights, "night")} × {nightly}
          </dt>
          <dd>{amount(currency, destination.stay_estimate)}</dd>
        </div>
        <div className={styles.total}>
          <dt>Flight + stay estimate</dt>
          <dd>{amount(currency, destination.flight_and_stay_estimate)}</dd>
        </div>
      </dl>
      {!destination.complete && <p className={styles.missing}>Add the missing estimate to see a total.</p>}
    </article>
  );
}

function Field({
  path,
  label,
  hint,
  error,
  children,
}: {
  path: string;
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
}) {
  const id = domId(path);
  return (
    <div className={styles.field}>
      <label htmlFor={id}>{label}</label>
      {hint && (
        <p id={`${id}-hint`} className={styles.hint}>
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-error`} className={styles.error}>
          {error}
        </p>
      )}
      {children}
    </div>
  );
}

function controlProps(path: string, error?: string, hasHint = false) {
  const id = domId(path);
  const describedBy = [hasHint ? `${id}-hint` : "", error ? `${id}-error` : ""].filter(Boolean).join(" ");
  return {
    id,
    "aria-invalid": error ? true : undefined,
    "aria-describedby": describedBy || undefined,
  };
}

// Field paths from the API, such as "destinations[0].label", become element
// ids such as "destinations-0-label".
function domId(path: string) {
  return path.replace(/[[\].]+/g, "-").replace(/-$/, "");
}

function toRequest(draft: Draft): ComparisonRequest {
  return {
    check_in: draft.checkIn,
    check_out: draft.checkOut,
    travelers: draft.travelers,
    currency: draft.currency,
    destinations: draft.destinations.map((d) => ({
      label: d.label,
      flight_estimate: optionalAmount(d.flight),
      nightly_stay_estimate: optionalAmount(d.nightly),
    })),
  };
}

// A blank estimate is unknown. The API validates everything else.
function optionalAmount(value: string): string | null {
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

function sameDraft(a: Draft, b: Draft) {
  return JSON.stringify(a) === JSON.stringify(b);
}

function amount(currency: string, value: string | null): ReactNode {
  return value === null ? <span className={styles.unknown}>Unknown</span> : `${currency} ${value}`;
}

function plural(count: number, noun: string) {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}
