// Bindings for DesktopApp

export interface Repository {
  id: string;
  path: string;
  display_name: string;
  git_branch?: string;
  git_remote?: string;
  is_active: boolean;
  last_active?: string;
}

export interface ConnectionStatus {
  server_running: boolean;
  server_port: number;
  server_address: string;
  connected_clients: number;
  claude_state: string;
  active_repo: string;
  session_id?: string;
}

export function GetRepositories(): Promise<Repository[]>;
export function AddRepository(path: string, displayName: string): Promise<Repository>;
export function RemoveRepository(id: string): Promise<void>;
export function SwitchRepository(id: string): Promise<void>;
export function GetConnectionStatus(): Promise<ConnectionStatus>;
export function GetQRCodeData(): Promise<string>;
export function GetConnectionURLs(): Promise<{ [key: string]: string }>;
export function GetConfig(): Promise<{ [key: string]: any }>;
export function UpdateConfig(key: string, value: any): Promise<void>;
export function OpenDirectoryDialog(): Promise<string>;
