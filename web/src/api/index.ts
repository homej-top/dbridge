import React from 'react';
import axios from 'axios';
import { message } from 'antd';
import type { APIResponse } from '../types';

type EventHandler = (data: any) => void;

class EventBus {
  private listeners: Record<string, EventHandler[]> = {};

  on(event: string, handler: EventHandler) {
    if (!this.listeners[event]) this.listeners[event] = [];
    this.listeners[event].push(handler);
    return () => this.off(event, handler);
  }

  off(event: string, handler: EventHandler) {
    if (!this.listeners[event]) return;
    this.listeners[event] = this.listeners[event].filter((h) => h !== handler);
  }

  emit(event: string, data?: any) {
    if (!this.listeners[event]) return;
    this.listeners[event].forEach((h) => h(data));
  }
}

export const subscriptionEventBus = new EventBus();

const request = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
});

request.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token');
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => Promise.reject(error)
);

request.interceptors.response.use(
  (response) => {
    if (response.config.responseType === 'blob') {
      return response;
    }
    const data: APIResponse = response.data;
    if (data.code !== 0) {
      message.error(data.message || '请求失败');
      return Promise.reject(new Error(data.message));
    }
    return response;
  },
  (error) => {
    // Extract error message from response data if available
    const errorMessage = error.response?.data?.message || error.message || '网络错误';
    
    if (error.response?.status === 401) {
      localStorage.removeItem('token');
      window.location.href = '/login';
      return Promise.reject(error);
    }
    if (error.response?.status === 403) {
      const data = error.response.data;
      if (data?.error === 'subscription_inactive') {
        subscriptionEventBus.emit('subscription-error', data);
        message.error(data?.message || '订阅已过期，请续费以继续使用');
      } else if (data?.error === 'subscription_readonly') {
        subscriptionEventBus.emit('subscription-readonly', data);
        message.warning(data?.message || '当前为只读模式，请续费后使用完整功能');
      } else if (data?.error === 'subscription_check_failed') {
        subscriptionEventBus.emit('subscription-error', data);
        const renewUrl = data?.renew_url;
        const errMsg = data?.message || '订阅校验失败，请重新登录或续费';
        if (renewUrl) {
          message.error(
            React.createElement('span', null,
              errMsg, ' ',
              React.createElement('a', { href: renewUrl, target: '_blank', rel: 'noopener noreferrer', style: { color: '#1677ff' } }, '前往续费'),
            ),
            5,
          );
        } else {
          message.error(errMsg);
        }
      } else if (data?.code === 1006) {
        // device_limit_reached — handled by AuthCallback, no toast here
      } else {
        message.error(errorMessage);
      }
      return Promise.reject(error);
    }
    if (error.response?.status === 503) {
      message.error(errorMessage);
      return Promise.reject(error);
    }
    // For all other HTTP errors (400, 500, etc.), show the backend message
    message.error(errorMessage);
    return Promise.reject(error);
  }
);

export default request;

export const authAPI = {
  login: (data: { username: string; password: string }) =>
    request.post('/auth/login', data),
  changePassword: (data: { old_password: string; new_password: string }) =>
    request.post('/auth/change-password', data),
  getLoginUrl: () => request.get('/auth/login-url'),
  loginWithCode: (code: string) => request.post('/auth/login-with-code', { code }),
  logout: () => request.post('/auth/logout'),
  getSession: () => request.get('/auth/session'),
  refresh: () => request.post('/auth/refresh'),
};

export const connectivityAPI = {
  check: () => request.get('/connectivity'),
};

export const agreementAPI = {
  getStatus: () => request.get('/agreements/status'),
  sign: (type: string) => request.post('/agreements/sign', { type }),
};

export const featureFlagAPI = {
  check: (name: string) => request.get(`/feature-flags/${name}`),
};

