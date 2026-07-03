import * as cp from "child_process";
import * as fs from "fs";
import * as vscode from "vscode";

export class ProcessManager {
  private child?: cp.ChildProcess;
  onExit?: (code: number | null, signal: NodeJS.Signals | null) => void;

  constructor(private readonly output: vscode.OutputChannel) {}

  get isRunning(): boolean {
    return !!this.child;
  }

  async start(binaryPath: string, cwd: string, port: number): Promise<void> {
    if (this.child) {
      return;
    }
    await fs.promises.mkdir(cwd, { recursive: true });

    this.output.appendLine(`[extension] starting ${binaryPath} (cwd=${cwd}, port=${port})`);
    const child = cp.spawn(binaryPath, [], {
      cwd,
      env: { ...process.env, PORT: String(port), NO_BROWSER: "1" },
      windowsHide: true,
    });
    this.child = child;

    child.stdout?.on("data", (d: Buffer) => this.output.append(d.toString()));
    child.stderr?.on("data", (d: Buffer) => this.output.append(d.toString()));
    child.on("exit", (code, signal) => {
      this.output.appendLine(`[extension] claude-proxy exited (code=${code} signal=${signal})`);
      this.child = undefined;
      this.onExit?.(code, signal);
    });
    child.on("error", (err) => {
      this.output.appendLine(`[extension] failed to start claude-proxy: ${err.message}`);
      this.child = undefined;
    });

    try {
      await this.waitUntilReady(port);
    } catch (err) {
      this.stop();
      throw err;
    }
  }

  private async waitUntilReady(port: number, timeoutMs = 5000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (!this.child) {
        throw new Error("claude-proxy exited before becoming ready");
      }
      try {
        const res = await fetch(`http://localhost:${port}/_proxy/api/setup/status`);
        if (res.ok) {
          return;
        }
      } catch {
        // not up yet
      }
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
    throw new Error("claude-proxy did not become ready in time");
  }

  stop(): void {
    if (this.child) {
      this.child.kill();
      this.child = undefined;
    }
  }
}
