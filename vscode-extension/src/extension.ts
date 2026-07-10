import * as vscode from "vscode";
import { ensureBinary } from "./binaryManager";
import { ProcessManager } from "./processManager";
import { ProxyStatusBar } from "./statusBar";
import { RateLimitMonitor, type HealthState } from "./rateLimitMonitor";

function currentPort(): number {
  return vscode.workspace.getConfiguration("claudeProxy").get<number>("port", 8080);
}

function fmtPrice(n: number | undefined): string {
  if (!n) return "0";
  return n >= 10 ? n.toFixed(1) : n.toFixed(2);
}

function terminalEnvConfigKey(): string {
  if (process.platform === "darwin") return "terminal.integrated.env.osx";
  if (process.platform === "win32") return "terminal.integrated.env.windows";
  return "terminal.integrated.env.linux";
}

let previousBaseUrl: string | undefined;
let activeProcessManager: ProcessManager | undefined;

function setAnthropicBaseUrl(port: number): void {
  const key = terminalEnvConfigKey();
  const config = vscode.workspace.getConfiguration(key);
  const url = `http://localhost:${port}`;
  previousBaseUrl = config.get<string>("ANTHROPIC_BASE_URL");
  void config.update("ANTHROPIC_BASE_URL", url, vscode.ConfigurationTarget.Global);
}

