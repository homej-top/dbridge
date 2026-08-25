import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Table,
  Button,
  Modal,
  Form,
  Input,
  Select,
  InputNumber,
  Space,
  Popconfirm,
  message,
  Tag,
  Upload,
  Alert,
  Tooltip,
} from 'antd';
import {
  PlusOutlined,
  ThunderboltOutlined,
  DownloadOutlined,
  UploadOutlined,
  CodeOutlined,
} from '@ant-design/icons';
import { Link } from 'react-router-dom';
import { dsAPI } from '../api';
import type { DataSource, DataSourceForm } from '../types';

const DB_TYPES = [
  { label: 'MySQL', value: 'mysql' },
  { label: 'MariaDB', value: 'mariadb' },
  { label: 'PostgreSQL', value: 'postgres' },
  { label: 'SQL Server', value: 'sqlserver' },
  { label: 'OceanBase', value: 'oceanbase' },
  { label: 'Oracle', value: 'oracle' },
  { label: 'SQLite', value: 'sqlite' },
];

const DataSources: React.FC = () => {
  const [data, setData] = useState<DataSource[]>([]);
  const { t: tr } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingIsSystem, setEditingIsSystem] = useState(false);
  const [testing, setTesting] = useState(false);
  const [form] = Form.useForm<DataSourceForm>();
  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importing, setImporting] = useState(false);
  const [importResult, setImportResult] = useState<{
    total: number; success: number; skip: number; errors: string[];
  } | null>(null);
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importPassword, setImportPassword] = useState('');
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const [exportPassword, setExportPassword] = useState('');
  const [exporting, setExporting] = useState(false);

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = await dsAPI.list();
      setData(res.data.data?.list || []);
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  const handleOpenModal = (record?: DataSource) => {
    if (record) {
      form.resetFields();
      setEditingId(record.id);
      setEditingIsSystem(!!record.is_system);
      // Parse Oracle extra_config
      let oracleConnectMode = 'service_name';
      let oracleRole = 'default';
      let oracleService = '';
      if (record.type === 'oracle' && record.extra_config) {
        try {
          const extra = JSON.parse(record.extra_config);
          oracleConnectMode = extra.connect_mode || 'service_name';
          oracleRole = extra.role || 'default';
          oracleService = extra.oracle_service || '';
        } catch {}
      }
      form.setFieldsValue({
        name: record.name,
        type: record.type,
        host: record.host,
        port: record.port,
        database: record.database,
        username: record.username,
        ssl_mode: record.ssl_mode,
        tags: record.tags ? record.tags.split(',').filter(Boolean) : [],
        oracle_connect_mode: oracleConnectMode,
        oracle_role: oracleRole,
        oracle_service: oracleService,
      } as any);
    } else {
      setEditingId(null);
      setEditingIsSystem(false);
      form.resetFields();
    }
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    const values = await form.validateFields();
    // Convert tags array to comma-separated string
    if (Array.isArray(values.tags)) {
      values.tags = values.tags.join(',');
    }
    // When editing, remove empty password to avoid backend rejection
    if (editingId && (!values.password || values.password.trim() === '')) {
      delete (values as any).password;
    }
    // Build Oracle extra_config JSON
    if (values.type === 'oracle') {
      const oracleService = (values as any).oracle_service || '';
      const oracleMode = (values as any).oracle_connect_mode || 'service_name';
      const oracleRole = (values as any).oracle_role || 'default';
      (values as any).extra_config = JSON.stringify({
        oracle_service: oracleService,
        connect_mode: oracleMode,
        role: oracleRole,
      });
    }
    try {
      if (editingId) {
        await dsAPI.update(editingId, values);
        message.success(tr('common.updateSuccess'));
      } else {
        await dsAPI.create(values);
        message.success(tr('common.createSuccess'));
      }
      setModalOpen(false);
      fetchData();
    } catch {
      // handled
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await dsAPI.delete(id);
      message.success(tr('common.deleteSuccess'));
      fetchData();
    } catch {
      // handled
    }
  };

  const handleTest = async () => {
    const values = await form.validateFields();
    setTesting(true);
    try {
      const payload = editingId ? { ...values, id: editingId } : values;
      const res = await dsAPI.test(payload);
      if (res.data.data?.connected) {
        message.success(tr('datasource.connSuccess'));
      } else {
        message.error(res.data.data?.error || tr('datasource.connFailed'));
      }
    } catch {
      // handled
    } finally {
      setTesting(false);
    }
  };

  const handleExport = () => {
    setExportPassword('');
    setExportModalOpen(true);
  };

  const handleExportConfirm = async () => {
    if (!exportPassword) {
      message.warning(tr('datasource.requireExportPwd'));
      return;
    }
    setExporting(true);
    try {
      const res = await dsAPI.export(exportPassword);
      // Extract the data array from the API response envelope
      const items = JSON.parse(res.data?.data || '[]');
      const blob = new Blob([JSON.stringify(items)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `datasources_${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      URL.revokeObjectURL(url);
      setExportModalOpen(false);
      message.success(tr('datasource.exportSuccess'));
    } catch {
      message.error(tr('datasource.exportFailed'));
    } finally {
      setExporting(false);
    }
  };

  const handleImportConfirm = async () => {
    if (!importFile) {
      message.warning(tr('datasource.selectJsonFile'));
      return;
    }
    if (!importPassword) {
      message.warning(tr('datasource.requireExportPwd'));
      return;
    }
    setImporting(true);
    setImportResult(null);
    try {
      const text = await importFile.text();
      const items = JSON.parse(text);
      if (!Array.isArray(items)) {
        message.error(tr('datasource.jsonFormatError'));
        setImporting(false);
        return;
      }
      const res = await dsAPI.import(items, importPassword);
      const result = res.data.data;
      setImportResult(result);
      if (result.success > 0) {
        message.success(`成功导入 ${result.success} 条数据源`);
        fetchData();
      }
      if (result.errors?.length > 0) {
        message.warning(`${result.errors.length} 条导入失败`);
      }
    } catch (err: any) {
      message.error(err?.response?.data?.message || tr('datasource.importFailed'));
    } finally {
      setImporting(false);
    }
  };

  const columns = [
    {
      title: tr('datasource.tableName'),
      dataIndex: 'name',
      key: 'name',
      render: (name: string, r: DataSource) => (
        <Space size={4}>
          <span>{name}</span>
          {r.is_system && <Tag color="gold">{tr('datasource.systemDataSource')}</Tag>}
        </Space>
      ),
    },
    {
      title: tr('datasource.tableType'),
      dataIndex: 'type',
      key: 'type',
      render: (type: string) => {
        const colors: Record<string, string> = {
          mysql: 'blue',
          mariadb: 'cyan',
          postgres: 'green',
          sqlserver: 'purple',
          oceanbase: 'geekblue',
          oracle: 'orange',
        };
        return <Tag color={colors[type] || 'default'}>{type.toUpperCase()}</Tag>;
      },
    },
    { title: '主机:端口', key: 'address', width: 200, render: (_: any, r: DataSource) => `${r.host}:${r.port}` },
    { title: tr('datasource.tableDatabase'), dataIndex: 'database', key: 'database' },
    { title: '用户名', dataIndex: 'username', key: 'username', width: 130 },
    { title: '环境', dataIndex: 'env', key: 'env', width: 80,
      render: (v: string) => {
        const envMap: Record<string, { color: string; label: string }> = { dev: { color: 'green', label: '开发' }, test: { color: 'orange', label: '测试' }, prod: { color: 'red', label: '生产' } };
        const e = envMap[v] || { color: 'default', label: v || 'dev' };
        return <Tag color={e.color}>{e.label}</Tag>;
      }},
    { title: '功能标签', dataIndex: 'tags', key: 'tags', width: 180,
      render: (v: string) => v ? v.split(',').map((t: string) => <Tag key={t} style={{ marginBottom: 2 }}>{t === 'data_query' ? '数据查询' : t}</Tag>) : <span style={{ color: '#ccc' }}>-</span> },
    {
      title: tr('datasource.tableAction'),
      key: 'action',
      width: 200,
      render: (_: any, record: DataSource) => (
        <Space size={8}>
          <Link to={`/query?ds=${encodeURIComponent(record.id)}`} style={{ color: '#20a53a' }}>
            <CodeOutlined /> {tr('datasource.query')}
          </Link>
          <span style={{ color: '#ddd' }}>|</span>
          <a
            style={{ color: '#20a53a' }}
            onClick={() => handleOpenModal(record)}
          >
            {tr('datasource.edit')}
          </a>
          <span style={{ color: '#ddd' }}>|</span>
          {record.is_system ? (
            <Tooltip title={tr('datasource.cannotDeleteSystem')}>
              <span style={{ color: '#ccc', cursor: 'not-allowed' }}>{tr('datasource.delete')}</span>
            </Tooltip>
          ) : (
            <Popconfirm
              title={tr('datasource.deleteConfirm')}
              onConfirm={() => handleDelete(record.id)}
              okText={tr('common.okText')}
              cancelText={tr('common.cancelText')}
            >
              <a style={{ color: '#e74c3c' }}>{tr('datasource.delete')}</a>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0 }}>{tr('nav.datasources')}</h2>
        <Space>
          <Button icon={<DownloadOutlined />} onClick={handleExport}>
            {tr('common.export')}
          </Button>
          <Button icon={<UploadOutlined />} onClick={() => { setImportResult(null); setImportFile(null); setImportPassword(''); setImportModalOpen(true); }}>
            {tr('common.import')}
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => handleOpenModal()}
          >
            {tr('datasource.add')}
          </Button>
        </Space>
      </div>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        pagination={{
          showSizeChanger: true,
          showTotal: (total) => `${tr('common.total')} ${total} ${tr('common.rows')}`,
          pageSizeOptions: ['10', '20', '50'],
        }}
      />

      <Modal
        title={editingId ? tr('datasource.edit') : tr('datasource.add')}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
        cancelText={tr('common.cancelText')}
        footer={[
          <Button key="test" icon={<ThunderboltOutlined />} loading={testing} onClick={handleTest}>
            {tr('datasource.testConn')}
          </Button>,
          <Button key="back" onClick={() => setModalOpen(false)}>
            {tr('common.cancelText')}
          </Button>,
          <Button key="submit" type="primary" onClick={handleSubmit}>
            {editingId ? tr('common.update') : tr('common.create')}
          </Button>,
        ]}
      >
        <Form form={form} layout="vertical">
          {editingIsSystem && (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 16 }}
              message={tr('datasource.systemDataSourceEditHint')}
            />
          )}
          <Form.Item
            name="name"
            label={tr('datasource.name')}
            rules={[{ required: true, message: tr('datasource.requireName') }]}
          >
            <Input placeholder={tr('datasource.namePlaceholder')} />
          </Form.Item>
          <Form.Item
            name="type"
            label={tr('datasource.tableType')}
            rules={[{ required: true, message: tr('datasource.selectType') }]}
          >
            <Select
              options={DB_TYPES}
              placeholder={tr('datasource.dbTypePlaceholder')}
              disabled={editingIsSystem}
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate>
            {({ getFieldValue }) => getFieldValue('type') !== 'sqlite' ? (
              <>
          <Form.Item
            name="host"
            label={tr('datasource.tableHost')}
            rules={[{ required: true, message: tr('datasource.requireHost') }]}
          >
            <Input placeholder={tr('datasource.hostPlaceholder')} />
          </Form.Item>
          <Form.Item
            name="port"
            label={tr('datasource.tablePort')}
            rules={[{ required: true, message: tr('datasource.requirePort') }]}
          >
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>
              </>
            ) : null}
          </Form.Item>
          <Form.Item
            name="database"
            label={tr('datasource.tableDatabase')}
            tooltip={tr('datasource.databaseTooltip')}
          >
            <Input placeholder={tr('datasource.databasePlaceholder')} />
          </Form.Item>
          <Form.Item name="tags" label="功能标签" tooltip="选择数据源在哪些功能页面中可用">
            <Select mode="multiple" placeholder="选择功能标签（可多选，不选则不出现在任何功能页）"
              options={[
                { label: '数据查询（SQL 编辑器）', value: 'data_query' },
              ]} />
          </Form.Item>
          <Form.Item name="env" label="环境" tooltip="dev=开发宽松，prod=生产严格" initialValue="dev">
            <Select options={[
              { label: '🟢 开发 (dev)', value: 'dev' },
              { label: '🟡 测试 (test)', value: 'test' },
              { label: '🔴 生产 (prod)', value: 'prod' },
            ]} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate>
            {({ getFieldValue }) => getFieldValue('type') !== 'sqlite' ? (
              <>
          <Form.Item
            name="username"
            label={tr('datasource.username')}
            rules={[{ required: true, message: tr('datasource.requireUsername') }]}
          >
            <Input placeholder={tr('datasource.usernamePlaceholder')} />
          </Form.Item>
          <Form.Item
            name="password"
            label={tr('datasource.password')}
            rules={[
              { required: !editingId, message: tr('datasource.requirePassword') },
              ...(editingId ? [] : [{ min: 6, message: tr('datasource.passwordMinLen') }]),
            ]}
          >
            <Input.Password placeholder={tr('datasource.password')} />
          </Form.Item>
              </>
            ) : null}
          </Form.Item>
          <Form.Item noStyle shouldUpdate>
            {({ getFieldValue }) =>
              getFieldValue('type') === 'oracle' ? (
                <>
                  <Form.Item
                    name="oracle_service"
                    label={tr('datasource.oracleService')}
                    rules={[{ required: true, message: tr('datasource.requireOracleService') }]}
                  >
                    <Input placeholder={tr('datasource.oracleServicePlaceholder')} />
                  </Form.Item>
                  <Form.Item
                    name="oracle_connect_mode"
                    label={tr('datasource.oracleConnectMode')}
                    initialValue="service_name"
                  >
                    <Select
                      options={[
                        { label: tr('datasource.serviceName'), value: 'service_name' },
                        { label: tr('datasource.sid'), value: 'sid' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item
                    name="oracle_role"
                    label={tr('datasource.oracleRole')}
                    initialValue="default"
                  >
                    <Select
                      options={[
                        { label: tr('datasource.roleDefault'), value: 'default' },
                        { label: 'SYSDBA', value: 'sysdba' },
                        { label: 'SYSOPER', value: 'sysoper' },
                      ]}
                    />
                  </Form.Item>
                </>
              ) : getFieldValue('type') === 'sqlite' ? (
                <>
                  <Form.Item name="host" label="数据库文件路径"
                    tooltip="输入 .db 文件路径或通过上方上传按钮上传"
                    rules={[{ required: true, message: '请输入文件路径或上传文件' }]}>
                    <Input placeholder="/data/sqlite/mydb.db" />
                  </Form.Item>
                </>
              ) : null
            }
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={tr('datasource.exportDs')}
        open={exportModalOpen}
        onCancel={() => setExportModalOpen(false)}
        footer={[
          <Button key="cancel" onClick={() => setExportModalOpen(false)}>
            {tr('common.cancelText')}
          </Button>,
          <Button key="export" type="primary" loading={exporting} onClick={handleExportConfirm}>
            {tr('common.export')}
          </Button>,
        ]}
      >
        <p style={{ color: '#666', fontSize: 13, marginBottom: 12 }}>
          {tr('datasource.importDesc')}
        </p>
        <div style={{ marginBottom: 12 }}>
          <label style={{ display: 'block', marginBottom: 4, fontWeight: 500 }}>{tr('datasource.exportPassword')}</label>
          <Input.Password
            value={exportPassword}
            onChange={(e) => setExportPassword(e.target.value)}
            placeholder={tr('datasource.exportPasswordPlaceholder')}
          />
        </div>
      </Modal>

      <Modal
        title={tr('datasource.importDs')}
        open={importModalOpen}
        onCancel={() => setImportModalOpen(false)}
        footer={[
          <Button key="close" onClick={() => setImportModalOpen(false)}>
            {tr('common.close')}
          </Button>,
          <Button key="import" type="primary" loading={importing} onClick={handleImportConfirm}>
            {tr('common.import')}
          </Button>,
        ]}
      >
        <div style={{ marginBottom: 16 }}>
          <p style={{ color: '#666', fontSize: 13 }}>
            {tr('datasource.importDesc2')}
          </p>
          <Upload.Dragger
            accept=".json"
            maxCount={1}
            beforeUpload={(file) => { setImportFile(file); return false; }}
            onRemove={() => setImportFile(null)}
            fileList={importFile ? [{ uid: '-1', name: importFile.name, status: 'done' }] : []}
          >
            <p style={{ fontSize: 32, color: '#999', margin: '16px 0 8px' }}>
              <UploadOutlined />
            </p>
            <p style={{ fontSize: 14 }}>{tr('datasource.dragHint')}</p>
            <p style={{ fontSize: 12, color: '#999' }}>{tr('datasource.jsonOnly')}</p>
          </Upload.Dragger>
        </div>
        <div style={{ marginBottom: 16 }}>
          <label style={{ display: 'block', marginBottom: 4, fontWeight: 500 }}>{tr('datasource.exportPassword')}</label>
          <Input.Password
            value={importPassword}
            onChange={(e) => setImportPassword(e.target.value)}
            placeholder={tr('datasource.importPasswordPlaceholder')}
          />
        </div>
        {importing && <p style={{ color: '#1890ff' }}>{tr('datasource.importing')}</p>}
        {importResult && (
          <div style={{ marginTop: 12, padding: 12, background: '#f5f5f5', borderRadius: 6 }}>
            <p><strong>{tr('datasource.totalCount')}:</strong> {importResult.total} {tr('common.rows')}</p>
            <p style={{ color: '#52c41a' }}><strong>{tr('datasource.successCount')}:</strong> {importResult.success} {tr('common.rows')}</p>
            {importResult.skip > 0 && (
              <p style={{ color: '#faad14' }}><strong>{tr('datasource.skipCount')}:</strong> {importResult.skip} {tr('common.rows')}</p>
            )}
            {importResult.errors?.length > 0 && (
              <div style={{ marginTop: 8 }}>
                <strong style={{ color: '#e74c3c' }}>{tr('datasource.failDetail')}:</strong>
                <ul style={{ fontSize: 12, color: '#666', maxHeight: 150, overflow: 'auto', marginTop: 4 }}>
                  {importResult.errors.map((e, i) => (
                    <li key={i}>{e}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
};

export default DataSources;
