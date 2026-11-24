# Placement App workflow

Handle one Linear issue per Codex chat and one feature branch per affected repository.

1. Start a new chat titled with the Linear issue ID and title. Read the ticket and its acceptance criteria. Pull in relevant decisions and test results from related chats when needed. Use the issue ID to find those chats and verify their details against Linear, GitHub, and the current code. If chat history is unavailable, use the ticket, merged pull requests, and commits to reconstruct the relevant context.
2. Fetch `origin/main` and create a separate issue branch from it in each repository the work touches. Use the issue ID in branch names and commits. Keep unrelated local edits out of the change.
3. Implement the issue and verify its acceptance criteria through the normal app flow. Run the relevant tests and builds, and record any checks that still need a person or a live environment.
4. Commit the verified changes, merge the branch into `main` through GitHub, and confirm the merge landed. Then update the Linear ticket with the merged changes and verification results and mark it Done. Start a new chat and branch for the next issue.

Follow the [No AI Slop manifesto](https://www.slopmanifesto.com/) throughout. Read it at the start of a fresh chat if its content is missing from context. Understand the existing code and the user’s goal before changing it. Keep changes and explanations easy to review, verify the behavior that matters, and address the cause when a problem recurs.
