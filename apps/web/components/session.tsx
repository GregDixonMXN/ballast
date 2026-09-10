"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { createContext, useContext, useState, ReactNode, FormEvent } from "react";
import { api } from "../lib/api";
import { Icon } from "./icons";
import { Notice } from "./ui";
const Session = createContext({ token: "", disconnect: () => {} });
export const useSession = () => useContext(Session);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState(""); const [input, setInput] = useState("");
  const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  const path = usePathname();
  async function connect(event: FormEvent) {
    event.preventDefault(); if (!input.trim()) return; setBusy(true); setError("");
    try { await api(input.trim(), "/projects"); setToken(input.trim()); setInput(""); }
    catch (e) { setError(e instanceof Error ? e.message : "Connection failed."); }
    finally { setBusy(false); }
  }
  const disconnect = () => { setToken(""); setInput(""); setError(""); };
  return <Session.Provider value={{ token, disconnect }}><a className="skip-link" href="#main">Skip to content</a><div className="app-shell">
    <aside className="sidebar"><Link href="/" className="brand" aria-label="Ballast home"><span className="brand-mark"><Icon name="branch"/></span>ballast<span className="brand-dot">.</span></Link><div className="sidebar-label">LOCAL WORKSPACE</div><nav aria-label="Main navigation"><Link href="/" className={path !== "/guide" ? "nav-link active" : "nav-link"}><Icon name="grid"/>Projects<Icon name="arrow" className="nav-arrow"/></Link><Link href="/guide" className={path === "/guide" ? "nav-link active" : "nav-link"}><Icon name="book"/>Getting started</Link></nav><div className="sidebar-bottom"><div className="local-note"><Icon name="shield"/><div><strong>Your code. Your machine.</strong><span>Local control plane</span></div></div><div className="sidebar-footer"><span className={`connection-dot ${token ? "connected" : ""}`}/>{token ? "Session unlocked" : "Not connected"}<span className="version">RC.1</span></div>{token && <button className="disconnect" onClick={disconnect}><Icon name="logout"/>Disconnect</button>}</div></aside>
    <div className="main-shell"><header className="topbar"><div className="breadcrumb"><span>Workspace</span><span className="slash">/</span><strong>{path === "/guide" ? "Getting started" : path === "/" ? "Projects" : "Project overview"}</strong></div><span className="local-pill"><span className="connection-dot connected"/>Local edition</span></header>
    {!token && path !== "/guide" ? <main id="main" className="connect-page"><div className="connect-intro"><span className="eyebrow">A STEADY HAND FOR YOUR CODE</span><h1>More progress.<br/><span>Less crossfire.</span></h1><p>Give every task its own workspace. Review the changes, keep the good work, and move your project forward together.</p><div className="connect-points"><span><Icon name="branch"/>Isolated Git worktrees</span><span><Icon name="shield"/>Deliberate integration</span><span><Icon name="server"/>Runs on your machine</span></div></div><section className="connect-card"><div className="connect-symbol"><Icon name="shield" width={28} height={28}/></div><h2>Connect to Ballast</h2><p>Unlock your local workspace with the operator token created by your server.</p><form onSubmit={connect}><label htmlFor="operator-token">Operator token</label><input id="operator-token" type="password" autoComplete="off" spellCheck={false} value={input} onChange={(e) => setInput(e.target.value)} placeholder="Paste your local operator token" required disabled={busy}/><div className="file-import"><label htmlFor="token-file">Or select your operator token file</label><input id="token-file" type="file" disabled={busy} onChange={async (e) => { const file = e.target.files?.[0]; if (!file) return; if (file.size > 1024) { setError("Select the small operator token file, not a repository or document."); return; } try { setInput((await file.text()).trim()); setError(""); } catch { setError("The token file could not be read."); } e.target.value = ""; }}/></div>{error && <Notice danger>{error}</Notice>}<button className="button primary full" disabled={busy || !input.trim()}>{busy ? "Connecting…" : "Connect to workspace"}<Icon name="arrow"/></button></form><p className="privacy-note"><Icon name="shield"/>Your token stays in this tab’s memory. Refreshing or disconnecting clears it.</p><Link href="/guide" className="text-link">First time here? Set up your workspace <span>↗</span></Link></section></main> : children}
    </div></div></Session.Provider>;
}
