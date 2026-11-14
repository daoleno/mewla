import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

/**
 * No <List> has an `empty` slot: a list's empty state is its footer while it has no rows.
 *
 * NSH 0.5.0's List renders `empty` in a root the list never disposes (longbridge/nsh#1984, fixed upstream by
 * longbridge/nsh#1990). A page left while its list was still empty (a computer switch away from one that had not
 * answered yet) kept that cell alive under a freed owner; once the slot was reused, the reactive graph's owner walk
 * looped and the app froze. The footer is disposed with the list, and with no rows NSH places it exactly where the
 * empty cell goes. Once the app runs on an NSH with #1990, `empty` may come back and this test may go.
 */

const UI = join(import.meta.dir, "../ui");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx$/.test(name) && !/\.test\.tsx$/.test(name) ? [path] : [];
  });
}

/** The top-level attribute names of each `<List …>` opening tag in `tsx`, with the line it starts on. */
export function listAttributes(tsx: string): { line: number; names: string[] }[] {
  const found: { line: number; names: string[] }[] = [];
  const open = /<List(?=[\s>/])/g;
  for (let m = open.exec(tsx); m; m = open.exec(tsx)) {
    const names: string[] = [];
    let depth = 0;
    let quote = "";
    let k = m.index + m[0].length;
    for (; k < tsx.length; k++) {
      const c = tsx[k];
      if (quote) {
        if (c === "\\") k++;
        else if (c === quote) quote = "";
        continue;
      }
      if (depth === 0 && (c === '"' || c === "'")) { quote = c; continue; }
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (depth === 0 && c === ">") break;
      else if (depth === 0 && /[A-Za-z_]/.test(c) && /\s/.test(tsx[k - 1] ?? "")) {
        const name = /^[A-Za-z_][\w-]*/.exec(tsx.slice(k))![0];
        names.push(name);
        k += name.length - 1;
      }
    }
    found.push({ line: tsx.slice(0, m.index).split("\n").length, names });
  }
  return found;
}

describe("a list's empty state", () => {
  test("the scan sees a List's attributes and nothing inside their values", () => {
    const tsx = `<List each={rows()} header={<View empty={1} />}\n  empty={() => <Empty />}>{(r) => <Text>{r()}</Text>}</List>`;
    expect(listAttributes(tsx)).toEqual([{ line: 1, names: ["each", "header", "empty"] }]);
    expect(listAttributes(`<ListRow empty={1} />`)).toEqual([]);
  });

  test("no List in the UI has an empty slot (its footer shows the empty state)", () => {
    const offenders = sources(UI).flatMap((file) =>
      listAttributes(readFileSync(file, "utf8"))
        .filter((list) => list.names.includes("empty"))
        .map((list) => `${relative(UI, file)}:${list.line}`),
    );
    expect(offenders).toEqual([]);
  });
});
