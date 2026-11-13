import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

/**
 * A branch of the UI is an <If>, never a ternary or `&&` around JSX.
 *
 * NSH builds a ternary's or `&&`'s branch again every time anything its condition reads changes, even when the
 * result is the same (NSH docs, jsx-and-components; measured: a Thread mounted 11 times over 10 snapshots under a
 * ternary, once under <If>). What is inside is made anew: a field loses its text and focus, a button the tap under
 * the finger. Owner 11-13 on Extensions: a pasted code vanished, buttons did nothing. `<If when={…} else={…}>` builds
 * a branch again only when the condition's result changes.
 *
 * The sources are read as Bun compiles them (JSX as `__J(…)` calls, no types, no comments, JSX text as strings), so
 * a `?` here is a real conditional.
 */

const UI = join(import.meta.dir, "../ui");

/**
 * Ternaries that stay, each for a reason: a slot whose presence the receiver tests (an <If> element is always
 * present), with a condition that changes only when the person navigates. `file` and the condition as compiled.
 */
const ALLOWED: { file: string; condition: string; why: string }[] = [
  { file: "brain/BrainMenu.tsx", condition: 'page() === "host"', why: "Sheet shows its header row only with a title or a leading; the Model page has neither" },
  { file: "more/friends/FriendsSettings.tsx", condition: "friendsEnabled() && me()", why: "Row's divider inset follows whether it has a leading (an avatar, nothing pressable)" },
];

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    if (statSync(path).isDirectory()) return name === "testing" ? [] : sources(path);
    return /\.tsx$/.test(name) && !/\.test\.tsx$/.test(name) ? [path] : [];
  });
}

const transpiler = new Bun.Transpiler({
  loader: "tsx",
  tsconfig: { compilerOptions: { jsx: "react", jsxFactory: "__J", jsxFragmentFactory: "__F" } } as never,
});

