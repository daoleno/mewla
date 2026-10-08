# Internal engineering notes

Engineering, maintainer and evaluation notes. They stay in the repository and
are **not** published on the docs site. User-facing guides live one level up
and start at [docs/README.md](../README.md).

Some maintainer documents stay at the top of `docs/` because code, scripts or
workflows reference them by path: [architecture](../architecture.md),
[work lifecycle](../work-lifecycle.md), [behavior testing](../behavior-testing.md),
[resource telemetry](../resource-telemetry.md), [CI release](../ci-release.md),
[iOS CI and release](../ios-ci-release.md), [release blockers](../release-blockers.md)
and [third-party assets](../third-party-assets.md).

## Contracts behind the user guides

- [Daemon builds and boot service](daemon-builds.md): installer trust, source and release builds, `mewla boot`
- [Executor internals](executor-internals.md): delegated adapters, gateway routing, resource ownership, structured Chat
- [Security engineering](security-engineering.md): Mewla Link trust, relay metadata, mobile bridge, device admin protocol
- [Mewla Link Relay operations](mewla-link-relay.md)
- [Calendar engineering](calendar-engineering.md)
- [Notification policy](notification-policy.md)
- [Telegram channel](telegram.md)
- [Plugins engineering](plugins-engineering.md), [publisher setup](plugins-publisher-setup.md),
  [Google exchange](plugins-google-exchange.md), [Google options](plugins-google-options.md),
  [Slack manifest](plugins-slack-manifest.json)
- [Usage and pricing details](usage-and-pricing.md) and [OpenCode local usage](opencode-local-usage.md)
- [Web UI](web-ui.md)

## Brain

- [Brain lifecycle](brain-lifecycle.md)
- [Brain engineering](brain-engineering.md) and [engineering evaluation](brain-engineering-evaluation.md)
- [Prompt design](prompting.md)
- [Worker pane identity](worker-pane-identity.md)
- [Control-plane verification](verification.md)

## App and terminal

- [Android development and native contract](android-development.md)
- [iOS development and native contract](ios-development.md)
- [Mobile interface structure](mobile-interface.md), [mobile copy](mobile-copy.md), [interface reading](interface-reading.md)
- [Terminal rendering](terminal-rendering.md) and [terminal scrolling](terminal-scrolling.md)
- [Git review](git-review.md) and [Git diff design](git-diff-design.md)
- [Persistent Browser](persistent-browser.md)
- [DSH Sessions](dsh-sessions.md)
- [Design lint](design-lint.md)

## History

- [Engineering health review, 2026-08-17](engineering-health-2026-08-17.md)
- [Issues redesign](issues-redesign.md) and its [implementation plans](superpowers/plans/)
- [GitHub repository metadata](github-repo-metadata.md)
- `evidence/`: screenshots attached to past reviews
