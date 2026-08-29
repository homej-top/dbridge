import React, { useState, useCallback, useMemo, useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Row,
  Col,
  Select,
  Button,
  Card,
  Space,
  Tag,
  message,
  Table,
  Tabs,
  List,
  Spin,
  Empty,
  Typography,
  Modal,
  Checkbox,
  Radio,
  Transfer,
  Alert,
  Input,
  Tooltip,
} from 'antd';
import {
  DiffOutlined,
  TableOutlined,
  EyeOutlined,
  SyncOutlined,
  DatabaseOutlined,
  SearchOutlined,
  CopyOutlined,
  SwapOutlined,
  DeleteOutlined,
} from '@ant-design/icons';
import HalfCircleIcon from '../components/HalfCircleIcon';
import { compareAPI, dsAPI, tableAPI, viewAPI, queryAPI } from '../api';
import { getDialect } from '../utils/dialect';
import type { DataSource, CompareObject, TableDataResult, TableStructureResult, ColumnDetail, SyncStructureResult, DataSyncResult } from '../types';

type CompareHistoryItem = {
  sourceDS: string;
  sourceDSName: string;
  sourceSchema: string;
  sourceDatabase: string;
  targetDS: string;
  targetDSName: string;
  targetSchema: string;
  targetDatabase: string;
  timestamp: number;
};

const { Text } = Typography;

