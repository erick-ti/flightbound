import Link from "next/link";

export default function Home() {
  return (
    <main>
      <h1>Flightbound</h1>
      <p>
        <Link href="/compare">Compare trips</Link>
      </p>
    </main>
  );
}
