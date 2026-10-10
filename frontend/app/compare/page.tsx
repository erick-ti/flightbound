import type { Metadata } from "next";
import TripComparison from "./TripComparison";

export const metadata: Metadata = {
  title: "Compare trips · Flightbound",
};

export default function ComparePage() {
  return (
    <main>
      <TripComparison />
    </main>
  );
}
