import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import ts from "typescript";
import {
  PRIMARY_DRAWER_DESTINATIONS,
  PRIMARY_DRAWER_PLACES,
  PRIMARY_DRAWER_SETTINGS,
} from "./primaryDrawerDestinations";

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
const destinations = [...PRIMARY_DRAWER_DESTINATIONS, PRIMARY_DRAWER_SETTINGS];

function rows(node: ts.Node) {
  return descendants(node).filter((child): child is ts.JsxSelfClosingElement =>
    ts.isJsxSelfClosingElement(child) && child.tagName.getText(file) === "DrawerRow",
  );
}

describe("primary drawer destinations", () => {
  test("one ordered menu: Brain and Sessions, the tools, then Settings apart", () => {
    expect(PRIMARY_DRAWER_PLACES.map((item) => [item.label, item.route])).toEqual([
      ["Brain", "brain"],
      ["Sessions", "list"],
    ]);
    expect(destinations.map((item) => item.label)).toEqual([
      "Calendar", "Plugins", "Skills", "Stats", "Resources", "Settings",
    ]);
    for (const key of ["key", "label", "pathname", "icon"] as const) {
      expect(new Set(destinations.map((item) => item[key])).size).toBe(destinations.length);
    }
  });

  test("every destination is a runtime route", () => {
    for (const { pathname } of destinations) {
      const file = new URL(`../../app${pathname}.tsx`, import.meta.url);
      const index = new URL(`../../app${pathname}/index.tsx`, import.meta.url);
      expect(existsSync(file) || existsSync(index)).toBe(true);
    }
  });

  test("Plugins lives in the drawer and not in Settings", () => {
    expect(destinations.filter((item) => item.pathname === "/plugins")).toHaveLength(1);
    expect(settingsSource).not.toContain('"/plugins"');
    expect(settingsSource).not.toContain('title="Plugins"');
  });
});

describe("primary drawer panel", () => {
  test("renders one uncaptioned list in the scroll, with Settings and the server status in the footer", () => {
    expect(scroll).toBeDefined();
    expect(footer).toBeDefined();
    const scrolled = rows(scroll);
    expect(scrolled).toHaveLength(1);
    expect(scrolled[0].getText(file)).toContain("onPress={() => openRoute(destination.pathname)}");
    expect(scroll.getText(file)).toContain("PRIMARY_DRAWER_PLACES.map(");
    expect(scroll.getText(file)).toContain("PRIMARY_DRAWER_DESTINATIONS.map(");
    expect(source).not.toContain("Caption");
    expect(source).not.toContain("On this computer");
    const footerRows = rows(footer);
    expect(footerRows).toHaveLength(1);
    expect(footerRows[0].getText(file)).toContain("openRoute(PRIMARY_DRAWER_SETTINGS.pathname)");
    expect(scroll.end).toBeLessThan(footer.pos);
  });

  test("Brain and Sessions lead on every layout and select the page in place", () => {
    expect(source).not.toContain("{docked ? (");
    expect(source).toContain("selected={activePrimaryRoute === place.route}");
    expect(source).toContain("if (!docked) onClose();\n      onSelectPrimaryRoute(route);");
  });

  test("server status is a read-only footer row after Settings, without an always-on dot", () => {
    const status = footer.getText(file);
    expect(status).toContain('accessibilityLabel={`Current server, ${connectionSummary}, ${connectionDetail}`}');
    expect(status.indexOf("PRIMARY_DRAWER_SETTINGS.label")).toBeLessThan(status.indexOf("Current server"));
    expect(scroll.getText(file)).not.toContain("Current server");
    expect(source).not.toContain("StatusPill");
    expect(source).not.toContain('openRoute("/settings")');
  });

  test("navigating dismisses the drawer and closed rows leave the focus order", () => {
    expect(source).toContain("onNavigateAway();\n      router.push(pathname);");
    expect(source).toContain('accessibilityRole="button"');
    expect(source).toContain("accessibilityLabel={label}");
    expect(source).toContain("tabIndex={drawerVisible ? 0 : -1}");
    expect(source).toContain("<Icon name={icon}");
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
