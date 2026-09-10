import React, { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Table, Button, Modal, Form, Input, Select, Space, Tag, message, Radio, Tooltip, Checkbox, Row, Col, Drawer, Tabs, Upload, Tree, Typography,
} from 'antd';
import {
  PlusOutlined, PlayCircleOutlined, StopOutlined, DeleteOutlined,
  UploadOutlined, ImportOutlined, EyeOutlined, FolderOutlined, FileOutlined, ArrowUpOutlined,
} from '@ant-design/icons';
import { exportTaskAPI, dsAPI, filesAPI } from '../api';
import request from '../api';
import type { DataSource } from '../types';
import ExecutionSubTable from '../components/ExecutionSubTable';
import ExecutionLogDrawer from '../components/ExecutionLogDrawer';
import ManifestDrawer from '../components/ManifestDrawer';
import ImportFromExportModal from '../components/ImportFromExportModal';

interface ExportTask {
  id: string;
  name: string;
  task_type: string;
  status: string;
  data_source_id: string;
  database_name?: string;
  schema_name?: string;
  // Export
  export_scope?: string;
  export_content?: string;
  export_format?: string;
  export_tables?: string[];
  export_batch_size?: number;
  // Import
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
  created_at: string;
  updated_at: string;
}

const fmtDate = (t: string) => {
  if (!t) return '';
  const d = new Date(t);
  if (isNaN(d.getTime())) return t;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

const ExportTasks: React.FC = () => {
  const { t: tr } = useTranslation();
  const { t: te } = useTranslation('exportTaskExtra');
  const [data, setData] = useState<ExportTask[]>([]);
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  const [loading, setLoading] = useState(false);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [taskType, setTaskType] = useState<string>('');
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importFromExportOpen, setImportFromExportOpen] = useState(false);
  const [preselectImportTaskId, setPreselectImportTaskId] = useState<string>('');
  const [logDrawerOpen, setLogDrawerOpen] = useState(false);
  const [logTaskId, setLogTaskId] = useState<string>('');
  const [logExecId, setLogExecId] = useState<string>('');
  const [execRefreshTrigger, setExecRefreshTrigger] = useState(0);
  const [manifestDrawerOpen, setManifestDrawerOpen] = useState(false);
  const [manifestExecId, setManifestExecId] = useState<string>('');
  const [taskDetailOpen, setTaskDetailOpen] = useState(false);
  const [taskDetail, setTaskDetail] = useState<ExportTask | null>(null);
  const [taskDetailTables, setTaskDetailTables] = useState<string[]>([]);
  const [schemaTables, setSchemaTables] = useState<string[]>([]);
  const [databases, setDatabases] = useState<string[]>([]);
  const [schemas, setSchemas] = useState<string[]>([]);
  const [selectedDS, setSelectedDS] = useState<any>(null);
  const [uploading, setUploading] = useState(false);
  const [form] = Form.useForm();
  const [importForm] = Form.useForm();
  const [importTargetDS, setImportTargetDS] = useState<any>(null);
  const [importDatabases, setImportDatabases] = useState<string[]>([]);
  const [importSchemas, setImportSchemas] = useState<string[]>([]);
  const [importTab, setImportTab] = useState<string>("upload");
  const [importFile, setImportFile] = useState<File | null>(null);
  const importFileRef = useRef<File | null>(null);
  const [browseOpen, setBrowseOpen] = useState(false);
  const [browseTree, setBrowseTree] = useState<any[]>([]);
  const [browseCurrentDir, setBrowseCurrentDir] = useState<string>("");
  const [storageProfiles, setStorageProfiles] = useState<any[]>([]);
  const [browseProfile, setBrowseProfile] = useState<string>("");

  const [importSourceDS, setImportSourceDS] = useState<any>(null);
  const [importSourceDBs, setImportSourceDBs] = useState<string[]>([]);
  const [importSourceSchemas, setImportSourceSchemas] = useState<string[]>([]);
  const [importSourceTables, setImportSourceTables] = useState<string[]>([]);
  const fetchData = async () => {
    setLoading(true);
    try {
      const params: any = { page, page_size: 20 };
      if (taskType) params.task_type = taskType;
      const res = await exportTaskAPI.list(params);
      setData(res.data.data?.list || []);
      setTotal(res.data.data?.total || 0);
    } catch { /* handled */ }
    finally { setLoading(false); }
  };

  const fetchDS = async () => {
    try {
      const res = await dsAPI.list();
      setDataSources(res.data.data?.list || []);
    } catch { /* handled */ }
  };

  useEffect(() => {
    fetchData();
    fetchDS();
    filesAPI.profiles().then(r => setStorageProfiles(r.data.data || [])).catch(() => {});
  }, [page, taskType]);

  const handleCreateExport = async () => {
    const values = await form.validateFields();
    try {
      await exportTaskAPI.createExport({
        name: values.name,
        data_source_id: values.data_source_id,
        database_name: values.database || undefined,
        schema_name: values.schema_name || undefined,
        config: {
          export: {
            export_scope: values.export_scope || 'tables',
            export_content: values.export_content || 'all',
            export_tables: values.export_tables || [],
            export_format: values.export_format || 'sql',
            export_batch_size: values.export_batch_size || 500,
          },
        },
      });
      message.success(te('exportCreated'));
      setExportModalOpen(false);
      form.resetFields();
      fetchData();
    } catch { /* handled */ }
  };

  const handleImportTargetDSChange = async (dsId: string) => {
    importForm.setFieldValue("import_target_ds_id", dsId);
    importForm.setFieldValue("import_target_database", undefined);
    importForm.setFieldValue("import_target_schema", undefined);
    setImportSchemas([]);
    const ds = dataSources.find(d => d.id === dsId);
    setImportTargetDS(ds || null);
    if (!ds) return;
    if (ds.type === "sqlserver" || ds.type === "mssql" || ds.type === "postgres" || ds.type === "postgresql") {
      try { const res = await dsAPI.databases(dsId); setImportDatabases(res.data.data || []); } catch { setImportDatabases([]); }
    } else {
      try { const res = await dsAPI.schemaNames(dsId); setImportSchemas(res.data.data || []); } catch { setImportSchemas([]); }
    }
  };

  const handleImportDBChange = async (dbName: string) => {
    importForm.setFieldValue("import_target_database", dbName);
    importForm.setFieldValue("import_target_schema", undefined);
    setImportSchemas([]);
    if (!importTargetDS) return;
    try { const res = await dsAPI.databaseSchemas(importTargetDS.id, dbName); setImportSchemas(res.data.data || []); } catch { setImportSchemas([]); }
  };

  const handleImportSourceDSChange = async (dsId: string) => {
    importForm.setFieldValue("import_source_ds_id", dsId);
    setImportSourceSchemas([]); setImportSourceTables([]);
    const ds = dataSources.find(d => d.id === dsId);
    setImportSourceDS(ds || null);
    if (!ds) return;
    const needsDB = ds.type === "sqlserver" || ds.type === "mssql" || ds.type === "postgres" || ds.type === "postgresql";
    if (needsDB) {
      try { const res = await dsAPI.databases(dsId); setImportSourceDBs(res.data.data || []); } catch { setImportSourceDBs([]); }
    } else {
      try { const res = await dsAPI.schemaNames(dsId); setImportSourceSchemas(res.data.data || []); } catch { setImportSourceSchemas([]); }
    }
  };

  const handleImportSourceDBChange = async (dbName: string) => {
    importForm.setFieldValue("import_source_database", dbName);
    importForm.setFieldValue("import_source_schema", undefined);
    setImportSourceSchemas([]); setImportSourceTables([]);
    if (!importSourceDS) return;
    try { const res = await dsAPI.databaseSchemas(importSourceDS.id, dbName); setImportSourceSchemas(res.data.data || []); } catch { setImportSourceSchemas([]); }
  };

  const handleImportSourceSchemaChange = async (schema: string) => {
    importForm.setFieldValue("import_source_schema", schema);
    importForm.setFieldValue("import_source_tables", []);
    setImportSourceTables([]);
    if (!importSourceDS) return;
    try {
      const res = await dsAPI.tableList(importSourceDS.id, schema, importForm.getFieldValue("import_source_database"));
      const raw = res.data.data; const list = Array.isArray(raw) ? raw : (raw?.list || []);
      setImportSourceTables(list.map((t: any) => t.name));
    } catch { setImportSourceTables([]); }
  };

  const fetchBrowseFiles = async (dir: string) => {
    setBrowseCurrentDir(dir);
    try {
      const res = await filesAPI.list({ dir, page_size: 200, profile: browseProfile || undefined });
      setBrowseTree(res.data?.data?.files || []);
    } catch { setBrowseTree([]); }
  };

  const handleCreateImport = async () => {
    const values = await importForm.validateFields();
    // 用 importTab 而非 values.import_source，因为 Tabs default 时不触发 onChange
    const importSource = values.import_source || importTab || 'upload';
    const file = importFileRef.current || importFile;

    // 根据导入来源确定文件名和路径
    let importFileName = '';
    let importFilePath = '';
    if (importSource === 'upload') {
      // upload: 文件名用于展示，路径由后端存文件后填充
      importFileName = file ? file.name : '';
      importFilePath = '';
    } else if (importSource === 'storage') {
      // storage: 用户从文件浏览器选择的路径，文件名从路径提取
      importFilePath = values.import_file_path || '';
      const parts = importFilePath.split('/');
      importFileName = parts[parts.length - 1] || importFilePath;
    }

    try {
      const payload: any = {
        name: values.name,
        data_source_id: values.import_target_ds_id,
        database_name: values.import_target_database || undefined,
        schema_name: values.import_target_schema || undefined,
        config: {
          import: {
            import_source: importSource,
            import_content: values.import_content || 'all',
            import_strategy: values.import_strategy || 'fail',
            skip_safety_check: values.skip_safety_check || false,
            import_file_name: importFileName,
            import_file_path: importFilePath,
            storage_profile: importSource === 'upload' ? '' : (values.storage_profile || ''),
            source_ds_id: values.import_source_ds_id || '',
            source_database: values.import_source_database || '',
            source_schema: values.import_source_schema || '',
            source_tables: values.import_source_tables || [],
            target_schema: values.import_target_schema || '',
            target_database: values.import_target_database || '',
          },
        },
      };

      // Upload mode: send as multipart with file + JSON data
      if (importSource === 'upload' && file) {
        setUploading(true);
        const formData = new FormData();
        formData.append('data', JSON.stringify(payload));
        formData.append('file', file);
        await request.post('/import-tasks', formData, {
          headers: { 'Content-Type': 'multipart/form-data' },
        });
      } else {
        await exportTaskAPI.createImport(payload);
      }
      message.success(te('importCreated'));
      setImportModalOpen(false);
      importForm.resetFields();
      setImportFile(null);
      importFileRef.current = null;
      fetchData();
    } catch { /* handled */ }
    finally { setUploading(false); }
  };

  const handleStart = async (id: string) => {
    try {
      await exportTaskAPI.start(id);
      message.success(te('taskStarted'));
      // 延迟 3 秒刷新，确保后端执行记录已创建完成
      setTimeout(() => {
        fetchData();
        setExecRefreshTrigger(t => t + 1);
      }, 3000);
    } catch { /* handled */ }
  };

  const handleCancel = async (id: string) => {
    try {
      await exportTaskAPI.cancel(id);
      message.success(te('taskCancelled'));
      fetchData();
    } catch { /* handled */ }
  };

  const handleViewTaskDetail = async (id: string) => {
    try {
      const res = await exportTaskAPI.get(id);
      const task = res.data.data;
      if (!task) return;
      const tables = Array.isArray(task.export_tables) ? task.export_tables : [];
      setTaskDetail(task);
      setTaskDetailTables(tables);
      setTaskDetailOpen(true);
    } catch { /* handled */ }
  };

  const handleDelete = async (id: string) => {
    Modal.confirm({
      title: tr('common.confirmDelete'),
      content: tr('common.deleteWarning'),
      okText: tr('common.delete'),
      okType: 'danger',
      cancelText: te('cancelText'),
      onOk: async () => {
        await exportTaskAPI.delete(id);
        message.success(te('taskDeleted'));
        fetchData();
      },
    });
  };

  const handleViewLogs = (taskId: string, execId: string) => {
    setLogTaskId(taskId);
    setLogExecId(execId);
    setLogDrawerOpen(true);
  };

  const handleViewManifest = (execId: string) => {
    setManifestExecId(execId);
    setManifestDrawerOpen(true);
  };

  const handleImportFromExecution = (taskId: string) => {
    setPreselectImportTaskId(taskId);
    setImportFromExportOpen(true);
  };

  const fetchTablesForSchema = async (dsId: string, schema: string) => {
    if (!dsId || !schema) {
      setSchemaTables([]);
      return;
    }
    try {
      const database = form.getFieldValue('database');
      const res = await dsAPI.tableList(dsId, schema, database || undefined);
      const raw = res.data.data;
      const list = Array.isArray(raw) ? raw : (raw?.list || []);
      setSchemaTables(list.map((t: any) => t.name));
    } catch { setSchemaTables([]); }
  };

  const handleDataSourceChange = async (dsId: string) => {
    form.setFieldValue('data_source_id', dsId);
    form.setFieldValue('database', undefined);
    form.setFieldValue('schema_name', undefined);
    setSchemaTables([]);
    setSchemas([]);
    setDatabases([]);
    const ds = dataSources.find(d => d.id === dsId);
    setSelectedDS(ds || null);
    if (!ds) return;

    // MSSQL/PG: need database selection. MySQL/Oracle: schema directly
    const needsDB = ds.type === 'sqlserver' || ds.type === 'mssql' || ds.type === 'postgres' || ds.type === 'postgresql';
    if (needsDB) {
      try {
        const res = await dsAPI.databases(dsId);
        setDatabases(res.data.data || []);
      } catch { setDatabases([]); }
    } else {
      // MySQL/Oracle: load schemas directly
      try {
        const res = await dsAPI.schemaNames(dsId);
        setSchemas(Array.isArray(res.data.data) ? res.data.data : []);
      } catch { setSchemas([]); }
    }
  };

  const handleDatabaseChange = async (dbName: string) => {
    form.setFieldValue('database', dbName);
    form.setFieldValue('schema_name', undefined);
    setSchemaTables([]);
    setSchemas([]);
    if (!selectedDS) return;
    try {
      const res = await dsAPI.databaseSchemas(selectedDS.id, dbName);
      setSchemas(Array.isArray(res.data.data) ? res.data.data : []);
    } catch { setSchemas([]); }
  };

  const handleSchemaChange = async (schema: string) => {
    form.setFieldValue('schema_name', schema);
    form.setFieldValue('export_tables', []);
    setSchemaTables([]);
    if (selectedDS) {
      await fetchTablesForSchema(selectedDS.id, schema);
    }
  };

  const columns = [
    { title: tr('exportTask.taskName'), dataIndex: 'name', key: 'name' },
    {
      title: tr('exportTask.taskType'), dataIndex: 'task_type', key: 'task_type', width: 70,
      render: (t: string) => t === 'export' ? <Tag color="blue">{tr('exportTask.export')}</Tag> : <Tag color="orange">{tr('exportTask.import')}</Tag>,
    },
    { title: tr('exportTask.format'), dataIndex: 'export_format', key: 'export_format', width: 60 },
    { title: tr('exportTask.createdAt'), dataIndex: 'created_at', key: 'created_at', width: 200, render: (t: string) => <span style={{ whiteSpace: 'nowrap' }}>{fmtDate(t)}</span> },
    {
      title: tr('datasource.tableAction'), key: 'action', width: 160,
      render: (_: any, record: ExportTask) => (
        <Space size={8}>
          <Tooltip title={tr('exportTask.detail')}><a style={{ color: '#1890ff' }} onClick={() => handleViewTaskDetail(record.id)}><EyeOutlined /></a></Tooltip>
          <Tooltip title={tr('exportTask.start')}><a style={{ color: '#20a53a' }} onClick={() => handleStart(record.id)}><PlayCircleOutlined /></a></Tooltip>
          {record.status === 'running' && (
            <Tooltip title={tr('exportTask.cancel')}><a style={{ color: '#fa8c16' }} onClick={() => handleCancel(record.id)}><StopOutlined /></a></Tooltip>
          )}
          <Tooltip title={tr('exportTask.delete')}><a style={{ color: '#e74c3c' }} onClick={() => handleDelete(record.id)}><DeleteOutlined /></a></Tooltip>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>{tr('nav.exportTasks')}</h2>
        <Space>
          <Select
            allowClear
            placeholder={tr('exportTask.taskType')}
            style={{ width: 100 }}
            value={taskType || undefined}
            onChange={v => { setTaskType(v || ''); setPage(1); }}
            options={[{ label: tr('common.all'), value: '' }, { label: tr('exportTask.export'), value: 'export' }, { label: tr('exportTask.import'), value: 'import' }]}
          />
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setExportModalOpen(true)}>{tr('exportTask.newExport')}</Button>
          <Button icon={<UploadOutlined />} onClick={() => setImportModalOpen(true)}>{tr('exportTask.newImport')}</Button>
          <Button icon={<ImportOutlined />} onClick={() => { setPreselectImportTaskId(''); setImportFromExportOpen(true); }}>{tr('exportTask.importFromExport')}</Button>
        </Space>
      </div>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        scroll={{ x: 680 }}
        pagination={{
          current: page, total, pageSize: 20,
          showSizeChanger: false,
          showTotal: (total) => `${tr('common.total')} ${total} ${tr('common.rows')}`,
          onChange: setPage,
        }}
        expandable={{
          expandedRowRender: (record) => (
            <ExecutionSubTable
              taskId={record.id}
              refreshTrigger={execRefreshTrigger}
              onViewLogs={handleViewLogs}
              onViewManifest={handleViewManifest}
              onImportFromExecution={handleImportFromExecution}
            />
          ),
          rowExpandable: () => true,
        }}
      />

      {/* Export task modal */}
      <Modal title={tr('exportTask.newExport')} open={exportModalOpen} onOk={handleCreateExport} onCancel={() => setExportModalOpen(false)} okText={tr('common.create')} cancelText={tr('common.cancelText')} width={800}>
        <Form form={form} layout="vertical" initialValues={{ export_format: 'sql', batch_size: 500, export_scope: 'tables', export_content: 'all', export_include_structure: true, export_include_data: true }}>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="name" label={tr('exportTask.taskName')} rules={[{ required: true }]}>
                <Input placeholder={tr('exportTask.taskNamePlaceholder')} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="data_source_id" label={tr('datasource.title')} rules={[{ required: true }]}>
                <Select
                  options={dataSources.map(ds => ({ label: `${ds.name} (${ds.type})`, value: ds.id }))}
                  placeholder={tr('exportTask.selectDS')}
                  onChange={handleDataSourceChange}
                />
              </Form.Item>
            </Col>
          </Row>

          {/* Database + Schema - side by side for MSSQL/PG, full width for MySQL/Oracle */}
          {selectedDS && (
            <Row gutter={16}>
              {databases.length > 0 && (
                <Col span={12}>
                  <Form.Item name="database" label={te('databaseLabel')} rules={[{ required: true, message: te('selectDatabaseRequired') }]}>
                    <Select options={databases.map(d => ({ label: d, value: d }))} placeholder={te('selectDatabase')} onChange={handleDatabaseChange} />
                  </Form.Item>
                </Col>
              )}
              <Col span={databases.length > 0 ? 12 : 24}>
                <Form.Item name="schema_name" label={te('schemaLabel')} rules={[{ required: true, message: te('selectSchemaRequired') }]}>
                  <Select
                    options={schemas.map(s => ({ label: s, value: s }))}
                    placeholder={schemas.length > 0 ? te('selectSchemaPlaceholder') : (selectedDS.type === 'sqlserver' || selectedDS.type === 'mssql' || selectedDS.type === 'postgres' || selectedDS.type === 'postgresql' ? te('selectDatabaseFirst') : te('loading'))}
                    onChange={handleSchemaChange}
                    showSearch
                    optionFilterProp="label"
                  />
                </Form.Item>
              </Col>
            </Row>
          )}

          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="export_scope" label={te('exportScope')}>
                <Radio.Group>
                  <Radio.Button value="tables">{te('selectedTables')}</Radio.Button>
                  <Radio.Button value="schema">{te('wholeSchema')}</Radio.Button>
                </Radio.Group>
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="export_content" label={te('exportContent')}>
                <Radio.Group>
                  <Radio.Button value="all">{te('structureAndData')}</Radio.Button>
                  <Radio.Button value="structure">{te('structureOnly')}</Radio.Button>
                  <Radio.Button value="data">{te('dataOnly')}</Radio.Button>
                </Radio.Group>
              </Form.Item>
            </Col>
          </Row>

          <Form.Item noStyle shouldUpdate={(prev, cur) => prev.data_source_id !== cur.data_source_id || prev.schema_name !== cur.schema_name || prev.export_tables !== cur.export_tables || prev.export_scope !== cur.export_scope}>
            {() => {
              if (form.getFieldValue('export_scope') !== 'tables') return null;
              if (schemaTables.length === 0) return null;
              const selectedTables: string[] = form.getFieldValue('export_tables') || [];
              const allChecked = selectedTables.length === schemaTables.length;
              const indeterminate = selectedTables.length > 0 && selectedTables.length < schemaTables.length;
              return (
                <Form.Item
                  name="export_tables"
                  label={
                    <div style={{ display: 'flex', justifyContent: 'space-between', width: '100%' }}>
                      <span>{te('selectTables')}</span>
                      <Checkbox
                        checked={allChecked}
                        indeterminate={indeterminate}
                        onChange={(e) => form.setFieldValue('export_tables', e.target.checked ? schemaTables : [])}
                      >
                        {te('selectAll')}
                      </Checkbox>
                    </div>
                  }
                >
                  <Checkbox.Group options={schemaTables} />
                </Form.Item>
              );
            }}
          </Form.Item>
          <Form.Item name="export_format" label={tr('query.exportFormat')}>
            <Radio.Group onChange={e => form.setFieldValue('export_format', e.target.value)}>
              <Radio value="sql" defaultChecked>SQL</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item name="export_batch_size" label={tr('query.batchSize')} initialValue={500}>
            <Select options={[{ label: '500', value: 500 }, { label: '100', value: 100 }, { label: '1000', value: 1000 }, { label: '5000', value: 5000 }]} />
          </Form.Item>
        </Form>
      </Modal>

      {/* Import task modal */}
      <Modal title={te('importModalTitle')} open={importModalOpen} onOk={handleCreateImport} onCancel={() => setImportModalOpen(false)} okText={te('createBtn')} cancelText={te('cancel')} width={700}>
        <Form form={importForm} layout="vertical" initialValues={{ import_source: 'upload', import_strategy: 'fail', import_content: 'all', skip_safety_check: false }}>
          <Form.Item name="name" label={te('taskName')} rules={[{ required: true }]}>
            <Input placeholder={te('taskNamePlaceholder')} />
          </Form.Item>

          <Tabs activeKey={importTab} onChange={(key) => { setImportTab(key); importForm.setFieldsValue({ import_source: key }); }} items={[
            { key: 'db2db', label: 'DB→DB', children: (
              <>
                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item name="import_source_ds_id" label={te('sourceDS')}>
                      <Select options={dataSources.map(ds => ({ label: `${ds.name} (${ds.type})`, value: ds.id }))} placeholder={te('sourceDSPlaceholder')} onChange={handleImportSourceDSChange} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    {importSourceDS && (importSourceDS.type === 'sqlserver' || importSourceDS.type === 'mssql' || importSourceDS.type === 'postgres' || importSourceDS.type === 'postgresql') && (
                      <Form.Item name="import_source_database" label={te('sourceDatabase')}>
                        <Select placeholder={te('selectDatabase')} options={importSourceDBs.map(d => ({ label: d, value: d }))} onChange={handleImportSourceDBChange} />
                      </Form.Item>
                    )}
                  </Col>
                </Row>
                <Form.Item name="import_source_schema" label={te('sourceSchema')}>
                  <Select placeholder={te('selectSchemaPlaceholder')} options={importSourceSchemas.map(s => ({ label: s, value: s }))} onChange={handleImportSourceSchemaChange} showSearch optionFilterProp="label" />
                </Form.Item>
                {importSourceTables.length > 0 && (
                  <Form.Item name="import_source_tables" label={te('sourceTables')}>
                    <Select mode="multiple" placeholder={te('sourceTablesPlaceholder')} options={importSourceTables.map(t => ({ label: t, value: t }))} allowClear maxTagCount={5} />
                  </Form.Item>
                )}
              </>
            )},
            { key: 'upload', label: te('uploadTab'), children: (
              <>
                <Form.Item name="import_file" label={te('sqlFile')}>
                  <Upload maxCount={1} beforeUpload={(file) => { importFileRef.current = file; setImportFile(file); return false; }} onRemove={() => { importFileRef.current = null; setImportFile(null); }} fileList={importFile ? [{ uid: '-1', name: importFile.name, status: 'done' }] : []}>
                    <Button icon={<UploadOutlined />} loading={uploading}>{te('selectFile')}</Button>
                  </Upload>
                </Form.Item>
              </>
            )},
            { key: 'storage', label: te('storageTab'), children: (
              <>
                <Form.Item name="storage_profile" label={te('storageProfileLabel')}>
                  <Select placeholder={te('storageProfilePlaceholder')} options={storageProfiles.map((p: any) => ({ label: `${p.name} (${p.backend})`, value: p.code }))}
                    onChange={(code) => { importForm.setFieldsValue({ storage_profile: code }); setBrowseProfile(code); }} />
                </Form.Item>
                <Form.Item name="import_file_path" label={te('filePath')}>
                  <Input.Search placeholder={te('filePathPlaceholder')} enterButton={te('browse')} onSearch={() => { setBrowseOpen(true); fetchBrowseFiles(''); }} />
                </Form.Item>
              </>
            )},
          ]} />

          {/* File Browser Modal */}
          <Modal title={te('selectFile')} open={browseOpen} onCancel={() => setBrowseOpen(false)} footer={null} width={700}>
            {/* Breadcrumb navigation */}
            <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12, minHeight: 32 }}>
              <div style={{ flex: 1, overflow: 'hidden' }}>
                {browseCurrentDir ? (
                  <span style={{ fontSize: 13 }}>
                    <Typography.Link onClick={() => fetchBrowseFiles('')} style={{ fontSize: 13 }}>{te('rootDir')}</Typography.Link>
                    {browseCurrentDir.split('/').filter(Boolean).map((part, idx, arr) => (
                      <span key={idx}>
                        <span style={{ margin: '0 4px', color: '#999' }}>/</span>
                        {idx < arr.length - 1 ? (
                          <Typography.Link onClick={() => fetchBrowseFiles(arr.slice(0, idx + 1).join('/'))} style={{ fontSize: 13 }}>{part}</Typography.Link>
                        ) : (
                          <span style={{ color: '#666' }}>{part}</span>
                        )}
                      </span>
                    ))}
                  </span>
                ) : (
                  <span style={{ fontSize: 13, color: '#666' }}>{te('rootDir')}</span>
                )}
              </div>
              {browseCurrentDir && (
                <Button size="small" icon={<ArrowUpOutlined />} onClick={() => {
                  const parts = browseCurrentDir.split('/').filter(Boolean);
                  parts.pop();
                  fetchBrowseFiles(parts.join('/'));
                }} style={{ marginLeft: 8 }} />
              )}
            </div>
            <div style={{ minHeight: 400, maxHeight: 600, overflow: 'auto' }}>
              <Tree showLine treeData={browseTree.map((n: any) => ({
                title: n.is_dir ? <Space><FolderOutlined style={{color:'#faad14'}}/>{n.name}</Space> : <Space><FileOutlined />{n.name}</Space>,
                key: n.path, isLeaf: true,
              }))}
                onSelect={(_: any, info: any) => {
                  const p = info.node.key as string;
                  const isDir = browseTree.find((n: any) => n.path === p)?.is_dir;
                  if (isDir) fetchBrowseFiles(p);
                  else { importForm.setFieldsValue({ import_file_path: p }); setBrowseOpen(false); }
                }}
              />
            </div>
          </Modal>

          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="import_target_ds_id" label={te('targetDS')} rules={[{ required: true }]}>
                <Select options={dataSources.map(ds => ({ label: `${ds.name} (${ds.type})`, value: ds.id }))} placeholder={te('targetDSPlaceholder')} onChange={handleImportTargetDSChange} />
              </Form.Item>
            </Col>
            <Col span={12}>
              {importTargetDS && (importTargetDS.type === 'sqlserver' || importTargetDS.type === 'mssql' || importTargetDS.type === 'postgres' || importTargetDS.type === 'postgresql') && (
                <Form.Item name="import_target_database" label={te('targetDatabase')} rules={[{ required: true }]}>
                  <Select placeholder={te('selectDatabase')} options={importDatabases.map(d => ({ label: d, value: d }))} onChange={handleImportDBChange} />
                </Form.Item>
              )}
            </Col>
          </Row>
          <Form.Item name="import_target_schema" label={te('targetSchema')} rules={[{ required: true }]}>
            <Select placeholder={te('selectSchemaPlaceholder')} options={importSchemas.map(s => ({ label: s, value: s }))} showSearch optionFilterProp="label" />
          </Form.Item>

          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="import_content" label={te('importContent')}>
                <Radio.Group>
                  <Radio.Button value="all">{te('structureAndData')}</Radio.Button>
                  <Radio.Button value="structure">{te('structureOnly')}</Radio.Button>
                  <Radio.Button value="data">{te('dataOnly')}</Radio.Button>
                </Radio.Group>
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="import_strategy" label={te('importStrategy')}>
                <Select options={[
                  { label: te('strategyFail'), value: 'fail' },
                  { label: te('strategySkip'), value: 'skip' },
                  { label: te('strategyReplace'), value: 'replace' },
                ]} />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="skip_safety_check" valuePropName="checked">
            <Checkbox>{te('skipSafetyCheck')}</Checkbox>
          </Form.Item>
        </Form>
      </Modal>

      {/* New components */}
      <ExecutionLogDrawer
        open={logDrawerOpen}
        taskId={logTaskId}
        execId={logExecId}
        onClose={() => setLogDrawerOpen(false)}
      />
      <ManifestDrawer
        open={manifestDrawerOpen}
        execId={manifestExecId}
        onClose={() => setManifestDrawerOpen(false)}
      />
      <ImportFromExportModal
        open={importFromExportOpen}
        onClose={() => setImportFromExportOpen(false)}
        onSuccess={fetchData}
        preselectTaskId={preselectImportTaskId}
      />
      <Drawer title={te('taskDetailTitle', { name: taskDetail?.name || '' })} open={taskDetailOpen} onClose={() => setTaskDetailOpen(false)} size="large">
        {taskDetail && (() => {
          const dsName = dataSources.find(d => d.id === taskDetail.data_source_id)?.name || taskDetail.data_source_id;
          const isImport = taskDetail.task_type === 'import';
          const importSource = taskDetail.import_source || '';
          const srcDSName = dataSources.find(d => d.id === taskDetail.source_ds_id)?.name || taskDetail.source_ds_id || '';
          return (
            <table style={{ fontSize: 14, lineHeight: 2.5, borderCollapse: 'collapse' }}>
              <tbody>
                <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('taskType')}</td><td style={{ paddingLeft: 4, fontWeight: 'bold' }}>{isImport ? te('importType') : te('exportType')}</td></tr>

                {isImport ? (<>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('importMethod')}</td><td style={{ paddingLeft: 4 }}>{importSource === 'db2db' ? te('methodDb2db') : importSource === 'storage' ? te('methodStorage') : te('methodUpload')}</td></tr>
                  {importSource === 'db2db' && (<>
                    <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('sourceDS')}</td><td style={{ paddingLeft: 4 }}>{srcDSName || '-'}</td></tr>
                    {taskDetail.source_database && <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('sourceDatabase')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.source_database}</td></tr>}
                    {taskDetail.source_schema && <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('sourceSchema')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.source_schema}</td></tr>}
                    {taskDetail.source_tables && <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('sourceTables')}</td><td style={{ paddingLeft: 4 }}>{Array.isArray(taskDetail.source_tables) ? taskDetail.source_tables.join(', ') || te('wholeDB') : (taskDetail.source_tables || '-')}</td></tr>}
                  </>)}
                  {importSource === 'upload' && (<>
                    <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('uploadFile')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.import_file_name || '-'}</td></tr>
                    <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('storageProfileDetail')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.storage_profile || '-'}</td></tr>
                    <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('storagePath')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.import_file_path || '-'}</td></tr>
                  </>)}
                  {importSource === 'storage' && (<>
                    <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('fileName')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.import_file_name || '-'}</td></tr>
                    <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('storageProfileDetail')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.storage_profile || '-'}</td></tr>
                    <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('filePath')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.import_file_path || '-'}</td></tr>
                  </>)}
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('targetDS')}</td><td style={{ paddingLeft: 4 }}>{dsName || '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('targetDatabase')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.database_name || taskDetail.target_database || '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('targetSchema')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.schema_name || taskDetail.target_schema || '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('importStrategy')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.import_strategy === 'fail' ? te('strategyFail') : taskDetail.import_strategy === 'skip' ? te('strategySkip') : taskDetail.import_strategy === 'replace' ? te('strategyReplace') : taskDetail.import_strategy || '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('importContent')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.import_content === 'structure' ? te('structureOnly') : taskDetail.import_content === 'data' ? te('dataOnly') : te('structureAndData')}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('safetyCheck')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.skip_safety_check ? te('skipped') : te('normal')}</td></tr>
                </>) : (<>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('datasourceLabel')}</td><td style={{ paddingLeft: 4 }}>{dsName}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('databaseLabel2')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.database_name || '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>Schema</td><td style={{ paddingLeft: 4 }}>{taskDetail.schema_name || '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('exportScope')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.export_scope === 'schema' ? te('wholeDB') : te('selectedTables')}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('exportContent')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.export_content === 'structure' ? te('structureOnly') : taskDetail.export_content === 'data' ? te('dataOnly') : te('structureAndData')}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('exportFormat')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.export_format || '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('tableList')}</td><td style={{ paddingLeft: 4 }}>{taskDetailTables.length > 0 ? taskDetailTables.join(', ') : '-'}</td></tr>
                  <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('batchSize')}</td><td style={{ paddingLeft: 4 }}>{taskDetail.export_batch_size || 500}</td></tr>
                </>)}
                <tr><td style={{ textAlign: 'right', paddingRight: 12, whiteSpace: 'nowrap', color: '#666' }}>{te('createdAt')}</td><td style={{ paddingLeft: 4 }}>{fmtDate(taskDetail.created_at)}</td></tr>
              </tbody>
            </table>
          );
        })()}
      </Drawer>
    </div>
  );
};

export default ExportTasks;
