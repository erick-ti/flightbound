"use client";

import { useState, type ReactNode, type SubmitEvent } from "react";
import {
  requestComparison,
  type Comparison,
  type ComparisonRequest,
  type DestinationEstimate,
  type ExchangeRates,
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
const ECB_RATES_URL =
  "https://www.ecb.europa.eu/stats/policy_and_exchange_rates/euro_reference_exchange_rates/html/index.en.html";

type DestinationDraft = {
  label: string;
  flight: string;
  // An amount's currency is null while it follows the comparison currency.
  // Typing the amount or choosing its currency sets it for good.
  flightCurrency: string | null;
  nightly: string;
  nightlyCurrency: string | null;
};

type Draft = {
  checkIn: string;
  checkOut: string;
  travelers: number;
  currency: string;
  budget: string;
  // Follows the comparison currency while null, like an estimate's currency.
  budgetCurrency: string | null;
  destinations: DestinationDraft[];
};

type Outcome =
  | { kind: "none" }
  | { kind: "result"; comparison: Comparison; submitted: Draft }
  | { kind: "invalid"; message: string; fieldErrors: Record<string, string> }
  | { kind: "failed"; message: string };

const blankDestination: DestinationDraft = {
  label: "",
  flight: "",
  flightCurrency: null,
  nightly: "",
  nightlyCurrency: null,
};

const initialDraft: Draft = {
  checkIn: "",
  checkOut: "",
  travelers: 2,
  currency: "USD",
  budget: "",
  budgetCurrency: null,
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

  // change receives the destination and the comparison currency as they are
  // when the update applies.
  function updateDestination(
    index: number,
    change: (destination: DestinationDraft, currency: string) => Partial<DestinationDraft>,
  ) {
    setDraft((current) => ({
      ...current,
      destinations: current.destinations.map((d, i) => (i === index ? { ...d, ...change(d, current.currency) } : d)),
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
          own estimates for the whole party, each in the currency you have it in, and leave one blank if
          you do not know it yet.
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
                label="Currency for totals"
                hint="Totals use this currency. Amounts in other currencies are converted at ECB reference rates."
                error={errors.currency}
              >
                <select
                  {...controlProps("currency", errors.currency, true)}
                  value={draft.currency}
                  onChange={(e) => update({ currency: e.target.value })}
                >
                  <CurrencyOptions />
                </select>
              </Field>
              <AmountField
                path="budget"
                label="Trip budget for the whole party"
                hint="Optional. Each destination shows what its flights and stay leave for everything else."
                currencyLabel="Currency of the budget"
                errors={errors}
                value={draft.budget}
                currency={draft.budgetCurrency ?? draft.currency}
                onValue={(budget) =>
                  setDraft((current) => ({ ...current, budget, budgetCurrency: current.budgetCurrency ?? current.currency }))
                }
                onCurrency={(budgetCurrency) => update({ budgetCurrency })}
              />
            </div>
          </fieldset>

          {draft.destinations.map((destination, i) => {
            const prefix = `destinations[${i}].`;
            const labelPath = `${prefix}label`;
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
                      onChange={(e) => {
                        const label = e.target.value;
                        updateDestination(i, () => ({ label }));
                      }}
                    />
                  </Field>
                  <AmountField
                    path={`${prefix}flight_estimate`}
                    label="Round-trip flights for the whole party"
                    currencyLabel="Currency of the flight estimate"
                    errors={errors}
                    value={destination.flight}
                    currency={destination.flightCurrency ?? draft.currency}
                    onValue={(flight) =>
                      updateDestination(i, (d, currency) => ({ flight, flightCurrency: d.flightCurrency ?? currency }))
                    }
                    onCurrency={(flightCurrency) => updateDestination(i, () => ({ flightCurrency }))}
                  />
                  <AmountField
                    path={`${prefix}nightly_stay_estimate`}
                    label="Accommodation per night for the whole party"
                    currencyLabel="Currency of the accommodation estimate"
                    errors={errors}
                    value={destination.nightly}
                    currency={destination.nightlyCurrency ?? draft.currency}
                    onValue={(nightly) =>
                      updateDestination(i, (d, currency) => ({ nightly, nightlyCurrency: d.nightlyCurrency ?? currency }))
                    }
                    onCurrency={(nightlyCurrency) => updateDestination(i, () => ({ nightlyCurrency }))}
                  />
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

// AmountField is an optional amount with its own currency menu. path is the
// amount's API field; its currency's field is path + "_currency".
function AmountField({
  path,
  label,
  hint = "Optional. Leave blank if unknown.",
  currencyLabel,
  errors,
  value,
  currency,
  onValue,
  onCurrency,
}: {
  path: string;
  label: string;
  hint?: string;
  currencyLabel: string;
  errors: Record<string, string>;
  value: string;
  currency: string;
  onValue: (value: string) => void;
  onCurrency: (currency: string) => void;
}) {
  const amountError = errors[path];
  const currencyPath = `${path}_currency`;
  const currencyError = errors[currencyPath];
  return (
    <Field path={path} label={label} hint={hint} error={amountError ?? currencyError}>
      <div className={styles.amount}>
        <input
          {...controlProps(path, amountError, true)}
          type="text"
          autoComplete="off"
          value={value}
          onChange={(e) => onValue(e.target.value)}
        />
        <select
          id={domId(currencyPath)}
          aria-label={currencyLabel}
          aria-invalid={currencyError ? true : undefined}
          aria-describedby={currencyError ? `${domId(path)}-error` : undefined}
          value={currency}
          onChange={(e) => onCurrency(e.target.value)}
        >
          <CurrencyOptions />
        </select>
      </div>
    </Field>
  );
}

function CurrencyOptions() {
  return CURRENCIES.map((code) => (
    <option key={code} value={code}>
      {code}
    </option>
  ));
}

function Results({ comparison, stale }: { comparison: Comparison; stale: boolean }) {
  const { currency, nights, travelers, exchange_rates: rates } = comparison;
  const jointLowest = comparison.destinations.filter((d) => d.lowest_estimate).length > 1;
  return (
    <section className={styles.results} aria-labelledby="results-heading">
      <h2 id="results-heading">Results</h2>
      <p className={styles.summary} role="status">
        {plural(nights, "night")} · {plural(travelers, "traveler")} · totals in {currency} · amounts are for the
        whole party
      </p>
      {comparison.budget !== null && <BudgetSummary comparison={comparison} />}
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
            ratesAvailable={rates?.available ?? true}
            budgetConverted={comparison.converted_budget !== null}
          />
        ))}
      </div>
      {!comparison.all_complete && (
        <p className={styles.note}>No lowest estimate is marked until every destination has a total.</p>
      )}
      {rates && <RatesNote rates={rates} />}
      <p className={styles.note}>
        These are your own estimates, not live prices or supplier offers. They cover flights and
        accommodation only, not a full trip budget.
      </p>
    </section>
  );
}

// BudgetSummary shows the budget as entered and in the comparison currency,
// and says why no destination is checked when it could not be converted.
function BudgetSummary({ comparison: c }: { comparison: Comparison }) {
  const ratesAvailable = c.exchange_rates?.available ?? true;
  return (
    <>
      <p className={styles.summary}>
        Trip budget: {c.budget_currency} {c.budget}
        <Converted from={c.budget_currency} to={c.currency} known value={c.converted_budget} />
      </p>
      {c.converted_budget === null && (
        <p className={styles.notice}>
          {ratesAvailable
            ? `There is no ECB reference rate to convert your budget to ${c.currency}, so no destination is checked against it.`
            : `Exchange rates are unavailable right now, so your budget could not be converted to ${c.currency} and no destination is checked against it.`}
        </p>
      )}
    </>
  );
}

function RatesNote({ rates }: { rates: ExchangeRates }) {
  if (!rates.available) {
    return (
      <p className={styles.note}>
        Exchange rates could not be loaded, so amounts in other currencies were not converted. Try again
        later.
      </p>
    );
  }
  const used = Object.entries(rates.per_euro)
    .map(([code, rate]) => `1 EUR = ${rate} ${code}`)
    .join(", ");
  return (
    <p className={styles.note}>
      {used && (
        <>
          Amounts in other currencies were converted with the European Central Bank&apos;s euro reference rates
          for {rates.date}: {used}.{" "}
        </>
      )}
      Reference rates are for information only; your bank or card rate will differ.{" "}
      <a href={ECB_RATES_URL}>Source: ECB statistics</a>.
    </p>
  );
}

function DestinationCard({
  destination: d,
  currency,
  nights,
  jointLowest,
  ratesAvailable,
  budgetConverted,
}: {
  destination: DestinationEstimate;
  currency: string;
  nights: number;
  jointLowest: boolean;
  ratesAvailable: boolean;
  budgetConverted: boolean;
}) {
  const nightly =
    d.nightly_stay_estimate === null ? "unknown" : `${d.nightly_stay_estimate_currency} ${d.nightly_stay_estimate}`;
  const missing = d.flight_estimate === null || d.nightly_stay_estimate === null;
  const notConverted =
    (d.flight_estimate !== null && d.converted_flight_estimate === null) ||
    (d.stay_estimate !== null && d.converted_stay_estimate === null);
  let incomplete = "";
  if (missing) {
    incomplete = "Add the missing estimate to see a total.";
  } else if (notConverted) {
    incomplete = ratesAvailable
      ? "There is no ECB reference rate for one of these currencies, so this total cannot be calculated."
      : "Exchange rates are unavailable right now, so this total cannot be calculated.";
  }
  return (
    <article className={styles.card}>
      <h3>{d.label}</h3>
      {d.lowest_estimate && <p className={styles.badge}>{jointLowest ? "Joint lowest estimate" : "Lowest estimate"}</p>}
      <dl className={styles.amounts}>
        <div>
          <dt>Round-trip flights</dt>
          <dd>
            {amount(d.flight_estimate_currency, d.flight_estimate)}
            <Converted
              from={d.flight_estimate_currency}
              to={currency}
              known={d.flight_estimate !== null}
              value={d.converted_flight_estimate}
            />
          </dd>
        </div>
        <div>
          <dt>
            Stay: {plural(nights, "night")} × {nightly}
          </dt>
          <dd>
            {amount(d.nightly_stay_estimate_currency, d.stay_estimate)}
            <Converted
              from={d.nightly_stay_estimate_currency}
              to={currency}
              known={d.stay_estimate !== null}
              value={d.converted_stay_estimate}
            />
          </dd>
        </div>
        <div className={styles.total}>
          <dt>Flight + stay estimate</dt>
          <dd>{amount(currency, d.flight_and_stay_estimate)}</dd>
        </div>
      </dl>
      <BudgetLine destination={d} currency={currency} budgetConverted={budgetConverted} />
      {!d.complete && incomplete && <p className={styles.missing}>{incomplete}</p>}
    </article>
  );
}

// BudgetLine says how a destination compares with the budget, with the
// difference the API returned. When the budget itself was not converted,
// BudgetSummary explains why nothing is checked.
function BudgetLine({
  destination: d,
  currency,
  budgetConverted,
}: {
  destination: DestinationEstimate;
  currency: string;
  budgetConverted: boolean;
}) {
  switch (d.budget_status) {
    case "within":
      return (
        <p className={styles.budget}>
          Leaves {currency} {d.budget_difference} of your budget for everything else.
        </p>
      );
    case "over":
      return (
        <p className={`${styles.budget} ${styles.over}`}>
          {currency} {d.budget_difference} over your budget on flights and stay alone.
        </p>
      );
    case "over_at_least": {
      // An incomplete destination has at most one converted part, and that is
      // the part the API checked.
      const part = d.converted_flight_estimate !== null ? "flights" : "the stay";
      return (
        <p className={`${styles.budget} ${styles.over}`}>
          At least {currency} {d.budget_difference} over your budget on {part} alone.
        </p>
      );
    }
    case "unknown":
      return budgetConverted ? (
        <p className={styles.budget}>Your budget can be checked once this destination has a total.</p>
      ) : null;
    case "not_set":
      return null;
  }
}

// Converted shows a known amount in the comparison currency, below the
// amount as entered, when the two currencies differ.
function Converted({ from, to, known, value }: { from: string; to: string; known: boolean; value: string | null }) {
  if (!known || from === to) {
    return null;
  }
  return <span className={styles.converted}>{value === null ? "Not converted" : `≈ ${to} ${value}`}</span>;
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
    budget: optionalAmount(draft.budget),
    budget_currency: draft.budgetCurrency ?? draft.currency,
    destinations: draft.destinations.map((d) => ({
      label: d.label,
      flight_estimate: optionalAmount(d.flight),
      flight_estimate_currency: d.flightCurrency ?? draft.currency,
      nightly_stay_estimate: optionalAmount(d.nightly),
      nightly_stay_estimate_currency: d.nightlyCurrency ?? draft.currency,
    })),
  };
}

// A blank estimate is unknown, and a blank budget is not set. The API
// validates everything else.
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