export const dsAPI = {
  list: (tag?: string) => request.get('/data-sources', { params: tag ? { tag } : {} }),
  get: (id: string) => request.get(`/data-sources/${id}`),
  create: (data: any) => request.post('/data-sources', data),
  update: (id: string, data: any) => request.put(`/data-sources/${id}`, data),
  delete: (id: string) => request.delete(`/data-sources/${id}`),
  test: (data: any) => request.post('/data-sources/test', data),
  schema: (id: string) => request.get(`/data-sources/${id}/schema`),
  schemaNames: (id: string) => request.get(`/data-sources/${id}/schemas`),
  schemaObjects: (id: string, schema: string) => request.get(`/data-sources/${id}/schemas/${encodeURIComponent(schema)}/objects`),
  schemaDetailList: (id: string) => request.get(`/data-sources/${id}/schema-detail-list`),
  tableList: (id: string, schema: string, database?: string) =>
    request.get(`/data-sources/${id}/schemas/${encodeURIComponent(schema)}/table-list`, { params: database ? { database } : {} }),
  treeMetadata: (id: string) => request.get(`/data-sources/${id}/tree-metadata`),
  databases: (id: string) => request.get(`/data-sources/${id}/databases`),
  databaseSchemas: (id: string, db: string) => request.get(`/data-sources/${id}/databases/${encodeURIComponent(db)}/schemas`),
  columnTypes: (id: string) => request.get(`/data-sources/${id}/column-types`),
  indexTypes: (id: string) => request.get(`/data-sources/${id}/index-types`),
  getDdl: (dataSourceId: string, schema: string, table: string) =>
    request.get('/query/ddl', { params: { data_source_id: dataSourceId, schema, table } }),
  export: (password: string) => request.post('/data-sources/export', { password }),
  import: (items: any[], password: string) => request.post('/data-sources/import', { items, password }),
  uploadSqlite: (name: string, file: File) => {
    const form = new FormData();
    form.append('name', name);
    form.append('file', file);
    return request.post('/data-sources/upload-sqlite', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
  updateSqlite: (id: string, name: string, file: File) => {
    const form = new FormData();
    form.append('name', name);
    form.append('file', file);
    return request.put(`/data-sources/${id}/upload-sqlite`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
  listObjectsByType: (id: string, schema: string, type: string, params?: { page?: number; page_size?: number; keyword?: string; database?: string }) =>
    request.get(`/data-sources/${id}/objects/${encodeURIComponent(schema)}/${type}`, { params }),
  getObjectDetail: (id: string, schema: string, type: string, name: string, database?: string) =>
    request.get(`/data-sources/${id}/objects/${encodeURIComponent(schema)}/${type}/${encodeURIComponent(name)}`, { params: database ? { database } : {} }),
  createObject: (id: string, schema: string, type: string, data: { ddl: string; database?: string }) =>
    request.post(`/data-sources/${id}/objects/${encodeURIComponent(schema)}/${type}`, data),
  alterObject: (id: string, schema: string, type: string, name: string, data: { ddl: string; force?: boolean; database?: string }) =>
    request.put(`/data-sources/${id}/objects/${encodeURIComponent(schema)}/${type}/${encodeURIComponent(name)}`, data),
  dropObject: (id: string, schema: string, type: string, name: string, data?: { force?: boolean; database?: string }) =>
    request.delete(`/data-sources/${id}/objects/${encodeURIComponent(schema)}/${type}/${encodeURIComponent(name)}`, { data }),
  getCreateTemplate: (id: string, schema: string, type: string, params?: { object_name?: string; database?: string }) =>
    request.get(`/data-sources/${id}/objects/${encodeURIComponent(schema)}/${type}/template`, { params }),
  refreshMatView: (id: string, schema: string, name: string, data: { mode: 'normal' | 'concurrently' | 'with-no-data'; database?: string }) =>
    request.post(`/data-sources/${id}/objects/${encodeURIComponent(schema)}/matview/${encodeURIComponent(name)}/refresh`, data),
};

export const queryAPI = {
  // Generic execute (auto-detect SQL type)
  execute: (data: { data_source_id: string; sql: string; schema?: string; database?: string; page?: number; page_size?: number; category?: string }) =>
    request.post('/query', data),
  // Typed endpoints
  executeDQL: (data: { data_source_id: string; sql: string; schema?: string; database?: string; page?: number; page_size?: number }) =>
    request.post('/query/dql', data),
  executeDML: (data: { data_source_id: string; sql: string; schema?: string; database?: string }) =>
    request.post('/query/dml', data),
  executeDDL: (data: { data_source_id: string; sql: string; schema?: string; database?: string }) =>
    request.post('/query/ddl-exec', data),
  executeDCL: (data: { data_source_id: string; sql: string; schema?: string; database?: string }) =>
    request.post('/query/dcl', data),
  executeTCL: (data: { data_source_id: string; sql: string; schema?: string; database?: string }) =>
    request.post('/query/tcl', data),
};

export const syncAPI = {
  list: () => request.get('/sync-tasks'),
  get: (id: string) => request.get(`/sync-tasks/${id}`),
  create: (data: any) => request.post('/sync-tasks', data),
  start: (id: string) => request.post(`/sync-tasks/${id}/start`),
  stop: (id: string) => request.post(`/sync-tasks/${id}/stop`),
};

export const compareAPI = {
  structure: (data: {
    source_ds: string;
    source_schema?: string;
    target_ds: string;
    target_schema?: string;
    source_database?: string;
    target_database?: string;
  }) => request.post('/compare/structure', data),
  tableData: (data: {
    data_source_id: string;
    schema?: string;
    table: string;
    page?: number;
    page_size?: number;
  }) => request.post('/compare/table-data', data),
  tableStructure: (data: {
    data_source_id: string;
    schema?: string;
    table: string;
  }) => request.post('/compare/table-structure', data),
  syncStructure: (data: {
    source_ds: string;
    source_schema?: string;
    target_ds: string;
    target_schema?: string;
    table: string;
    action: 'create' | 'alter';
    dry_run?: boolean;
    override_ddl?: string;
    source_database?: string;
    target_database?: string;
  }) => request.post('/compare/sync-structure', data, { timeout: 300000 }),
  syncData: (data: {
    source_ds: string;
    source_schema?: string;
    target_ds: string;
    target_schema?: string;
    table: string;
    source_database?: string;
    target_database?: string;
    options: {
      truncate_target?: boolean;
      sync_id?: boolean;
      mode: 'full' | 'selected' | 'diff';
      check_fields?: string[];
      sync_columns?: string[];
      selected_rows?: Record<string, any>[];
      transactional?: boolean;
    };
  }) => request.post('/compare/sync-data', data, { timeout: 600000 }),
};

export const aiAPI = {
  chat: (data: { data_source_id?: string; schema?: string; messages: { role: string; content: string }[] }) =>
    request.post('/ai/chat', data, { timeout: 60000 }),
  explain: (data: { data_source_id?: string; schema?: string; sql: string }) =>
    request.post('/ai/explain', data, { timeout: 60000 }),
  optimize: (data: { data_source_id?: string; schema?: string; sql: string }) =>
    request.post('/ai/optimize', data, { timeout: 60000 }),
  fix: (data: { data_source_id?: string; schema?: string; sql: string; error: string }) =>
    request.post('/ai/fix', data, { timeout: 60000 }),
};

export const settingsAPI = {
  get: () => request.get('/settings'),
  update: (data: any) => request.put('/settings', data),
  addAiModel: (data: any) => request.post('/settings/ai-models', data),
  updateAiModel: (id: string, data: any) => request.put(`/settings/ai-models/${id}`, data),
  deleteAiModel: (id: string) => request.delete(`/settings/ai-models/${id}`),
  activateAiModel: (id: string) => request.put(`/settings/ai-models/${id}/activate`),
};

export const dashboardAPI = {
  stats: () => request.get('/dashboard/stats'),
  getTabs: () => request.get('/dashboard/tabs'),
  saveTabs: (tabs: any[]) => request.put('/dashboard/tabs', tabs),
};

export const serverAPI = {
  // MSSQL Login Management
  listLogins: (dsId: string) => request.get(`/server/${dsId}/logins`),
  createLogin: (dsId: string, data: any) => request.post(`/server/${dsId}/logins`, data),
  getLoginDetail: (dsId: string, name: string) => request.get(`/server/${dsId}/logins/${encodeURIComponent(name)}`),
  alterLogin: (dsId: string, name: string, data: any) => request.put(`/server/${dsId}/logins/${encodeURIComponent(name)}`, data),
  dropLogin: (dsId: string, name: string, cascade?: boolean) => request.delete(`/server/${dsId}/logins/${encodeURIComponent(name)}`, { params: cascade ? { cascade: 'true' } : {} }),

  // MSSQL Database User Management
  listDatabaseUsers: (dsId: string, db: string) => request.get(`/server/${dsId}/database/${encodeURIComponent(db)}/users`),
  createDatabaseUser: (dsId: string, db: string, data: any) => request.post(`/server/${dsId}/database/${encodeURIComponent(db)}/users`, data),
  dropDatabaseUser: (dsId: string, db: string, name: string) => request.delete(`/server/${dsId}/database/${encodeURIComponent(db)}/users/${encodeURIComponent(name)}`),
  batchCreateDatabaseUsers: (dsId: string, loginName: string, mappings: any[]) => request.post(`/server/${dsId}/logins/${encodeURIComponent(loginName)}/batch-map-users`, { mappings }),

  // MSSQL Orphaned Users
  detectOrphanedUsers: (dsId: string, db: string) => request.get(`/server/${dsId}/database/${encodeURIComponent(db)}/orphaned-users`),
  fixOrphanedUser: (dsId: string, db: string, name: string, loginName: string) => request.post(`/server/${dsId}/database/${encodeURIComponent(db)}/orphaned-users/${encodeURIComponent(name)}/fix`, { login_name: loginName }),

  // MSSQL Effective Permissions
  getEffectivePermissions: (dsId: string, db: string, data: { principal_name: string; object_type?: string; object_name?: string }) => request.post(`/server/${dsId}/database/${encodeURIComponent(db)}/effective-permissions`, data),

  // MSSQL Guest Compliance
  checkGuestStatus: (dsId: string, db: string) => request.get(`/server/${dsId}/database/${encodeURIComponent(db)}/guest-status`),
  disableGuest: (dsId: string, db: string) => request.post(`/server/${dsId}/database/${encodeURIComponent(db)}/disable-guest`),

  // SQLite Management
  getSQLitePragma: (dsId: string) => request.get(`/server/${dsId}/sqlite/pragma`),
  setSQLitePragma: (dsId: string, data: { name: string; value: string }) => request.put(`/server/${dsId}/sqlite/pragma`, data),
  getSQLiteStorage: (dsId: string) => request.get(`/server/${dsId}/sqlite/storage`),
  vacuumSQLite: (dsId: string) => request.post(`/server/${dsId}/sqlite/vacuum`),

  // PG Extension Management
  listPgExtensions: (dsId: string) => request.get(`/server/${dsId}/pg/extensions`),
  installPgExtension: (dsId: string, name: string) => request.post(`/server/${dsId}/pg/extensions`, { name }),
  dropPgExtension: (dsId: string, name: string) => request.delete(`/server/${dsId}/pg/extensions/${encodeURIComponent(name)}`),

  // Existing server management APIs
  getServerInfo: (dsId: string) => request.get(`/server/${dsId}/info`),
  getMetrics: (dsId: string) => request.get(`/server/${dsId}/metrics`),
  getMetricsV2: (dsId: string) => request.get(`/server/${dsId}/metrics/v2`),
  listDatabases: (dsId: string) => request.get(`/server/${dsId}/databases`),
  listProcesses: (dsId: string) => request.get(`/server/${dsId}/processes`),
  listUsers: (dsId: string) => request.get(`/server/${dsId}/users`),
  listTablespaces: (dsId: string) => request.get(`/server/${dsId}/tablespaces`),
  createDatabase: (dsId: string, name: string) => request.post(`/server/${dsId}/databases`, { name }),
  dropDatabase: (dsId: string, name: string) => request.delete(`/server/${dsId}/databases/${encodeURIComponent(name)}`),
  createUser: (dsId: string, data: any) => request.post(`/server/${dsId}/users`, data),
  dropUser: (dsId: string, name: string) => request.delete(`/server/${dsId}/users/${encodeURIComponent(name)}`),
  alterUserPassword: (dsId: string, name: string, password: string) => request.put(`/server/${dsId}/users/${encodeURIComponent(name)}/password`, { password }),
  alterUserLock: (dsId: string, name: string, lock: boolean) => request.put(`/server/${dsId}/users/${encodeURIComponent(name)}/lock`, { lock }),
  alterUserRename: (dsId: string, name: string, newName: string) => request.put(`/server/${dsId}/users/${encodeURIComponent(name)}/rename`, { new_name: newName }),
  alterUserDefaultSchema: (dsId: string, name: string, schema: string) => request.put(`/server/${dsId}/users/${encodeURIComponent(name)}/schema`, { schema }),
  getUserPrivileges: (dsId: string, name: string) => request.get(`/server/${dsId}/users/${encodeURIComponent(name)}/privileges`),
  applyUserPrivileges: (dsId: string, name: string, data: any) => request.post(`/server/${dsId}/users/${encodeURIComponent(name)}/privileges`, data),
  getCapability: (dsId: string) => request.get(`/server/${dsId}/capability`),
  listRoles: (dsId: string, database?: string) => request.get(`/server/${dsId}/roles`, { params: database ? { database } : {} }),
  createRole: (dsId: string, name: string, database?: string) => request.post(`/server/${dsId}/roles`, { name, database }),
  getRoleDetails: (dsId: string, name: string, database?: string) => request.get(`/server/${dsId}/roles/${encodeURIComponent(name)}/details`, { params: database ? { database } : {} }),
  alterRoleAttribute: (dsId: string, name: string, attribute: string, value: string) => request.put(`/server/${dsId}/roles/${encodeURIComponent(name)}/attribute`, { attribute, value }),
  dropRole: (dsId: string, name: string, database?: string) => request.delete(`/server/${dsId}/roles/${encodeURIComponent(name)}`, { params: database ? { database } : {} }),
  addRoleMember: (dsId: string, roleName: string, member: string, database?: string) => request.post(`/server/${dsId}/roles/${encodeURIComponent(roleName)}/members`, { member, database }),
  removeRoleMember: (dsId: string, roleName: string, member: string, database?: string) => request.delete(`/server/${dsId}/roles/${encodeURIComponent(roleName)}/members/${encodeURIComponent(member)}`, { params: database ? { database } : {} }),
};

export const slowSQLAPI = {
  getRealTime: (dsId: string, threshold?: number) => request.get(`/server/${dsId}/slow-sql/real-time`, { params: { threshold } }),
  getHistory: (dsId: string, limit?: number) => request.get(`/server/${dsId}/slow-sql/history`, { params: { limit } }),
  getBlockingChain: (dsId: string) => request.get(`/server/${dsId}/slow-sql/blocking-chain`),
  getPreCheck: (dsId: string) => request.get(`/server/${dsId}/slow-sql/pre-check`),
  getExplainPlan: (dsId: string, sql: string) => request.post(`/server/${dsId}/slow-sql/explain`, { sql }),
  killSession: (dsId: string, sessionId: string) => request.post(`/server/${dsId}/slow-sql/kill/${sessionId}`),
};

export const inspectionAPI = {
  listTasks: () => request.get('/inspection/tasks'),
  createTask: (data: any) => request.post('/inspection/tasks', data),
  deleteTask: (id: string) => request.delete(`/inspection/tasks/${id}`),
  triggerTask: (id: string) => request.post(`/inspection/tasks/${id}/trigger`),
  listReports: (dsId: string) => request.get(`/inspection/reports?ds_id=${dsId}`),
  deleteReport: (id: string) => request.delete(`/inspection/reports/${id}`),
};

export const auditAPI = {
  list: (params?: { page?: number; page_size?: number; operation?: string }) =>
    request.get('/audit-logs', { params }),
};

export type ColumnChange = {
  name: string;
  type?: string;
  length?: string;
  nullable?: boolean;
  default?: string;
  has_default?: boolean;
  comment?: string;
  after?: string;
  new_name?: string;
};

export type IndexChange = {
  name: string;
  type?: string;
  columns?: string[];
  comment?: string;
};

export type AlterChange = {
  action:
    | 'ADD_COLUMN'
    | 'MODIFY_COLUMN'
    | 'DROP_COLUMN'
    | 'RENAME_COLUMN'
    | 'ADD_INDEX'
    | 'DROP_INDEX'
    | 'ADD_CONSTRAINT'
    | 'DROP_CONSTRAINT'
    | 'TABLE_COMMENT'
    | 'INDEX_COMMENT';
  column?: ColumnChange;
  index?: IndexChange;
  comment?: string;
};

export const tableAPI = {
  structure: (data: { data_source_id: string; schema?: string; table: string; database?: string }) =>
    request.post('/table/structure', data, { timeout: 30000 }),
  previewAlter: (data: {
    data_source_id: string;
    schema?: string;
    database?: string;
    table: string;
    changes: AlterChange[];
    override_ddl?: string;
  }) => request.post('/table/preview-alter', data, { timeout: 30000 }),
  alter: (data: {
    data_source_id: string;
    schema?: string;
    database?: string;
    table: string;
    changes: AlterChange[];
    override_ddl?: string;
  }) => request.post('/table/alter', data, { timeout: 300000 }),
};

export const viewAPI = {
  structure: (data: { data_source_id: string; schema?: string; view: string; database?: string }) =>
    request.post('/view/structure', data, { timeout: 30000 }),
  definition: (data: { data_source_id: string; schema?: string; view: string; database?: string }) =>
    request.post('/view/definition', data, { timeout: 30000 }),
  update: (data: { data_source_id: string; schema?: string; view: string; definition: string; database?: string }) =>
    request.post('/view/update', data, { timeout: 60000 }),
  executeDDL: (data: { data_source_id: string; schema?: string; view?: string; sql: string; database?: string }) =>
    request.post('/view/ddl-exec', data, { timeout: 30000 }),
};

export const mcpAPI = {
  /** List all MCP API tokens (without secret) */
  listTokens: () => request.get('/mcp/tokens'),
  /** Create a new MCP API token (returns plaintext ONLY once) */
  createToken: (data: { name: string; permissions: string[]; expires_in_days?: number }) =>
    request.post('/mcp/tokens', data),
  /** Revoke an MCP API token */
  revokeToken: (id: string) => request.delete(`/mcp/tokens/${id}`),
  toggleMCP: (enabled: boolean) => request.put("/mcp/toggle", { enabled }),
  /** Generate MCP server config JSON for third-party agents */
  generateConfig: (data: { mode: string; api_url: string; api_token: string }) =>
    request.post('/mcp/generate-config', data),
};

export const sessionAPI = {
  list: () => request.get('/ai-sessions'),
  create: (data: { title?: string; agent_id?: string; data_source_id?: string; schema?: string; mode?: string }) =>
    request.post('/ai-sessions', data),
  get: (id: string) => request.get(`/ai-sessions/${id}`),
  update: (id: string, data: { title?: string; agent_id?: string }) => request.put(`/ai-sessions/${id}`, data),
  delete: (id: string) => request.delete(`/ai-sessions/${id}`),
  appendMessage: (id: string, data: { role: string; content: string; sql_content?: string; query_result?: string; approval?: string }) =>
    request.post(`/ai-sessions/${id}/messages`, data),
};

export type Script = {
  id: string;
  name: string;
  description: string;
  content: string;
  category: string;
  parent_id: string | null;
  node_type: 'folder' | 'script';
  db_type: string;
  sort_order: number;
  created_at: string;
  updated_at: string;
};

export const scriptAPI = {
  list: (keyword?: string) =>
    request.get('/scripts', { params: { keyword } }),
  tree: () =>
    request.get('/scripts/tree'),
  get: (id: string) => request.get(`/scripts/${id}`),
  create: (data: { name: string; description?: string; content?: string; category?: string; parent_id?: string | null; db_type?: string }) =>
    request.post('/scripts', data),
  createFolder: (data: { name: string; parent_id?: string | null }) =>
    request.post('/scripts/folders', data),
  update: (id: string, data: { name?: string; description?: string; content?: string; category?: string; db_type?: string }) =>
    request.put(`/scripts/${id}`, data),
  delete: (id: string) => request.delete(`/scripts/${id}`),
  move: (id: string, data: { parent_id: string | null }) =>
    request.post(`/scripts/${id}/move`, data),
};

export interface ScriptFileInfo {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  mod_time: string;
  content_type: string;
}

export interface ScriptListResult {
  files: ScriptFileInfo[];
  total: number;
  total_mode: string;
  page: number;
  page_size: number;
}

export const scriptFsAPI = {
  list: (dir?: string) =>
    request.get('/scripts/fs/list', { params: { dir: dir || '' } }),
  read: (path: string) =>
    request.get('/scripts/fs/read', { params: { path } }),
  save: (path: string, content: string) =>
    request.put('/scripts/fs/save', { path, content }),
  mkdir: (path: string) =>
    request.post('/scripts/fs/mkdir', { path }),
  remove: (path: string, isDir: boolean) =>
    request.delete('/scripts/fs/delete', { params: { path, is_dir: isDir } }),
  rename: (oldPath: string, newPath: string) =>
    request.put('/scripts/fs/rename', { old_path: oldPath, new_path: newPath }),
};

export const dbTransferAPI = {
  export: (data: {
    ds_id: string;
    schema: string;
    target_db_type: string;
    tables?: string[];
    include_structure?: boolean;
    include_data?: boolean;
    batch_size?: number;
  }) => request.post('/db-transfer/export', data, { timeout: 120000 }),
  import: (data: { ds_id: string; sql: string }) =>
    request.post('/db-transfer/import', data, { timeout: 120000 }),
};

export const exportTaskAPI = {
  list: (params?: { page?: number; page_size?: number; task_type?: string; status?: string }) =>
    request.get('/export-tasks', { params }),
  get: (id: string) => request.get(`/export-tasks/${id}`),
  createExport: (data: {
    name: string;
    data_source_id: string;
    database_name?: string;
    schema_name?: string;
    config: {
      export: {
        export_scope?: string;
        export_content?: string;
        export_tables?: string[];
        export_format?: string;
        export_batch_size?: number;
        storage_profile?: string;
        storage_path?: string;
      };
    };
  }) => request.post('/export-tasks', data),
  createImport: (data: {
    name: string;
    data_source_id: string;
    database_name?: string;
    schema_name?: string;
    config: {
      import: {
        import_source?: string;
        import_content?: string;
        import_strategy?: string;
        skip_safety_check?: boolean;
        import_file_name?: string;
        import_file_path?: string;
        storage_profile?: string;
        source_ds_id?: string;
        source_database?: string;
        source_schema?: string;
        source_tables?: string[];
        target_schema?: string;
        target_database?: string;
        target_table?: string;
      };
    };
  }) => request.post('/import-tasks', data),
  delete: (id: string) => request.delete(`/export-tasks/${id}`),
  start: (id: string) => request.post(`/export-tasks/${id}/start`),
  cancel: (id: string) => request.post(`/export-tasks/${id}/cancel`),
  listExecutions: (id: string) => request.get(`/export-tasks/${id}/executions`),
  getExecution: (id: string, execId: string) => request.get(`/export-tasks/${id}/executions/${execId}`),
  getExecutionLogs: (taskId: string, execId: string) => request.get(`/export-tasks/${taskId}/executions/${execId}/logs`),
  downloadExecution: (taskId: string, execId: string) => request.get(`/export-tasks/${taskId}/executions/${execId}/download`, { responseType: 'blob' }),
  createImportFromExport: (data: any) => request.post('/import-tasks/from-export', data),
  getManifest: (execId: string) => request.get(`/export-tasks/executions/${execId}/manifest`),
};

export type AISkillVersion = {
  id: string;
  skill_id: string;
  version: number;
  name: string;
  description: string;
  prompt_template: string;
  system_prompt: string;
  tools: string;
  script_files: string;
  resources: string;
  author_id: string;
  created_at: string;
};

export type AISkill = {
  id: string;
  slug: string;
  name: string;
  description: string;
  icon: string;
  category: string;
  prompt_template: string;
  system_prompt: string;
  tools: string;
  script_files: string;
  resources: string;
  input_vars: string;
  output_format: string;
  script_sandbox: string;
  is_builtin: boolean;
  is_active: boolean;
  version: number;
  author_id: string;
  tenant_id: string;
  created_at: string;
  updated_at: string;
};

export const aiSkillAPI = {
  list: (category?: string) =>
    request.get('/ai-skills', { params: { category } }),
  get: (id: string) => request.get(`/ai-skills/${id}`),
  create: (data: {
    slug?: string; name: string; description?: string; icon?: string; category?: string;
    prompt_template: string; system_prompt?: string; tools?: string;
    input_vars?: string; output_format?: string; script_files?: string; resources?: string;
  }) => request.post('/ai-skills', data),
  update: (id: string, data: {
    slug?: string; name?: string; description?: string; icon?: string; category?: string;
    prompt_template?: string; system_prompt?: string; tools?: string;
    input_vars?: string; output_format?: string; script_files?: string; resources?: string;
  }) => request.put(`/ai-skills/${id}`, data),
  delete: (id: string) => request.delete(`/ai-skills/${id}`),
  toggle: (id: string) => request.post(`/ai-skills/${id}/toggle`),
  preview: (id: string) => request.get(`/ai-skills/${id}/preview`),
  test: (id: string, data: { prompt?: string; variables?: Record<string, string>; data_source_id?: string }) =>
    request.post(`/ai-skills/${id}/test`, data),
  versions: (id: string) => request.get(`/ai-skills/${id}/versions`),
  rollback: (id: string, version: number) => request.post(`/ai-skills/${id}/rollback`, { version }),
  export: (id: string) => request.get(`/ai-skills/${id}/export`, { responseType: 'blob' }),
  import: (file: File) => {
    const fd = new FormData();
    fd.append('file', file);
    return request.post('/ai-skills/import', fd, { headers: { 'Content-Type': 'multipart/form-data' } });
  },
};

export const reportAPI = {
  list: (categoryId?: string) =>
    request.get('/reports', { params: categoryId ? { category_id: categoryId } : {} }),
  get: (id: string) => request.get(`/reports/${id}`),
  create: (data: {
    name: string;
    description?: string;
    data_source_id: string;
    schema_name?: string;
    sql_content: string;
    chart_type?: string;
    chart_config?: string;
    category_id?: string | null;
  }) => request.post('/reports', data),
  update: (id: string, data: {
    name?: string;
    description?: string;
    data_source_id?: string;
    schema_name?: string;
    sql_content?: string;
    chart_type?: string;
    chart_config?: string;
    category_id?: string | null;
  }) => request.put(`/reports/${id}`, data),
  delete: (id: string) => request.delete(`/reports/${id}`),
  execute: (id: string) => request.post(`/reports/${id}/execute`),
  generateSql: (data: {
    data_source_id: string;
    schema?: string;
    description: string;
  }) => request.post('/reports/generate-sql', data, { timeout: 60000 }),
};

export const reportCategoryAPI = {
  list: () => request.get('/report-categories'),
  create: (data: { name: string; parent_id?: string | null }) =>
    request.post('/report-categories', data),
  update: (id: string, data: { name?: string }) =>
    request.put(`/report-categories/${id}`, data),
  delete: (id: string) => request.delete(`/report-categories/${id}`),
  move: (id: string, data: { parent_id: string | null }) =>
    request.post(`/report-categories/${id}/move`, data),
};

export const dashboardReportAPI = {
  list: () => request.get('/dashboards'),
  get: (id: string) => request.get(`/dashboards/${id}`),
  create: (data: {
    name: string;
    description?: string;
    layout?: string;
  }) => request.post('/dashboards', data),
  update: (id: string, data: {
    name?: string;
    description?: string;
    layout?: string;
  }) => request.put(`/dashboards/${id}`, data),
  delete: (id: string) => request.delete(`/dashboards/${id}`),
};

export const sqlAPI = {
  transpile: (data: { sql: string; source_dialect: string; target_dialect: string }) =>
    request.post('/sql/transpile', data),
  format: (data: { sql: string; dialect: string }) =>
    request.post('/sql/format', data),
};

export const filesAPI = {
  list: (params: {
    dir?: string; page?: number; page_size?: number;
    search?: string; recursive?: boolean; next_token?: string;
    profile?: string;
  }) => request.get('/files', { params }),
  tree: (params: { dir?: string; depth?: number }) =>
    request.get('/files/tree', { params }),
  upload: (dir: string, file: File) => {
    const form = new FormData();
    form.append('dir', dir);
    form.append('file', file);
    return request.post('/files/upload', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
  download: (path: string) =>
    request.get('/files/download', {
      params: { path },
      responseType: 'blob',
    }),
  stat: (path: string) => request.get('/files/info', { params: { path } }),
  mkdir: (path: string) => request.post('/files/mkdir', { path }),
  rename: (oldPath: string, newPath: string) =>
    request.put('/files/rename', { old_path: oldPath, new_path: newPath }),
  delete: (path: string) => request.delete('/files', { params: { path } }),
  removeDir: (path: string) => request.delete('/files/dir', { data: { path } }),
  deleteBatch: (paths: string[]) => request.delete('/files/batch', { data: { paths } }),
  batchDownload: (paths: string[]) =>
    request.post('/files/batch/download', { paths }, { responseType: 'blob' }),
  quota: () => request.get('/files/quota'),
  profiles: () => request.get('/files/profiles'),
  createProfile: (data: {
    name: string; backend: string;
    local?: { root_dir: string };
    s3?: Record<string, any>;
  }) => request.post('/files/profiles', data),
  deleteProfile: (name: string) => request.delete(`/files/profiles/${name}`),
  updateProfile: (name: string, data: {
    backend: string;
    local?: { root_dir: string };
    s3?: Record<string, any>;
  }) => request.put(`/files/profiles/${name}`, data),
  testProfile: (name: string) => request.get(`/files/profiles/${name}/test`),
  toggleProfile: (name: string) => request.put(`/files/profiles/${name}/toggle`),
  setDefaultProfile: (name: string) =>
    request.put('/files/profiles/default', { name }),
  bindings: () => request.get('/files/bindings'),
  updateBinding: (
    moduleCode: string,
    data: { profile_code: string; base_path: string }
  ) => request.put(`/files/bindings/${moduleCode}`, data),
  copyFiles: (data: {
    source_profile: string;
    source_path: string;
    target_profile?: string;
    target_path?: string;
    overwrite?: boolean;
  }) => request.post('/files/copy', data),
  moveFiles: (data: {
    source_profile: string;
    source_path: string;
    target_profile?: string;
    target_path?: string;
    overwrite?: boolean;
  }) => request.post('/files/move', data),
  syncFiles: (data: {
    source_profile: string;
    source_path: string;
    target_profile?: string;
    target_path?: string;
    conflict?: string;
    mode?: string;
    dry_run?: boolean;
    filter?: { patterns?: string[]; min_size?: number; max_size?: number };
  }) => request.post('/files/sync', data),
};
