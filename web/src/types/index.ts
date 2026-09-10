export interface User {
  id: string;
  username: string;
  email: string;
  role: string;
  status: number;
  tenant_id: string;
  created_at: string;
  updated_at: string;
}

export interface LoginResponse {
  token: string;
  expires_in: number;
  user: User;
}

export interface DataSource {
  id: string;
  name: string;
  type: string;
  host: string;
  port: number;
  database?: string;
  username: string;
  ssl_mode?: string;
  extra_config?: string;
  tags?: string;
  env?: string;
  is_system?: boolean;
  tenant_id: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface DataSourceForm {
  name: string;
  type: string;
  host: string;
  port: number;
  database?: string;
  username: string;
  password: string;
  ssl_mode?: string;
  tags?: string;
  env?: string;
}

export interface SyncTask {
  id: string;
  name: string;
  source_ds: string;
  target_ds: string;
  source_table: string;
  target_table: string;
  sync_mode: string;
  status: string;
  progress: number;
  last_sync_time?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
}

export interface QueryResult {
  columns: string[];
  rows: any[][];
  total_rows: number;
  duration: number;
  mode?: string;
  truncated?: boolean;
  affected_rows?: number;
}

export interface APIResponse<T = any> {
  code: number;
  message: string;
  data: T;
}

export interface ColumnInfo {
  name: string;
  type: string;
  nullable: boolean;
  key: string;
}

export interface ObjectInfo {
  name: string;
  columns?: ColumnInfo[];
}

export interface SchemaInfo {
  name: string;
  tables: ObjectInfo[];
  views: ObjectInfo[];
}

export interface CompareObject {
  name: string;
  type: string;
  status: string;
}

export interface TableDataResult {
  columns: string[];
  rows: any[][];
  total_rows: number;
  page: number;
  page_size: number;
}

export interface ColumnDetail {
  name: string;
  type: string;
  length: string;
  nullable: string;
  default: string;
  comment: string;
  key: string;
}

export interface TableStructureResult {
  columns: ColumnDetail[];
  ddl: string;
}

export interface SyncStructureResult {
  ddl: string;
  success: boolean;
  message: string;
}

export interface DataSyncResult {
  success: boolean;
  total_rows: number;
  synced_rows: number;
  skipped_rows: number;
  errors: string[];
}

export interface AISession {
  id: string;
  user_id: string;
  title: string;
  data_source_id: string;
  schema_name: string;
  mode: string;
  tenant_id: string;
  message_count?: number;
  created_at: string;
  updated_at: string;
}

export interface AIMessage {
  id: string;
  session_id: string;
  role: 'user' | 'assistant';
  content: string;
  sql_content?: string;
  query_result?: string;
  created_at: string;
}

export interface SchemaDetailItem {
  name: string;
  table_count: number;
  view_count: number;
  charset: string;
  collation: string;
}

export interface TableListItem {
  name: string;
  type: 'table' | 'view';
  engine: string | null;
  row_count: number | null;
  comment: string;
  create_time: string | null;
  update_time: string | null;
}

export interface Report {
  id: string;
  name: string;
  description: string;
  data_source_id: string;
  schema_name: string;
  sql_content: string;
  chart_type: 'table' | 'bar' | 'line' | 'pie' | 'area' | 'bar_grouped' | 'bar_stacked' | 'bar_percent' | 'area_percent' | 'area_basic' | 'line_series';
  chart_config: string;
  category_id: string | null;
  user_id: string;
  tenant_id: string;
  is_system?: boolean;
  created_at: string;
  updated_at: string;
}

export interface ReportCategory {
  id: string;
  name: string;
  parent_id: string | null;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export interface DashboardItem {
  id: string;
  name: string;
  description: string;
  layout: string;
  user_id: string;
  tenant_id: string;
  created_at: string;
  updated_at: string;
}

export interface DashboardLayoutItem {
  report_id: string;
  x: number;
  y: number;
  w: number;
  h: number;
  width?: number;
  chart_type?: string;
}

export interface ExportTask {
  id: string;
  name: string;
  task_type: 'export' | 'import';
  data_source_id: string;
  database_name?: string;
  schema_name?: string;
  // Export flat fields (expanded from config.export by backend)
  export_scope?: string;
  export_content?: string;
  export_tables?: string[];
  export_format?: string;
  export_batch_size?: number;
  storage_profile?: string;
  storage_path?: string;
  // Import flat fields (expanded from config.import by backend)
  import_source?: string;
  import_content?: string;
  import_strategy?: string;
  skip_safety_check?: boolean;
  import_file_name?: string;  // 仅展示用
  import_file_path?: string;  // 存储路径
  source_ds_id?: string;
  source_database?: string;
  source_schema?: string;
  source_tables?: string[];
  target_schema?: string;
  target_database?: string;
  target_table?: string;
  // Common
  status?: string;
  tenant_id: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ExportTaskExecution {
  id: string;
  task_id: string;
  run_number: number;
  status: string;
  progress: number;
  result_file_path?: string;
  result_file_name?: string;
  result_file_size?: number;
  log_file_path?: string;
  log_text?: string;
  error_msg?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
}

export interface FileInfo {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  mod_time: string;
  content_type: string;
  etag?: string;
}

export interface FileListResult {
  files: FileInfo[];
  total: number;
  total_mode: 'exact' | 'unknown';
  page: number;
  page_size: number;
  next_token?: string;
}

export interface FileTreeNode {
  name: string;
  path: string;
  is_dir: boolean;
  size?: number;
  children?: FileTreeNode[];
}

export interface StorageProfile {
  name: string;
  code: string;
  backend: string;
  enabled: boolean;
  is_default: boolean;
  summary?: Record<string, string>;
}

export interface StorageQuota {
  usage_bytes: number;
  quota_bytes: number;
  unlimited: boolean;
  usage_percent: number;
  max_single_file: number;
  file_count?: number;
}

export interface StorageBinding {
  id: string;
  module_code: string;
  module_name: string;
  profile_code: string;
  base_path: string;
  profile_available: boolean;
}

export interface TransferError {
  path: string;
  message: string;
}

export interface TransferResult {
  total_files: number;
  transferred: number;
  skipped: number;
  failed: number;
  total_bytes: number;
  transferred_bytes: number;
  total_errors: number;
  errors_truncated: boolean;
  errors?: TransferError[];
  duration: number;
}

export interface PreviewItem {
  path: string;
  size: number;
  conflict?: string;
}

export interface SyncPreviewSummary {
  new_files: number;
  updated_files: number;
  deleted_files: number;
  source_files_to_delete: number;
  total_bytes: number;
  is_move_mode: boolean;
}

export interface SyncPreview {
  dry_run: boolean;
  mode: string;
  to_create: PreviewItem[];
  to_update: PreviewItem[];
  to_delete: PreviewItem[];
  summary: SyncPreviewSummary;
}

// ─── Server Management Types ────────────────────────────────────────────────

export type DbType = 'mysql' | 'mariadb' | 'oceanbase' | 'postgres' | 'postgresql' | 'oracle' | 'sqlserver' | 'sqlite';

export interface CapabilitySet {
  db_type: string;
  version: string;
  major_version: number;
  has_roles: boolean;
  has_den: boolean;
  can_vacuum: boolean;
  can_manage_extensions: boolean;
}

export interface ConnectionMetrics {
  total: number;
  active: number;
  idle: number;
  waiting?: number;
  usage_percent?: number;
  max_connections?: number;
}

export interface ThroughputMetrics {
  qps?: number;
  commit_total?: number;
  rollback_total?: number;
  slow_queries?: number;
}

export interface CacheMetrics {
  hit_rate?: number;
  total_mb?: number;
  dirty_pages?: number;
}

export interface LockMetrics {
  deadlocks?: number;
  lock_waits?: number;
  blocked_sessions?: number;
  long_transactions?: number;
}

export interface ReplicationMetrics {
  lag_seconds?: number;
  replica_count?: number;
}

export interface DatabaseInfo {
  name: string;
  owner?: string;
  size_mb?: number;
  charset?: string;
  collation?: string;
  created_at?: string;
}

export interface ProcessInfo {
  pid: string;
  username?: string;
  state?: string;
  seconds?: number;
  database_name?: string;
  host?: string;
  program_name?: string;
  application_name?: string;
  query?: string;
}

export interface TablespaceInfo {
  name: string;
  size_mb?: number;
  [key: string]: any;
}

export interface ServerMetricsV2 {
  connections?: ConnectionMetrics;
  throughput?: ThroughputMetrics;
  buffer_cache?: CacheMetrics;
  locks?: LockMetrics;
  storage?: { tablespaces?: TablespaceInfo[] };
  replication?: ReplicationMetrics | null;
  database_specific?: Record<string, any>;
  warnings?: string[];
}