/** The code with every string, template text, regular expression and comment blanked to spaces (same length). */
function code(js: string): string {
  const out = js.split("");
  const blank = (from: number, to: number) => { for (let k = from; k < to; k++) if (out[k] !== "\n") out[k] = " "; };
  const lastSignificant = (at: number) => { let k = at - 1; while (k >= 0 && /\s/.test(out[k])) k--; return k >= 0 ? out[k] : ""; };
  const quoted = (start: number): number => {
    const q = js[start];
    let k = start + 1;
    while (k < js.length && js[k] !== q) k += js[k] === "\\" ? 2 : 1;
    blank(start + 1, k);
    return k + 1;
  };
  const template = (start: number): number => {
    // From after the opening backtick to after the closing one; `${…}` is code, scanned as code is.
    let k = start;
    let textFrom = k;
    while (k < js.length && js[k] !== "`") {
      if (js[k] === "\\") { k += 2; continue; }
      if (js[k] === "$" && js[k + 1] === "{") {
        blank(textFrom, k);
        k = scan(k + 2, true);
        textFrom = k;
        continue;
      }
      k++;
    }
    blank(textFrom, k);
    return k + 1;
  };
  // Code from `start`: strings, templates, comments and regular expressions at any depth. `nested`: inside a
  // template's `${`, it ends after the `}` that closes it (returns the index after it).
  const scan = (start: number, nested: boolean): number => {
    let i = start;
    let depth = 0;
    while (i < js.length) {
      const c = js[i];
      if (c === "'" || c === '"') { i = quoted(i); continue; }
      if (c === "`") { i = template(i + 1); continue; }
      if (c === "/" && js[i + 1] === "/") { const e = js.indexOf("\n", i); blank(i, e < 0 ? js.length : e); i = e < 0 ? js.length : e; continue; }
      if (c === "/" && js[i + 1] === "*") { const e = js.indexOf("*/", i + 2) + 2; blank(i, e); i = e; continue; }
      if (c === "/" && /[(,=:[!&|?{};]|^$/.test(lastSignificant(i))) {
        // A regular expression literal.
        let k = i + 1;
        let inClass = false;
        while (k < js.length && (js[k] !== "/" || inClass)) {
          if (js[k] === "\\") k++;
          else if (js[k] === "[") inClass = true;
          else if (js[k] === "]") inClass = false;
          k++;
        }
        blank(i + 1, k);
        i = k + 1;
        continue;
      }
      if (nested && c === "{") depth++;
      else if (nested && c === "}" && depth-- === 0) return i + 1;
      i++;
    }
    return i;
  };
  scan(0, false);
  return out.join("");
}

/** A branch that is JSX, or a list of it (`xs.map((x) => <Row />)`); JSX in a function made for later is not built here. */
const makesJsx = (segment: string) => /^[\s(]*__J\(/.test(segment) || /\.map\([^]*__J\(/.test(segment);

/** The end of the operand that starts at `from`: the first `,` `)` `]` `}` `;` or `:` (for a ternary's first branch) at depth 0. */
function operandEnd(src: string, from: number, stopAtColon: boolean): number {
  let depth = 0;
  let pending = 0;
  for (let k = from; k < src.length; k++) {
    const c = src[k];
    if (c === "(" || c === "[" || c === "{") depth++;
    else if (c === ")" || c === "]" || c === "}") { if (depth === 0) return k; depth--; }
    else if (depth === 0 && (c === "," || c === ";")) return k;
    else if (depth === 0 && c === "?" && src[k + 1] !== "." && src[k + 1] !== "?" && src[k - 1] !== "?") pending++;
    else if (depth === 0 && c === ":" && stopAtColon) { if (pending === 0) return k; pending--; }
  }
  return src.length;
}

/** The condition before a `?` or `&&` at `at`, as written: back to the start of its operand. */
function conditionBefore(src: string, at: number): string {
  let depth = 0;
  let k = at - 1;
  for (; k >= 0; k--) {
    const c = src[k];
    if (c === ")" || c === "]" || c === "}") depth++;
    else if (c === "(" || c === "[" || c === "{") { if (depth === 0) break; depth--; }
    else if (depth === 0 && (c === "," || c === ";" || c === "?" || c === ":" || (c === ">" && src[k - 1] === "="))) break;
    else if (depth === 0 && c === "&" && src[k - 1] === "&") { k--; continue; }
  }
  return src.slice(k + 1, at).replace(/\s+/g, " ").trim();
}

export function jsxBranches(source: string): string[] {
  const js = transpiler.transformSync(source);
  const src = code(js);
  const found: string[] = [];
  for (let i = 0; i < src.length; i++) {
    const c = src[i];
    if (c === "?" && src[i + 1] !== "." && src[i + 1] !== "?" && src[i - 1] !== "?") {
      const colon = operandEnd(src, i + 1, true);
      if (src[colon] !== ":") continue;
      const whenTrue = src.slice(i + 1, colon);
      const whenFalse = src.slice(colon + 1, operandEnd(src, colon + 1, false));
      if (makesJsx(whenTrue) || makesJsx(whenFalse)) found.push(conditionBefore(js, i));
    } else if (c === "&" && src[i + 1] === "&") {
      if (makesJsx(src.slice(i + 2, operandEnd(src, i + 2, true)))) found.push(conditionBefore(js, i));
      i++;
    }
  }
  return found;
}

describe("a branch of the UI is an <If> (NSH rebuilds a ternary's branch whenever its condition's reads change)", () => {
  test("the scanner finds a ternary and an && around JSX, and nothing else", () => {
    const sample = `export function A(props: { x?: string }) {
      return <View>
        <Text>What? Yes: no</Text>
        {props.x ? <Text>{props.x}</Text> : null}
        {ready() && <Row title={\`a \${b ? "c" : "d"}\`} />}
        <Text>{count() > 1 ? "many" : "one"}</Text>
        <If when={open()} else={<Text>closed</Text>}><Field /></If>
        <Row trailing={busy() ? <Spinner /> : <Button />} />
        <Text>{\`'\${value.replace(/'/g, \`'"'"'\`)}'\`}</Text>
        {late() ? <Row /> : null}
      </View>;
    }`;
    expect(jsxBranches(sample)).toEqual(["props.x", "ready()", "busy()", "late()"]);
  });

  test("no ternary or && around JSX in the client's UI", () => {
    const found = sources(UI).flatMap((path) => {
      const file = relative(UI, path);
      return jsxBranches(readFileSync(path, "utf8"))
        .filter((condition) => !ALLOWED.some((allowed) => allowed.file === file && allowed.condition === condition))
        .map((condition) => `${file}: ${condition} ? <…>  (use <If when={…}>)`);
    });
    expect(found).toEqual([]);
  });

  test("every allowed ternary is still there (an entry that matches nothing goes)", () => {
    for (const allowed of ALLOWED) expect(jsxBranches(readFileSync(join(UI, allowed.file), "utf8"))).toContain(allowed.condition);
  });
});
