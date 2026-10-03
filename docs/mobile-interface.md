# Mobile interface structure

This page describes how the Android and iOS app is laid out and which shared patterns each screen uses. Both platforms share every screen. On iOS 26 and later, the glass material uses system Liquid Glass. Elsewhere it falls back to a translucent fill with one continuous 1pt outline and no separate lit edge. Reduce Transparency switches it to an opaque fill.

The guiding rule: a primary page shows only what you need right now. Low-frequency and advanced actions live in the page action menu, a sheet, or a detail page. Status marks appear only for states you can act on.

## Primary shell

- The app bar floats over content. It has a circular menu button, a segmented **Brain / Sessions** switch, and one page action button.
- The menu button stays clean while the current server is healthy. A dot appears only when there is something to act on: red for a connection issue, amber while the server is offline. The button's accessibility label names the state. Connecting is transient and shows no dot.
- The drawer is pure navigation:
  - A read-only header shows the current server and its connection state. The state text is colored only when the server is offline or has an issue.
  - The first card holds the current server's destinations: **Plugins**, **Skills**, **Stats**, and **Resources**. A second card holds **Settings**. Each destination appears once, with a tinted icon tile like Settings rows.
  - Open it with the menu button, or swipe right from the leading edge on Brain (Sessions keeps horizontal swipes for the pager). Close it with the close button, a tap or swipe on the backdrop, a left swipe anywhere, or system Back. The open state lives on the UI thread, so a close is never lost while JavaScript is busy.
  - The footer shows the app version.

## Brain (launch screen)

Brain is the chat with the current server's Brain. Errors and historical read-only threads appear as inline notices above the chat. When the executor has no structured chat view, the empty state offers **Switch executor** and **Open terminal**. The page action opens the Brain action sheet: New chat, Switch executor, Open terminal, Browse workspace, and Calendar.

## Sessions

The Sessions page is the list of the current server's Sessions, grouped by directory into inset rounded cards. Above the list, a notice appears only when needed:

1. **Server offline** (or the connection issue) with **Retry**, when the current server is unreachable.

In every other case nothing sits above the list.

- The floating **+** button creates a Session.
- The page action opens a menu with **New session** and **Services**.
- Long press enters multi-select for termination.
- There is no pull-down gesture.

All data comes from the current server, and sheets close when you switch servers.

### Services

Services is a sheet with one row per listening port, grouped by project. Each row shows the port, the process, and its Session or persistent unit. A **Public** pill marks a running Quick Tunnel. Tapping a row opens its detail page inside the same sheet:

- **Open**: the service's URLs (or DSH Web), plus **Open Session terminal** when the Session is live.
- **Public access**: start a Quick Tunnel (after a confirmation alert), then open or copy the public URL, or stop the tunnel.
- **Details**: the process, command, bind address, and persistent status.

## Session screen

The header is three glass capsules: Back, the Session identity, and actions. The identity capsule shows a status dot for running, blocked, or failed Sessions, and tapping it opens Session details. Session actions open as a bottom sheet with **Terminate** last. Empty, loading, error, and "chat view unavailable" states use one chat-canvas empty state. A fresh Session shows **Ready** along with its workspace path.

### Chat

- The composer is one glass capsule: a quiet tinted **+** on the leading side and a filled accent send disc on the trailing side. Both are 34pt discs inside 44pt targets. The send disc is muted when disabled, shows dots while sending, and turns into a neutral stop disc (with elapsed time) while the agent runs.
- Attachments are uniform rounded tiles with an in-bounds remove target. Uploads show progress and a Cancel.
- User messages are rounded bubbles. Assistant replies are full-width prose. Tool and activity rows are quieter notes: a small tinted icon tile, muted 12pt title, and a detail rail when expanded.
- Each send has one user bubble. A canonical receipt replaces its local pending row, including when the receipt arrives first; provider echoes use causal identity, never body-only deduplication. Reconnect snapshots upsert by event ID. Brain preserves the receipt row when its Host transcript is discovered after sending, and records the exact native Session and consumed echo ID for replay. Independent sends with identical text remain separate; retry keeps the same logical input.
- Composer errors appear as an alert strip above the capsule. The date divider is a pill, and jump-to-latest is a glass disc.

### Git

The Git sheet has one Back/Close control, a title with a one-line change summary, and a single **More actions** menu. Changed files sit in one inset group with status letter tiles. Diffs use soft add/remove tints with a two-column line gutter. See [git-diff-design.md](git-diff-design.md).

## Settings

Settings is one grouped list:

- **Servers**: each server shows a dot only when it is not connected. The current server carries an **In use** tag. Tapping a server opens its actions: Use, Connect, Disconnect or Retry, then Edit, then Remove.
- **Channels**: one **Telegram** row with the bot name and a status pill. It opens the Telegram page, which shows the identity, one primary next step (Verify token, Connect Telegram, Open Telegram, or Reconnect), grouped secondary actions, and diagnostics as one-line label/value rows (long IDs truncate in the middle and copy on tap). Destructive actions (Unlink account, Remove bot) sit behind **Advanced**.
- **Agents**: one **Model Providers** row, which opens Model Providers: how Codex and Claude reach models (official login or your own API keys).
- **Appearance**: one segmented control (Auto / Light / Dark). Theme lives only here.

