import * as vscode from "vscode";

export type ProxyState = "downloading" | "starting" | "running" | "stopped" | "error";

export class ProxyStatusBar {
  private readonly proxyItem: vscode.StatusBarItem;
  private readonly routeItem: vscode.StatusBarItem;
  private readonly healthItem: vscode.StatusBarItem;
  private proxyState: ProxyState = "stopped";
  private proxyPort = 0;

  constructor() {
    this.proxyItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    this.routeItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 95);
    this.healthItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 90);
    this.proxyItem.show();
    this.healthItem.show();
    this.setStopped();
  }

  setDownloading(): void {
    this.proxyState = "downloading";
    this.setProxy("$(cloud-download) Claude Proxy: Downloading…", undefined, "claudeProxy.showLogs");
  }

  setStarting(): void {
    this.proxyState = "starting";
    this.setProxy("$(sync~spin) Claude Proxy: Starting…", undefined, "claudeProxy.showLogs");
  }

  setRunning(port: number): void {
    this.proxyState = "running";
    this.proxyPort = port;
    this.setProxy(`$(check) Claude Proxy :${port}`, `Running on port ${port} — click to open dashboard`, "claudeProxy.openDashboard");
  }

  setStopped(): void {
    this.proxyState = "stopped";
    this.proxyPort = 0;
    this.setProxy("$(circle-slash) Claude Proxy: Stopped", "Click to start", "claudeProxy.start");
  }

  setError(message: string): void {
    this.proxyState = "error";
    this.setProxy("$(warning) Claude Proxy: Error", message, "claudeProxy.showLogs");
  }

  setActiveRoute(text: string, tooltip: string): void {
    this.routeItem.text = `$(circuit-board) ${text}`;
    this.routeItem.tooltip = tooltip;
    this.routeItem.command = "claudeProxy.openSetup";
    this.routeItem.show();
  }

  clearActiveRoute(): void {
    this.routeItem.hide();
  }

  setHealthWarning(text: string, tooltip: string): void {
    this.healthItem.text = `$(warning) ${text}`;
    this.healthItem.tooltip = tooltip;
    this.healthItem.command = "claudeProxy.openDashboard";
  }

  setHealthRateLimit(text: string, tooltip: string): void {
    this.healthItem.text = `$(dashboard) ${text}`;
    this.healthItem.tooltip = tooltip;
    this.healthItem.command = "claudeProxy.openDashboard";
  }

  clearHealth(): void {
    this.healthItem.text = "$(dash) Usage: —";
    this.healthItem.tooltip = "Usage data loads when the Claude usage API becomes available";
    this.healthItem.command = "claudeProxy.openDashboard";
  }

  dispose(): void {
    this.proxyItem.dispose();
    this.routeItem.dispose();
    this.healthItem.dispose();
  }

  private setProxy(text: string, tooltip: string | undefined, command: string): void {
    this.proxyItem.text = text;
    this.proxyItem.tooltip = tooltip;
    this.proxyItem.command = command;
  }
}
