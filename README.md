--- a/README.md
+++ b/README.md
@@
+## Connecting Linear (assign-to-Zeroth)
+
+Zeroth's primary interface is issue assignment: you assign a Linear issue to
+the Zeroth agent identity, and that assignment starts a run. There is no
+separate queue to manage and no command syntax to learn.
+
+The Zeroth identity is a **Linear OAuth application** authorized for your
+workspace — the same pattern Linear's built-in Cursor integration uses. It
+appears as its own actor in the assignee picker, authors its own comments, and
+does not consume a teammate or guest seat. A personal API key also works for
+quick experiments, at the cost of every agent comment appearing under your own
+name.
+
+Once configured, the loop is: assign the issue -> Zeroth reads the issue and
+the project's memory, works in a sandbox, and comments its plan -> you approve
+from the CLI or the web UI's Approvals inbox -> Zeroth executes and posts back
+the cost, a transcript link, a PR link, and an audit summary. Un-assigning the
+issue mid-run cancels it and tears down the sandbox.
+
+`zerothd` reads its Linear configuration from `ZEROTH_LINEAR_API_KEY`,
+`ZEROTH_LINEAR_AUTH_STYLE`, `ZEROTH_LINEAR_AGENT_USER`,
+`ZEROTH_LINEAR_TEAM_ID`, `ZEROTH_LINEAR_PROJECT_ID`,
+`ZEROTH_LINEAR_POLL_INTERVAL`, and the optional
+`ZEROTH_LINEAR_WEBHOOK_SECRET`, each with a matching `--linear-*` flag.
+
+**See [docs/operator/linear-setup.md](docs/operator/linear-setup.md)** for the
+full walkthrough: creating and authorizing the OAuth application, what every
+variable and flag means, the end-to-end operator flow, and troubleshooting.
+The most common setup failure is a mismatch between the style of credential in
+`ZEROTH_LINEAR_API_KEY` and the value of `ZEROTH_LINEAR_AUTH_STYLE`.
+
