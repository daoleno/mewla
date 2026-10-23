import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { slashCommandEmptyText } from "./codexSlashCommandPresentation";

describe("Codex slash commands have one source", () => {
  const source = readFileSync(
    join(import.meta.dir, "CodexSlashCommands.ts"),
    "utf8",
  );

  test("keeps no second command list in the app", () => {
    expect(source).not.toContain("FALLBACK_SLASH_COMMAND_SPECS");
    expect(source).not.toContain("chatui-fallback");
    expect(source).not.toContain("commandFromFallbackSpec");
  });

  test("starts empty and keeps a loaded list across focus changes", () => {
    expect(source).toContain("useState<CodexSlashCommand[]>([])");
    // Cleared only when the current server or agent kind changes.
    expect(source).toMatch(
      /setSlashCommands\(\[\]\);\s*\}, \[serverId, slashCommandsEnabled\]\)/,
    );
    expect(source).not.toMatch(/\.catch\(\(\) => \{\s*if \(!cancelled\) \{\s*setSlashCommands/);
  });

  test("an unloaded list reads as unavailable, not as no match", () => {
    expect(slashCommandEmptyText(true)).toBe("Codex commands unavailable");
    expect(slashCommandEmptyText(false)).toBe("No matching command");
  });

  test("the composer presentation flags an empty daemon list", () => {
    const model = readFileSync(
      join(import.meta.dir, "InterfaceChatSurfaceModel.ts"),
      "utf8",
    );
    expect(model).toContain(
      "commandsUnavailable: slashCommandsEnabled && slashCommands.length === 0",
    );
  });
});
