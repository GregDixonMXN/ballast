# Runners

The runner is a same-host execution daemon. The API creates detached Git worktrees
and dispatches work; the runner polls, executes an adapter, runs a supplied test
command, and reports the outcome. The operator separately reviews and integrates.

See [Installation](INSTALL.md) for startup flags and file paths. Runners must see
the exact same absolute repository/worktree paths as the control plane. They are
not remote artifact-relay workers and do not provide a sandbox.

Use trusted commands only. Child execution is bounded and control-plane environment
credentials are not inherited indiscriminately. Diffs, task prompts, and tool output
may still contain secrets; Ballast does not promise universal redaction.

The adapters invoke installed command-line tools; Ballast does not include agent
subscriptions or provider credentials. A custom executable can be supplied with
`--agent /absolute/path/to/executable`. Use `--agent-home` to opt into a dedicated private provider login/configuration
directory; the default disposable home is unauthenticated. This is explicit
read/write access, not a sandbox. Provider authentication must be configured
through that tool's supported private setup, never placed into task prompts.

Stop and restart/re-register runners after restarting the control plane. Inspect
interrupted tasks and retained workspaces before retrying. Exactly-once remote
execution and automatic in-flight replay are not promised by this release.
