import * as vscode from "vscode";
import { ensureBinary } from "./binaryManager";
import { ProcessManager } from "./processManager";
import { ProxyStatusBar } from "./statusBar";

function currentPort(): number {
  return vscode.workspace.getConfiguration("claudeProxy").get<number>("port", 8080);
}

export function activate(context: vscode.ExtensionContext): void {
  const output = vscode.window.createOutputChannel("Claude Proxy");
  const statusBar = new ProxyStatusBar();
  const pm = new ProcessManager(output);
  pm.onExit = () => statusBar.setStopped();

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
      statusBar.setRunning(port);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      statusBar.setError(message);
      vscode.window.showErrorMessage(`Claude Proxy failed to start: ${message}`);
    }
  };

  const stopCmd = () => {
    pm.stop();
    statusBar.setStopped();
  };

  context.subscriptions.push(
    output,
    statusBar,
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
        await ensureBinary(context, true);
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
  // Cleanup is handled by the disposables pushed to context.subscriptions.
}
