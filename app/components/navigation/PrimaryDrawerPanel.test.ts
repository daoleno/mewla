import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import ts from "typescript";
import { PRIMARY_DRAWER_GROUPS } from "./primaryDrawerDestinations";

const source = readFileSync(new URL("./PrimaryDrawerPanel.tsx", import.meta.url), "utf8");
const settingsSource = readFileSync(new URL("../../app/settings.tsx", import.meta.url), "utf8");
const file = ts.createSourceFile("PrimaryDrawerPanel.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);

function descendants(node: ts.Node): ts.Node[] {
  const children: ts.Node[] = [];
  node.forEachChild((child) => { children.push(child, ...descendants(child)); });
  return children;
}

const nodes = descendants(file);
const scroll = nodes.find((node): node is ts.JsxElement =>
  ts.isJsxElement(node) && node.openingElement.tagName.getText(file) === "ScrollView",
)!;
const footer = nodes.find((node): node is ts.JsxElement =>
  ts.isJsxElement(node) && node.openingElement.attributes.getText(file).includes("styles.drawerFooter"),
)!;
const destinations = PRIMARY_DRAWER_GROUPS.flat();

function rows(node: ts.Node) {
  return descendants(node).filter((child): child is ts.JsxSelfClosingElement =>
    ts.isJsxSelfClosingElement(child) && child.tagName.getText(file) === "DrawerRow",
  );
}

describe("primary drawer destinations", () => {
  test("lists each destination exactly once, current-server tools before Settings", () => {
    expect(PRIMARY_DRAWER_GROUPS.map((group) => group.map((item) => item.label))).toEqual([
      ["Plugins", "Skills", "Stats", "Browser", "Resources"],
      ["Settings"],
    ]);
    for (const key of ["key", "label", "pathname", "icon"] as const) {
      expect(new Set(destinations.map((item) => item[key])).size).toBe(destinations.length);
    }
  });

  test("every destination is a runtime route", () => {
    for (const { pathname } of destinations) {
      expect(existsSync(new URL(`../../app${pathname}.tsx`, import.meta.url))).toBe(true);
    }
  });

  test("Plugins lives in the drawer and not in Settings", () => {
    expect(destinations.filter((item) => item.pathname === "/plugins")).toHaveLength(1);
    expect(settingsSource).not.toContain('"/plugins"');
    expect(settingsSource).not.toContain('title="Plugins"');
  });
});

describe("primary drawer panel", () => {
  test("renders one row per destination inside the scroll, with only the version in the footer", () => {
    expect(scroll).toBeDefined();
    expect(footer).toBeDefined();
    const rendered = rows(file);
    expect(rendered).toHaveLength(1);
    expect(descendants(scroll)).toContain(rendered[0]);
    expect(source).toContain("PRIMARY_DRAWER_GROUPS.map(");
    expect(rendered[0].getText(file)).toContain("onPress={() => openRoute(destination.pathname)}");
    expect(rows(footer)).toHaveLength(0);
    expect(scroll.end).toBeLessThan(footer.pos);
    expect(footer.getText(file)).toContain("Zen v{appVersion}");
  });

  test("server status is a read-only header without a second Settings entry or always-on dot", () => {
    expect(source).toContain('accessibilityLabel={`Current server, ${connectionSummary}, ${connectionDetail}`}');
    expect(source).not.toContain("StatusPill");
    expect(source).not.toContain('openRoute("/settings")');
  });

  test("navigating dismisses the drawer and closed rows leave the focus order", () => {
    expect(source).toContain("onNavigateAway();\n      router.push(pathname);");
    expect(source).toContain('accessibilityRole="button"');
    expect(source).toContain("accessibilityLabel={label}");
    expect(source).toContain("tabIndex={drawerVisible ? 0 : -1}");
    expect(source).toContain("satisfies Record<PrimaryDrawerIcon,");
  });

  test("reserves footer space within safe area instead of overlaying scroll content", () => {
    const stylesCall = nodes.find((node): node is ts.CallExpression =>
      ts.isCallExpression(node) && node.expression.getText(file) === "StyleSheet.create",
    )!;
    const styles = stylesCall.arguments[0] as ts.ObjectLiteralExpression;
    const style = (name: string) => (styles.properties.find((node) => node.name?.getText(file) === name) as ts.PropertyAssignment).initializer.getText(file);
    expect(style("drawerScroll")).toContain("flex: 1");
    expect(style("drawerScroll")).toContain("minHeight: 0");
    expect(style("drawerFooter")).toContain("flexShrink: 0");
    expect(style("drawerFooter")).not.toContain("absolute");
    expect(style("drawerRow")).toContain("minHeight: 52");
    expect(source).toContain('edges={["top", "bottom"]}');
  });
});
