/**
 * 本文件由 cmd/tsgen 自动生成，请勿手工编辑。
 * 它以 Go 后端的响应结构体为单一事实源生成前端契约类型，防止手写类型与后端漂移。
 * 重新生成：`go run ./cmd/tsgen`；CI 会校验其与源一致。
 */

export interface TaskLimits {
  scan_profile?: string;
  scanner_workers_configured?: number;
  scanner_workers_effective?: number;
  storage_profile?: string;
  volume_key?: string;
  scan_concurrency?: number;
  archive_open_concurrency?: number;
  cover_concurrency?: number;
  hash_concurrency?: number;
  pause_background_when_reading: boolean;
  idle_only_heavy_tasks: boolean;
  disable_same_disk_page_cache: boolean;
}

export interface RunSnapshot {
  run_id: number;
  task_id: number;
  type: string;
  scope: string;
  scope_id?: number;
  variant?: string;
  scope_name?: string;
  trigger?: string;
  status: string;
  message: string;
  message_code?: string;
  message_params?: Record<string, string>;
  error?: string;
  current: number;
  total: number;
  percent?: number;
  rate_per_minute?: number;
  eta_seconds?: number;
  can_cancel: boolean;
  can_pause: boolean;
  can_resume: boolean;
  retryable: boolean;
  paused_at?: string;
  pause_reason?: string;
  coalesced_count?: number;
  phase?: string;
  current_item?: string;
  effective_limit?: TaskLimits;
  metrics?: Record<string, number>;
  labels?: Record<string, string>;
  params?: Record<string, string>;
  started_at?: string;
  updated_at: string;
  finished_at?: string;
}

export interface RunLiveSummary {
  active: number;
  queued: number;
  slots: number;
  paused: boolean;
  paused_all: boolean;
}

export interface RunLive {
  active: number;
  queued: number;
  slots: number;
  paused: boolean;
  paused_all: boolean;
  runs: RunSnapshot[];
}

export interface RunPush {
  sequence: number;
  prev: number;
  run?: RunSnapshot;
  live?: RunLiveSummary;
}

export interface TaskSummary {
  task_id: number;
  type: string;
  scope: string;
  scope_id?: number;
  variant?: string;
  scope_name?: string;
  disabled: boolean;
  fail_streak: number;
  last_success_at?: string;
  backoff_until?: string;
  stall_reason?: string;
  last_run?: RunSnapshot;
}

export interface RunEvent {
  at: string;
  kind: string;
  phase?: string;
  item?: string;
  reason?: string;
  action?: string;
  code?: string;
  detail?: string;
  count?: number;
}

export interface RunPhaseSpan {
  phase: string;
  started_at: string;
  duration_ms: number;
  current?: boolean;
}

export interface RunEventsResponse {
  run_id: number;
  events: RunEvent[];
  phases: RunPhaseSpan[];
  omitted_failures?: number;
  truncated?: boolean;
}

export interface RunSample {
  at: string;
  current: number;
  throughput_per_minute: number;
}

export interface RunSamplesResponse {
  run_id: number;
  samples: RunSample[];
  retention_days: number;
  expired?: boolean;
  truncated?: boolean;
}

export interface ValidationIssue {
  field: string;
  message: string;
  severity: string;
}

export interface ValidationResult {
  valid: boolean;
  issues: ValidationIssue[];
}

export interface SystemCapabilitiesResponse {
  supported_scan_formats: string[];
  supported_scan_profiles: string[];
  supported_log_levels: string[];
  supported_storage_profiles: string[];
  default_scan_formats: string;
  default_scan_interval: number;
  supported_llm_providers: string[];
  supported_llm_api_modes: string[];
}

export interface StorageFailureResponse {
  error: string;
  reason: string;
  library_id: number;
  library_name: string;
  library_path: string;
  path: string;
}

