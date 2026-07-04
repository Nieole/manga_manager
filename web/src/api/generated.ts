/**
 * 业务说明：本文件由 cmd/tsgen 自动生成（M47），请勿手工编辑。
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

export interface TaskStatus {
  key: string;
  type: string;
  scope: string;
  scope_id?: number;
  scope_name?: string;
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
  phase?: string;
  current_item?: string;
  effective_limit?: TaskLimits;
  metrics?: Record<string, number>;
  labels?: Record<string, string>;
  params?: Record<string, string>;
  started_at: string;
  updated_at: string;
  finished_at?: string;
}

