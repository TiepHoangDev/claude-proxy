import * as vscode from "vscode";

export interface DeepSeekBalance {
  isAvailable: boolean;
  currency: string;
  total: string;
  display: string;
}

export interface ClaudeUsage {
  fiveHourPercent: number;
  fiveHourReset: string;
  sevenDayPercent: number;
  sevenDayReset: string;
}

export interface HealthState {
  apiKeyError?: string;
  rateLimit: RateLimitInfo;
  claudeUsage?: ClaudeUsage;
  deepseek?: DeepSeekBalance;
  lastCheck: string;
}

export interface RateLimitInfo {
  requestsRemaining: number;
  requestsReset: string;
  tokensRemaining: number;
  tokensReset: string;
  fiveHourReset: number;
  sevenDayReset: number;
}

export type HealthChangeCallback = (state: HealthState | null) => void;

export class RateLimitMonitor {
  private interval?: NodeJS.Timeout;

  constructor(
    private readonly onHealthChange: HealthChangeCallback,
    private readonly port: () => number,
  ) {}

  start(): void {
    this.stop();
    void this.poll();
    this.interval = setInterval(() => void this.poll(), 10000);
  }

  stop(): void {
    if (this.interval) {
      clearInterval(this.interval);
      this.interval = undefined;
    }
  }

  dispose(): void {
    this.stop();
  }

  private async poll(): Promise<void> {
    try {
      const port = this.port();
      const res = await fetch(`http://localhost:${port}/_proxy/api/health`, {
        signal: AbortSignal.timeout(3000),
      });
      if (!res.ok) {
        this.onHealthChange(null);
        return;
      }
      const state = (await res.json()) as HealthState;
      this.onHealthChange(state);
    } catch {
      this.onHealthChange(null);
    }
  }

  formatRateLimit(info: RateLimitInfo): string {
    const parts: string[] = [];
    if (info.requestsRemaining > 0) {
      parts.push(`req:${info.requestsRemaining}`);
    }
    if (info.tokensRemaining > 0) {
      parts.push(`tok:${this.fmtNum(info.tokensRemaining)}`);
    }
    return parts.length > 0 ? parts.join(" ") : "";
  }

  formatResetStr(isoTimestamp: string): string {
    if (!isoTimestamp) return "";
    try {
      const d = new Date(isoTimestamp);
      const now = Date.now();
      const diff = d.getTime() - now;
      if (diff <= 0) return "now";
      const mins = Math.ceil(diff / 60000);
      if (mins < 60) return `${mins}m`;
      const hours = Math.floor(diff / 3600000);
      const remainMins = Math.floor((diff % 3600000) / 60);
      if (hours < 24) return remainMins > 0 ? `${hours}h${remainMins}m` : `${hours}h`;
      const days = Math.floor(hours / 24);
      const remainHours = hours % 24;
      return remainHours > 0 ? `${days}d${remainHours}h` : `${days}d`;
    } catch {
      return "";
    }
  }

  formatEpochReset(epoch: number): string {
    if (!epoch || epoch <= 0) return "";
    const now = Math.floor(Date.now() / 1000);
    const diff = epoch - now;
    if (diff <= 0) return "now";
    const mins = Math.ceil(diff / 60);
    if (mins < 60) return `${mins}m`;
    const hours = Math.floor(diff / 3600);
    const remainMins = Math.floor((diff % 3600) / 60);
    if (hours < 24) return remainMins > 0 ? `${hours}h${remainMins}m` : `${hours}h`;
    const days = Math.floor(hours / 24);
    const remainHours = hours % 24;
    return remainHours > 0 ? `${days}d${remainHours}h` : `${days}d`;
  }

  formatReset(resetTime: string): string {
    if (!resetTime) return "";
    try {
      const d = new Date(resetTime);
      const now = Date.now();
      const diff = d.getTime() - now;
      if (diff <= 0) return "resetting…";
      const mins = Math.ceil(diff / 60000);
      if (mins < 60) return `${mins}m`;
      const hours = Math.ceil(diff / 3600000);
      return `${hours}h`;
    } catch {
      return "";
    }
  }

  private fmtNum(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
    return String(n);
  }
}
