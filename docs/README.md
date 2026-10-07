# Mewla documentation

Mewla is a phone app plus a small daemon on your own computer. Every coding agent
and shell on that computer becomes a Session you can read as chat, drive as a
live terminal, or hand to Brain, which splits a goal into Work for other agents.

New here? Read [Get started](get-started.md), then follow the guides in order.

## Start

1. [Get started](get-started.md): what Mewla is and how the pieces fit.
2. [Install](install-daemon.md): the daemon on your computer and the app on your phone.
3. [Connect and pair](connect-and-pair.md): reach the daemon from your phone, on the same Wi-Fi or from anywhere.

## Agents

- [Agents and executors](executors.md): which agent CLIs Mewla runs, the safe profile, and how Brain picks a model.
- [Brain and Work](brain-and-work.md): give Brain a goal and follow the Work it hands to Workers.

## Around the agents

- [Plugins](plugins.md): connect Linear, Notion, GitHub, Slack, Google Workspace or a custom service.
- [Calendar](calendar.md): events, reminders, deadlines and scheduled actions.
- [Services](services.md): ports your agents opened, with an optional temporary public URL.
- [Notifications and Telegram](notifications.md): when Mewla interrupts you, and Telegram as a second channel.
- [Providers and usage](providers-and-usage.md): your own model endpoints and keys, and what each model cost.

## Reference

- [Security and privacy](security-and-privacy.md): what is trusted, what is exposed and where data lives.
- [Troubleshooting](troubleshooting.md): fixes for the common problems.
- [Releases](releases/README.md): release notes and known issues.

<!-- repo-only -->
## For contributors

These files stay in the repository and are not published on the docs site.

- [Contributing](../CONTRIBUTING.md) and [internal engineering notes](internal/README.md)
- [Architecture](architecture.md)
- [Work lifecycle](work-lifecycle.md) and [behavior testing](behavior-testing.md)
- [Resource telemetry contract](resource-telemetry.md)
- [CI release pipeline](ci-release.md) and [iOS CI and TestFlight](ios-ci-release.md)
- [Known release blockers](release-blockers.md)
- [Third-party assets and licenses](third-party-assets.md)

The public docs site is generated from the pages above by
`scripts/site-docs/build.py`; its page list is `scripts/site-docs/nav.json`.
<!-- /repo-only -->
