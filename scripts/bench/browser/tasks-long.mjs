// Longer tasks: ten or more actions each, where per-action cost compounds.
// Same shape as tasks.mjs; suite.mjs picks them up with --long.
const has = (answer, ...needles) => needles.every(n => answer.toLowerCase().includes(String(n).toLowerCase()));

export const longTasks = [
  { id: 'long-wizard', kind: 'long',
    prompt: 'Open {base}/wizard, a five-step registration. Enter first name Grace, last name Hopper, email grace@navy.example, phone 555-0142, street 1 Cobol Way, city Arlington, choose the Team plan, tick the newsletter box, go through every step and press Finish. Tell me the confirmation text.',
    check: (a, ev) => ev.some(e => e.type === 'wizard' && e.first === 'Grace' && e.last === 'Hopper' && e.email === 'grace@navy.example' &&
      e.phone === '555-0142' && e.street === '1 Cobol Way' && e.city === 'Arlington' && e.plan === 'team' && e.news === true) && has(a, 'Registered Grace Hopper') },
  // SKU-0742 is part 742: 742 * 3.25 + 10 = 2421.50
  { id: 'long-pages', kind: 'long',
    prompt: 'Open {base}/pages, a 12-page parts catalog. Find the price of the part with SKU-0742 by paging through it (there is no search box). Answer with the price.',
    check: a => /2,?421\.50/.test(a) },
  { id: 'long-tabs', kind: 'long',
    prompt: 'Open {base}/product/1, {base}/product/2, {base}/product/3 and {base}/product/4, each in its own tab. Which product is the cheapest, and what does it cost?',
    check: a => has(a, 'Beta lamp') && has(a, '38.9') },
  { id: 'long-todo', kind: 'long',
    prompt: 'Open {base}/todo. Add these six items in order: milk, eggs, bread, rice, tea, salt. Then tick the boxes of eggs, rice and salt. Tell me what the counter under the list says.',
    check: (a, ev) => ev.some(e => e.type === 'todo' && e.total === 6 && e.left === 3) && has(a, '3 left') },
];