const Compare: React.FC = () => {
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  const { t: tr } = useTranslation();
  const [sourceDS, setSourceDS] = useState<string>(() => localStorage.getItem('compare_sourceDS') || '');
  const [targetDS, setTargetDS] = useState<string>(() => localStorage.getItem('compare_targetDS') || '');
  const [sourceDBType, setSourceDBType] = useState<string>('');
  const [targetDBType, setTargetDBType] = useState<string>('');
  const [sourceSchemas, setSourceSchemas] = useState<string[]>([]);
  const [targetSchemas, setTargetSchemas] = useState<string[]>([]);
  const [sourceSchema, setSourceSchema] = useState<string>(() => localStorage.getItem('compare_sourceSchema') || '');
  const [targetSchema, setTargetSchema] = useState<string>(() => localStorage.getItem('compare_targetSchema') || '');
  const [sourceDatabase, setSourceDatabase] = useState<string>(() => localStorage.getItem('compare_sourceDatabase') || '');
  const [targetDatabase, setTargetDatabase] = useState<string>(() => localStorage.getItem('compare_targetDatabase') || '');
  const [sourceDatabases, setSourceDatabases] = useState<string[]>([]);
  const [targetDatabases, setTargetDatabases] = useState<string[]>([]);
  const isTwoLevel = (dbType: string) => dbType === 'postgres' || dbType === 'postgresql' || dbType === 'sqlserver';

  const [sourceMeta, setSourceMeta] = useState<any>(null); void sourceMeta; void setSourceMeta;
  const [targetMeta, setTargetMeta] = useState<any>(null); void targetMeta; void setTargetMeta;
  const schemaLabel = (dbType: string) => {
    if (dbType === 'mysql' || dbType === 'mariadb' || dbType === 'oceanbase') return 'Database';
    if (dbType === 'oracle') return 'User';
    if (dbType === 'postgres' || dbType === 'postgresql' || dbType === 'sqlserver') return 'Database / Schema';
    return 'Schema';
  };
  const [loading, setLoading] = useState(false);
  const [objects, setObjects] = useState<CompareObject[]>([]);
  const [selectedObj, setSelectedObj] = useState<string>('');
  const [dataLoading, setDataLoading] = useState(false);
  const [structLoading, setStructLoading] = useState(false);
  const [sourceData, setSourceData] = useState<TableDataResult | null>(null);
  const [targetData, setTargetData] = useState<TableDataResult | null>(null);
  const [sourceStruct, setSourceStruct] = useState<TableStructureResult | null>(null);
  const [targetStruct, setTargetStruct] = useState<TableStructureResult | null>(null);
  const [sourcePage, setSourcePage] = useState(1);
  const [targetPage, setTargetPage] = useState(1);
  const [sourcePageSize, setSourcePageSize] = useState(10);
  const [targetPageSize, setTargetPageSize] = useState(10);

  const [structSyncModal, setStructSyncModal] = useState(false);
  const [structSyncResult, setStructSyncResult] = useState<SyncStructureResult | null>(null);
  const [structSyncLoading, setStructSyncLoading] = useState(false);
  const [structSyncPhase, setStructSyncPhase] = useState<'preview' | 'executed'>('preview');
  const [editableDDL, setEditableDDL] = useState('');

  const [dataSyncModal, setDataSyncModal] = useState(false);
  const [dataSyncLoading, setDataSyncLoading] = useState(false);
  const [dataSyncResult, setDataSyncResult] = useState<DataSyncResult | null>(null);

  const [syncTruncate, setSyncTruncate] = useState(false);
  const [syncID, setSyncID] = useState(true);
  const [syncTransactional, setSyncTransactional] = useState(false);
  const [syncMode, setSyncMode] = useState<'full' | 'selected' | 'diff'>('full');
  const [checkFields, setCheckFields] = useState<string[]>([]);
  const [syncColumns, setSyncColumns] = useState<string[]>([]);

  // Comparison history
  const [compareHistory, setCompareHistory] = useState<CompareHistoryItem[]>(() => {
    try { return JSON.parse(localStorage.getItem('compare_history') || '[]'); } catch { return []; }
  });

  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [selectedRows, setSelectedRows] = useState<Record<string, any>[]>([]);
  const [objectFilter, setObjectFilter] = useState('');
  const [leftListHeight, setLeftListHeight] = useState<number>(0);
  const rightColRef = useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const fetchDS = async () => {
      try {
        const res = await dsAPI.list('data_query');
        setDataSources(res.data.data?.list || []);
      } catch {
        // handled
      }
    };
    fetchDS();
  }, []);

  useEffect(() => { localStorage.setItem('compare_sourceDS', sourceDS); }, [sourceDS]);
  useEffect(() => { localStorage.setItem('compare_targetDS', targetDS); }, [targetDS]);
  useEffect(() => { localStorage.setItem('compare_sourceSchema', sourceSchema); }, [sourceSchema]);
  useEffect(() => { localStorage.setItem('compare_targetSchema', targetSchema); }, [targetSchema]);
  useEffect(() => { localStorage.setItem('compare_sourceDatabase', sourceDatabase); }, [sourceDatabase]);
  useEffect(() => { localStorage.setItem('compare_targetDatabase', targetDatabase); }, [targetDatabase]);

  useEffect(() => {
    const el = rightColRef.current;
    if (!el || objects.length === 0) return;
    const compute = () => {
      const row = el.closest('.ant-row');
      const rowTop = row ? row.getBoundingClientRect().top : 120;
      const minH = window.innerHeight - rowTop;
      const rightH = el.offsetHeight;
      setLeftListHeight(Math.max(minH, rightH));
    };
    const ro = new ResizeObserver(compute);
    ro.observe(el);
    compute();
    window.addEventListener('resize', compute);
    return () => { ro.disconnect(); window.removeEventListener('resize', compute); };
  }, [objects.length, selectedObj]);

  const restoredRef = useRef(false);
  useEffect(() => {
    if (dataSources.length === 0 || restoredRef.current) return;
    restoredRef.current = true;
    const restore = async () => {
      if (sourceDS && dataSources.some(ds => ds.id === sourceDS)) {
        const dbType = dataSources.find(ds => ds.id === sourceDS)?.type || '';
        setSourceDBType(dbType);
        if (isTwoLevel(dbType)) {
          const dbs = await fetchDatabases(sourceDS);
          setSourceDatabases(dbs);
          const srcDB = sourceDatabase || (dataSources.find(ds => ds.id === sourceDS)?.database) || dbs[0] || '';
          if (srcDB && !sourceDatabase) setSourceDatabase(srcDB);
          if (srcDB) {
            const schemas = await fetchSchemas(sourceDS, srcDB);
            setSourceSchemas(schemas);
          }
        } else {
          const schemas = await fetchSchemas(sourceDS);
          setSourceSchemas(schemas);
        }
      }
      if (targetDS && dataSources.some(ds => ds.id === targetDS)) {
        const dbType = dataSources.find(ds => ds.id === targetDS)?.type || '';
        setTargetDBType(dbType);
        if (isTwoLevel(dbType)) {
          const dbs = await fetchDatabases(targetDS);
          setTargetDatabases(dbs);
          const tgtDB = targetDatabase || (dataSources.find(ds => ds.id === targetDS)?.database) || dbs[0] || '';
          if (tgtDB && !targetDatabase) setTargetDatabase(tgtDB);
          if (tgtDB) {
            const schemas = await fetchSchemas(targetDS, tgtDB);
            setTargetSchemas(schemas);
          }
        } else {
          const schemas = await fetchSchemas(targetDS);
          setTargetSchemas(schemas);
        }
      }
    };
    restore();
  }, [dataSources]);

  const fetchDatabases = async (dsId: string): Promise<string[]> => {
    if (!dsId) return [];
    try {
      const res = await dsAPI.databases(dsId);
      return res.data.data || [];
    } catch { return []; }
  };

  const fetchSchemas = async (dsId: string, database?: string): Promise<string[]> => {
    if (!dsId) return [];
    try {
      if (database) {
        const res = await dsAPI.databaseSchemas(dsId, database);
        return res.data.data || [];
      }
      const res = await dsAPI.schemaNames(dsId);
      return res.data.data || [];
    } catch {
      return [];
    }
  };

  const handleSourceDSChange = async (value: string) => {
    setSourceDS(value);
    const info = dataSources.find((ds) => ds.id === value);
    const dbType = info?.type || '';
    setSourceDBType(dbType);
    setSourceSchema('');
    setSourceDatabase('');
    setSourceSchemas([]);
    setSourceDatabases([]);
    if (!value) return;
    if (isTwoLevel(dbType)) {
      const dbs = await fetchDatabases(value);
      setSourceDatabases(dbs);
      if (dbs.length > 0) {
        const db = info?.database || dbs[0];
        setSourceDatabase(db);
        const schemas = await fetchSchemas(value, db);
        setSourceSchemas(schemas);
        if (schemas.length > 0) setSourceSchema(schemas[0]);
      }
    } else {
      const schemas = await fetchSchemas(value);
      setSourceSchemas(schemas);
      if (schemas.length > 0) setSourceSchema(schemas[0]);
    }
  };

  const handleTargetDSChange = async (value: string) => {
    setTargetDS(value);
    const info = dataSources.find((ds) => ds.id === value);
    const dbType = info?.type || '';
    setTargetDBType(dbType);
    setTargetSchema('');
    setTargetDatabase('');
    setTargetSchemas([]);
    setTargetDatabases([]);
    if (!value) return;
    if (isTwoLevel(dbType)) {
      const dbs = await fetchDatabases(value);
      setTargetDatabases(dbs);
      if (dbs.length > 0) {
        const db = info?.database || dbs[0];
        setTargetDatabase(db);
        const schemas = await fetchSchemas(value, db);
        setTargetSchemas(schemas);
        if (schemas.length > 0) setTargetSchema(schemas[0]);
      }
    } else {
      const schemas = await fetchSchemas(value);
      setTargetSchemas(schemas);
      if (schemas.length > 0) setTargetSchema(schemas[0]);
    }
  };

  const handleSourceDatabaseChange = async (value: string) => {
    setSourceDatabase(value);
    setSourceSchema('');
    setSourceSchemas([]);
    if (value) {
      const schemas = await fetchSchemas(sourceDS, value);
      setSourceSchemas(schemas);
      if (schemas.length > 0) setSourceSchema(schemas[0]);
    }
  };

  const handleTargetDatabaseChange = async (value: string) => {
    setTargetDatabase(value);
    setTargetSchema('');
    setTargetSchemas([]);
    if (value) {
      const schemas = await fetchSchemas(targetDS, value);
      setTargetSchemas(schemas);
      if (schemas.length > 0) setTargetSchema(schemas[0]);
    }
  };

  const handleCompare = async () => {
    if (!sourceDS || !targetDS) {
      message.warning(tr('compare.selectBothDS'));
      return;
    }
    setLoading(true);
    setSelectedObj('');
    setObjects([]);
    try {
      const res = await compareAPI.structure({
        source_ds: sourceDS,
        source_schema: sourceSchema,
        target_ds: targetDS,
        target_schema: targetSchema,
        source_database: sourceDatabase || undefined,
        target_database: targetDatabase || undefined,
      });
      const objs = (res.data.data?.objects || []).sort((a: any, b: any) => a.name.localeCompare(b.name));
      setObjects(objs);

      // Save to history
      const srcName = dataSources.find(d => d.id === sourceDS)?.name || sourceDS;
      const tgtName = dataSources.find(d => d.id === targetDS)?.name || targetDS;
      const newItem: CompareHistoryItem = {
        sourceDS, sourceDSName: srcName, sourceSchema, sourceDatabase,
        targetDS, targetDSName: tgtName, targetSchema, targetDatabase,
        timestamp: Date.now(),
      };
      setCompareHistory(prev => {
        const filtered = prev.filter(h =>
          !(h.sourceDS === sourceDS && h.sourceSchema === sourceSchema && h.sourceDatabase === sourceDatabase &&
            h.targetDS === targetDS && h.targetSchema === targetSchema && h.targetDatabase === targetDatabase)
        );
        const updated = [newItem, ...filtered].slice(0, 5);
        localStorage.setItem('compare_history', JSON.stringify(updated));
        return updated;
      });
      message.success(tr('compare.compareDone', { count: objs.length }));
    } catch {
      // handled
    } finally {
      setLoading(false);
    }
  };

  const loadTableData = useCallback(
    async (
      dsId: string,
      schema: string,
      table: string,
      page: number,
      pageSize: number,
      side: 'source' | 'target'
    ) => {
      setDataLoading(true);
      try {
        const dbType = dataSources.find(d => d.id === dsId)?.type || (side === 'source' ? sourceDBType : targetDBType) || 'mysql';
        const gen = getDialect(dbType);
        const db = side === 'source' ? sourceDatabase : targetDatabase;
        const res = await queryAPI.execute({
          data_source_id: dsId,
          sql: gen.selectQuery(schema, table),
          schema,
          database: db || undefined,
          page,
          page_size: pageSize,
        });
        if (side === 'source') {
          setSourceData(res.data.data);
        } else {
          setTargetData(res.data.data);
        }
      } catch {
        if (side === 'source') setSourceData(null);
        else setTargetData(null);
      } finally {
        setDataLoading(false);
      }
    },
    [sourceDBType, targetDBType, sourceDatabase, targetDatabase]
  );

  const loadTableStructure = useCallback(
    async (dsId: string, schema: string, table: string, side: 'source' | 'target', isView?: boolean) => {
      setStructLoading(true);
      try {
        const db = side === 'source' ? sourceDatabase : targetDatabase;
        const body = { data_source_id: dsId, schema, database: db || undefined };
        const res = isView
          ? await viewAPI.structure({ ...body, view: table })
          : await tableAPI.structure({ ...body, table });
        if (side === 'source') {
          setSourceStruct(res.data.data);
        } else {
          setTargetStruct(res.data.data);
        }
      } catch {
        if (side === 'source') setSourceStruct(null);
        else setTargetStruct(null);
      } finally {
        setStructLoading(false);
      }
    },
    [sourceDatabase, targetDatabase]
  );

  const handleSelectObject = (objName: string) => {
    setSelectedObj(objName);
    setSourcePage(1);
    setTargetPage(1);
    setSourcePageSize(10);
    setTargetPageSize(10);
    setSourceData(null);
    setTargetData(null);
    setSourceStruct(null);
    setTargetStruct(null);
    setSelectedRowKeys([]);
    setSelectedRows([]);

    const obj = objects.find((o) => o.name === objName);
    if (!obj) return;

    if (obj.status !== 'target_only') {
      loadTableData(sourceDS, sourceSchema, objName, 1, 10, 'source');
      loadTableStructure(sourceDS, sourceSchema, objName, 'source', obj.type === 'view');
    }
    if (obj.status !== 'source_only') {
      loadTableData(targetDS, targetSchema, objName, 1, 10, 'target');
      loadTableStructure(targetDS, targetSchema, objName, 'target', obj.type === 'view');
    }
  };

  const handleSourcePageChange = (page: number, pageSize: number) => {
    setSourcePage(page);
    setSourcePageSize(pageSize);
    loadTableData(sourceDS, sourceSchema, selectedObj, page, pageSize, 'source');
  };

  const handleTargetPageChange = (page: number, pageSize: number) => {
    setTargetPage(page);
    setTargetPageSize(pageSize);
    loadTableData(targetDS, targetSchema, selectedObj, page, pageSize, 'target');
  };

  const statusIcon = (status: string) => {
    if (status === 'both') return <HalfCircleIcon status="both" size={14} />;
    if (status === 'source_only') return <HalfCircleIcon status="source_only" size={14} />;
    return <HalfCircleIcon status="target_only" size={14} />;
  };

  const statusBg = (status: string) => {
    if (status === 'both') return 'transparent';
    if (status === 'source_only') return '#fffbe6';
    return '#e6f7ff';
  };

  const typeIcon = (type: string) =>
    type === 'view' ? <EyeOutlined /> : <TableOutlined />;

  const buildDataColumns = (data: TableDataResult | null, withSelection?: boolean) => {
    if (!data) return [];
    const cols = data.columns.map((col) => ({
      title: col,
      dataIndex: col,
      key: col,
      width: 160,
      ellipsis: { showTitle: false },
      render: (val: any) => (
        <Tooltip title={val != null ? String(val) : ''} placement="topLeft">
          <div style={{ maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {val != null ? String(val) : ''}
          </div>
        </Tooltip>
      ),
    }));
    if (withSelection) {
      cols.unshift({
        title: '',
        dataIndex: '__selection__',
        key: '__selection__',
        width: 50,
        ...({} as any),
      });
    }
    return cols;
  };

  const buildDataRows = (data: TableDataResult | null) => {
    if (!data) return [];
    return data.rows.map((row, i) => {
      const obj: Record<string, any> = { key: i };
      data.columns.forEach((col, j) => {
        obj[col] = row[j];
      });
      return obj;
    });
  };

  interface AlignedRow {
    key: string;
    status: 'both' | 'source_only' | 'target_only';
    hasDiff: boolean;
    s_name: string; s_type: string; s_length: string; s_nullable: string; s_default: string; s_comment: string; s_key: string;
    t_name: string; t_type: string; t_length: string; t_nullable: string; t_default: string; t_comment: string; t_key: string;
    diff_type: boolean; diff_length: boolean; diff_nullable: boolean; diff_default: boolean; diff_comment: boolean; diff_key: boolean;
  }

  const emptyCol = (): ColumnDetail => ({ name: '', type: '', length: '', nullable: '', default: '', comment: '', key: '' });

  const alignedStructRows = useMemo((): AlignedRow[] => {
    if (!sourceStruct && !targetStruct) return [];
    const srcCols = sourceStruct?.columns || [];
    const tgtCols = targetStruct?.columns || [];
    const tgtMap = new Map(tgtCols.map((c) => [c.name, c]));
    const rows: AlignedRow[] = [];
    const seen = new Set<string>();

    for (const sc of srcCols) {
      seen.add(sc.name);
      const tc = tgtMap.get(sc.name);
      const d_type = tc ? sc.type !== tc.type : false;
      const d_length = tc ? sc.length !== tc.length : false;
      const d_nullable = tc ? sc.nullable !== tc.nullable : false;
      const d_default = tc ? sc.default !== tc.default : false;
      const d_comment = tc ? sc.comment !== tc.comment : false;
      const d_key = tc ? sc.key !== tc.key : false;
      const e = tc || emptyCol();
      rows.push({
        key: sc.name, status: tc ? 'both' : 'source_only',
        hasDiff: tc ? (d_type || d_length || d_nullable || d_default || d_comment || d_key) : false,
        s_name: sc.name, s_type: sc.type, s_length: sc.length, s_nullable: sc.nullable, s_default: sc.default, s_comment: sc.comment, s_key: sc.key,
        t_name: e.name, t_type: e.type, t_length: e.length, t_nullable: e.nullable, t_default: e.default, t_comment: e.comment, t_key: e.key,
        diff_type: d_type, diff_length: d_length, diff_nullable: d_nullable, diff_default: d_default, diff_comment: d_comment, diff_key: d_key,
      });
    }
    for (const tc of tgtCols) {
      if (!seen.has(tc.name)) {
        const sc = emptyCol();
        rows.push({
          key: tc.name, status: 'target_only', hasDiff: false,
          s_name: sc.name, s_type: sc.type, s_length: sc.length, s_nullable: sc.nullable, s_default: sc.default, s_comment: sc.comment, s_key: sc.key,
          t_name: tc.name, t_type: tc.type, t_length: tc.length, t_nullable: tc.nullable, t_default: tc.default, t_comment: tc.comment, t_key: tc.key,
          diff_type: false, diff_length: false, diff_nullable: false, diff_default: false, diff_comment: false, diff_key: false,
        });
      }
    }
    return rows;
  }, [sourceStruct, targetStruct]);

  const diffCell = (val: string, isDiff: boolean) => ({
    children: val || '\u00A0',
    props: isDiff ? { style: { background: '#fff1b8' } } : {},
  });

  const buildStructColumns = (prefix: 's' | 't') => [
    { title: tr('compare.fieldName'), dataIndex: `${prefix}_name`, key: 'name', width: 140, render: (v: string) => v || '\u00A0' },
    {
      title: tr('compare.colType'), dataIndex: `${prefix}_type`, key: 'type', width: 150,
      render: (v: string, r: AlignedRow) => diffCell(v, r.diff_type),
    },
    {
      title: tr('compare.length'), dataIndex: `${prefix}_length`, key: 'length', width: 80,
      render: (v: string, r: AlignedRow) => diffCell(v, r.diff_length),
    },
    {
      title: tr('compare.nullable'), dataIndex: `${prefix}_nullable`, key: 'nullable', width: 70,
      render: (v: string, r: AlignedRow) => diffCell(v, r.diff_nullable),
    },
    {
      title: tr('compare.defaultValue'), dataIndex: `${prefix}_default`, key: 'default', width: 120, ellipsis: true,
      render: (v: string, r: AlignedRow) => diffCell(v, r.diff_default),
    },
    {
      title: tr('compare.comment'), dataIndex: `${prefix}_comment`, key: 'comment', width: 200, ellipsis: true,
      render: (v: string, r: AlignedRow) => diffCell(v, r.diff_comment),
    },
    {
      title: tr('compare.key'), dataIndex: `${prefix}_key`, key: 'key', width: 60,
      render: (v: string, r: AlignedRow) => diffCell(v, r.diff_key),
    },
  ];

  const structRowClassName = (row: AlignedRow) => {
    if (row.status === 'source_only') return 'row-source-only';
    if (row.status === 'target_only') return 'row-target-only';
    if (row.hasDiff) return 'row-diff';
    return '';
  };

  const selectedObjStatus = objects.find((o) => o.name === selectedObj)?.status;

  const refreshAfterSync = async () => {
    setLoading(true);
    try {
      const res = await compareAPI.structure({
        source_ds: sourceDS,
        source_schema: sourceSchema,
        target_ds: targetDS,
        target_schema: targetSchema,
        source_database: sourceDatabase || undefined,
        target_database: targetDatabase || undefined,
      });
      const objs = (res.data.data?.objects || []).sort((a: any, b: any) => a.name.localeCompare(b.name));
      setObjects(objs);

      if (selectedObj && objs.some((o: CompareObject) => o.name === selectedObj)) {
        setSourceData(null);
        setTargetData(null);
        setSourceStruct(null);
        setTargetStruct(null);
        setSourcePage(1);
        setTargetPage(1);
        setSourcePageSize(10);
        setTargetPageSize(10);
        loadTableData(sourceDS, sourceSchema, selectedObj, 1, 10, 'source');
        const selObj = objects.find(o => o.name === selectedObj);
        loadTableStructure(sourceDS, sourceSchema, selectedObj, 'source', selObj?.type === 'view');
        loadTableData(targetDS, targetSchema, selectedObj, 1, 10, 'target');
        loadTableStructure(targetDS, targetSchema, selectedObj, 'target', selObj?.type === 'view');
      }
    } catch {
      // handled
    } finally {
      setLoading(false);
    }
  };

  const handleSyncStructure = async () => {
    setStructSyncModal(true);
    setStructSyncResult(null);
    setStructSyncPhase('preview');
    setStructSyncLoading(true);

    const selObj = objects.find(o => o.name === selectedObj);
    const isView = selObj?.type === 'view';

    if (isView) {
      // Views: get definition from source, generate CREATE VIEW via dialect
      const srcSide = selectedObjStatus !== 'target_only' ? 'source' : 'target';
      const srcDS = srcSide === 'source' ? sourceDS : targetDS;
      const srcSchema = srcSide === 'source' ? sourceSchema : targetSchema;
      const tgtSchema = srcSide === 'source' ? targetSchema : sourceSchema;
      const srcDB = srcSide === 'source' ? sourceDatabase : targetDatabase;
      const _tgtDB = srcSide === 'source' ? targetDatabase : sourceDatabase;
      void _tgtDB;
      const tgtDS = srcSide === 'source' ? targetDS : sourceDS;
      const tgtType = dataSources.find(d => d.id === tgtDS)?.type || 'mysql';
      try {
        const defRes = await viewAPI.definition({ data_source_id: srcDS, schema: srcSchema || undefined, view: selectedObj, database: srcDB || undefined });
        let def = defRes.data.data?.definition || '';
        // Strip any CREATE VIEW prefix — the dialect will add its own
        def = def.replace(/^CREATE\s+(OR\s+REPLACE\s+)?(OR\s+ALTER\s+)?VIEW\s+\S+\s+AS\s*/i, '');
        if (def) {
          const gen = getDialect(tgtType);
          const ddl = gen.createViewDDL(tgtSchema, selectedObj, def, true);
          setEditableDDL(ddl);
          setStructSyncResult({ ddl, success: true, message: tr('compare.viewDDLPreview') });
        } else {
          setEditableDDL(tr('compare.noViewDef'));
        }
      } catch (err: any) {
        setEditableDDL(tr('compare.getViewDefFailed'));
      }
    } else if (selectedObjStatus === 'both') {
      // Table exists on both sides — generate ALTER DDL via backend
      try {
        const res = await compareAPI.syncStructure({
          source_ds: sourceDS, source_schema: sourceSchema, source_database: sourceDatabase || undefined,
          target_ds: targetDS, target_schema: targetSchema, target_database: targetDatabase || undefined,
          table: selectedObj, action: 'alter', dry_run: true,
        });
        const result = res.data.data;
        setStructSyncResult(result);
        setEditableDDL(result?.ddl || tr('compare.noDDLDash'));
      } catch (err: any) {
        const msg = err?.response?.data?.message || err?.message || tr('compare.getStructureFailed');
        message.error(msg);
        setEditableDDL(tr('compare.fetchError'));
      }
    } else {
      // Table only on source — use CREATE DDL
      const srcDDL = sourceStruct?.ddl || '';
      if (srcDDL) {
        let ddl = srcDDL;
        if (sourceSchema && targetSchema && sourceSchema !== targetSchema) {
          ddl = ddl.replace(new RegExp(sourceSchema.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi'), targetSchema);
        }
        setEditableDDL(ddl);
        setStructSyncResult({ ddl, success: true, message: tr('compare.ddlPreview') });
      } else {
        setEditableDDL(tr('compare.noDDLFetch'));
      }
    }
    setStructSyncLoading(false);
  };

  const handleConfirmSync = async () => {
    setStructSyncPhase('executed');
    setStructSyncLoading(true);
    setStructSyncResult(null);
    try {
      await queryAPI.executeDDL({
        data_source_id: targetDS,
        sql: editableDDL,
        schema: targetSchema,
        database: targetDatabase || undefined,
      });
      setStructSyncResult({ success: true, message: tr('compare.ddlExecSuccess'), ddl: editableDDL });
      message.success(tr('compare.syncStructureSuccess'));
      refreshAfterSync();
    } catch (err: any) {
      const msg = err?.response?.data?.message || err?.message || tr('compare.syncStructureFailed');
      message.error(`${tr('compare.syncStructureFailedMsg')}: ${msg}`);
      setStructSyncResult({ success: false, message: msg, ddl: editableDDL });
    } finally {
      setStructSyncLoading(false);
    }
  };

  const handleOpenDataSyncModal = () => {
    const selObj = objects.find(o => o.name === selectedObj);
    if (selObj?.type === 'view') {
      message.warning(tr('compare.viewDataSyncWarning'));
      return;
    }
    setDataSyncModal(true);
    setDataSyncResult(null);
    setSyncTruncate(false);
    setSyncID(true);
    setSyncTransactional(false);
    setSyncMode('full');
    setCheckFields([]);
    setSyncColumns([]);
  };

  const handleSyncData = async () => {
    if (syncMode === 'diff' && checkFields.length === 0) {
      message.warning(tr('compare.needCheckFields'));
      return;
    }
    if (syncMode === 'selected' && selectedRows.length === 0) {
      message.warning(tr('compare.selectRowsFirst'));
      return;
    }

    setDataSyncLoading(true);
    setDataSyncResult(null);
    try {
      const res = await compareAPI.syncData({
        source_ds: sourceDS,
        source_schema: sourceSchema,
        source_database: sourceDatabase || undefined,
        target_ds: targetDS,
        target_schema: targetSchema,
        target_database: targetDatabase || undefined,
        table: selectedObj,
        options: {
          truncate_target: syncTruncate,
          sync_id: syncID,
          transactional: syncTransactional,
          mode: syncMode,
          check_fields: syncMode === 'diff' ? checkFields : undefined,
          sync_columns: syncColumns.length > 0 ? syncColumns : undefined,
          selected_rows: syncMode === 'selected' ? selectedRows : undefined,
        },
      });
      const result: DataSyncResult = res.data.data;
      setDataSyncResult(result);
      if (result.success) {
        message.success(tr('compare.syncDataSuccess', { total: result.total_rows, synced: result.synced_rows }));
        if (selectedObjStatus !== 'source_only') {
          loadTableData(targetDS, targetSchema, selectedObj, 1, targetPageSize, 'target');
        }
      } else {
        message.warning(tr('compare.dataSyncWithErrors', { count: result.errors?.length || 0 }));
      }
    } catch (err: any) {
      const msg = err?.response?.data?.message || err?.message || tr('compare.syncFailed');
      message.error(`${tr('compare.dataSyncFailedMsg')}: ${msg}`);
      setDataSyncResult({ success: false, message: msg, total_rows: 0, synced_rows: 0, errors: [msg] } as any);
    } finally {
      setDataSyncLoading(false);
    }
  };

  const sourceColumns = sourceStruct?.columns?.map((c: any) => c.name) || sourceData?.columns || [];
  const checkFieldTransferData = useMemo(
    () => sourceColumns.map((c) => ({ key: c, title: c })),
    [sourceColumns]
  );
  const syncColTransferData = useMemo(
    () => sourceColumns.map((c) => ({ key: c, title: c })),
    [sourceColumns]
  );

  const handleSourceRowSelect = (keys: React.Key[], rows: Record<string, any>[]) => {
    setSelectedRowKeys(keys);
    setSelectedRows(rows);
  };

  const filteredObjects = useMemo(() => {
    if (!objectFilter) return objects;
    const kw = objectFilter.toLowerCase();
    return objects.filter((o) => o.name.toLowerCase().includes(kw));
  }, [objects, objectFilter]);

  const replayHistory = (item: CompareHistoryItem) => {
    setSourceDS(item.sourceDS);
    setTargetDS(item.targetDS);
    setSourceSchema(item.sourceSchema);
    setTargetSchema(item.targetSchema);
    setSourceDatabase(item.sourceDatabase);
    setTargetDatabase(item.targetDatabase);
    setObjects([]);
    setSelectedObj('');
    // Run compare after a tick (state updates settle)
    setTimeout(() => compareRef.current?.(), 150);
  };

  const compareRef = useRef(handleCompare);
  compareRef.current = handleCompare;

  const removeHistory = (item: CompareHistoryItem) => {
    setCompareHistory(prev => {
      const updated = prev.filter(h => h.timestamp !== item.timestamp);
      localStorage.setItem('compare_history', JSON.stringify(updated));
      return updated;
    });
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16, flexWrap: 'wrap', gap: 8 }}>
        <h2 style={{ fontSize: 18, margin: 0 }}>{tr('nav.compare')}</h2>
        <Space wrap>
          <Space size={4}>
            <Tag color="blue">{tr('compare.source')}</Tag>
            <Select
              style={{ width: 160 }}
              placeholder={tr('compare.sourceDS')}
              value={sourceDS || undefined}
              onChange={handleSourceDSChange}
              options={dataSources.map((ds) => ({
                label: `${ds.name} ${ds.host}:${ds.port} (${ds.type})`,
                value: `${ds.id}`,
              }))}
            />
            {isTwoLevel(sourceDBType) && (
              <Select
                style={{ width: 100 }}
                placeholder={tr('compare.database') || 'Database'}
                value={sourceDatabase || undefined}
                onChange={handleSourceDatabaseChange}
                options={sourceDatabases.map((s) => ({ label: s, value: s }))}
                disabled={!sourceDS}
              />
            )}
            <Select
              style={{ width: 130 }}
              placeholder={schemaLabel(sourceDBType)}
              value={sourceSchema || undefined}
              onChange={setSourceSchema}
              options={sourceSchemas.map((s) => ({
                label: s,
                value: s,
              }))}
              disabled={!sourceDS}
          />
          </Space>
          <Button
            icon={<SwapOutlined />}
            size="small"
            disabled={!sourceDS || !targetDS}
            onClick={() => {
              const tmpDS = sourceDS;
              const tmpSchema = sourceSchema;
              const tmpDB = sourceDatabase;
              setSourceDS(targetDS);
              setTargetDS(tmpDS);
              setSourceSchema(targetSchema);
              setTargetSchema(tmpSchema);
              setSourceDatabase(targetDatabase);
              setTargetDatabase(tmpDB);
              setObjects([]);
              setSelectedObj('');
            }}
          />
          <Space size={4}>
            <Tag color="green">{tr('compare.target')}</Tag>
            <Select
              style={{ width: 160 }}
              placeholder={tr('compare.targetDS')}
              value={targetDS || undefined}
              onChange={handleTargetDSChange}
              options={dataSources.map((ds) => ({
                label: `${ds.name} ${ds.host}:${ds.port} (${ds.type})`,
                value: `${ds.id}`,
              }))}
            />
            {isTwoLevel(targetDBType) && (
              <Select
                style={{ width: 100 }}
                placeholder={tr('compare.database') || 'Database'}
                value={targetDatabase || undefined}
                onChange={handleTargetDatabaseChange}
                options={targetDatabases.map((s) => ({ label: s, value: s }))}
                disabled={!targetDS}
              />
            )}
            <Select
              style={{ width: 130 }}
              placeholder={schemaLabel(targetDBType)}
              value={targetSchema || undefined}
              onChange={setTargetSchema}
              options={targetSchemas.map((s) => ({
                label: s,
                value: s,
              }))}
              disabled={!targetDS}
            />
          </Space>
          <Button
            type="primary"
            icon={<DiffOutlined />}
            loading={loading}
            onClick={handleCompare}
          >
            {tr('compare.startCompare')}
          </Button>
        </Space>
      </div>

      {objects.length === 0 && (
        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: 'calc(100vh - 200px)', flexDirection: 'column', gap: 24 }}>
          <Empty
            image={false}
            description={
              <div style={{ textAlign: 'left', lineHeight: 2, fontSize: 14, color: '#666', maxWidth: 580, margin: '0 auto' }}>
                <div style={{ fontSize: 18, fontWeight: 600, color: '#333', marginBottom: 12, textAlign: 'center' }}>{tr('compare.guideTitle')}</div>
                <p>{tr('compare.guideDesc')}</p>
                <p style={{ marginBottom: 4 }}><b>{tr('compare.guideSteps')}</b></p>
                <ol style={{ margin: '0 0 12px 0', paddingLeft: 20 }}>
                  <li>{tr('compare.guideStep1')}</li>
                  <li>{tr('compare.guideStep2')}</li>
                  <li>{tr('compare.guideStep3')}</li>
                </ol>
                <p style={{ marginBottom: 4 }}><b>{tr('compare.guideSupported')}</b></p>
                <ul style={{ margin: '0 0 12px 0', paddingLeft: 20 }}>
                  <li>{tr('compare.guideSameType')}</li>
                  <li>{tr('compare.guideDiffType')}</li>
                </ul>
                <p style={{ marginBottom: 4 }}><b>{tr('compare.guideNotSupport')}</b></p>
                <ul style={{ margin: '0 0 12px 0', paddingLeft: 20 }}>
                  <li>{tr('compare.guideNoSync')}</li>
                </ul>
                <p style={{ fontSize: 12, color: '#999', textAlign: 'center' }}>{tr('compare.supportedDBs')}</p>
              </div>
            }
          />
          {compareHistory.length > 0 && (
            <Card title={tr('compare.recentCompare')} size="small" style={{ width: '100%', maxWidth: 900 }}>
              <List
                size="small"
                dataSource={compareHistory.slice(0, 3)}
                renderItem={(item) => (
                  <List.Item
                    style={{ cursor: 'pointer' }}
                    onClick={() => replayHistory(item)}
                    actions={[
                      <Button size="small" type="primary" icon={<DiffOutlined />}>{tr('compare.startCompare')}</Button>,
                      <Button size="small" danger icon={<DeleteOutlined />} onClick={(e: any) => { e.stopPropagation(); removeHistory(item); }} />
                    ]}
                  >
                    <List.Item.Meta
                      title={
                        <div style={{ lineHeight: 2 }}>
                          <Tag color="blue">{item.sourceDSName}</Tag>
                          {item.sourceDatabase ? <><Text type="secondary">{item.sourceDatabase} /</Text> </> : null}
                          <Text strong>{item.sourceSchema}</Text>
                          <Text type="secondary" style={{ margin: '0 8px' }}>→</Text>
                          <Tag color="green">{item.targetDSName}</Tag>
                          {item.targetDatabase ? <><Text type="secondary">{item.targetDatabase} /</Text> </> : null}
                          <Text strong>{item.targetSchema}</Text>
                        </div>
                      }
                      description={new Date(item.timestamp).toLocaleString()}
                    />
                  </List.Item>
                )}
              />
            </Card>
          )}
        </div>
      )}
      {objects.length > 0 && (
        <Row gutter={4} style={{ display: 'flex', flexWrap: 'nowrap', alignItems: 'stretch', minHeight: 'calc(100vh - 160px)' }}>
          <Col span={4} style={{ display: 'flex' }}>
            <Card
              size="small"
              title={
                <Input
                  size="small"
                  placeholder={tr('compare.searchTable')}
                  prefix={<SearchOutlined style={{ color: '#999' }} />}
                  allowClear
                  value={objectFilter}
                  onChange={(e) => setObjectFilter(e.target.value)}
                  style={{ fontSize: 12 }}
                  suffix={<span style={{ fontSize: 11, color: '#999' }}>{filteredObjects.length}/{objects.length}</span>}
                />
              }
              style={leftListHeight ? { height: leftListHeight, overflow: 'auto', width: '100%' } : { minHeight: 'calc(100vh - 120px)', overflow: 'auto', width: '100%' }}
              bodyStyle={{ padding: '4px' }}
            >
              <List
                size="small"
                split={false}
                dataSource={filteredObjects}
                renderItem={(obj) => (
                  <List.Item
                    onClick={() => handleSelectObject(obj.name)}
                    style={{
                      cursor: 'pointer',
                      padding: '3px 6px',
                      background: selectedObj === obj.name ? '#e6ffe6' : statusBg(obj.status),
                      borderRadius: 4,
                      marginBottom: 1,
                      border: selectedObj === obj.name ? '1px solid #20a53a' : '1px solid transparent',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
                      <Space size={4}>
                        {typeIcon(obj.type)}
                        <Text
                          ellipsis={{ tooltip: obj.name }}
                          style={{ maxWidth: 110, fontSize: 13 }}
                        >
                          {obj.name}
                        </Text>
                      </Space>
                      <span style={{ flexShrink: 0 }}>{statusIcon(obj.status)}</span>
                    </div>
                  </List.Item>
                )}
              />
            </Card>
          </Col>

          <Col span={20} ref={rightColRef} style={{ display: 'flex', flexDirection: 'column', alignSelf: 'flex-start' }}>
            {selectedObj ? (
              <Card size="small">
                <Tabs
                  defaultActiveKey="data"
                  tabBarExtraContent={
                    <Space>
                      <Button
                        type="primary"
                        icon={<SyncOutlined />}
                        onClick={handleSyncStructure}
                        size="small"
                      >
                        {tr('compare.syncStructure')}
                      </Button>
                      <Button
                        type="primary"
                        icon={<DatabaseOutlined />}
                        onClick={handleOpenDataSyncModal}
                        size="small"
                      >
                        {tr('compare.syncData')}
                      </Button>
                    </Space>
                  }
                  items={[
                    {
                      key: 'data',
                      label: tr('compare.data'),
                      children: (
                        <Spin spinning={dataLoading}>
                          {selectedRowKeys.length > 0 && (
                            <Alert
                              message={tr('compare.selectedRowsData', { count: selectedRowKeys.length })}
                              type="info"
                              showIcon
                              style={{ marginBottom: 8 }}
                              closable
                            />
                          )}
                          <Row gutter={8} style={{ overflow: 'hidden' }}>
                            <Col span={12} style={{ minWidth: 0 }}>
                              <Card
                                size="small"
                                title={<span style={{ fontSize: 13 }}>{tr('compare.source')}: {selectedObj}</span>}
                                style={{ minHeight: 300, overflow: 'hidden' }}
                              >
                                {sourceData ? (
                                  <Table
                                    columns={buildDataColumns(sourceData, true)}
                                    dataSource={buildDataRows(sourceData)}
                                    size="small"
                                    scroll={{ x: 'max-content' }}
                                    rowSelection={{
                                      selectedRowKeys,
                                      onChange: (keys, rows) => handleSourceRowSelect(keys, rows),
                                    }}
                                    pagination={{
                                      current: sourcePage,
                                      pageSize: sourcePageSize,
                                      total: sourceData.total_rows,
                                      onChange: handleSourcePageChange,
                                      size: 'small',
                                      showTotal: (total) => `${tr('common.total')} ${total} ${tr('common.rows')}`,
                                      showSizeChanger: true,
                                      pageSizeOptions: ['10', '20', '50', '100'],
                                    }}
                                  />
                                ) : (
                                  <Empty description={tr('compare.noData')} />
                                )}
                              </Card>
                            </Col>
                            <Col span={12} style={{ minWidth: 0 }}>
                              <Card
                                size="small"
                                title={<span style={{ fontSize: 13 }}>{tr('compare.target')}: {selectedObj}</span>}
                                style={{ minHeight: 300, overflow: 'hidden' }}
                              >
                                {targetData ? (
                                  <Table
                                    columns={buildDataColumns(targetData)}
                                    dataSource={buildDataRows(targetData)}
                                    size="small"
                                    scroll={{ x: 'max-content' }}
                                    pagination={{
                                      current: targetPage,
                                      pageSize: targetPageSize,
                                      total: targetData.total_rows,
                                      onChange: handleTargetPageChange,
                                      size: 'small',
                                      showTotal: (total) => `${tr('common.total')} ${total} ${tr('common.rows')}`,
                                      showSizeChanger: true,
                                      pageSizeOptions: ['10', '20', '50', '100'],
                                    }}
                                  />
                                ) : (
                                  <Empty description={tr('compare.noData')} />
                                )}
                              </Card>
                            </Col>
                          </Row>
                        </Spin>
                      ),
                    },
                    {
                      key: 'structure',
                      label: tr('compare.structure'),
                      children: (
                        <Spin spinning={structLoading}>
                          <style>{`
                            .row-diff td { background: #fffbe6 !important; }
                            .row-source-only td { background: #fff7e6 !important; }
                            .row-target-only td { background: #e6f7ff !important; }
                          `}</style>
                          <Row gutter={8} style={{ overflow: 'hidden' }}>
                            <Col span={12} style={{ minWidth: 0 }}>
                              <Card size="small" title={<span style={{ fontSize: 13 }}>{tr('compare.sourceFieldStruct', { name: selectedObj })}</span>} style={{ overflow: 'hidden' }}>
                                <Table
                                  columns={buildStructColumns('s')}
                                  dataSource={alignedStructRows}
                                  rowKey="key"
                                  size="small"
                                  scroll={{ x: 'max-content' }}
                                  pagination={false}
                                  rowClassName={structRowClassName}
                                />
                              </Card>
                            </Col>
                            <Col span={12} style={{ minWidth: 0 }}>
                              <Card size="small" title={<span style={{ fontSize: 13 }}>{tr('compare.targetFieldStruct', { name: selectedObj })}</span>} style={{ overflow: 'hidden' }}>
                                <Table
                                  columns={buildStructColumns('t')}
                                  dataSource={alignedStructRows}
                                  rowKey="key"
                                  size="small"
                                  scroll={{ x: 'max-content' }}
                                  pagination={false}
                                  rowClassName={structRowClassName}
                                />
                              </Card>
                            </Col>
                          </Row>
                          <Row gutter={8} style={{ marginTop: 16, overflow: 'hidden' }}>
                            <Col span={12} style={{ minWidth: 0 }}>
                              <Card size="small" title={<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}><span style={{ fontSize: 13 }}>{tr('compare.sourceDDLLabel')}</span>{sourceStruct?.ddl && <Button size="small" type="link" icon={<CopyOutlined />} onClick={() => { navigator.clipboard.writeText(sourceStruct!.ddl); message.success(tr('query.copied')); }}>{tr('common.copy')}</Button>}</div>} style={{ overflow: 'hidden' }}>
                                <pre
                                  style={{
                                    margin: 0,
                                    fontSize: 12,
                                    maxHeight: 300,
                                    overflow: 'auto',
                                    background: '#f5f5f5',
                                    padding: 12,
                                    borderRadius: 4,
                                  }}
                                >
                                  {sourceStruct?.ddl || tr('compare.noDataDash')}
                                </pre>
                              </Card>
                            </Col>
                            <Col span={12} style={{ minWidth: 0 }}>
                              <Card size="small" title={<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}><span style={{ fontSize: 13 }}>{tr('compare.targetDDLLabel')}</span>{targetStruct?.ddl && <Button size="small" type="link" icon={<CopyOutlined />} onClick={() => { navigator.clipboard.writeText(targetStruct!.ddl); message.success(tr('query.copied')); }}>{tr('common.copy')}</Button>}</div>} style={{ overflow: 'hidden' }}>
                                <pre
                                  style={{
                                    margin: 0,
                                    fontSize: 12,
                                    maxHeight: 300,
                                    overflow: 'auto',
                                    background: '#f5f5f5',
                                    padding: 12,
                                    borderRadius: 4,
                                  }}
                                >
                                  {targetStruct?.ddl || tr('compare.noDataDash')}
                                </pre>
                              </Card>
                            </Col>
                          </Row>
                        </Spin>
                      ),
                    },
                  ]}
                />
              </Card>
            ) : (
              <Card style={leftListHeight ? { height: leftListHeight, display: 'flex', alignItems: 'center', justifyContent: 'center' } : { minHeight: 'calc(100vh - 120px)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <Empty description={tr('compare.clickHint')} />
              </Card>
            )}
          </Col>
        </Row>
      )}

      <Modal
        title={structSyncPhase === 'preview' ? tr('compare.syncStructurePreview') : tr('compare.syncStructureResult')}
        open={structSyncModal}
        onCancel={() => setStructSyncModal(false)}
        footer={
          structSyncPhase === 'preview'
            ? [
                <Button key="cancel" onClick={() => setStructSyncModal(false)}>
                  {tr('compare.cancelBtn')}
                </Button>,
                <Button
                  key="confirm"
                  type="primary"
                  onClick={handleConfirmSync}
                  loading={structSyncLoading}
                  disabled={!editableDDL.trim()}
                >
                  {tr('compare.confirmExecute')}
                </Button>,
              ]
            : [
                <Button key="close" onClick={() => setStructSyncModal(false)}>
                  {tr('compare.closeBtn')}
                </Button>,
              ]
        }
        width={700}
      >
        <Spin spinning={structSyncLoading}>
          {structSyncResult ? (
            <>
              <Alert
                message={structSyncResult.message}
                type={structSyncResult.success ? (structSyncPhase === 'preview' ? 'info' : 'success') : 'error'}
                showIcon
                style={{ marginBottom: 16 }}
              />
              {structSyncPhase === 'preview' ? (
                <>
                  <Text strong>{tr('compare.ddlEditable')}</Text>
                  <Input.TextArea
                    value={editableDDL}
                    onChange={(e) => setEditableDDL(e.target.value)}
                    autoSize={{ minRows: 8, maxRows: 20 }}
                    style={{
                      marginTop: 8,
                      fontSize: 12,
                      fontFamily: 'monospace',
                    }}
                  />
                </>
              ) : (
                <>
                  <Text strong>{tr('compare.ddlExecuted')}</Text>
                  <pre
                    style={{
                      marginTop: 8,
                      fontSize: 12,
                      maxHeight: 400,
                      overflow: 'auto',
                      background: '#f5f5f5',
                      padding: 12,
                      borderRadius: 4,
                      whiteSpace: 'pre-wrap',
                      wordBreak: 'break-all',
                    }}
                  >
                    {structSyncResult.ddl}
                  </pre>
                </>
              )}
            </>
          ) : (
            <div style={{ textAlign: 'center', padding: 40 }}>
              <Spin />
              <div style={{ marginTop: 8 }}>{tr('compare.generatingDDL')}</div>
            </div>
          )}
        </Spin>
      </Modal>

      <Modal
        title={tr('compare.syncData')}
        open={dataSyncModal}
        onCancel={() => setDataSyncModal(false)}
        onOk={handleSyncData}
        okText={tr('compare.executeSync')}
        okButtonProps={{ loading: dataSyncLoading }}
        width={700}
      >
        <div style={{ marginBottom: 16 }}>
          <Text strong>{tr('compare.syncTable')} </Text>
          <Tag color="blue">{selectedObj}</Tag>
        </div>

        <div style={{ marginBottom: 12 }}>
          <Checkbox checked={syncTruncate} onChange={(e) => setSyncTruncate(e.target.checked)}>
            {tr('compare.truncateTarget')}
          </Checkbox>
          {syncTruncate && (
            <Alert
              message={tr('compare.truncateWarning')}
              type="warning"
              showIcon
              style={{ marginTop: 4 }}
            />
          )}
        </div>

        <div style={{ marginBottom: 12 }}>
          <Checkbox checked={syncID} onChange={(e) => setSyncID(e.target.checked)}>
            {tr('compare.syncAutoInc')}
          </Checkbox>
        </div>
        <div style={{ marginBottom: 12 }}>
          <Checkbox checked={syncTransactional} onChange={(e) => setSyncTransactional(e.target.checked)}>
            {tr('compare.transactionMode')}
          </Checkbox>
        </div>

        <div style={{ marginBottom: 12 }}>
          <Text strong>{tr('compare.syncModeLabel')}</Text>
          <Radio.Group
            value={syncMode}
            onChange={(e) => setSyncMode(e.target.value)}
            style={{ display: 'block', marginTop: 4 }}
          >
            <Radio value="full">{tr('compare.fullSync')}</Radio>
            <Radio value="selected" disabled={selectedRows.length === 0}>
              {tr('compare.syncSelectedRows')} {selectedRows.length > 0 && `(${selectedRows.length} ${tr('compare.nRows')})`}
            </Radio>
            <Radio value="diff">{tr('compare.diffSync')}</Radio>
          </Radio.Group>
        </div>

        {syncMode === 'diff' && (
          <div style={{ marginBottom: 12 }}>
            <Text strong>{tr('compare.checkFields')}</Text>
            <Transfer
              dataSource={checkFieldTransferData}
              titles={[tr('compare.available'), tr('compare.selected')]}
              targetKeys={checkFields}
              onChange={(keys) => setCheckFields(keys as string[])}
              render={(item) => item.title || ''}
              listStyle={{ width: 250, height: 200 }}
              style={{ marginTop: 4 }}
            />
          </div>
        )}

        {(syncMode === 'diff' || syncMode === 'full') && (
          <div style={{ marginBottom: 12 }}>
            <Text strong>{tr('compare.syncColumns')}</Text>
            <Transfer
              dataSource={syncColTransferData}
              titles={[tr('compare.available'), tr('compare.selected')]}
              targetKeys={syncColumns}
              onChange={(keys) => setSyncColumns(keys as string[])}
              render={(item) => item.title || ''}
              listStyle={{ width: 250, height: 200 }}
              style={{ marginTop: 4 }}
            />
          </div>
        )}

        {dataSyncResult && (
          <div style={{ marginTop: 16 }}>
            <Alert
              message={dataSyncResult.success ? tr('compare.syncSuccess') : tr('compare.syncWithErrors')}
              type={dataSyncResult.success ? 'success' : 'warning'}
              showIcon
              description={
                <div>
                  <div>{tr('compare.totalRowsLabel')}: {dataSyncResult.total_rows}</div>
                  <div>{tr('compare.syncedRows')}: {dataSyncResult.synced_rows}</div>
                  <div>{tr('compare.skippedRows')}: {dataSyncResult.skipped_rows}</div>
                  {dataSyncResult.errors?.length > 0 && (
                    <div style={{ color: '#ff4d4f', marginTop: 4 }}>
                      {tr('compare.errorsLabel')}: {dataSyncResult.errors.join(', ')}
                    </div>
                  )}
                </div>
              }
            />
          </div>
        )}
      </Modal>
    </div>
  );
};

export default Compare;