## Plugins

Plugins connects outside services to the current server. A context row names that server and its connection state. **Connected** lists each active account with a status pill (Connected, Not verified, Last call failed, Reconnect required, Disabled). **Add a service** lists the reviewed services, and **Custom services** holds MCP and OpenAPI. A service page shows the service, then either the connection steps, the connected account, or the access to grant before connecting. See [Plugins](plugins.md).

## Skills

Skills manages the Skills that local Agents load on the current server. A context row names the server, the project the inventory was read for (or **Global only**), and the connection state. A segmented control switches between **Skills** and **Agent Plugins**:

- **Skills** lists standalone Skills with search, filters and pull to refresh. Skills that an installed Agent Plugin provides are listed inside that Plugin instead, and a notice links to them. If Plugin ownership can't be read, a notice says so and offers **Retry**. Those Skills stay protected.
- A Skill opens a detail sheet (a side panel on wide screens). Its header shows where the Skill comes from (**Standalone**, **Built-in** or **From** the Plugin) and whether it can be deleted. Deleting works on one exact copy and needs the server-built confirmation. Plugin-provided copies can't be deleted here and link to their Plugin.
- **Agent Plugins** lists installed Agent Plugins. Each one shows its Skills, components, Agents and copies. Uninstalling removes one exact copy after confirmation and is checked against a fresh inventory.

Loading, offline, empty and error states use the shared empty state with **Try again**, **Refresh** or **Open Settings**.

## Browser

Browser manages reusable browsers on the current server: sign in once, then Agent tasks can reuse those sign-ins. The list shows server context, each browser's state and Agent access, and a create row; the viewer keeps one primary control action (Take control / Release control / Reconnect). Errors use readable cards with a single recovery and collapsed details. See [persistent-browser.md](persistent-browser.md).

## Model Providers

A segmented **Codex / Claude** switch picks the agent. Below it, one grouped list shows which connection that agent uses: **Official login** first, then saved Providers. Each has a radio mark, its host, model count and catalog age, and any test result. A **Key required** pill marks Providers without a credential. Each Provider's **…** opens an action menu: Models, Test connection, Edit, and Delete (confirmed). **Add** sits in the section header. Search appears only once the list is long. The Zen Provider Gateway is its own row with a status pill, and tapping it copies the endpoint.

## Shared patterns

- **Overflow menus** always use the bottom-sheet `ActionMenu`. This covers Brain, Sessions, Session, Work detail, per-server actions in Settings, and per-Provider actions.
- **Destructive actions** go through `confirmDestructive` or a native alert where the destructive button is never the default. This covers terminating Sessions, removing a server, deleting a Work item, deleting a Provider, and removing or unlinking Telegram.
- **Status** uses `StatusPill`, whose live pulse stops under Reduce Motion. Recoverable problems and in-flow status use `InlineNotice`, and empty, loading, and blocking error states use `EmptyState`.
- **Lists** use grouped `ListSection` / `ListRow` rows. Small exclusive choices use `SegmentedControl`.

## Visual tokens

Color lives in `app/theme/primitives.ts` and reaches components through `useZenTheme()` / `useAppColors()`. Type, spacing, radii and shadows live in `app/constants/tokens.ts`. The terminal emulator palette in `app/constants/terminalThemes.ts` is separate.

- **Canvas.** Dark mode uses a warm ink canvas (`#12120E`), not a blue-black one, so sage sits on it without vibrating. Light mode uses a warm stone canvas (`#F5F3EE`) with near-white paper cards.
- **Elevation.** Canvas, surface, elevated and pressed are distinct tonal steps. Hairlines are not the only thing that separates them. Buttons, chips, the composer and other in-flow controls are flat. Only floating sheets, menus, toasts and the FAB cast a shadow, and on Android each one owns its radius and an opaque fill so the elevation shadow follows the shape.
- **Accent.** Settings → Appearance picks one of four accents: Sage (default), Ink, Clay or Stone. Every ramp shares the sage OKLCH lightness profile at its own hue (156, 262, 36, 80). Clay is dusty so it stays clear of danger and warning. Stone is near-monochrome, so its running status borrows Ink to stay apart from the grey unknown status. Ivory `#F2EEE5` remains the brand detail colour on the dark mark.
- **Status.** Success is teal, not a second sage, so it stays distinct from every accent, danger and warning under common colour-vision deficiencies. Status is never conveyed by hue alone: worker rows also carry a status icon.
- **Contrast.** Contrast is an acceptance gate. `app/theme/contrast.test.ts` checks every shipped text/background pairing in both schemes against WCAG AA:
  - 4.5:1 for text.
  - 3:1 for focus rings, strong borders, status glyphs and the densest heatmap cell.
  - Translucent materials and the selection colour are blended onto their real background before measuring.
  - Decorative hairlines (`border`, `borderSubtle`, material separators) are intentionally exempt.
- **Radii.** One 4pt ladder (8/12/16/20/24/28). `Radii.card` is 20 for grouped sections, cards and menus. `Radii.sheet` is 28 for bottom sheets. Circular controls use half their size.

Terminal mode retains the Ghostty live grid and uses native viewport scrolling over styled pane history. See [Terminal scrolling](terminal-scrolling.md) for history limits, full-screen app behavior, and resize semantics.
