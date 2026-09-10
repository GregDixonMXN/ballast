# Contributing

Keep Ballast focused on trustworthy local coordination: task → isolated worktree
→ exact review → explicit integration. Keep changes reviewable and preserve
existing user files and worktrees on failure.

Run `./scripts/check.sh` before submitting a change. Storage, Git, execution,
authorization, and HTTP changes need regressions exercising actual failure modes.
Use temporary repositories and isolated state only. Never run a database test
against a production or personal database; use the dedicated fixture command in
the development guide.

Do not commit `.ballast/`, tokens, `.env` files, test recordings containing private
source, build caches, or dependency directories. Include documentation changes
when altering startup flags, recovery behavior, or security boundaries.

Frontend checks live in `apps/web`; use its locked dependencies with `npm ci`.
UI changes should be reviewed at narrow and desktop sizes and with keyboard-only
navigation, loading errors, disconnected state, and hostile user-supplied strings.

The repository owner has not selected a distribution/contribution license yet.
Agree on contribution rights before accepting third-party code.
