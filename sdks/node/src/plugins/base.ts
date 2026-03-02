export interface InstallConfig {
  readonly agentName: string;
  readonly orgId: string;
  readonly agentId: string;
  readonly privateKey: string;
  readonly kid: string;
  readonly token?: string;
  readonly baseUrl: string;
  readonly hookScriptPath: string;
  readonly guardScriptPath: string;
}

export interface PluginStatus {
  readonly installed: boolean;
  readonly agentName: string;
  readonly displayName: string;
  readonly hookConfigured: boolean;
  readonly hookScriptExists: boolean;
  readonly configPath: string;
}

export interface AgentPlugin {
  install(config: InstallConfig): Promise<void>;
  uninstall(agentId?: string): Promise<void>;
  status(): Promise<PluginStatus>;
}
