// The browser task suite. Each task: prompt (with {base} for the fixture),
// and check(answer, events) -> boolean, judged from the fixture server's own
// event log and the agent's final answer, never from the agent's claim alone.
const has = (answer, ...needles) => needles.every(n => answer.toLowerCase().includes(String(n).toLowerCase()));
export const tasks = [
  { id: 'form', kind: 'fixture',
    prompt: 'Open {base}/form. Fill in the name "Ada Lovelace", the email ada@example.com, choose the Pro plan, tick "I agree", and submit the form. Then tell me the text the page shows after saving.',
    check: (a, ev) => ev.some(e => e.type === 'form' && e.name === 'Ada Lovelace' && e.email === 'ada@example.com' && e.plan === 'pro' && e.agree === true) && has(a, 'Saved Ada Lovelace') },
  { id: 'async', kind: 'fixture',
    prompt: 'Open {base}/delay, press the Go button and wait for the status text to change. Tell me what it changes to.',
    check: a => has(a, 'Loaded') },
  { id: 'xhr', kind: 'fixture',
    prompt: 'Open {base}/xhr, press Load and tell me the text that appears under it.',
    check: a => has(a, 'XHR done') },
  { id: 'table', kind: 'fixture',
    prompt: 'Open {base}/table. What is the price of "Item 37" and is it open or closed? Answer as "<price> <state>".',
    check: a => has(a, '277.50') && has(a, 'open') },
  { id: 'busy-react', kind: 'fixture',
    prompt: 'Open {base}/busy, a busy chat page. Press the "React 613" button of message 613, then tell me the name of the user who wrote message 613.',
    check: (a, ev) => ev.some(e => e.type === 'react' && e.n === 613) && has(a, 'User 13') },
  { id: 'busy-find', kind: 'fixture',
    prompt: 'Open {base}/busy and tell me the time shown on message 457 (the message that says "Message 457:").',
    check: a => has(a, '10:37') },
  { id: 'multi-tab', kind: 'fixture',
    prompt: 'Open {base}/tab2 and {base}/table in two tabs. Tell me the heading of the first page and whether row 12 of the table is open or closed.',
    check: a => has(a, 'Tab 2') && has(a, 'closed') },
  { id: 'example', kind: 'public',
    prompt: 'Open https://example.com and tell me its main heading.',
    check: a => has(a, 'Example Domain') },
  { id: 'wiki', kind: 'public',
    prompt: 'Using Wikipedia (en.wikipedia.org), find in which year the Eiffel Tower was completed. Answer with the year.',
    check: a => has(a, '1889') },
  { id: 'hn-item', kind: 'public',
    prompt: 'Open https://news.ycombinator.com/item?id=8863 and tell me the title of that post.',
    check: a => has(a, 'Dropbox') },
];

// Run only by naming them with --only: checks of what a task needs from the page being drawn (a screenshot, an
// animation frame), for changes that could stop the page being drawn (a window kept out of the way).
export const extraTasks = [
  { id: 'shot', kind: 'extra',
    prompt: 'Open {base}/table and take a screenshot of the page with the browser screenshot tool. Tell me the first column header you can read in the screenshot.',
    check: a => has(a, 'ID') },
  { id: 'raf', kind: 'extra',
    prompt: 'Open {base}/raf, press Start and wait until the counter shows Done. Tell me the number it ends on.',
    check: a => has(a, '30') },
];
