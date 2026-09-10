"use client";
import Link from "next/link";
import { use, useState } from "react";
import { api, Changeset, Project, Task, short, when } from "../../../../../lib/api";
import { useSession } from "../../../../../components/session";
import { useData } from "../../../../../components/data";
import { Icon } from "../../../../../components/icons";
import { Badge, Dialog, Diff, Notice } from "../../../../../components/ui";

export default function ChangesetPage({ params }: { params: Promise<{ id: string; csid: string }> }) {
  const { id, csid } = use(params); const { token } = useSession();
  const { data: [changeset, project, rawTasks], error, loading, refresh } = useData<[Changeset | null, Project | null, Task[] | null]>([`/changesets/${csid}`, `/projects/${id}`, `/projects/${id}/tasks`], [null, null, []]);
  const [busy, setBusy] = useState(false); const [actionError, setActionError] = useState(""); const [notice, setNotice] = useState("");
  const [confirm, setConfirm] = useState<"approve" | "reject" | "integrate" | null>(null); const [acknowledged, setAcknowledged] = useState(false);
  async function act() {
    if (!confirm) return; const action = confirm; setBusy(true); setActionError("");
    try {
      if (action === "integrate") {
        const result = await api<{ merged: boolean; reason?: string; status: string; new_head?: string }>(token, `/changesets/${csid}/integrate`, { method: "POST", body: "{}" });
        if (!result.merged) setActionError(result.reason || `Integration stopped: ${result.status.toLowerCase().replaceAll("_", " ")}. Rebase or resolve the workspace and submit a new changeset.`);
        else setNotice(`Integrated into ${project?.branch ?? "the canonical branch"} at ${short(result.new_head)}. Your changeset is now part of the project history.`);
      } else {
        await api(token, `/changesets/${csid}/decision`, { method: "POST", body: JSON.stringify({ decision: action }) });
        setNotice(action === "approve" ? "Changeset approved. Integration is a separate step and revalidates the canonical branch." : "Changeset rejected. The workspace is preserved for follow-up.");
      }
      setConfirm(null); refresh();
    } catch (e) { setActionError(e instanceof Error ? e.message : "The action could not be completed."); }
    finally { setBusy(false); }
  }
  if (loading) return <main id="main" className="page"><p role="status">Loading changeset…</p></main>;
  if (!changeset || !project || changeset.project_id !== id) return <main id="main" className="page"><Link href={`/projects/${id}`} className="back-link">← Back to project</Link><h1>Changeset unavailable</h1><Notice danger>{error || "This changeset is not part of this project."}</Notice></main>;
  const task = (rawTasks ?? []).find(t => t.id === changeset.task_id);
  const reviewable = ["IN_REVIEW", "DRAFT"].includes(changeset.status);
  const oversized = changeset.diff.length > 350000;
  return <main id="main" className="page review-page"><Link href={`/projects/${id}`} className="back-link">← {project.name}</Link><div className="page-heading"><div><span className="eyebrow">CHANGESET REVIEW · {short(csid)}</span><h1>{task?.title ?? "Review proposed changes"}</h1><p>Inspect the snapshot. Make the call. Keep the branch steady.</p></div><Badge status={changeset.status}/></div>
    {error && <Notice danger>{error} Showing the last successful snapshot. <button onClick={refresh} className="text-button">Retry</button></Notice>}{notice && <Notice>{notice}</Notice>}{actionError && !confirm && <Notice danger>{actionError}</Notice>}
    {["CONFLICTED", "NEEDS_REBASE"].includes(changeset.status) && <Notice danger>This snapshot cannot integrate. Update the original workspace against the current canonical branch, resolve any conflicts, and submit a fresh changeset for review.</Notice>}
    <div className="review-layout"><section><div className="panel diff-panel"><div className="section-header"><h2><Icon name="code"/>Proposed changes</h2><span className="count">{changeset.files?.length ?? 0} files</span></div><Diff value={changeset.diff}/></div></section><aside className="review-sidebar"><section className="panel"><h2>Review summary</h2><dl className="summary-list"><div><dt>Canonical branch</dt><dd><Icon name="branch"/>{project.branch}</dd></div><div><dt>Snapshot base</dt><dd><code>{short(changeset.base_commit)}</code></dd></div><div><dt>Current head</dt><dd><code>{short(project.canonical_sha)}</code></dd></div><div><dt>Submitted</dt><dd>{when(changeset.created_at)}</dd></div></dl><div className="test-note"><Icon name="shield"/><div><strong>{changeset.test_ref ? "Test reference attached" : "No test evidence attached"}</strong><p>{changeset.test_ref || "Check task activity and verify the changes before approving. A completed run is not proof that tests passed."}</p></div></div>
    {reviewable && <div className="review-buttons"><button className="button primary full" disabled={busy || oversized || !!error} onClick={() => { setConfirm("approve"); setActionError(""); }}><Icon name="check"/>Approve changeset</button><button className="button secondary full" disabled={busy || !!error} onClick={() => { setConfirm("reject"); setActionError(""); }}>Reject changeset</button></div>}
    {changeset.status === "APPROVED" && <div className="review-buttons"><button className="button primary full" disabled={busy || !!error} onClick={() => { setConfirm("integrate"); setAcknowledged(false); setActionError(""); }}><Icon name="branch"/>Integrate into {project.branch}</button><p className="field-help">Revalidates the snapshot against the current branch before creating an integration commit.</p></div>}
    {changeset.status === "MERGED" && <div className="integrated"><Icon name="check"/>Integrated into {project.branch}</div>}
    {oversized && <p className="field-help">This diff exceeds the full-review preview limit. Review and approve the complete snapshot with the CLI.</p>}
    </section><section className="panel files-panel"><h2>Changed files</h2>{changeset.files?.length ? <ul>{changeset.files.map((file, i) => <li key={`${file}-${i}`}><Icon name="code"/><code>{file}</code></li>)}</ul> : <p className="subtle">No files in this snapshot.</p>}</section></aside></div>
    {confirm && <Dialog title={confirm === "integrate" ? `Integrate into ${project.branch}?` : confirm === "approve" ? "Approve this changeset?" : "Reject this changeset?"} onClose={() => !busy && setConfirm(null)}><p className="form-intro">{confirm === "integrate" ? `This creates a commit on ${project.branch} from the approved snapshot. The server checks the current head and stops if the changeset is stale or conflicts.` : confirm === "approve" ? "Confirm that you have reviewed the complete diff and any available test evidence. Approval does not integrate code." : "This marks the snapshot rejected. No repository files are deleted; its workspace remains available for revisions."}</p>{confirm === "integrate" && <label className="checkbox-label"><input type="checkbox" checked={acknowledged} onChange={e => setAcknowledged(e.target.checked)}/>I’m ready to update the canonical branch.</label>}{actionError && <Notice danger>{actionError}</Notice>}<div className="dialog-actions"><button className="button secondary" disabled={busy} onClick={() => setConfirm(null)}>Cancel</button><button className={`button ${confirm === "reject" ? "destructive" : "primary"}`} disabled={busy || (confirm === "integrate" && !acknowledged)} onClick={() => void act()}>{busy ? "Working…" : confirm === "integrate" ? "Integrate approved snapshot" : confirm === "approve" ? "Confirm approval" : "Confirm rejection"}</button></div></Dialog>}
  </main>;
}
