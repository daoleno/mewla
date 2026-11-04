# Experiments: why navigate, new tab and screenshot cost ~1-5 s in an agent's tab

Each file is a self-contained probe against the isolated Chrome from `../env.sh up` (CDP :9333, fixture on :8765 via `PORT=8765 node ../fixture.mjs`).

- `background-tab-cdp-settings.mjs`: raw CDP, a background tab of the same window. Frames per 1.5 s after each setting: baseline 0, focus emulation 32, `Page.setWebLifecycleState active` 90, screencast 90.
- `own-window.mjs`: `Target.createTarget {newWindow: true}`: 24-92 frames, navigate 15-29 ms, screenshot 40 ms.
- `popup-from-agent-tab.mjs`: through Mewla's MCP (extension), a popup opened by a real click in the agent's tab: Playwright attaches it, navigation ~21 ms, but it is hidden (0 frames, outerWidth 0) and screenshots time out.

Results and what they mean: SCOREBOARD-browser.md, "Round 4".
