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
  extra_config?: string;
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
