import React, { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Select, Button, Table, message, Card, Space, Spin, Empty, Tooltip, Input, Dropdown, Modal,
  Tabs, Form, Radio, Checkbox, Popconfirm, Tag,
} from 'antd';
import Editor, { loader } from '@monaco-editor/react';
import * as monaco from 'monaco-editor';
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';

// Use local monaco-editor instead of CDN
loader.config({ monaco });

// Configure web worker for local monaco
(self as any).MonacoEnvironment = {
  getWorker() {
    return new editorWorker();
  },
};
import { useSearchParams } from 'react-router-dom';
import {
  PlayCircleOutlined,
  StopOutlined,
  DatabaseOutlined,
  TableOutlined,
  EyeOutlined,
  FolderOutlined,
  PlusOutlined,
  CodeOutlined,
  FilterOutlined,
  SortAscendingOutlined,
  SortDescendingOutlined,
  ExportOutlined,
  MoreOutlined,
  DeleteOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { dsAPI, queryAPI, tableAPI, viewAPI } from '../api';
import type { DataSource, SchemaInfo } from '../types';
import type { DataNode } from 'antd/es/tree';
import TableStructureDrawer from '../components/TableStructureDrawer';
import CreateTableModal from '../components/CreateTableModal';
import CreateViewModal from '../components/CreateViewModal';
import SchemaFormModal from '../components/SchemaFormModal';
import SchemaTree from '../components/SchemaTree';
import { getDialect } from '../utils/dialect';

// --- Types ---

interface QueryResult {
  columns: string[];
  rows: any[][];
  total_rows: number;
  duration: number;
}

interface BaseTab {
  id: string;
  title: string;
  closable: boolean;
  loading: boolean;
  result: QueryResult | null;
  page: number;
  pageSize: number;
  lastUsedAt: number;
  dsId?: string;
  dsType?: string; // For convenience, store the data source type here to avoid looking it up repeatedly
  database?: string;
  schema?: string;
}

interface TableTab extends BaseTab {
  type: 'table';
  table: string;
  isView: boolean;
  where: string;
  orderBy: string;
  columnFilters: Record<string, string>;
  columnSort: { column: string; direction: 'asc' | 'desc' } | null;
  totalRows: number;
}

interface SqlTab extends BaseTab {
  type: 'sql';
  sql: string;
  scriptId?: string;
  scriptName?: string;
  scriptSaveStatus: 'idle' | 'saving' | 'saved' | 'error';
}

interface SchemaListTab extends BaseTab {
  type: 'schema_list';
  database: string;
  schema: string;
  dsId: string;
  page: number;
  pageSize: number;
  totalItems: number;
  items: any[];
}

type TabItem = TableTab | SqlTab | SchemaListTab;

let tabIdCounter = 0;
const nextTabId = () => `tab-${++tabIdCounter}`;

// --- Component ---

const SQLEditor: React.FC = () => {
  const { t: tr } = useTranslation();
  // New SchemaTree replaces old tree; these remain for gradual migration
  // eslint-disable-next-line
  void (() => {}) as unknown;
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  
  const [treeDS, setTreeDS] = useState<string>(() => localStorage.getItem('sql_treeDS') || '');
  const treeDSRef = useRef<string>(localStorage.getItem('sql_treeDS') || '');
  const [_treeDatabase, setTreeDatabase] = useState<string>('');
  const [_treeSchema, setTreeSchema] = useState<string>('');
  useEffect(() => { setTreeSchema(''); setTreeDatabase(''); }, [treeDS]); // Reset schema when datasource changes
  // unused after SchemaTree migration:
  const [schemaLoading, setSchemaLoading] = useState(false); void schemaLoading;
  const [schemaNameList, setSchemaNameList] = useState<string[]>([]);
  const [treeData, setTreeData] = useState<DataNode[]>([]);
  const [schemasRaw, setSchemasRaw] = useState<SchemaInfo[]>([]);
  const [searchKeyword, _setSearchKeyword] = useState('');
  const loadedSchemasRef = useRef<Set<string>>(new Set());
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]); void expandedKeys;
  const [loadedKeys, setLoadedKeys] = useState<React.Key[]>([]); void loadedKeys;
  const [treeWidth, setTreeWidth] = useState(280);
  const [currentSchema, setCurrentSchema] = useState<string>(() => localStorage.getItem('sql_currentSchema') || '');
  const [currentDatabase, _setCurrentDatabase] = useState<string>(() => localStorage.getItem('sql_currentDatabase') || '');

  // Per-tab database/schema lists cache (key: tabId)
  const [tabDbListCache, setTabDbListCache] = useState<Record<string, string[]>>({});
  const [tabSchemaListCache, setTabSchemaListCache] = useState<Record<string, string[]>>({});
  const rightPanelRef = useRef<HTMLDivElement>(null);
  const draggingRef = useRef(false);

  // Tabs
  const [tabs, setTabs] = useState<TabItem[]>([]);
  const [activeTabId, setActiveTabId] = useState<string>('');
  const tabsRef = useRef<TabItem[]>(tabs);
  useEffect(() => { tabsRef.current = tabs; }, [tabs]);

  const MAX_TABS = 10;

  // Enforce max tab limit: close the least recently used tab
  const enforceMaxTabs = useCallback((currentTabs: TabItem[], excludeId?: string) => {
    if (currentTabs.length <= MAX_TABS) return currentTabs;
    const sorted = [...currentTabs]
      .filter((t) => t.id !== excludeId)
      .sort((a, b) => a.lastUsedAt - b.lastUsedAt);
    const toRemove = sorted.slice(0, currentTabs.length - MAX_TABS);
    return currentTabs.filter((t) => !toRemove.find((r) => r.id === t.id));
  }, []);

  // Add row handler
  const handleAddRow = async (values: Record<string, any>) => {
    const tab = rowAdd.tab as any;
    if (!tab) return;
    try {
      const ds = tab.dsId || treeDSRef.current;
      const dsInfo = dataSources.find(d => d.id === ds);
      const tabDbType = dsInfo?.type || dbType;
      const gen = getDialect(tabDbType);
      const schema = tab.schema || currentSchema || '';
      // Only include non-empty values; PK left empty = DB auto-increment/default
      const filledKeys = Object.keys(values).filter(k => values[k] !== undefined && values[k] !== '' && values[k] !== null);
      if (filledKeys.length === 0) { message.warning('请至少填写一个字段'); return; }
      const valuesObj: Record<string, any> = {};
      const colTypes: Record<string, string> = {};
      for (const k of filledKeys) {
        valuesObj[k] = values[k];
        colTypes[k] = columnMeta[k]?.type || '';
      }
      const sql = gen.insertQuery(schema, rowAdd.tableName, filledKeys, valuesObj, colTypes);
      await queryAPI.executeDML({ data_source_id: ds, sql, schema: tab.schema || currentSchema || undefined, database: tab.database || currentDatabase || undefined });
      message.success(tr('query.addSuccess'));
      setRowAdd({ open: false, tab: null, tableName: '', pkColumn: null });
      addForm.resetFields();
      if (tab?.type === 'table') loadTableTab(tab.id, tab.page, tab.pageSize);
    } catch (err: any) {
      message.error(err?.response?.data?.message || tr('query.addFailed'));
    }
  };

  // Delete single row
  const handleDeleteRow = async (record: Record<string, any>, pkCol: string, tab: any) => {
    if (!pkCol) { message.warning('该表无主键，无法删除'); return; }
    const pkVal = record[pkCol];
    if (pkVal == null) { message.warning('主键值为空，无法删除'); return; }
    const t = tab as any;
    const ds = tab.dsId || treeDSRef.current;
    const dsInfo = dataSources.find(d => d.id === ds);
    const tabDbType = dsInfo?.type || dbType;
    const gen = getDialect(tabDbType);
    const schema = t.schema || currentSchema || '';
    const whereClause = `${gen.quoteIdent(pkCol)} = ${gen.formatValue(String(pkVal), columnMeta[pkCol]?.type)}`;
    const sql = gen.deleteQuery(schema, t.table, whereClause);
    try {
      await queryAPI.executeDML({ data_source_id: ds, sql, schema: t.schema || currentSchema || undefined, database: t.database || currentDatabase || undefined });
      message.success(tr('query.deleteSuccess'));
      if (tab?.type === 'table') loadTableTab(tab.id, tab.page, tab.pageSize);
    } catch (err: any) {
      message.error(err?.response?.data?.message || tr('query.deleteFailed'));
    }
  };

  // Batch delete
  const handleBatchDelete = async () => {
    if (selectedRows.length === 0) { message.warning('请选择要删除的行'); return; }
    // Find the active table tab to get schema/table info
    const activeTableTab = tabs.find(t => t.type === 'table') as any;
    if (!activeTableTab) { message.warning('无法确定当前表'); return; }
    const ds = activeTableTab?.dsId || treeDSRef.current;
    const key = `${ds}:${activeTableTab.schema || ''}:${activeTableTab.table || ''}`;
    const pkCol = tablePKMap[key] || rowAdd.pkColumn;
    if (!pkCol) { message.warning('无法确定主键'); return; }
    const t = activeTableTab;
    const dsInfo = dataSources.find(d => d.id === ds);
    const tabDbType = dsInfo?.type || dbType;
    const gen = getDialect(tabDbType);
    const schema = t.schema || currentSchema || '';
    const pkType = columnMeta[pkCol]?.type || '';
    const pkVals = selectedRows.map(r => gen.formatValue(String(r[pkCol]), pkType)).join(', ');
    const whereClause = `${gen.quoteIdent(pkCol)} IN (${pkVals})`;
    const sql = gen.deleteQuery(schema, t.table, whereClause);
    try {
      await queryAPI.executeDML({ data_source_id: ds, sql, schema: t.schema || currentSchema || undefined, database: t.database || currentDatabase || undefined });
      message.success(tr('query.batchDeleteSuccess', { n: selectedRows.length }));
      setSelectedRows([]);
      if (t?.type === 'table') loadTableTab(t.id, t.page, t.pageSize);
    } catch (err: any) {
      message.error(err?.response?.data?.message || tr('query.batchDeleteFailed'));
    }
  };

  // Open add row modal for table tab
  const openAddRowModal = async (tab: TabItem) => {
    const t = tab as any;
    const tableName = t.type === 'table' ? t.table : '';
    const schema = t.type === 'table' ? t.schema || currentSchema || '' : '';
    const ds = (t as any)?.dsId ;
    const cacheKey = `${ds}:${schema}:${tableName}`;
    let pk = tablePKMap[`${ds}:${schema}:${tableName}`];
    // Use cached structure if available
    const cached = tableStructureCache[cacheKey];
    if (cached) {
      const meta: Record<string, { type: string; nullable: boolean; key: string; comment: string }> = {};
      cached.columns.forEach((c: any) => {
        meta[c.name] = { type: c.type || '', nullable: c.nullable ?? true, key: c.key || '', comment: c.comment || '' };
      });
      setColumnMeta(meta);
      if (cached.pkColumn) pk = cached.pkColumn;
      if (pk) setTablePKMap(prev => ({ ...prev, [`${ds}:${schema}:${tableName}`]: pk }));
    } else {
      // Fallback: fetch from API
      try {
        const res = await tableAPI.structure({ data_source_id: ds, schema, table: tableName });
        const cols = res.data?.data?.columns || [];
        const meta: Record<string, { type: string; nullable: boolean; key: string; comment: string }> = {};
        cols.forEach((c: any) => {
          meta[c.name] = { type: c.type || '', nullable: c.nullable ?? true, key: c.key || '', comment: c.comment || '' };
          if (c.key === 'PRI') pk = c.name;
        });
        setColumnMeta(meta);
        setTableStructureCache(prev => ({ ...prev, [cacheKey]: { columns: cols, pkColumn: pk } }));
        if (pk) setTablePKMap(prev => ({ ...prev, [`${ds}:${schema}:${tableName}`]: pk }));
      } catch { setColumnMeta({}); }
    }
    addForm.resetFields();
    setRowAdd({ open: true, tab, tableName, pkColumn: pk });
  };

  // Monaco editor ref (per active tab)
  const editorRef = useRef<any>(null);

  // Table structure drawer
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerTarget, setDrawerTarget] = useState<{ schema: string; table: string; isView: boolean; database?: string; dsId?: string } | null>(null);

  // Query result export
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [exportForm] = Form.useForm();
  const [_exportContext, setExportContext] = useState<{ sql?: string; table?: string; schema?: string; database?: string }>({});
  const [, setActiveTable] = useState<{ schema: string; table: string; isView: boolean } | null>(null);
  const [viewDefOpen, setViewDefOpen] = useState(false);
  const [viewDef, setViewDef] = useState<{ name: string; definition: string } | null>(null);
  const [viewDefLoading, setViewDefLoading] = useState(false);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; nodeKey: string } | null>(null);

  const dbType: string = 'mysql'; // DB type computed per-tab via getDsType()
  const currentUser = useMemo(() => {
    try { return JSON.parse(localStorage.getItem('user') || '{}'); } catch { return {}; }
  }, []);
  const readOnly = !currentUser.role || !['admin', 'operator'].includes(currentUser.role);

  // Row detail / edit modal state
  const [rowDetail, setRowDetail] = useState<{ open: boolean; data: Record<string, any> | null; columns: string[] }>({ open: false, data: null, columns: [] });
  const [rowEdit, setRowEdit] = useState<{ open: boolean; data: Record<string, any> | null; columns: string[]; tab: TabItem | null }>({ open: false, data: null, columns: [], tab: null });
  const [rowAdd, setRowAdd] = useState<{ open: boolean; tab: TabItem | null; tableName: string; pkColumn: string | null }>({ open: false, tab: null, tableName: '', pkColumn: null });
  const [addForm] = Form.useForm();
  const [selectedRows, setSelectedRows] = useState<Record<string, any>[]>([]);
  const [tablePKMap, setTablePKMap] = useState<Record<string, string | null>>({});
  const [columnMeta, setColumnMeta] = useState<Record<string, { type: string; nullable: boolean; key: string; comment: string }>>({});
  const [tableStructureCache, setTableStructureCache] = useState<Record<string, { columns: any[]; pkColumn: string | null }>>({});
  const [deleteTarget, setDeleteTarget] = useState<{ open: boolean; schema: string; name: string; isView: boolean; database?: string }>({ open: false, schema: '', name: '', isView: false });
  const [deleteConfirmName, setDeleteConfirmName] = useState('');
  const [queryCreateTable, setQueryCreateTable] = useState<{ open: boolean; schema: string; database?: string }>({ open: false, schema: '' });
  const [queryCreateView, setQueryCreateView] = useState<{ open: boolean; schema: string; database?: string }>({ open: false, schema: '' });
  const [schemaForm, setSchemaForm] = useState<{ open: boolean; mode: 'create' | 'edit'; database?: string; initValues?: { name: string; charset: string; collation: string } }>({ open: false, mode: 'create' });
  const [treeRefreshKey, setTreeRefreshKey] = useState(0);

  // Get DB type for the tree's current data source
  const treeDbType = useMemo(() => {
    const ds = dataSources.find(d => d.id === treeDSRef.current);
    return ds?.type || 'mysql';
  }, [dataSources, treeDS]);

  // Detect primary key column from table structure
  const detectPK = useCallback(async (dsId: string, schema: string, table: string): Promise<string | null> => {
    try {
      const res = await tableAPI.structure({ data_source_id: dsId, schema, table });
      const cols = res.data?.data?.columns || [];
      const pk = cols.find((c: any) => c.key === 'PRI' || c.key === 'PRI');
      return pk?.name || null;
    } catch { return null; }
  }, []);
  const [editForm] = Form.useForm();
  const [editSaving, setEditSaving] = useState(false);

  const activeTab = useMemo(() => tabs.find((t) => t.id === activeTabId) || null, [tabs, activeTabId]);

  // --- Drag resize ---
  const onDragStart = (e: React.MouseEvent) => {
    e.preventDefault();
    draggingRef.current = true;
    const startX = e.clientX;
    const startW = treeWidth;
    const onMove = (ev: MouseEvent) => {
      if (!draggingRef.current) return;
      const w = Math.max(150, Math.min(600, startW + ev.clientX - startX));
      setTreeWidth(w);
    };
    const onUp = () => {
      draggingRef.current = false;
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  };

  // --- Data source loading ---
  useEffect(() => {
    dsAPI.list('data_query').then(res => setDataSources(res.data.data?.list || [])).catch(() => {});
  }, []);

  useEffect(() => { localStorage.setItem('sql_currentSchema', currentSchema); }, [currentSchema]);
  useEffect(() => { localStorage.setItem('sql_currentDatabase', currentDatabase); }, [currentDatabase]);
  useEffect(() => { localStorage.setItem('sql_treeDS', treeDS); }, [treeDS]);

  // --- Restore from localStorage ---
  const restoredRef = useRef(false);
  useEffect(() => {
    restoredRef.current = true;
    if (!treeDSRef.current) return;
    (async () => {
      setSchemaLoading(true);
      try {
        const res = await dsAPI.schemaNames(treeDSRef.current);
        const names: string[] = res.data.data || [];
        setSchemaNameList(names);
      } catch {} finally { setSchemaLoading(false); }
    })();
  }, [dataSources]);

  // --- URL param auto-select ---
  const [searchParams, setSearchParams] = useSearchParams();
  useEffect(() => {
    const dsFromUrl = searchParams.get('ds');
    if (!dsFromUrl || dataSources.length === 0) return;
    const exists = dataSources.some((ds) => ds.id === dsFromUrl);
    if (!exists) { message.warning(`数据源 ${dsFromUrl} 不存在`); searchParams.delete('ds'); setSearchParams(searchParams, { replace: true }); return; }

    treeDSRef.current = dsFromUrl;
    setTreeDS(dsFromUrl);
    setSchemaNameList([]); setTreeData([]); setSchemasRaw([]);
    setExpandedKeys([]); setLoadedKeys([]); loadedSchemasRef.current.clear();
    (async () => {
      setSchemaLoading(true);
      try {
        const res = await dsAPI.schemaNames(dsFromUrl);
        const names: string[] = res.data.data || [];
        setSchemaNameList(names);
        if (names.length > 0) setCurrentSchema(names[0]);
      } catch {} finally { setSchemaLoading(false); }
    })();
    searchParams.delete('ds');
    setSearchParams(searchParams, { replace: true });
  }, [searchParams, dataSources]);

  // --- Tree data ---
  // Schema node title renderer (defined before useMemo)
  const renderSchemaTitle = (schemaName: string) => (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>{schemaName}</span>
      <Dropdown menu={{
        items: [
          { key: 'create-table', label: tr('query.createTable') },
          { key: 'create-view', label: tr('query.createView') },
        ],
        onClick: ({ key }: { key: string }) => {
          if (key === 'create-table') setQueryCreateTable({ open: true, schema: schemaName });
          else if (key === 'create-view') setQueryCreateView({ open: true, schema: schemaName });
        },
      }} trigger={['click']} placement="bottomRight">
        <Button type="text" size="small"
          icon={<MoreOutlined style={{ fontSize: 14 }} />}
          onClick={(e) => e.stopPropagation()}
          style={{ flexShrink: 0, opacity: 0.5 }}
        />
      </Dropdown>
    </div>
  );

  const displayTreeData = useMemo(() => {
    return schemaNameList.map((name) => {
      const key = `schema-${name}`;
      const loaded = treeData.find((n) => n.key === key);
      return {
        title: renderSchemaTitle(name),
        key, icon: <DatabaseOutlined />, isLeaf: false, children: loaded?.children,
      };
    });
  }, [schemaNameList, treeData, currentSchema]);

  const updateNodeChildren = (data: DataNode[], key: string, children: DataNode[]): DataNode[] => {
    return data.map((node) => {
      if (node.key === key) return { ...node, children };
      if (node.children) return { ...node, children: updateNodeChildren(node.children, key, children) };
      return node;
    });
  };

  const onLoadData = async (node: DataNode) => { void node; void onLoadData;
    const key = String(node.key);
    if (!key.startsWith('schema-') || !treeDSRef.current) return;
    const schemaName = key.slice('schema-'.length);
    if (loadedSchemasRef.current.has(schemaName)) return;
    try {
      const res = await dsAPI.schemaObjects(treeDSRef.current, schemaName);
      const schema: SchemaInfo = res.data.data;
      loadedSchemasRef.current.add(schemaName);
      const tables = schema.tables || [];
      const views = schema.views || [];
      const children: DataNode[] = [
        {
          title: `Tables (${tables.length})`, key: `tables-${schemaName}`, icon: <FolderOutlined />,
          children: tables.length > 0 ? tables.map((table) => ({
            title: renderTreeNodeTitle(table.name, schemaName, false),
            key: `table-${schemaName}-${table.name}`, icon: <TableOutlined />, isLeaf: true,
          })) : undefined, isLeaf: tables.length === 0,
        },
        {
          title: `Views (${views.length})`, key: `views-${schemaName}`, icon: <FolderOutlined />,
          children: views.length > 0 ? views.map((view) => ({
            title: renderTreeNodeTitle(view.name, schemaName, true),
            key: `view-${schemaName}-${view.name}`, icon: <EyeOutlined />, isLeaf: true,
          })) : undefined, isLeaf: views.length === 0,
        },
      ];
      setTreeData((prev) => {
        const exists = prev.some((n) => n.key === key);
        if (exists) return updateNodeChildren(prev, key, children);
        return [...prev, { key, children } as any];
      });
      setExpandedKeys((prev) => prev.includes(key) ? prev : [...prev, key]);
      const folderKeys = [`tables-${schemaName}`, `views-${schemaName}`];
      setLoadedKeys((prev) => {
        const newKeys = [...prev];
        if (!newKeys.includes(key)) newKeys.push(key);
        for (const fk of folderKeys) { if (!newKeys.includes(fk)) newKeys.push(fk); }
        return newKeys;
      });
    } catch {
      loadedSchemasRef.current.add(schemaName);
      setLoadedKeys((prev) => prev.includes(key) ? prev : [...prev, key]);
    }
  };

  const _handleDSChange = async (value: string) => {
    treeDSRef.current = value;
    setExpandedKeys([]); setLoadedKeys([]); loadedSchemasRef.current.clear();
    // Clear per-tab database/schema cache and tab selections when DS changes
    setTabDbListCache({}); setTabSchemaListCache({});
    setTabs(prev => prev.map(t => ({ ...t, database: undefined, schema: undefined, dsId: t.type === 'sql' ? value : t.dsId } as TabItem)));
    if (!value) return;
    setSchemaLoading(true);
    try {
      const res = await dsAPI.schemaNames(value);
      const names: string[] = res.data.data || [];
      setSchemaNameList(names);
      if (names.length > 0) setCurrentSchema(names[0]);
    } catch {} finally { setSchemaLoading(false); }
  };
  void _handleDSChange;

  const [searchLoading, setSearchLoading] = useState(false); void searchLoading;
  useEffect(() => {
    if (!searchKeyword || !treeDSRef.current) { setSearchLoading(false); return; }
    if (schemasRaw.length > 0) return;
    let cancelled = false;
    (async () => {
      setSearchLoading(true);
      try { const res = await dsAPI.schema(treeDSRef.current); if (!cancelled) setSchemasRaw(res.data.data || []); } catch {}
      finally { if (!cancelled) setSearchLoading(false); }
    })();
    return () => { cancelled = true; };
  }, [searchKeyword]);

  const filteredTreeData = useMemo(() => { void 0 as unknown as typeof filteredTreeData;
    if (!searchKeyword) return displayTreeData;
    const kw = searchKeyword.toLowerCase();
    return schemasRaw.map((schema) => {
      const tables = (schema.tables || []).filter((t) => t.name.toLowerCase().includes(kw));
      const views = (schema.views || []).filter((v) => v.name.toLowerCase().includes(kw));
      if (tables.length === 0 && views.length === 0) return null;
      return {
        title: renderSchemaTitle(schema.name), key: `schema-${schema.name}`, icon: <DatabaseOutlined />,
        children: [
          { title: `Tables (${tables.length})`, key: `tables-${schema.name}`, icon: <FolderOutlined />,
            children: tables.map((t) => ({ title: renderTreeNodeTitle(t.name, schema.name, false), key: `table-${schema.name}-${t.name}`, icon: <TableOutlined />, isLeaf: true })),
            isLeaf: tables.length === 0 },
          { title: `Views (${views.length})`, key: `views-${schema.name}`, icon: <FolderOutlined />,
            children: views.map((v) => ({ title: renderTreeNodeTitle(v.name, schema.name, true), key: `view-${schema.name}-${v.name}`, icon: <EyeOutlined />, isLeaf: true })),
            isLeaf: views.length === 0 },
        ],
      } as DataNode;
    }).filter(Boolean) as DataNode[];
  }, [searchKeyword, schemasRaw, displayTreeData]);

  // --- Tab management ---
  const updateTab = useCallback((id: string, updates: Partial<TabItem>) => {
    setTabs((prev) => prev.map((t) => t.id === id ? { ...t, ...updates } as TabItem : t));
  }, []);

  // Update lastUsedAt when tab becomes active
  useEffect(() => {
    if (!activeTabId) return;
    updateTab(activeTabId, { lastUsedAt: Date.now() } as any);
  }, [activeTabId, updateTab]);

  const closeTab = useCallback((id: string) => {
    setTabs((prev) => {
      const idx = prev.findIndex((t) => t.id === id);
      if (idx < 0) return prev;
      const newTabs = prev.filter((t) => t.id !== id);
      if (activeTabId === id) {
        const newActive = newTabs[Math.min(idx, newTabs.length - 1)];
        setActiveTabId(newActive?.id || '');
      }
      return newTabs;
    });
  }, [activeTabId]);

  // --- Query execution ---
  const executeQuery = useCallback(async (sql: string, schema: string | undefined, page: number, pageSize: number, database?: string, dsId?: string): Promise<QueryResult | null> => {
    const ds = dsId ;
    if (!ds) { message.warning(tr('query.selectDS')); return null; }
    try {
      const res = await queryAPI.execute({ data_source_id: ds, sql, schema: schema || undefined, page, page_size: pageSize, database });
      return res.data.data;
    } catch { return null; }
  }, []);

  // Fetch available databases/schemas for the tab (dsId passed directly to avoid stale state)
  const fetchDatabasesForTab = useCallback(async (tabId: string, dsId: string) => {
    const ds = dsId ;
    if (!ds) return;
    const dsInfo = dataSources.find(d => d.id === ds);
    const tabDbType = dsInfo?.type || dbType;
    try {
      if (tabDbType === 'postgres' || tabDbType === 'sqlserver') {
        const res = await (dsAPI as any).databases(ds);
        const dbs: string[] = Array.isArray(res.data) ? res.data : (res.data?.data || []);
        setTabDbListCache(prev => ({ ...prev, [tabId]: dbs }));
      } else {
        const res = await dsAPI.schemaNames(ds);
        const schemas: string[] = Array.isArray(res.data) ? res.data : (res.data?.data || res.data?.list || []);
        setTabSchemaListCache(prev => ({ ...prev, [tabId]: schemas }));
      }
    } catch {}
  }, [dbType, dataSources]);

  const fetchSchemasForTab = useCallback(async (tabId: string, database: string, dsId: string) => {
    const ds = dsId ;
    if (!ds || !database) return;
    const dsInfo = dataSources.find(d => d.id === ds);
    const tabDbType = dsInfo?.type || dbType;
    if (tabDbType !== 'postgres' && tabDbType !== 'sqlserver') return;
    try {
      const res = await (dsAPI as any).databaseSchemas(ds, database);
      const schemas: string[] = Array.isArray(res.data) ? res.data : (res.data?.data || []);
      setTabSchemaListCache(prev => ({ ...prev, [tabId]: schemas }));
    } catch {}
  }, [dbType, dataSources]);

  // --- Table tab: build SQL ---
  const buildTableSQL = useCallback((tab: TableTab): string => {
    const dsInfo = dataSources.find(d => d.id === (tab.dsId ));
    const tabDbType = dsInfo?.type || dbType;
    const gen = getDialect(tabDbType);
    const conditions: string[] = [];
    if (tab.where.trim()) conditions.push(`(${tab.where.trim()})`);
    for (const [col, val] of Object.entries(tab.columnFilters)) {
      if (val.trim()) {
        const q = gen.quoteIdent(col);
        if (/^\d+$/.test(val.trim())) {
          conditions.push(`${q} = ${val.trim()}`);
        } else {
          conditions.push(`${q} LIKE '%${val.trim().replace(/'/g, "''")}%'`);
        }
      }
    }
    const whereClause = conditions.length > 0 ? conditions.join(' AND ') : undefined;
    const orderParts: string[] = [];
    if (tab.columnSort) {
      orderParts.push(`${gen.quoteIdent(tab.columnSort.column)} ${tab.columnSort.direction.toUpperCase()}`);
    } else if (tab.orderBy.trim()) {
      orderParts.push(tab.orderBy.trim());
    }
    const orderBy = orderParts.length > 0 ? orderParts.join(', ') : undefined;
    return gen.selectQuery(tab.schema || '', tab.table, undefined, whereClause, orderBy);
  }, [dbType, dataSources]);

  const countSQL = useCallback((tab: TableTab): string => {
    const dsInfo = dataSources.find(d => d.id === (tab.dsId ));
    const tabDbType = dsInfo?.type || dbType;
    const gen = getDialect(tabDbType);
    const conditions: string[] = [];
    if (tab.where.trim()) conditions.push(`(${tab.where.trim()})`);
    for (const [col, val] of Object.entries(tab.columnFilters)) {
      if (val.trim()) {
        const q = gen.quoteIdent(col);
        if (/^\d+$/.test(val.trim())) {
          conditions.push(`${q} = ${val.trim()}`);
        } else {
          conditions.push(`${q} LIKE '%${val.trim().replace(/'/g, "''")}%'`);
        }
      }
    }
    return gen.countQuery(tab.schema || '', tab.table, conditions.length > 0 ? conditions.join(' AND ') : undefined);
  }, [dbType]);

  // --- Load table tab data ---
  const loadTableTab = useCallback(async (tabId: string, page: number, pageSize: number) => {
    const tab = tabsRef.current.find((t) => t.id === tabId) as TableTab | undefined;
    if (!tab) return;
    updateTab(tabId, { loading: true });
    try {
      const sql = buildTableSQL(tab);
      const result = await executeQuery(sql, tab.schema || '', page, pageSize, tab.database, tab.dsId);
      const totalRows = result?.total_rows || 0;
      updateTab(tabId, { result, page, pageSize, loading: false, totalRows });
      // Auto-detect primary key
      const ds = tab.dsId || treeDSRef.current;
      const schema = tab.schema || '';
      // PK already detected when tab opened — no need to re-query structure
      const cacheKey = `${ds}:${schema}:${tab.table}`;
      if (!tableStructureCache[cacheKey] && !tablePKMap[`${ds}:${schema}:${tab.table}`]) {
        detectPK(ds, schema, tab.table).then(pk => {
          setTablePKMap(prev => ({ ...prev, [`${ds}:${schema}:${tab.table}`]: pk || null }));
        }).catch(() => {});
      }
    } catch {
      updateTab(tabId, { loading: false });
    }
  }, [tabs, updateTab, buildTableSQL, countSQL, executeQuery]);

  // --- Load SQL tab data ---
  const loadSQLTab = useCallback(async (tabId: string) => {
    const tab = tabs.find((t) => t.id === tabId) as SqlTab | undefined;
    if (!tab) return;
    // Get selected text from Monaco editor, fall back to full SQL
    let sql = tab.sql;
    if (editorRef.current) {
      const selection = editorRef.current.getSelection();
      if (selection && !selection.isEmpty()) {
        const model = editorRef.current.getModel();
        if (model) {
          const selected = model.getValueInRange(selection) as string;
          if (selected.trim()) sql = selected;
        }
      }
    }
    if (!sql.trim()) return;
    updateTab(tabId, { loading: true });
    const result = await executeQuery(sql, tab.schema, 1, tab.pageSize, tab.database, tab.dsId);
    updateTab(tabId, { result, page: 1, loading: false });
  }, [tabs, updateTab, executeQuery, currentSchema]);

  // --- Tree select: open table tab ---
  const parseNodeKey = (key: string): { kind: 'table' | 'view' | 'other'; schema: string; name: string } => {
    if (key.startsWith('table-')) {
      const rest = key.slice('table-'.length);
      const idx = rest.indexOf('-');
      if (idx < 0) return { kind: 'other', schema: '', name: '' };
      return { kind: 'table', schema: rest.slice(0, idx), name: rest.slice(idx + 1) };
    }
    if (key.startsWith('view-')) {
      const rest = key.slice('view-'.length);
      const idx = rest.indexOf('-');
      if (idx < 0) return { kind: 'other', schema: '', name: '' };
      return { kind: 'view', schema: rest.slice(0, idx), name: rest.slice(idx + 1) };
    }
    return { kind: 'other', schema: '', name: '' };
  };

  const openTableTab = useCallback((schema: string, table: string, isView: boolean, database?: string, dsId?: string) => {
    const ds = dsId ;
    if (!ds) return;
    // Check if already open (use ref to avoid stale closure on rapid clicks)
    // 必须同时区分数据源/数据库/schema/表名/表视图，否则 MSSQL 下不同 database 或不同 schema 的同名表会相互覆盖。
    const existing = tabsRef.current.find((t) =>
      t.type === 'table'
      && (t as TableTab).schema === schema
      && (t as TableTab).table === table
      && (t as TableTab).database === database
      && (t as TableTab).dsId === ds
      && (t as TableTab).isView === isView
    );
    if (existing) { setActiveTabId(existing.id); return; }
    const id = nextTabId();
    const newTab: TableTab = {
      id, type: 'table', title: schema ? `${schema}.${table}` : table, closable: true, loading: false,
      result: null, page: 1, pageSize: 20, lastUsedAt: Date.now(),
      schema, table, database, dsId: ds, isView, where: '', orderBy: '',
      columnFilters: {}, columnSort: null, totalRows: 0,
    };
    setActiveTable({ schema, table, isView });
    setTabs((prev) => enforceMaxTabs([...prev, newTab], id));
    setActiveTabId(id);
    // Fetch table structure first, then load data
    const cacheKey = `${ds}:${schema}:${table}`;
    const loadData = () => {
      setTimeout(() => {
        setTabs((prev) => {
          const t = prev.find((x) => x.id === id) as TableTab | undefined;
          if (t) updateTab(id, { loading: true });
          return prev;
        });
        const tab = tabsRef.current.find((x) => x.id === id) as TableTab | undefined;
        if (tab) {
          const sql = buildTableSQL(tab);
          executeQuery(sql, schema, 1, 20, database, ds).then((result) => {
            const totalRows = result?.total_rows || 0;
            updateTab(id, { result, page: 1, pageSize: 20, loading: false, totalRows });
          });
        }
      }, 0);
    };
    if (tableStructureCache[cacheKey]) {
      loadData();
    } else {
      tableAPI.structure({ data_source_id: ds, schema, table, database }).then(res => {
        const cols = res.data?.data?.columns || [];
        let pk: string | null = null;
        const meta: Record<string, { type: string; nullable: boolean; key: string; comment: string }> = {};
        cols.forEach((c: any) => {
          meta[c.name] = { type: c.type || '', nullable: c.nullable ?? true, key: c.key || '', comment: c.comment || '' };
          if (c.key === 'PRI') pk = c.name;
        });
        setColumnMeta(prev => ({ ...prev, ...meta }));
        setTableStructureCache(prev => ({ ...prev, [cacheKey]: { columns: cols, pkColumn: pk } }));
        if (pk) setTablePKMap(prev => ({ ...prev, [`${ds}:${schema}:${table}`]: pk }));
        loadData();
      }).catch(() => loadData());
    }
  }, [treeDSRef.current, tabs, buildTableSQL, countSQL, executeQuery, updateTab]);

  const handleTreeSelect = (keys: React.Key[]) => { void keys; void handleTreeSelect;
    if (!keys.length || !treeDSRef.current) return;
    const parsed = parseNodeKey(String(keys[0]));
    if (parsed.kind === 'other') return;
    openTableTab(parsed.schema, parsed.name, parsed.kind === 'view');
  };

  // --- Open schema list tab (when clicking database/schema node) ---
  const openSchemaListTab = useCallback((database: string, schema: string, dsId: string) => {
    const dsName = dataSources.find(d => d.id === dsId)?.name || dsId.slice(0, 8);
    const label = schema ? `📋 ${schema}@${dsName}` : `📋 ${database}@${dsName}`;
    const existing = tabsRef.current.find((t) =>
      t.type === 'schema_list' && (t as SchemaListTab).database === database && (t as SchemaListTab).schema === schema && (t as SchemaListTab).dsId === dsId
    );
    if (existing) { setActiveTabId(existing.id); return; }
    const id = nextTabId();
    const newTab: SchemaListTab = {
      id, type: 'schema_list', title: label, closable: true, loading: true, lastUsedAt: Date.now(),
      result: null, database, schema, dsId, page: 1, pageSize: 20, totalItems: 0, items: [],
    };
    setTabs((prev) => enforceMaxTabs([...prev, newTab], id));
    setActiveTabId(id);
    // Load table list async
    dsAPI.tableList(dsId, schema, database).then((res: any) => {
      const list = res.data?.data || [];
      updateTab(id, { items: list, totalItems: list.length, loading: false });
    }).catch(() => updateTab(id, { loading: false }));
  }, [enforceMaxTabs]);

  // --- Add SQL tab ---
  const addSQLTab = useCallback(() => {
    const id = nextTabId();
    const sqlCount = tabs.filter((t) => t.type === 'sql').length + 1;
    const newTab: SqlTab = {
      id, type: 'sql', title: `${tr('query.sqlQuery')} ${sqlCount}`, closable: true, loading: false,
      result: null, page: 1, pageSize: 20, lastUsedAt: Date.now(),
      sql: 'SELECT 1', scriptSaveStatus: 'idle',
      database: undefined,
      schema: undefined,
      dsId: treeDSRef.current,
    };
    
    setTabs((prev) => enforceMaxTabs([...prev, newTab], id));
    setActiveTabId(id);
  }, [tabs, enforceMaxTabs, currentDatabase, currentSchema]);

  // --- Tree node actions ---
  const handleCopyDDL = async (schema: string, table: string) => {
    if (!treeDSRef.current) return;
    let ddl = '';
    try {
      const res = await dsAPI.getDdl(treeDSRef.current, schema, table);
      ddl = res.data.data?.ddl || '';
    } catch {
      message.error('获取建表语句失败');
      return;
    }
    if (!ddl) {
      message.warning('未获取到建表语句');
      return;
    }
    try {
      await navigator.clipboard.writeText(ddl);
      message.success('建表语句已复制到剪贴板');
    } catch {
      // Clipboard API 在非安全上下文中不可用,尝试 fallback
      const textarea = document.createElement('textarea');
      textarea.value = ddl;
      textarea.style.position = 'fixed';
      textarea.style.opacity = '0';
      document.body.appendChild(textarea);
      textarea.select();
      try {
        document.execCommand('copy');
        message.success('建表语句已复制到剪贴板');
      } catch {
        message.error('复制失败,请手动复制');
      }
      document.body.removeChild(textarea);
    }
  };

  const handleExportTreeNode = (schema: string, table: string) => {
    setActiveTable({ schema, table, isView: false });
    openExportModal({ table, schema });
  };

  // Dropdown menu for tree nodes
  const treeNodeMenu = (schema: string, name: string, isView: boolean) => ({
    items: [
      { key: 'structure', label: isView ? tr('query.viewDefinition') : tr('query.viewStructure') },
      { key: 'copy-ddl', label: tr('query.copyDdl') },
      { key: 'export', label: tr('query.exportTable') },
      { type: 'divider' as const },
      { key: 'delete', label: `删除${isView ? '视图' : '表'}`, danger: true },
    ],
    onClick: ({ key }: { key: string }) => {
      if (key === 'structure') {
        if (isView) openViewDef(schema, name);
        else openStructureDrawer(schema, name, false);
      } else if (key === 'copy-ddl') {
        handleCopyDDL(schema, name);
      } else if (key === 'export') {
        handleExportTreeNode(schema, name);
      } else if (key === 'delete') {
        setDeleteTarget({ schema, name, isView, open: true });
      }
    },
  });

  const renderTreeNodeTitle = (name: string, schema: string, isView: boolean) => (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>{name}</span>
      <Dropdown menu={treeNodeMenu(schema, name, isView)} trigger={['click']} placement="bottomRight">
        <Button type="text" size="small"
          icon={<MoreOutlined style={{ fontSize: 14 }} />}
          onClick={(e) => e.stopPropagation()}
          style={{ flexShrink: 0, opacity: 0.5 }}
        />
      </Dropdown>
    </div>
  );

  // --- Structure drawer ---
  const openStructureDrawer = (schema: string, table: string, isView: boolean, database?: string) => {
    if (!treeDSRef.current) { message.warning('请先选择数据源'); return; }
    setDrawerTarget({ schema, table, isView, database }); setDrawerOpen(true);
  };

  const openViewDef = async (schema: string, viewName: string, database?: string) => {
    if (!treeDSRef.current) return;
    setViewDefLoading(true); setViewDef({ name: viewName, definition: '-- loading...' }); setViewDefOpen(true);
    try {
      const res = await viewAPI.definition({ data_source_id: treeDSRef.current, schema: schema || undefined, view: viewName, database: database || undefined });
      setViewDef({ name: viewName, definition: res.data.data?.definition || '-- 无定义' });
    } catch { setViewDef({ name: viewName, definition: '-- 获取失败' }); }
    finally { setViewDefLoading(false); }
  };

  // --- Context menu ---
  const handleTreeRightClick = (info: any) => { void info; void handleTreeRightClick;
    const key = String(info.node.key);
    const parsed = parseNodeKey(key);
    if (parsed.kind === 'other') return;
    setActiveTable({ schema: parsed.schema, table: parsed.name, isView: parsed.kind === 'view' });
    setContextMenu({ x: info.event.clientX, y: info.event.clientY, nodeKey: key });
  };

  const handleContextMenuClick = ({ key }: { key: string }) => {
    if (!contextMenu) return;
    const parsed = parseNodeKey(contextMenu.nodeKey);
    if (key === 'select') openTableTab(parsed.schema, parsed.name, parsed.kind === 'view');
    else if (key === 'structure') openStructureDrawer(parsed.schema, parsed.name, parsed.kind === 'view');
    else if (key === 'view-def') openViewDef(parsed.schema, parsed.name);
    setContextMenu(null);
  };

  const refreshTree = async () => {
    if (!treeDSRef.current) return;
    loadedSchemasRef.current.clear(); setSchemasRaw([]); setExpandedKeys([]); setLoadedKeys([]);
    try { const res = await dsAPI.schemaNames(treeDSRef.current); setSchemaNameList(res.data.data || []); setTreeData([]); } catch {}
  };

  // --- Keyboard shortcut ---
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault();
      if (activeTab?.type === 'sql') loadSQLTab(activeTab.id);
    }
  };

  // --- Render table result ---
  const renderTableResult = (tab: TabItem, onPageChange?: (page: number, size?: number) => void) => {
    const result = tab.result;
    if (!result) return null;
    const columns: any[] = result.columns.map((col) => {
      const meta = columnMeta[col];
      const tooltip = meta ? `${meta.type}${meta.key === 'PRI' ? ' PK' : ''}${meta.nullable ? '' : ' NOT NULL'}${meta.comment ? ' - ' + meta.comment : ''}` : '';
      return {
      title: (
        <Tooltip title={tooltip || col}>
        <Dropdown
          trigger={['click']}
          menu={{
            items: tab.type === 'table' ? [
              { key: 'sort-asc', label: tr('query.sortAsc'), icon: <SortAscendingOutlined /> },
              { key: 'sort-desc', label: tr('query.sortDesc'), icon: <SortDescendingOutlined /> },
              { key: 'sort-clear', label: tr('query.sortClear') },
              { type: 'divider' as const },
              { key: 'filter', label: (
                <Input
                  size="small"
                  placeholder="过滤值..."
                  value={(tab as TableTab).columnFilters?.[col] || ''}
                  onChange={(e) => {
                    const t = tab as TableTab;
                    updateTab(tab.id, { columnFilters: { ...t.columnFilters, [col]: e.target.value } } as any);
                  }}
                  onPressEnter={() => { if (tab.type === 'table') loadTableTab(tab.id, 1, tab.pageSize); }}
                  onClick={(e) => e.stopPropagation()}
                />
              )},
            ] : [],
            onClick: ({ key: k }) => {
              if (tab.type === 'table') {
                const newSort = k === 'sort-asc' ? { column: col, direction: 'asc' as const } : k === 'sort-desc' ? { column: col, direction: 'desc' as const } : null;
                setTabs(prev => prev.map(t => t.id === tab.id ? { ...t, columnSort: newSort, orderBy: '', page: 1 } as TableTab : t));
                // Defer reload so state is updated
                setTimeout(() => loadTableTab(tab.id, 1, (tab as TableTab).pageSize), 0);
              }
            },
          }}
        >
          <span style={{ cursor: 'pointer' }}>
            {col}
            {tab.type === 'table' && (tab as TableTab).columnSort?.column === col && (
              (tab as TableTab).columnSort?.direction === 'asc'
                ? <SortAscendingOutlined style={{ marginLeft: 4, color: '#20a53a' }} />
                : <SortDescendingOutlined style={{ marginLeft: 4, color: '#20a53a' }} />
            )}
            {tab.type === 'table' && (tab as TableTab).columnFilters?.[col] && (
              <FilterOutlined style={{ marginLeft: 4, color: '#faad14' }} />
            )}
          </span>
        </Dropdown>
        </Tooltip>
      ),
      dataIndex: col, key: col, width: 160,
      ellipsis: { showTitle: false },
      render: (val: any) => (
        <Tooltip title={val != null ? String(val) : ''} placement="topLeft">
          <div style={{ maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {val != null ? String(val) : ''}
          </div>
        </Tooltip>
      ),
    };
  });
    // Add action column
    const dataSource = result.rows.map((row, i) => {
      const obj: Record<string, any> = { key: i };
      result.columns.forEach((c, j) => { obj[c] = row[j]; });
      return obj;
    });
    const isTableTab = tab.type === 'table';
    if (isTableTab) {
      columns.unshift({
        title: (
          <Checkbox
            checked={dataSource.length > 0 && selectedRows.length === dataSource.length}
            indeterminate={selectedRows.length > 0 && selectedRows.length < dataSource.length}
            onChange={(e) => setSelectedRows(e.target.checked ? [...dataSource] : [])}
          />
        ), key: 'select', width: 40, fixed: 'left' as const,
        render: (_: any, record: Record<string, any>) => (
          <Checkbox
            checked={selectedRows.some(r => r.key === record.key)}
            onChange={(e) => { e.target.checked ? setSelectedRows([...selectedRows, record]) : setSelectedRows(selectedRows.filter(r => r.key !== record.key)); }}
          />
        ),
      });
    }
    columns.push({
      title: tr('datasource.tableAction'), key: 'action', width: 160, fixed: 'right' as const,
      render: (_: any, record: Record<string, any>) => (
        <Space size={4}>
          <Button type="link" size="small" onClick={() => {
            setRowDetail({ open: true, data: record, columns: result.columns });
          }}>{tr('query.view')}</Button>
          <Button type="link" size="small" onClick={async () => {
            if (isTableTab) {
              const t = tab as any;
              // Use cached structure if available, otherwise fetch
              const cacheKey = `${t.dsId }:${t.schema || ''}:${t.table || ''}`;
              if (tableStructureCache[cacheKey]) {
                const { columns: cols } = tableStructureCache[cacheKey];
                const meta: Record<string, { type: string; nullable: boolean; key: string; comment: string }> = {};
                cols.forEach((c: any) => {
                  meta[c.name] = { type: c.type || '', nullable: c.nullable ?? true, key: c.key || '', comment: c.comment || '' };
                });
                setColumnMeta(prev => ({ ...prev, ...meta }));
              }
            }
            setRowEdit({ open: true, data: record, columns: result.columns, tab }); editForm.setFieldsValue(record);
          }}>{tr('query.edit')}</Button>
          {isTableTab && (
            <Popconfirm title={tr('query.confirmDelete')} onConfirm={async () => {
              const t = tab as any;
              const ds = t.dsId || treeDSRef.current;
              const key = `${ds}:${t.schema || ''}:${t.table || ''}`;
              let pk = tablePKMap[key];
              if (!pk) {
                pk = await detectPK(ds, t.schema || '', t.table || '');
                if (pk) setTablePKMap(prev => ({ ...prev, [key]: pk }));
              }
              if (!pk) { message.warning(tr('query.noPK')); return; }
              handleDeleteRow(record, pk, tab);
            }}>
              <Button type="link" size="small" danger>删除</Button>
            </Popconfirm>
          )}
        </Space>
      ),
    });
    const total = tab.type === 'table' ? (tab as TableTab).totalRows : result.total_rows;
    return (
      <>
      <Table
        columns={columns} dataSource={dataSource}
        scroll={{ x: 'max-content' }} size="small" tableLayout="fixed"
        loading={tab.loading}
        pagination={{
          current: tab.page, pageSize: tab.pageSize, total,
          showSizeChanger: true, showTotal: (total) => tab.result ? `${tr('query.duration')} ${tab.result.duration}ms | ${tr('common.total')} ${total} ${tr('common.rows')}` : `${tr('common.total')} ${total} ${tr('common.rows')}`,
          pageSizeOptions: ['10', '20', '50', '100'],
          onChange: (p, s) => {
            if (onPageChange) onPageChange(p, s);
            else if (tab.type === 'table') loadTableTab(tab.id, p, s || tab.pageSize);
          },
        }}
      />
      <Modal title={tr('query.rowDetail')} open={rowDetail.open} onCancel={() => setRowDetail({ open: false, data: null, columns: [] })}
        footer={null} width={800}
      >
        {rowDetail.data && (
          <Table
            tableLayout="fixed"
            dataSource={rowDetail.columns.map((col, i) => {
              const meta = columnMeta[col];
              return {
                key: i,
                name: col,
                value: rowDetail.data![col] != null ? String(rowDetail.data![col]) : null,
                meta: meta
                  ? `${meta.type}${meta.key === 'PRI' ? ' · PK' : ''}${!meta.nullable ? ' · NOT NULL' : ' · nullable'}${meta.comment ? ` · ${meta.comment}` : ''}`
                  : '',
              };
            })}
            columns={[
              { title: '列名', dataIndex: 'name', key: 'name', width: 150, ellipsis: { showTitle: true } },
              {
                title: '值', dataIndex: 'value', key: 'value',
                render: (val: string | null) =>
                  val != null ? (
                    <div style={{ wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>{val}</div>
                  ) : (
                    <span style={{ color: '#ccc', fontStyle: 'italic' }}>NULL</span>
                  ),
              },
              {
                title: '字段信息', dataIndex: 'meta', key: 'meta', width: 280,
                render: (m: string) => (
                  <span style={{ fontSize: 12, color: '#666', wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>{m}</span>
                ),
              },
            ]}
            size="small"
            pagination={false}
            bordered
          />
        )}
      </Modal>
      <Modal title={tr('query.editRow')} open={rowEdit.open}
        onCancel={() => setRowEdit({ open: false, data: null, columns: [], tab: null })}
        onOk={async () => {
          const values = await editForm.validateFields();
          setEditSaving(true);
          try {
            // Build SET clause from changed values
            const changedFields = rowEdit.columns
              .filter((col) => String(values[col] ?? '') !== String(rowEdit.data![col] ?? ''));
            if (changedFields.length === 0) { message.info(tr('query.noChange')); setEditSaving(false); return; }
            // For table tabs, we know the table name
            const tab = rowEdit.tab;
            if (tab?.type === 'table') {
              const t = tab as TableTab;
              const ds = (tab as any).dsId || treeDSRef.current;
              const dsInfo = dataSources.find(d => d.id === ds);
              const tabDbType = dsInfo?.type || dbType;
              const gen = getDialect(tabDbType);
              // Build SET values
              const sets: Record<string, any> = {};
              const colTypes: Record<string, string> = {};
              for (const col of changedFields) {
                sets[col] = values[col];
                colTypes[col] = columnMeta[col]?.type || '';
              }
              // Use PK columns for WHERE (if known), otherwise all non-null original values
              const pkKey = `${ds}:${t.schema}:${t.table}`;
              const pkCol = tablePKMap[pkKey];
              let whereCols: string[];
              if (pkCol) {
                whereCols = [pkCol].filter(col => rowEdit.data![col] != null);
                if (whereCols.length === 0) whereCols = rowEdit.columns.filter(col => rowEdit.data![col] != null);
              } else {
                whereCols = rowEdit.columns.filter(col => rowEdit.data![col] != null);
              }
              const where: Record<string, any> = {};
              for (const col of whereCols) {
                where[col] = rowEdit.data![col];
                if (!colTypes[col]) colTypes[col] = columnMeta[col]?.type || '';
              }
              const schema = t.schema || currentSchema || '';
              const sql = gen.updateQuery(schema, t.table, sets, where, colTypes);
              await queryAPI.executeDML({ data_source_id: ds, sql, schema: schema || undefined, database: t.database || currentDatabase || undefined });
              message.success(tr('query.updateSuccess'));
              setRowEdit({ open: false, data: null, columns: [], tab: null });
              if (tab?.type === 'table') loadTableTab(tab.id, (tab as any).page, (tab as any).pageSize);
            } else {
              // For SQL tabs, generate UPDATE and copy to clipboard
              const gen = getDialect(dbType);
              const setParts = changedFields.map((col) => `${gen.quoteIdent(col)} = ${gen.formatValue(String(values[col]), columnMeta[col]?.type)}`).join(', ');
              const whereParts = rowEdit.columns.map((col) => `${gen.quoteIdent(col)} = ${gen.formatValue(String(rowEdit.data![col] ?? ''), columnMeta[col]?.type)}`).join(' AND ');
              const sql = `UPDATE ? SET ${setParts} WHERE ${whereParts};`;
              await navigator.clipboard.writeText(sql);
              message.info('SQL 已复制到剪贴板，请手动执行');
              setRowEdit({ open: false, data: null, columns: [], tab: null });
            }
          } catch (err: any) {
            message.error(err?.response?.data?.message || tr('query.updateFailed'));
          } finally { setEditSaving(false); }
        }}
        confirmLoading={editSaving} okText={tr('query.save')} cancelText={tr('common.cancelText')} width={600}
      >
        {rowEdit.data && (
          <Form form={editForm} layout="vertical">
            {rowEdit.columns.map((col) => {
              const meta = columnMeta[col];
              const label = meta
                ? `${col}  [${meta.type}]${meta.key === 'PRI' ? ' 🔑PK' : ''}${!meta.nullable ? ' *NOT NULL' : ''}${meta.comment ? ` - ${meta.comment}` : ''}`
                : col;
              return (
              <Form.Item key={col} name={col} label={<span style={{ fontSize: 12 }}>{label}</span>}>
                <Input.TextArea autoSize={{ minRows: 1, maxRows: 3 }} />
              </Form.Item>
              );
            })}
          </Form>
        )}
      </Modal>
      {/* Add Row Modal */}
      <Modal title={tr('query.addRow')} open={rowAdd.open}
        onCancel={() => { setRowAdd({ open: false, tab: null, tableName: '', pkColumn: null }); addForm.resetFields(); }}
        onOk={() => addForm.submit()}
        okText={tr('query.add')} width={500}
      >
        <Form form={addForm} layout="vertical" onFinish={handleAddRow}>
          {rowAdd.tab && (rowAdd.tab.result?.columns || []).map((col: string) => {
            const meta = columnMeta[col];
            const label = meta
              ? `${col}  [${meta.type}]${meta.key === 'PRI' ? ' 🔑PK' : ''}${!meta.nullable ? ' *NOT NULL' : ''}${meta.comment ? ` - ${meta.comment}` : ''}`
              : col;
            return (
            <Form.Item key={col} name={col} label={<span style={{ fontSize: 14, fontWeight: 500 }}>{label}</span>}>
              <Input placeholder={col === rowAdd.pkColumn ? '主键 (留空跳过)' : ''} />
            </Form.Item>
            );
          })}
        </Form>
      </Modal>
      </>
    );
  };

  // --- Render tab content ---
  const renderTabContent = (tab: TabItem) => {
    if (tab.type === 'table') {
      const t = tab as TableTab;
      return (
        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          <div style={{ display: 'flex', gap: 8, marginBottom: 8, flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between' }}>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {tab.result && (
              <>
                <Button size="small" icon={<PlusOutlined />}
                  onClick={() => openAddRowModal(tab)}>
                  {tr('query.add')}
                </Button>
                {selectedRows.length > 0 && (
                  <Popconfirm title={tr('query.confirmBatchDelete', { n: selectedRows.length })} onConfirm={async () => {
                    const tt = tab as any;
                    const ds = tt.dsId ;
                    const key = `${ds}:${tt.schema || ''}:${tt.table || ''}`;
                    let pk = tablePKMap[key];
                    if (!pk) {
                      pk = await detectPK(ds, tt.schema || '', tt.table || '');
                      if (pk) setTablePKMap(prev => ({ ...prev, [key]: pk }));
                    }
                    if (!pk) { message.warning(tr('query.noPKBatch')); return; }
                    setRowAdd(prev => ({ ...prev, pkColumn: pk }));
                    handleBatchDelete();
                  }}>
                    <Button size="small" danger icon={<DeleteOutlined />}>{tr('query.batchDelete')}({selectedRows.length})</Button>
                  </Popconfirm>
                )}
              </>
            )}
            </div>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <Space size={4}>
              <span style={{ fontSize: 12, color: '#666' }}>{tr('query.whereLabel')}:</span>
              <Input
                size="small" style={{ width: 300 }} placeholder={tr('query.wherePlaceholder')}
                value={t.where} onChange={(e) => updateTab(tab.id, { where: e.target.value } as any)}
                onPressEnter={() => loadTableTab(tab.id, 1, tab.pageSize)}
              />
            </Space>
            <Space size={4}>
              <span style={{ fontSize: 12, color: '#666' }}>{tr('query.orderByLabel')}:</span>
              <Input
                size="small" style={{ width: 200 }} placeholder={tr('query.sortPlaceholder')}
                value={t.orderBy} onChange={(e) => updateTab(tab.id, { orderBy: e.target.value, columnSort: null } as any)}
                onPressEnter={() => loadTableTab(tab.id, 1, tab.pageSize)}
              />
            </Space>
            <Button size="small" type="primary" icon={<PlayCircleOutlined />}
              loading={tab.loading} onClick={() => loadTableTab(tab.id, 1, tab.pageSize)}>
              {tr('query.queryStr')}
            </Button>
            </div>
          </div>
          <div style={{ flex: 1, overflow: 'auto' }}>
            {renderTableResult(tab, (p, s) => loadTableTab(tab.id, p, s || tab.pageSize))}
          </div>
        </div>
      );
    }
    // Schema list tab (table management for a database/schema)
    if (tab.type === 'schema_list') {
      const s = tab as SchemaListTab;
      return (
        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          <div style={{ display: 'flex', gap: 8, marginBottom: 8, alignItems: 'center' }}>
            <span style={{ fontWeight: 600 }}>{s.title}</span>
            <Button size="small" icon={<ReloadOutlined />} onClick={() => {
              updateTab(tab.id, { loading: true });
              dsAPI.tableList(s.dsId, s.schema, s.database).then((res: any) => {
                updateTab(tab.id, { items: res.data?.data || [], totalItems: (res.data?.data || []).length, loading: false });
              }).catch(() => updateTab(tab.id, { loading: false }));
            }}>刷新</Button>
            <Button size="small" type="primary" icon={<PlusOutlined />}
              onClick={() => setQueryCreateTable({ open: true, schema: s.schema, database: s.database })}>
              新建表
            </Button>
            <Button size="small" icon={<PlusOutlined />}
              onClick={() => setQueryCreateView({ open: true, schema: s.schema, database: s.database })}>
              新建视图
            </Button>
          </div>
          <Spin spinning={s.loading}>
            <Table
              size="small"
              dataSource={s.items}
              rowKey="name"
              pagination={{ pageSize: s.pageSize, total: s.totalItems, size: 'small', showTotal: (t: number) => `${t} 个对象` }}
              columns={[
                { title: '名称', dataIndex: 'name', key: 'name', render: (v: string, r: any) => (
                  <a onClick={() => openTableTab(r.schema || s.schema, v, r.type === 'view', r.database || s.database, s.dsId)}>
                    {r.type === 'view' ? <EyeOutlined style={{ marginRight: 4, color: '#52c41a' }} /> : <TableOutlined style={{ marginRight: 4, color: '#1890ff' }} />}
                    {v}
                  </a>
                )},
                { title: '类型', dataIndex: 'type', key: 'type', width: 80, render: (v: string) => v === 'view' ? <Tag>视图</Tag> : <Tag color="blue">表</Tag> },
                { title: '操作', key: 'action', width: 180, render: (_: any, r: any) => (
                  <Space size={4}>
                    <Button type="link" size="small" onClick={() => openTableTab(r.schema || s.schema, r.name, r.type === 'view', r.database || s.database, s.dsId)}>数据</Button>
                    <Button type="link" size="small" onClick={() => {
                      setDrawerTarget({ schema: r.schema || s.schema, table: r.name, isView: r.type === 'view', database: r.database || s.database, dsId: s.dsId });
                      setDrawerOpen(true);
                    }}>结构</Button>
                    <Button type="link" size="small" danger onClick={() => {
                      Modal.confirm({
                        title: `确认删除 ${r.type === 'view' ? '视图' : '表'} ${r.name}?`,
                        content: '此操作不可撤销',
                        okType: 'danger',
                        onOk: async () => {
                          try {
                            const dropSQL = r.type === 'view' ? `DROP VIEW ${r.name}` : `DROP TABLE ${r.name}`;
                            await queryAPI.executeDDL({ data_source_id: s.dsId, sql: dropSQL, schema: r.schema || s.schema, database: r.database || s.database });
                            message.success('已删除');
                            updateTab(tab.id, { loading: true });
                            dsAPI.tableList(s.dsId, s.schema, s.database).then((res: any) => {
                              updateTab(tab.id, { items: res.data?.data || [], totalItems: (res.data?.data || []).length, loading: false });
                            });
                          } catch { message.error('删除失败'); }
                        }
                      });
                    }}>删除</Button>
                  </Space>
                )},
              ]}
            />
          </Spin>
        </div>
      );
    }
    // SQL tab
    const t = tab as SqlTab;
    return (
      <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        <div style={{ display: 'flex', gap: 8, marginBottom: 8, alignItems: 'center' }}>
          {/* Data source + Database/Schema — per tab, independent */}
          <Select
            size="small"
            style={{ minWidth: 180 }}
            placeholder={tr('query.selectDS')}
            value={t.dsId  || undefined}
            onChange={(v) => {
              updateTab(tab.id, { dsId: v, database: undefined, schema: undefined } as any);
              // Clear this tab's cache
              setTabDbListCache(prev => { const n = {...prev}; delete n[tab.id]; return n; });
              setTabSchemaListCache(prev => { const n = {...prev}; delete n[tab.id]; return n; });
            }}
            options={dataSources.map((ds) => ({ label: `${ds.name} (${ds.type})`, value: ds.id }))}
          />
          {/* Database / Schema selectors — per tab type */}
          {(() => {
            const tabDs = dataSources.find(ds => ds.id === (t.dsId ));
            const tabDbType = tabDs?.type || dbType;
            if (tabDbType === 'postgres' || tabDbType === 'sqlserver') {
            return (<>
              <Select
                size="small"
                style={{ minWidth: 120 }}
                placeholder="Database"
                value={t.database || undefined}
                allowClear
                onDropdownVisibleChange={(open) => { if (open && !tabDbListCache[tab.id]) fetchDatabasesForTab(tab.id, t.dsId || ''); }}
                onChange={(db) => {
                  updateTab(tab.id, { database: db, schema: undefined } as any);
                  if (db) fetchSchemasForTab(tab.id, db, t.dsId || '');
                }}
                options={(tabDbListCache[tab.id] || []).map((d) => ({ label: d, value: d }))}
              />
              <Select
                size="small"
                style={{ minWidth: 120 }}
                placeholder="Schema"
                value={t.schema || undefined}
                allowClear
                disabled={!t.database}
                onDropdownVisibleChange={(open) => { if (open && t.database) fetchSchemasForTab(tab.id, t.database, t.dsId || ''); }}
                onChange={(sch) => updateTab(tab.id, { schema: sch } as any)}
                options={(tabSchemaListCache[tab.id] || []).map((s) => ({ label: s, value: s }))}
              />
            </>);
            }
            return (
            <Select
              size="small"
              style={{ minWidth: 140 }}
              placeholder={tabDbType === 'oracle' ? 'User' : 'Database'}
              value={t.schema || t.database || undefined}
              allowClear
              onDropdownVisibleChange={(open) => { if (open && !tabSchemaListCache[tab.id]) fetchDatabasesForTab(tab.id, t.dsId || ''); }}
              onChange={(val) => updateTab(tab.id, { schema: val, database: undefined } as any)}
              options={(tabSchemaListCache[tab.id] || []).map((s) => ({ label: s, value: s }))}
            />
            );
          })()}
          <Button size="small" type="primary" icon={<PlayCircleOutlined />}
            loading={tab.loading} onClick={() => loadSQLTab(tab.id)}>
            {tr('query.run')}
          </Button>
          <Button size="small" icon={<StopOutlined />} disabled={!tab.loading}>{tr('query.stop')}</Button>
          {tab.result && tab.result.total_rows > 0 && (
            <Button size="small" icon={<ExportOutlined />}
              onClick={() => openExportModal({ sql: (tab as SqlTab).sql })}>
              {tr('query.exportResult')}
            </Button>
          )}
        </div>
        <div style={{ resize: 'vertical', overflow: 'auto', height: 120, minHeight: 60, maxHeight: 400, border: '1px solid #d9d9d9', borderRadius: 4, marginBottom: 8 }}>
          <Editor
            height="100%" defaultLanguage="sql" value={t.sql}
            loading={<div style={{ padding: 12, color: '#999' }}>{tr('query.editorLoading')}</div>}
            onMount={(editor) => { editorRef.current = editor; }}
            onChange={(v) => {
              updateTab(tab.id, { sql: v || '' } as any);
            }}
            options={{ minimap: { enabled: false }, fontSize: 14, wordWrap: 'on', scrollBeyondLastLine: false }}
          />
        </div>
        <div style={{ flex: 1, overflow: 'auto' }}>
          {renderTableResult(tab, (p, s) => {
            const t = tab as SqlTab;
            executeQuery(t.sql, t.schema || undefined, p, s || t.pageSize, t.database || undefined, t.dsId).then((result) => {
              if (result) updateTab(tab.id, { result, page: p, pageSize: s || t.pageSize, loading: false, totalRows: result.total_rows } as any);
            });
          })}
        </div>
      </div>
    );
  };

  const handleDeleteTableOrView = async () => {
    const { schema, name, isView } = deleteTarget;
    const treeType = dataSources.find(d => d.id === treeDSRef.current)?.type || 'mysql';
    const gen = getDialect(treeType);
    let sql = null;
    if(isView) {
      sql = gen.dropView(schema, name, false);
    } else {
      sql = gen.dropTable(schema, name, false);
    }
    try {
      await queryAPI.executeDDL({ data_source_id: treeDSRef.current || '', sql, schema: schema || undefined, database: deleteTarget.database || currentDatabase || undefined });
      message.success(`已删除${isView ? '视图' : '表'}: ${name}`);
      setDeleteTarget({ open: false, schema: '', name: '', isView: false });
      setDeleteConfirmName('');
      // Refresh tree
      if (treeDSRef.current && currentSchema) {
        await dsAPI.schemaObjects(treeDSRef.current, currentSchema);
      }
    } catch (err: any) {
      message.error(err?.response?.data?.message || tr('query.deleteFailed'));
    }
  };

  const openExportModal = (context: { sql?: string; table?: string; schema?: string; database?: string }) => {
    setExportContext(context);
    exportForm.resetFields();
    exportForm.setFieldsValue({
      export_format: 'sql',
      export_batch_size: 500,
      export_content: 'all',
    });
    setExportModalOpen(true);
  };

  const handleExportSubmit = async () => {
    message.info('导出任务功能为专业版功能');
    setExportModalOpen(false);
  };

  return (
    <div onKeyDown={handleKeyDown}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
        <h2 style={{ fontSize: 18, margin: 0 }}>{tr('query.title')}</h2>
      </div>
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
        {/* Left: Schema tree */}
        <div style={{ width: treeWidth, flexShrink: 0, display: 'flex', flexDirection: 'column',
          height: 'calc(100vh - 140px)', minHeight: 'calc(100vh - 140px)' }}>
          <Select style={{ width: '100%', marginBottom: 8 }} size="small" placeholder={tr('query.selectDS')}
            value={treeDS || undefined} onChange={(v) => { setTreeDS(v || ''); treeDSRef.current = v || ''; }}
            options={dataSources.map((ds) => ({ label: `${ds.name} (${ds.host}:${ds.port})`, value: ds.id }))} />
          <Card size="small" style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
            styles={{ body: { flex: 1, overflow: 'auto', padding: 4, minHeight: 'calc(100vh - 220px)' } }}>
            <SchemaTree
              dataSourceId={treeDS}
              selectedKey={undefined}
              refreshTrigger={treeRefreshKey}
              onSelect={(_key, ctx) => {
                if (ctx.database) setTreeDatabase(ctx.database);
                if (ctx.schema) setTreeSchema(ctx.schema);
                if (ctx.table) {
                  openTableTab(ctx.schema || '', ctx.table, ctx.isView || false, ctx.database, treeDSRef.current);
                } else if (ctx.schema) {
                  // PG/MSSQL: schema node → open table list
                  openSchemaListTab(ctx.database || '', ctx.schema, treeDSRef.current);
                } else if (ctx.user) {
                  // Oracle: user node → open table list (user = schema)
                  openSchemaListTab('', ctx.user, treeDSRef.current);
                } else if (ctx.database) {
                  // MySQL: database node (database = schema) → open table list
                  const isTwoLevel = treeDbType === 'postgres' || treeDbType === 'postgresql' || treeDbType === 'sqlserver';
                  if (!isTwoLevel) {
                    openSchemaListTab('', ctx.database, treeDSRef.current);
                  }
                }
              }}
              onTableAction={(action, schema, table, isView, database) => {
                if (action === 'structure') {
                  if (isView) {
                    openViewDef(schema, table, database);
                  } else {
                    setDrawerTarget({ schema, table, isView, database });
                    setDrawerOpen(true);
                  }
                } else if (action === 'copy-ddl') {
                  handleCopyDDL(schema, table);
                } else if (action === 'export') {
                  setActiveTable({ schema, table, isView });
                  openExportModal({ table, schema, database });
                } else if (action === 'delete') {
                  setDeleteTarget({ schema, name: table, isView, open: true, database });
                }
              }}
              onSchemaAction={(action, schema, database) => {
                if (action === 'create-table') {
                  setQueryCreateTable({ open: true, schema, database });
                } else if (action === 'create-view') {
                  setQueryCreateView({ open: true, schema, database });
                } else if (action === 'create-schema') {
                  setSchemaForm({ open: true, mode: 'create', database });
                } else if (action === 'edit-schema') {
                  setSchemaForm({ open: true, mode: 'edit', database, initValues: { name: schema, charset: '', collation: '' } });
                }
              }}
            />
          </Card>
        </div>
        {/* Drag handle */}
        <div onMouseDown={onDragStart} style={{ width: 4, cursor: 'col-resize', background: '#f0f0f0', borderRadius: 2, flexShrink: 0, alignSelf: 'stretch',
          transition: 'background 0.15s' }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLDivElement).style.background = '#bfbfbf'; }}
          onMouseLeave={(e) => { if (!draggingRef.current) (e.currentTarget as HTMLDivElement).style.background = '#f0f0f0'; }} />
        {/* Right: Tabs */}
        <div ref={rightPanelRef} style={{ flex: 1, minWidth: 0, minHeight: 'calc(100vh - 140px)' }}>
          {tabs.length === 0 ? (
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', gap: 16 }}>
              <Empty description={tr('query.clickToOpen')} />
              <Space>
                <Button type="primary" icon={<PlusOutlined />} onClick={addSQLTab}>{tr('query.newSQLQuery')}</Button>
              </Space>
            </div>
          ) : (
            <Tabs
              type="editable-card"
              activeKey={activeTabId}
              onChange={setActiveTabId}
              onEdit={(targetKey, action) => {
                if (action === 'add') addSQLTab();
                else if (action === 'remove') closeTab(targetKey as string);
              }}
              addIcon={<Tooltip title={tr('query.newSQLQuery')}><PlusOutlined /></Tooltip>}
              hideAdd={false}
              items={tabs.map((tab) => {
                // Build tooltip showing data source > database > schema
                let tooltipText = tab.title;
                const tabDsId = (tab as any).dsId;
                const tabDb = (tab as any).database;
                const tabSch = (tab as any).schema;
                if (tabDsId) {
                  const dsInfo = dataSources.find(d => d.id === tabDsId);
                  if (dsInfo) {
                    tooltipText = dsInfo.name;
                    if (tabDb) tooltipText += ` > ${tabDb}`;
                    if (tabSch) tooltipText += ` > ${tabSch}`;
                  }
                }
                return { key: tab.id,
                label: (
                  <Tooltip title={tooltipText}>
                  <span>
                    {tab.type === 'table' ? <TableOutlined style={{ marginRight: 4 }} /> : tab.type === 'schema_list' ? <FolderOutlined style={{ marginRight: 4 }} /> : <CodeOutlined style={{ marginRight: 4 }} />}
                    {tab.title}
                    {tab.loading && <Spin size="small" style={{ marginLeft: 4 }} />}
                  </span>
                  </Tooltip>
                ),
                closable: tab.closable,
                children: <div style={{ height: 'calc(100vh - 200px)', overflow: 'auto' }}>
                  {renderTabContent(tab)}
                </div>,
              };
            })}
            />
          )}
        </div>
      </div>

      {/* Context menu */}
      <Dropdown open={!!contextMenu} onOpenChange={(open) => { if (!open) setContextMenu(null); }} trigger={['click']}
        menu={{
          items: (() => {
            if (!contextMenu) return [];
            const parsed = parseNodeKey(contextMenu.nodeKey);
            if (parsed.kind === 'table') return [
              { key: 'select', label: tr('query.openDataTab') },
              { key: 'structure', label: tr('query.viewStructure') },
            ];
            if (parsed.kind === 'view') return [
              { key: 'select', label: tr('query.openDataTab') },
              { key: 'view-def', label: tr('query.viewDefinition') },
            ];
            return [];
          })(),
          onClick: handleContextMenuClick,
        }}>
        <div style={{ position: 'fixed', left: contextMenu?.x ?? -9999, top: contextMenu?.y ?? -9999, width: 1, height: 1, pointerEvents: 'none' }} />
      </Dropdown>

      {/* Table structure drawer */}
      <TableStructureDrawer open={drawerOpen} dataSourceId={drawerTarget?.dsId || treeDSRef.current} dataSourceName={dataSources.find(d => d.id === (drawerTarget?.dsId || treeDSRef.current))?.name || ''}
        dbType={dataSources.find(d => d.id === (drawerTarget?.dsId || treeDSRef.current))?.type || 'mysql'} schema={drawerTarget?.schema || ''} table={drawerTarget?.table || ''}
        isView={drawerTarget?.isView} readOnly={readOnly} database={drawerTarget?.database}
        onClose={() => setDrawerOpen(false)} onRefreshTree={refreshTree} />

      {/* View definition modal */}
      <Modal title={`视图定义 · ${viewDef?.name || ''}`} open={viewDefOpen} onCancel={() => setViewDefOpen(false)} footer={null} width={800}>
        <Spin spinning={viewDefLoading}>
          <div style={{ border: '1px solid #d9d9d9', borderRadius: 4 }}>
            <Editor height={400} defaultLanguage="sql" value={viewDef?.definition || ''}
              options={{ minimap: { enabled: false }, fontSize: 13, wordWrap: 'on', readOnly: true, scrollBeyondLastLine: false }} />
          </div>
        </Spin>
      </Modal>

      {/* Query Result Export Modal */}
      <Modal
        title={tr('query.exportTitle')}
        open={exportModalOpen}
        onOk={handleExportSubmit}
        onCancel={() => setExportModalOpen(false)}
        okText={tr('query.createAndRun')}
        cancelText={tr('common.cancelText')}
        width={450}
      >
        <Form form={exportForm} layout="vertical">
          <Form.Item label="导出内容" name="export_content" initialValue="all">
            <Radio.Group>
              <Radio value="all">结构 + 数据</Radio>
              <Radio value="structure">仅结构</Radio>
              <Radio value="data">仅数据</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item label={tr('query.exportFormat')} name="export_format">
            <Radio.Group>
              <Radio value="sql">SQL</Radio>
              <Radio value="csv">CSV</Radio>
              <Radio value="json">JSON</Radio>
              <Radio value="markdown">Markdown</Radio>
              <Radio value="xml">XML</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item label={tr('query.batchSize')} name="export_batch_size" initialValue={500}>
            <Select
              options={[
                { label: '500', value: 500 },
                { label: '100', value: 100 },
                { label: '1000', value: 1000 },
                { label: '5000', value: 5000 },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>

      {/* Create Table Modal (from query page) */}
      <CreateTableModal
        open={queryCreateTable.open}
        dataSourceId={treeDSRef.current}
        schema={queryCreateTable.schema}
        dbType={treeDbType}
        database={queryCreateTable.database}
        onClose={() => setQueryCreateTable({ open: false, schema: '' })}
        onSuccess={() => { setQueryCreateTable({ open: false, schema: '' }); }}
      />

      {/* Create View Modal (from query page) */}
      <CreateViewModal
        open={queryCreateView.open}
        dataSourceId={treeDSRef.current}
        schema={queryCreateView.schema}
        dbType={treeDbType}
        database={queryCreateView.database}
        onClose={() => setQueryCreateView({ open: false, schema: '' })}
        onSuccess={() => { setQueryCreateView({ open: false, schema: '' }); }}
      />

      <SchemaFormModal
        open={schemaForm.open}
        mode={schemaForm.mode}
        dataSourceId={treeDSRef.current}
        dbType={treeDbType}
        level="schema"
        database={schemaForm.database}
        initValues={schemaForm.initValues}
        onClose={() => setSchemaForm({ open: false, mode: 'create' })}
        onSuccess={() => { setSchemaForm({ open: false, mode: 'create' }); setTreeRefreshKey(k => k + 1); }}
      />

      {/* Delete Table/View Confirmation Modal */}
      <Modal
        title={`删除${deleteTarget.isView ? '视图' : '表'}`}
        open={deleteTarget.open}
        onCancel={() => { setDeleteTarget({ open: false, schema: '', name: '', isView: false }); setDeleteConfirmName(''); }}
        onOk={handleDeleteTableOrView}
        okText="确认删除"
        okButtonProps={{ danger: true, disabled: deleteConfirmName !== deleteTarget.name }}
      >
        <div style={{ marginBottom: 12 }}>
          <p style={{ color: '#e74c3c', fontWeight: 500 }}>
            此操作将永久删除 {deleteTarget.isView ? '视图' : '表'} <strong>{deleteTarget.schema}.{deleteTarget.name}</strong> 及其所有数据，不可恢复。
          </p>
          <p>请输入 {deleteTarget.isView ? '视图' : '表'} 名称以确认：</p>
          <Input
            placeholder={deleteTarget.name}
            value={deleteConfirmName}
            onChange={(e) => setDeleteConfirmName(e.target.value)}
          />
        </div>
      </Modal>
    </div>
  );
};

export default SQLEditor;