function unsetAnthropicBaseUrl(): void {
  const key = terminalEnvConfigKey();
  const config = vscode.workspace.getConfiguration(key);
  void config.update("ANTHROPIC_BASE_URL", previousBaseUrl ?? undefined, vscode.ConfigurationTarget.Global);
  previousBaseUrl = undefined;
}

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel("Claude Proxy");
  const statusBar = new ProxyStatusBar();
  const pm = new ProcessManager(output);
  activeProcessManager = pm;
  pm.onExit = () => {
    statusBar.setStopped();
    unsetAnthropicBaseUrl();
  };

  const monitor = new RateLimitMonitor(
    (state: HealthState | null) => {
      if (!state) {
        if (!pm.isRunning) {
          statusBar.clearHealth();
          statusBar.clearActiveRoute();
        }
        return;
      }

      const route = state.activeRoute;
      if (route) {
        const price =
          route.promptPricePerM || route.completionPricePerM
            ? ` $${fmtPrice(route.promptPricePerM)}/$${fmtPrice(route.completionPricePerM)} per 1M`
            : "";
        statusBar.setActiveRoute(
          `${route.model} (${route.provider})${price}`,
          `Routing to ${route.provider}: ${route.model}${price ? "\nPrompt/completion price:" + price : ""}\nClick to open setup`
        );
      } else {
        statusBar.clearActiveRoute();
      }

      if (state.apiKeyError) {
        statusBar.setHealthWarning(
          `Claude API: ${state.apiKeyError}`,
          `API key issue detected — check your API key on the setup page`
        );
        return;
      }

      const cu = state.claudeUsage;
      const rl = monitor.formatRateLimit(state.rateLimit);
      const perMinReset = monitor.formatReset(
        state.rateLimit.requestsReset || state.rateLimit.tokensReset
      );
      const ds = state.deepseek?.display || "";

      const tooltipParts: string[] = [];
      const textParts: string[] = [];

      // Claude subscription usage (primary for sub users).
      if (cu) {
        const fhReset = monitor.formatResetStr(cu.fiveHourReset);
        const sdReset = monitor.formatResetStr(cu.sevenDayReset);

        const fhPct = Math.round(cu.fiveHourPercent);
        const sdPct = Math.round(cu.sevenDayPercent);
        const fhLabel = fhPct >= 100 ? "LIMITED" : `${fhPct}%`;
        const fhDetail = fhPct >= 100 ? "rate limited" : `${fhPct}% used`;
        const sdLabel = sdPct >= 100 ? "LIMITED" : `${sdPct}%`;

        textParts.push(`5h:${fhLabel}${fhReset ? " (" + fhReset + ")" : ""}`);
        textParts.push(`7d:${sdLabel}${sdReset ? " (" + sdReset + ")" : ""}`);

        tooltipParts.push(
          `5-hour window: ${fhDetail}, resets ${cu.fiveHourReset || "—"}`,
          `7-day window: ${sdPct}% used, resets ${cu.sevenDayReset || "—"}`
        );
      }

      // Per-minute rate limit headers (secondary).
      if (rl) {
        if (textParts.length === 0) {
          textParts.push(perMinReset ? `Rate: ${rl} (${perMinReset})` : `Rate: ${rl}`);
        }
        tooltipParts.push(
          `Requests remaining: ${state.rateLimit.requestsRemaining || "—"}`,
          `Tokens remaining: ${state.rateLimit.tokensRemaining || "—"}`,
          `Reset: ${state.rateLimit.requestsReset || state.rateLimit.tokensReset || "—"}`
        );
      }

      if (ds) {
        textParts.push(`DeepSeek: ${ds}`);
        tooltipParts.push(`DeepSeek balance: ${ds}`);
      }

      if (tooltipParts.length === 0) {
        tooltipParts.push(`Last check: ${state.lastCheck || "—"}`);
      }

      if (textParts.length > 0) {
        const hasWarning = cu && cu.fiveHourPercent >= 100;
        if (hasWarning && !state.apiKeyError) {
          statusBar.setHealthWarning(textParts.join("  "), tooltipParts.join("\n"));
        } else {
          statusBar.setHealthRateLimit(textParts.join("  "), tooltipParts.join("\n"));
        }
      } else {
        statusBar.clearHealth();
      }
    },
    currentPort,
  );
  monitor.start();

  const startCmd = async () => {
    if (pm.isRunning) {
      return;
    }
    try {
      statusBar.setDownloading();
      const bin = await ensureBinary(context);
      const port = currentPort();
      statusBar.setStarting();
      await pm.start(bin, context.globalStorageUri.fsPath, port);
      setAnthropicBaseUrl(port);
      statusBar.setRunning(port);
    } catch (err) {
      unsetAnthropicBaseUrl();
      const message = err instanceof Error ? err.message : String(err);
      statusBar.setError(message);
      vscode.window.showErrorMessage(`Claude Proxy failed to start: ${message}`);
    }
  };

  const stopCmd = () => {
    pm.stop();
    statusBar.setStopped();
    unsetAnthropicBaseUrl();
  };

  context.subscriptions.push(
    output,
    statusBar,
    monitor,
    { dispose: () => pm.stop() },
    vscode.commands.registerCommand("claudeProxy.start", startCmd),
    vscode.commands.registerCommand("claudeProxy.stop", stopCmd),
    vscode.commands.registerCommand("claudeProxy.restart", async () => {
      stopCmd();
      await startCmd();
    }),
    vscode.commands.registerCommand("claudeProxy.openDashboard", () =>
      vscode.env.openExternal(vscode.Uri.parse(`http://localhost:${currentPort()}/_proxy/dashboard`))
    ),
    vscode.commands.registerCommand("claudeProxy.openSetup", () =>
      vscode.env.openExternal(vscode.Uri.parse(`http://localhost:${currentPort()}/_proxy/setup`))
    ),
    vscode.commands.registerCommand("claudeProxy.showLogs", () => output.show()),
    vscode.commands.registerCommand("claudeProxy.checkForUpdate", async () => {
      try {
        const wasRunning = pm.isRunning;
        if (wasRunning) {
          stopCmd();
          await new Promise((r) => setTimeout(r, 500));
        }
        await ensureBinary(context, true);
        if (wasRunning) {
          await startCmd();
        }
        vscode.window.showInformationMessage("Claude Proxy: binary is up to date.");
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(`Claude Proxy: update check failed: ${message}`);
      }
    })
  );

  if (vscode.workspace.getConfiguration("claudeProxy").get<boolean>("autoStart")) {
    void startCmd();
  }
}

export function deactivate(): void {
  activeProcessManager?.stop();
  activeProcessManager = undefined;
  unsetAnthropicBaseUrl();
}
