"use client";
import { useState } from "react";
import { api } from "../../../lib/api";

// Review screen: base, files, diff, tests, conflicts, approve/reject.
// Reachable at /projects/[id]/changesets/[csid]. Phone-sized by default.
export default function ChangesetPage({ params }: { params: { csid: string } }) {
  const [cs, setCs] = useState<any>(null);
  async function load() {
    setCs(await api(`/changesets/${params.csid}`));
  }
  async function decide(decision: "approve" | "reject") {
    setCs(await api(`/changesets/${params.csid}/decision`, {
      method: "POST",
      body: JSON.stringify({ decision }),
    }));
  }
  return (
    <main style={{ padding: 16, fontFamily: "system-ui", maxWidth: 720 }}>
      <h1>Changeset {params.csid}</h1>
      <button onClick={load}>Load</button>{" "}
      <button onClick={() => decide("approve")}>Approve</button>{" "}
      <button onClick={() => decide("reject")}>Reject</button>
      {cs && (
        <>
          <p>Base {(cs.base_commit ?? "").slice(0, 8)} — {cs.status}</p>
          <ul>{(cs.files ?? []).map((f: string) => <li key={f}>{f}</li>)}</ul>
          <pre style={{ whiteSpace: "pre-wrap", background: "#f4f4f4", padding: 8 }}>{cs.diff}</pre>
        </>
      )}
    </main>
  );
}
