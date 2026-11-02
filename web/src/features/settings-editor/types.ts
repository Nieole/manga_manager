import type { ValidationResult, SystemCapabilitiesResponse, Publication, FieldEffect } from '../../api/generated';

export interface Config {
  server: { host: string; port: number; allowed_origins: string[] };
  database: { path: string };
  library: {
    paths: string[];
    storage_profile: string;
    io_policy: StorageIOPolicy;
    storage_policies: LibraryStoragePolicy[];
  };
  cache: { dir: string; page_disk_cache_enabled: boolean };
  logging: { level: string };
  scanner: {
    workers: number;
    scan_profile: string;
    thumbnail_format: string;
    waifu2x_path: string;
    realcugan_path: string;
    archive_pool_size: number;
    max_ai_concurrency: number;
  };
  tasks: {
    run_slots: number;
    retain_runs_per_task: number;
    retain_terminal_run_days: number;
    retain_sample_days: number;
    sample_interval_seconds: number;
    backoff_factor: number;
    backoff_max_hours: number;
    backoff_stop_after: number;
    resume_after_restart: boolean;
  };
  llm: {
    provider: string;
    api_mode: string;
    base_url: string;
    request_path: string;
    endpoint: string;
    model: string;
    api_key: string;
    timeout: number;
  };
  protocols: {
    opds: { enabled: boolean };
    mihon: { enabled: boolean };
  };
  koreader: {
    enabled: boolean;
    base_path: string;
    allow_registration: boolean;
    match_mode: string;
    path_ignore_extension: boolean;
  };
}

// ValidationResult / Capabilities 已改由 cmd/tsgen 从 Go 结构体生成（web/src/api/generated.ts），此处按原名
// 重导出以维持既有用法；Capabilities 是后端 SystemCapabilitiesResponse 的别名。CI 的 tsgen 漂移门禁保证不与后端漂移。
export type { ValidationResult };
export type Capabilities = SystemCapabilitiesResponse;

export interface StorageIOPolicy {
  scan_concurrency: number;
  archive_open_concurrency: number;
  cover_concurrency: number;
  hash_concurrency: number;
  pause_background_when_reading: boolean;
  idle_only_heavy_tasks: boolean;
  disable_same_disk_page_cache: boolean;
}

export interface LibraryStoragePolicy {
  path: string;
  storage_profile: string;
  io_policy: StorageIOPolicy;
}

export interface ConfigEnvelope {
  config: Config;
  validation: ValidationResult;
  capabilities: Capabilities;
  field_effects?: FieldEffect[];
  publication?: Publication;
}

export interface ConfigSaveResponse { message?: string; validation: ValidationResult; publication?: Publication; config?: Config }
