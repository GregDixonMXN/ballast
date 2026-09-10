"use client";
import { useEffect, useId, useRef, ReactNode } from "react";
import { Icon } from "./icons";
export function Badge({ status }: { status: string }) { return <span className={`badge status-${status.toLowerCase()}`}>{status.replaceAll("_", " ").toLowerCase()}</span>; }
export function Empty({ title, children, icon = "grid" }: { title: string; children?: ReactNode; icon?: string }) { return <div className="empty"><div className="empty-icon"><Icon name={icon}/></div><h3>{title}</h3><p>{children}</p></div>; }
export function Notice({ children, danger = false }: { children: ReactNode; danger?: boolean }) { return <div className={`notice ${danger ? "danger" : ""}`} role={danger ? "alert" : "status"}><Icon name={danger ? "alert" : "check"}/><div>{children}</div></div>; }
export function Dialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null); const id = useId();
  useEffect(() => { const element = ref.current; element?.showModal(); return () => element?.close(); }, []);
  return <dialog ref={ref} aria-labelledby={id} onCancel={(e) => { e.preventDefault(); onClose(); }} onClick={(e) => { if (e.target === e.currentTarget) { const box = e.currentTarget.getBoundingClientRect(); if (e.clientX < box.left || e.clientX > box.right || e.clientY < box.top || e.clientY > box.bottom) onClose(); } }}><header className="dialog-heading"><h2 id={id}>{title}</h2><button type="button" className="icon-button" onClick={onClose} aria-label="Close dialog"><Icon name="close"/></button></header>{children}</dialog>;
}
export function Diff({ value }: { value: string }) { const limited = value.slice(0, 350000); return <div><pre className="diff" tabIndex={0} aria-label="Unified diff">{limited ? limited.split("\n").map((line, i) => <span key={i} className={line.startsWith("+++") || line.startsWith("---") || line.startsWith("diff ") ? "diff-file" : line.startsWith("+") ? "diff-add" : line.startsWith("-") ? "diff-remove" : line.startsWith("@@") ? "diff-hunk" : ""}>{line || " "}{"\n"}</span>) : "No file changes in this snapshot."}</pre>{value.length > limited.length && <Notice>Preview limited to 350 KB. Use the CLI to review the complete diff before approving.</Notice>}</div>; }
