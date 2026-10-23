import { describe, expect, test } from "bun:test";

import { detectTerminalFlavor, terminalFlavorLabel } from "./terminalFlavor";

describe("detectTerminalFlavor", () => {
  test("follows the foreground binary, whatever its path or arguments", () => {
    const cases: Array<[string, ReturnType<typeof detectTerminalFlavor>]> = [
      ["ssh dev@host", "ssh"],
      ["/usr/local/go/bin/go test ./...", "go"],
      ["cargo build --release", "rust"],
      ["python3 manage.py runserver", "python"],
      ["python3.12 -m http.server", "python"],
      ["uv run pytest", "python"],
      ["npm run dev", "node"],
      ["node22 server.js", "node"],
      ["vitest --watch", "typescript"],
      ["bunx expo start", "bun"],
      ["docker compose up", "docker"],
      ["kubectl get pods", "kubernetes"],
      ["psql -h db", "postgres"],
      ["redis-cli", "redis"],
      ["git rebase -i HEAD~3", "git"],
      ["mvn test", "java"],
      ["ruby3.2 app.rb", "ruby"],
      ["php artisan serve", "php"],
      ["terraform plan", "terraform"],
      ["aws s3 ls", "aws"],
      ["-zsh", "shell"],
      ["zsh", "bash"],
      ["fish", "bash"],
    ];
    for (const [command, flavor] of cases) {
      expect({ command, flavor: detectTerminalFlavor({ command }) }).toEqual({ command, flavor });
    }
  });

  test("ignores everything but the command", () => {
    // Words that used to steer the guess: a Python traceback in the pane, an
    // "arch" directory, a project called "ubuntu-docker". None is the program.
    const worker = {
      command: "tail -f build.log",
      name: "docker python go",
      project: "ubuntu-docker",
      cwd: "/home/me/arch/go.mod",
      summary: "Traceback (most recent call last):",
      last_output_lines: ["Traceback (most recent call last):", "ok  example.com/x 0.1s"],
    };
    expect(detectTerminalFlavor(worker)).toBe("shell");
  });

  test("an unknown or empty command is a plain shell", () => {
    expect(detectTerminalFlavor({ command: "" })).toBe("shell");
    expect(detectTerminalFlavor({ command: undefined as unknown as string })).toBe("shell");
    expect(detectTerminalFlavor({ command: "htop" })).toBe("shell");
    expect(terminalFlavorLabel("shell")).toBe("Shell terminal");
    expect(terminalFlavorLabel("go")).toBe("Go");
  });
});
