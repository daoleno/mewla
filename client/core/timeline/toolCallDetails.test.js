import { describe, expect, test } from "bun:test";
import {
  buildExpandedToolDetails,
  cleanUserFacingOutput,
  extractTransportMetadata,
  isTransportOnlyPayload,
  isWaitLikeToolName,
  isWaitSessionPoll,
} from "./toolCallDetails";
import { buildTimeline } from "./model";

describe("toolCallDetails", () => {
  test("read details prefer file paths over exec_command", () => {
    const details = buildExpandedToolDetails({
      toolName: "exec",
      input: `const r = await tools.exec_command({"cmd":"sed -n \\"1,40p\\" app/services/toolCallSemantics.ts","workdir":"/repo"});`,
      output: "export type SemanticActionKind =",
      status: "done",
      semanticKind: "read_files",
    });
    expect(details.files).toEqual(["app/services/toolCallSemantics.ts"]);
    expect(details.command).toBe(
      'sed -n "1,40p" app/services/toolCallSemantics.ts',
    );
    expect(details.result).toContain("SemanticActionKind");
    expect(details.quietDetail).toBe("app/services/toolCallSemantics.ts");
    expect(details.statusLine).toBe("Done");
    expect(details.developer?.providerToolId).toBeUndefined();
  });

  test("search details surface query", () => {
    const details = buildExpandedToolDetails({
      toolName: "Grep",
      input: JSON.stringify({ pattern: "SemanticAction", path: "app" }),
      status: "done",
      semanticKind: "search_code",
    });
    expect(details.query).toBe("SemanticAction");
    expect(details.quietDetail).toBe("SemanticAction");
  });

  test("wait session poll hides card", () => {
    expect(
      isWaitSessionPoll(
        "wait",
        JSON.stringify({ session_id: 98430, chars: "", yield_time_ms: 30000 }),
      ),
    ).toBe(true);
    const details = buildExpandedToolDetails({
      toolName: "wait",
      input: JSON.stringify({
        session_id: 98430,
        chars: "",
        yield_time_ms: 30000,
      }),
      output: JSON.stringify({
        chunk_id: "build-2",
        wall_time: 7.8274,
        session_id: 98430,
        original_token_count: 4,
      }),
      status: "done",
    });
    expect(details.hideCard).toBe(true);
  });

  test("unlinked wait shows Finished line without raw JSON", () => {
    const details = buildExpandedToolDetails({
      toolName: "wait",
      output: JSON.stringify({
        chunk_id: "build-2",
        wall_time: 7.8274,
        session_id: 98430,
        original_token_count: 4,
      }),
      status: "done",
    });
    expect(details.hideCard).toBeFalsy();
    expect(details.statusLine).toContain("Finished");
    expect(details.statusLine).toContain("7.8s");
    expect(details.result).toBeUndefined();
  });

  test("strips Codex metadata", () => {
    expect(
      cleanUserFacingOutput(
        "Chunk ID: abc\nWall time: 1.0 seconds\nProcess exited with code 0\nOriginal token count: 4\nOutput:\nBUILD SUCCESSFUL",
      ),
    ).toBe("BUILD SUCCESSFUL");
    expect(
      isTransportOnlyPayload(
        JSON.stringify({
          chunk_id: "x",
          wall_time: 1,
          session_id: 1,
          original_token_count: 0,
        }),
      ),
    ).toBe(true);
    expect(
      extractTransportMetadata(undefined, "Wall time: 2.5 seconds\n"),
    ).toEqual({
      wall_time: "2.5",
    });
    expect(isWaitLikeToolName("write_stdin")).toBe(true);
  });
});

describe("timeline wait/read presentation", () => {
});
