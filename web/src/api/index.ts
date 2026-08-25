import axios from 'axios';
import { message } from 'antd';
import type { APIResponse } from '../types';

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
    if (error.response?.status === 401) {
      localStorage.removeItem('token');
      window.location.href = '/login';
      return Promise.reject(error);
    }
    if (error.response?.status === 403) {
      message.error('权限不足');
      return Promise.reject(error);
    }
    if (error.response?.status === 503) {
      message.error('服务不可用');
      return Promise.reject(error);
    }
    message.error(error.message || '网络错误');
    return Promise.reject(error);
  }
);

export default request;

export const authAPI = {
  login: (data: { username: string; password: string }) =>
    request.post('/auth/login', data),
  changePassword: (data: { old_password: string; new_password: string }) =>
    request.post('/auth/change-password', data),
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
};

export const queryAPI = {
  // Generic execute (auto-detect SQL type)
  execute: (data: { data_source_id: string; sql: string; schema?: string; database?: string; page?: number; page_size?: number }) =>
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
    source_database?: string;
    target_ds: string;
    target_schema?: string;
    target_database?: string;
    table: string;
    action: 'create' | 'alter';
    dry_run?: boolean;
    override_ddl?: string;
  }) => request.post('/compare/sync-structure', data, { timeout: 300000 }),
  syncData: (data: {
    source_ds: string;
    source_schema?: string;
    source_database?: string;
    target_ds: string;
    target_schema?: string;
    target_database?: string;
    table: string;
    options: {
      truncate_target?: boolean;
      sync_id?: boolean;
      transactional?: boolean;
      mode: 'full' | 'selected' | 'diff';
      check_fields?: string[];
      sync_columns?: string[];
      selected_rows?: Record<string, any>[];
    };
  }) => request.post('/compare/sync-data', data, { timeout: 600000 }),
};

export const settingsAPI = {
  get: () => request.get('/settings'),
  update: (data: any) => request.put('/settings', data),
};

export const dashboardAPI = {
  stats: () => request.get('/dashboard/stats'),
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
  executeDDL: (data: { data_source_id: string; schema?: string; view?: string; sql: string; database?: string }) =>
    request.post('/view/ddl-exec', data, { timeout: 30000 }),
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
