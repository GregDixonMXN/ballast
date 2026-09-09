"use client";
import { useState } from "react";
import { api, useEvents } from "../../lib/api";

// Operational project screen: header, tasks, workspaces, conflicts,
// activity feed. Intentionally plain — function before polish.
export default function ProjectPage({ params }: { params: { id: string } }) {
  const id = params.id;
  const [tasks, setTasks] = useState<any[]>([]);
  const [workspaces, setWorkspaces] = useState<any[]>([]);
  const [feed, setFeed] = useState<any[]>([]);
  const [title, setTitle] = useState("");

  useEvents(id, (e) => setFeed((f) => [e, ...f].slice(0, 100)));

  async function refresh() {
    setTasks(await api(`/projects/${id}/tasks`));
    setWorkspaces(await api(`/projects/${id}/workspaces`));
  }
  async function createTask() {
    if (!title.trim()) return;
    await api(`/projects/${id}/tasks`, {
      method: "POST",
      body: JSON.stringify({ title }),
    });
    setTitle("");
    refresh();
  }

  return (
    <main style={{ padding: 16, fontFamily: "system-ui" }}>
      <h1>Project {id}</h1>
      <button onClick={refresh}>Refresh</button>
      <section>
        <h2>New task</h2>
        <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Modify the backend API" />
        <button onClick={createTask}>Create</button>
      </section>
      <section>
        <h2>Tasks</h2>
        <ul>{tasks.map((t: any) => <li key={t.id}>{t.title} — {t.status}</li>)}</ul>
      </section>
      <section>
        <h2>Workspaces</h2>
        <ul>
          {workspaces.map((w: any) => (
            <li key={w.id}>{w.task_id} @ {String(w.base_commit).slice(0, 8)} — {w.status}</li>
          ))}
        </ul>
      </section>
      <section>
        <h2>Activity</h2>
        <ul>{feed.map((e: any, i) => <li key={i}>{e.type} — {e.entity_id}</li>)}</ul>
      </section>
    </main>
  );
}
