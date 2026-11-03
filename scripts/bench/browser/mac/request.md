REQUEST from the Linux Brain on behalf of Worker "BrowserSpeed" (browser-use speed). One bundled job, read-only: discovery of the Codex desktop app (bundle ChatGPT.app, com.openai.codex 26.1027), then a 10-task browser benchmark run through that app's own tools. Test pages are served from this Mac on loopback; the public sites are read-only. No logged-in sites, no sending, no purchases. The user wants Codex.app (the desktop app) as the reference for speed and behaviour, not the codex CLI.

You are not to use Mewla's own browser or computer tools to do the tasks; only to start Codex.app and read its logs. Use one Worker, one task at a time, and say "Mac free" back to me when done (computers send linux -kind reply).

## A. Discovery (record every answer in the result file)
1. App: version/build from Info.plist, bundle path, bundled plugins (browser@, chrome@, computer-use@, unified-computer-use@ openai-bundled): each plugin's manifest, whether it is an MCP server (command, args, transport), and where they live (look inside the app bundle and ~/.codex/plugins, ~/.codex/config.toml, ~/Library/Application Support).
2. Settings that matter: computer use on/off, browser use mode (in-app browser vs @Chrome), Chrome extension for @Chrome installed or not (and in which Chrome profile), approval/permission mode, model and reasoning effort, macOS permissions it holds (Screen Recording, Accessibility).
3. How to start a task with no person: check Info.plist CFBundleURLTypes (a URL scheme such as codex:// and its new-task parameters), the app's AppleScript/Shortcuts/App Intents, the bundled `codex` binary (app-server / exec / remote-control options) and whether tasks it starts appear in the desktop app and use the same plugins, and a shared sessions directory. Pick the way that runs the desktop app's own agent with its own computer and browser tools. If the only way is typing into its window, do that through the Mac's own screen tools and keep the same window for all tasks; count only Codex's own time from its logs. If a task truly needs a human keypress, collect them all into ONE question to me.
4. Session logs: where Codex.app writes them (likely ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl); confirm the event types that carry timestamps, tool calls (name, arguments, output) and token counts.

## B. Fixture site (save as fixture.py in a scratch dir, run `python3 -I fixture.py 8765`, stop it at the end; GET /__events shows what pages did, POST /__reset clears it)
@@FIXTURE@@

## C. Tasks (the prompt is what you give Codex.app, with BASE=http://127.0.0.1:8765; one fresh chat per run; reset /__reset before each run)
1 form: "Open BASE/form. Fill in the name "Ada Lovelace", the email ada@example.com, choose the Pro plan, tick "I agree", and submit the form. Then tell me the text the page shows after saving." Pass: /__events has {type:form,name:Ada Lovelace,email:ada@example.com,plan:pro,agree:true} and the answer says "Saved Ada Lovelace".
2 async: "Open BASE/delay, press the Go button and wait for the status text to change. Tell me what it changes to." Pass: answer has "Loaded".
3 xhr: "Open BASE/xhr, press Load and tell me the text that appears under it." Pass: "XHR done".
4 table: "Open BASE/table. What is the price of "Item 37" and is it open or closed? Answer as "<price> <state>"." Pass: 277.50 and open.
5 busy-react: "Open BASE/busy, a busy chat page. Press the "React 613" button of message 613, then tell me the name of the user who wrote message 613." Pass: /__events has {type:react,n:613} and the answer has "User 13".
6 busy-find: "Open BASE/busy and tell me the time shown on message 457 (the message that says "Message 457:")." Pass: 10:37.
7 multi-tab: "Open BASE/tab2 and BASE/table in two tabs. Tell me the heading of the first page and whether row 12 of the table is open or closed." Pass: "Tab 2" and closed.
8 example: "Open https://example.com and tell me its main heading." Pass: Example Domain.
9 wiki: "Using Wikipedia (en.wikipedia.org), find in which year the Eiffel Tower was completed. Answer with the year." Pass: 1889.
10 hn-item: "Open https://news.ycombinator.com/item?id=8863 and tell me the title of that post." Pass: Dropbox.
Run tasks 1-10 once first, then repeat the whole set until 3 runs each (stop at 2 if each run needs hand-driving). Use the browser tool Codex picks by itself for a web task (and note which: in-app browser, @Chrome, or computer use). Run each task a second time with the prompt prefixed by "@Chrome " (if the extension is installed) and once with "@Computer " for tasks 1, 4, 5 only, as separate comparison rows.

## D. What to measure per run (from the session log, not stopwatch)
wall-clock (first user message to final assistant message), number of model turns, number of tool calls with the tool name of each, per-tool-call latency (call to output), input/cached/output/reasoning tokens (token_count events), final answer, pass/fail by the checks above, errors and retries. Also total including the time Codex.app took to open/restore a window if you see it.

## E. Behaviour notes (short, factual, with evidence)
How it observes the page (pixels vs accessibility/DOM text; does it take screenshots every step), whether it works in the background or takes the foreground (window focus, pointer movement, did the Mac user's other apps get disturbed), whether it uses a virtual or background screen or its own browser window/tab group, how it recovers from a failed step, what it asked the user (permission prompts, approvals) and when, how many steps it batches in one call, whether it re-reads the page after each action, and anything it does that makes it fast.

## F. Output
Write one JSON file (array of runs with the fields in D, plus the task id and tool used) and one short Markdown report (discovery A + notes E + per-task median table) under your workspace worklog; reply to me with both paths and a 10-line summary: median wall-clock, turns, tool calls, tokens and pass rate per task. Clean up: stop the fixture server, close tabs/windows and chats you opened, delete scratch files. Report anything you could not do.
