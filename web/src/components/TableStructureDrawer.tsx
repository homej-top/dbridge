import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Drawer, Tabs, Table, Button, Space, Tag, Descriptions, Empty, Popconfirm,
  message, Spin, Modal, Form, Input, Select, Tooltip, Typography, Alert,
} from 'antd';
import { ReloadOutlined, PlusOutlined, CopyOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons';
import Editor from '@monaco-editor/react';
import { tableAPI, dsAPI } from '../api';
import type { AlterChange } from '../api';
import ColumnFormModal, { type ColumnFormValues } from './ColumnFormModal';
import AlterTableModal from './AlterTableModal';
import { dialectOf } from '../utils/dialect';

const { Text } = Typography;

type Column = {
  name: string;
  type: string;
  length?: string;
  nullable: boolean;
  default: string;
  has_default: boolean;
  comment: string;
  key?: string;
  extra?: string;
  charset?: string;
  collation?: string;
};

type Index = {
  name: string;
  type: string;
  columns: string[];
  comment?: string;
};

type Constraint = {
  name: string;
  type: string;
  columns: string[];
  ref_table?: string;
  ref_columns?: string[];
  on_delete?: string;
  on_update?: string;
};

type TableMeta = {
  engine?: string;
  charset?: string;
  collation?: string;
  row_format?: string;
  comment?: string;
  row_count?: number;
  create_time?: string;
  update_time?: string;
};

type FullStructure = {
  columns: Column[];
  indexes: Index[];
  constraints: Constraint[];
  table_meta: TableMeta;
  ddl: string;
  is_view?: boolean;
};

type Props = {
  open: boolean;
  dataSourceId: string;
  dataSourceName: string;
  dbType: string;
  schema: string;
  table: string;
  isView?: boolean;
  readOnly?: boolean;
  database?: string;
  onClose: () => void;
  onRefreshTree?: () => void;
};

const TableStructureDrawer: React.FC<Props> = ({
  open, dataSourceId, dataSourceName, dbType, schema, table, isView, readOnly, database, onClose, onRefreshTree,
}) => {
  const { t: tr } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [structure, setStructure] = useState<FullStructure | null>(null);

  // Edit state
  const [pendingChanges, setPendingChanges] = useState<AlterChange[]>([]);
  const [columnModal, setColumnModal] = useState<{ open: boolean; mode: 'add' | 'edit'; init?: ColumnFormValues }>({
    open: false, mode: 'add',
  });
  const [indexModal, setIndexModal] = useState<{ open: boolean; mode: 'add' | 'edit'; init?: Index }>({ open: false, mode: 'add' });
  const [indexForm] = Form.useForm();
  const [commentModal, setCommentModal] = useState(false);
  const [commentForm] = Form.useForm();
  const [alterModal, setAlterModal] = useState(false);

  const canEdit = !readOnly && !isView;

  const load = async () => {
    if (!dataSourceId || !table) return;
    setLoading(true);
    try {
      const res = await tableAPI.structure({
        data_source_id: dataSourceId,
        schema: schema || undefined,
        table,
        database: database || undefined,
      });
      setStructure(res.data.data);
    } catch {
      // handled
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) {
      load();
      setPendingChanges([]);
    } else {
      setStructure(null);
    }
  }, [open, dataSourceId, schema, table]);

  const handleAddColumn = (values: ColumnFormValues) => {
    const change: AlterChange = {
      action: 'ADD_COLUMN',
      column: {
        name: values.name,
        type: values.type,
        length: values.length ? String(values.length) : undefined,
        nullable: values.nullable ?? true,
        default: values.default ?? '',
        has_default: values.has_default ?? false,
        comment: values.comment ?? '',
        after: values.after,
      },
    };
    setPendingChanges([...pendingChanges, change]);
    setColumnModal({ open: false, mode: 'add' });
    message.success(`已加入变更队列: 新增列 ${values.name}`);
  };

  const handleEditColumn = (values: ColumnFormValues) => {
    const changes: AlterChange[] = [{
      action: 'MODIFY_COLUMN',
      column: {
        name: values.name,
        type: values.type,
        length: values.length ? String(values.length) : undefined,
        nullable: values.nullable,
        default: values.default ?? '',
        has_default: values.has_default ?? false,
        comment: values.comment ?? '',
      },
    }];
    // Handle primary key toggle
    const origCol = structure?.columns?.find(c => c.name === values.name);
    const wasPK = origCol?.key === 'PRI';
    if (values.key && !wasPK) {
      // PK columns must be NOT NULL
      if (changes[0].column) changes[0].column.nullable = false;
      changes.push({ action: 'ADD_CONSTRAINT', index: { name: `pk_${table || values.name}`, type: 'PRIMARY KEY', columns: [values.name] } });
    } else if (!values.key && wasPK) {
      changes.push({ action: 'DROP_CONSTRAINT', index: { name: 'PRIMARY' } });
    }
    setPendingChanges([...pendingChanges, ...changes]);
    setColumnModal({ open: false, mode: 'edit' });
    message.success(`已加入变更队列: 修改列 ${values.name}`);
  };

  const handleDropColumn = (name: string) => {
    setPendingChanges([...pendingChanges, { action: 'DROP_COLUMN', column: { name } }]);
    message.success(`已加入变更队列: 删除列 ${name}`);
  };

  const handleDropIndex = (name: string) => {
    setPendingChanges([...pendingChanges, { action: 'DROP_INDEX', index: { name } }]);
    message.success(`已加入变更队列: 删除索引 ${name}`);
  };

  const handleAddIndex = () => {
    indexForm.validateFields().then((vals) => {
      const cols = (vals.columns as string).split(',').map((s) => s.trim()).filter(Boolean);
      if (indexModal.mode === 'edit' && indexModal.init) {
        const init = indexModal.init;
        const sameCols = JSON.stringify(init.columns) === JSON.stringify(cols);
        const sameName = init.name === vals.name;
        const sameType = (init.type || 'INDEX') === (vals.type || 'INDEX');
        // If only comment changed, just update comment (no DROP+ADD)
        if (sameName && sameCols && sameType && init.comment !== vals.comment) {
          setPendingChanges([...pendingChanges, {
            action: 'INDEX_COMMENT',
            index: { name: vals.name, comment: vals.comment || '' },
          }]);
          message.success(`已加入变更队列: 修改索引注释 ${vals.name}`);
        } else {
          // Edit existing index: drop old, add new
          setPendingChanges([...pendingChanges,
            { action: 'DROP_INDEX', index: { name: init.name } },
            { action: 'ADD_INDEX', index: { name: vals.name, type: vals.type || 'INDEX', columns: cols, comment: vals.comment || '' } },
          ]);
          message.success(`已加入变更队列: 修改索引 ${vals.name}`);
        }
      } else {
        setPendingChanges([...pendingChanges, {
          action: 'ADD_INDEX',
          index: { name: vals.name, type: vals.type || 'INDEX', columns: cols, comment: vals.comment || '' },
        }]);
        message.success(`已加入变更队列: 新增索引 ${vals.name}`);
      }
      setIndexModal({ open: false, mode: 'add' });
      indexForm.resetFields();
    });
  };

  const handleEditIndex = (idx: Index) => {
    indexForm.setFieldsValue({
      name: idx.name,
      type: idx.type || 'INDEX',
      columns: (idx.columns || []).join(', '),
      comment: idx.comment || '',
    });
    setIndexModal({ open: true, mode: 'edit', init: idx });
  };

  const handleTableComment = () => {
    commentForm.validateFields().then((vals) => {
      setPendingChanges([...pendingChanges, { action: 'TABLE_COMMENT', comment: vals.comment || '' }]);
      setCommentModal(false);
      message.success('已加入变更队列: 修改表注释');
    });
  };

  const handleApply = () => {
    if (pendingChanges.length === 0) {
      message.info('暂无待应用的变更');
      return;
    }
    setAlterModal(true);
  };

  const handleAlterSuccess = () => {
    setAlterModal(false);
    setPendingChanges([]);
    load();
    if (onRefreshTree) onRefreshTree();
  };

  const handleCopyDDL = () => {
    if (!structure?.ddl) return;
    navigator.clipboard.writeText(structure.ddl);
    message.success(tr('dbManage.copySuccess'));
  };

  const renderColumns = () => {
    if (!structure) return null;
    if (structure.is_view) {
      return <Alert type="info" showIcon message="视图不支持字段编辑" />;
    }
    const columns: any[] = [
      { title: tr('dbManage.fieldName'), dataIndex: 'name', key: 'name', render: (v: string, r: Column) => (
        <Space>
          <Text strong>{v}</Text>
          {r.extra === 'auto_increment' && <Tag color="purple">AUTO</Tag>}
        </Space>
      )},
      { title: tr('dbManage.tableType'), key: 'type', render: (_: unknown, r: Column) => <Text code>{r.type}</Text> },
      { title: tr('dbManage.length'), key: 'length', render: (_: unknown, r: Column) => r.length && /^\d+$/.test(r.length) ? <Text code>{r.length}</Text> : <Text type="secondary">-</Text> },
      { title: tr('dbManage.key'), dataIndex: 'key', key: 'key', render: (v: string) => {
        if (v === 'PRI') return <Tag color="gold">PRI</Tag>;
        if (v === 'UNI') return <Tag color="blue">UNI</Tag>;
        if (v === 'MUL') return <Tag>MUL</Tag>;
        return <Text type="secondary">-</Text>;
      } },
      { title: tr('dbManage.nullable'), dataIndex: 'nullable', key: 'nullable', render: (v: boolean) => v ? <Tag>YES</Tag> : <Tag color="red">NO</Tag> },
      { title: tr('dbManage.defaultValue'), key: 'default', render: (_: unknown, r: Column) => r.has_default ? (r.default === '' ? <Text type="secondary">{tr('dbManage.emptyString')}</Text> : <Text code>{r.default}</Text>) : <Text type="secondary">-</Text> },
      { title: tr('dbManage.comment'), dataIndex: 'comment', key: 'comment', ellipsis: true, render: (v: string) => v || <Text type="secondary">-</Text> },
    ];
    if (canEdit) {
      columns.push({
        title: tr('datasource.tableAction'), key: 'action', width: 140,
        render: (_: unknown, r: Column) => (
          <Space size={4}>
            <Tooltip title={tr('dbManage.editColumn')}>
              <Button size="small" type="link" icon={<EditOutlined />} onClick={() => setColumnModal({
                open: true, mode: 'edit',
                init: {
                  name: r.name, type: r.type, length: r.length,
                  nullable: r.nullable, default: r.default, has_default: r.has_default, comment: r.comment,
                  key: r.key === 'PRI',
                },
              })} />
            </Tooltip>
            <Popconfirm title={`确认删除列 "${r.name}" ? 数据将永久丢失`} onConfirm={() => handleDropColumn(r.name)} okText="确认" cancelText="取消" okButtonProps={{ danger: true }}>
              <Button size="small" type="link" danger icon={<DeleteOutlined />} />
            </Popconfirm>
          </Space>
        ),
      });
    }
    return (
      <Table
        dataSource={structure.columns || []}
        columns={columns}
        rowKey="name"
        size="small"
        pagination={false}
        scroll={{ y: 500 }}
      />
    );
  };

  const renderIndexes = () => {
    if (!structure) return null;
    const columns: any[] = [
      { title: tr('dbManage.indexName'), dataIndex: 'name', key: 'name', render: (v: string) => <Text strong>{v}</Text> },
      { title: tr('dbManage.tableType'), dataIndex: 'type', key: 'type', render: (v: string) => {
        const color = v === 'PRIMARY' ? 'gold' : v === 'UNIQUE' ? 'blue' : 'default';
        return <Tag color={color}>{v}</Tag>;
      }},
      { title: tr('dbManage.columns'), dataIndex: 'columns', key: 'columns', render: (cols: string[]) => cols?.join(', ') || '-' },
      { title: tr('dbManage.comment'), dataIndex: 'comment', key: 'comment', render: (v: string) => v || '-' },
    ];
    if (canEdit) {
      columns.push({
        title: tr('datasource.tableAction'), key: 'action', width: 80,
        render: (_: unknown, r: Index) => (
          <Space size={0}>
            <Tooltip title="编辑"><Button size="small" type="link" icon={<EditOutlined />} onClick={() => handleEditIndex(r)} /></Tooltip>
            <Popconfirm title={`确认删除索引 "${r.name}" ?`} onConfirm={() => handleDropIndex(r.name)} okButtonProps={{ danger: true }}>
              <Button size="small" type="link" danger icon={<DeleteOutlined />} />
            </Popconfirm>
          </Space>
        ),
      });
    }
    return (
      <Table dataSource={structure.indexes || []} columns={columns} rowKey="name" size="small" pagination={false} />
    );
  };

  const renderConstraints = () => {
    if (!structure) return null;
    const columns: any[] = [
      { title: tr('dbManage.constraintName'), dataIndex: 'name', key: 'name', render: (v: string) => <Text strong>{v}</Text> },
      { title: tr('dbManage.tableType'), dataIndex: 'type', key: 'type', render: (v: string) => <Tag>{v}</Tag> },
      { title: tr('dbManage.columns'), dataIndex: 'columns', key: 'columns', render: (cols: string[]) => cols?.join(', ') || '-' },
      { title: tr('dbManage.refTable'), dataIndex: 'ref_table', key: 'ref_table', render: (v: string) => v || '-' },
      { title: tr('dbManage.refColumns'), dataIndex: 'ref_columns', key: 'ref_columns', render: (cols: string[]) => cols?.join(', ') || '-' },
      { title: 'ON DELETE', dataIndex: 'on_delete', key: 'on_delete', render: (v: string) => v || '-' },
      { title: 'ON UPDATE', dataIndex: 'on_update', key: 'on_update', render: (v: string) => v || '-' },
    ];
    return (
      <Table dataSource={structure.constraints || []} columns={columns} rowKey="name" size="small" pagination={false} />
    );
  };

  const renderDDL = () => {
    if (!structure) return null;
    return (
      <div style={{ position: 'relative' }}>
        <Button
          icon={<CopyOutlined />}
          size="small"
          onClick={handleCopyDDL}
          style={{ position: 'absolute', top: 8, right: 8, zIndex: 1 }}
        >
          复制
        </Button>
        <div style={{ border: '1px solid #d9d9d9', borderRadius: 4 }}>
          <Editor
            height={500}
            defaultLanguage="sql"
            value={structure.ddl || '-- 无 DDL'}
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              wordWrap: 'on',
              readOnly: true,
              scrollBeyondLastLine: false,
            }}
          />
        </div>
      </div>
    );
  };

  const renderMeta = () => {
    if (!structure) return null;
    const m = structure.table_meta || {};
    const isMySQL = dbType === 'mysql' || dbType === 'mariadb' || dbType === 'oceanbase';
    return (
      <>
        <Descriptions bordered size="small" column={2}>
          {isMySQL && <Descriptions.Item label="引擎">{m.engine || '-'}</Descriptions.Item>}
          {isMySQL && <Descriptions.Item label="字符集">{m.charset || '-'}</Descriptions.Item>}
          {isMySQL && <Descriptions.Item label="排序规则">{m.collation || '-'}</Descriptions.Item>}
          {isMySQL && <Descriptions.Item label="行格式">{m.row_format || '-'}</Descriptions.Item>}
          <Descriptions.Item label="行数">{m.row_count ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="表注释">{m.comment || '-'}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{m.create_time || '-'}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{m.update_time || '-'}</Descriptions.Item>
        </Descriptions>
        {canEdit && (
          <div style={{ marginTop: 16 }}>
            <Button onClick={() => { commentForm.setFieldsValue({ comment: m.comment || '' }); setCommentModal(true); }}>
              修改表注释
            </Button>
          </div>
        )}
      </>
    );
  };

  const tabItems = [
    { key: 'columns', label: `${tr('dbManage.fields')} (${structure?.columns?.length || 0})`, children: renderColumns() },
    { key: 'indexes', label: `${tr('dbManage.indexes')} (${structure?.indexes?.length || 0})`, children: renderIndexes() },
    { key: 'constraints', label: `${tr('dbManage.constraints')} (${structure?.constraints?.length || 0})`, children: renderConstraints() },
    { key: 'ddl', label: tr('dbManage.ddl'), children: renderDDL() },
    { key: 'meta', label: tr('dbManage.tableInfo'), children: renderMeta() },
  ];

  const drawerTitle = (
    <Space>
      <Text strong>{dataSourceName}</Text>
      <Text type="secondary">·</Text>
      <Text>{schema ? `${schema}.${table}` : table}</Text>
      <Text type="secondary">· {tr('dbManage.tableStructure')}</Text>
    </Space>
  );

  const extraActions = (
    <Space>
      <Tooltip title={tr('dbManage.refresh')}>
        <Button size="small" icon={<ReloadOutlined />} onClick={load} loading={loading} />
      </Tooltip>
      {canEdit && (
        <>
          <Tooltip title={tr('dbManage.addColumn')}>
            <Button size="small" icon={<PlusOutlined />} onClick={() => setColumnModal({ open: true, mode: 'add' })}>
              {tr('dbManage.column')}
            </Button>
          </Tooltip>
          <Tooltip title={tr('dbManage.addIndex')}>
            <Button size="small" icon={<PlusOutlined />} onClick={() => { indexForm.resetFields(); setIndexModal({ open: true, mode: 'add', init: undefined }); }}>
              {tr('dbManage.index')}
            </Button>
          </Tooltip>
          {pendingChanges.length > 0 && (
            <Button type="primary" onClick={handleApply}>
              应用变更 ({pendingChanges.length})
            </Button>
          )}
        </>
      )}
    </Space>
  );

  return (
    <>
      <Drawer
        title={drawerTitle}
        open={open}
        onClose={onClose}
        styles={{ wrapper: { width: 960 } }}
        zIndex={1000}
        extra={extraActions}
        destroyOnHidden
      >
        <Spin spinning={loading}>
          {!structure ? (
            <Empty description="暂无数据" />
          ) : (
            <>
              {pendingChanges.length > 0 && (
                <Alert
                  type="info"
                  showIcon
                  message={`当前有 ${pendingChanges.length} 条待应用变更`}
                  description={
                    <ul style={{ margin: 0, paddingLeft: 20 }}>
                      {pendingChanges.map((c, i) => (
                        <li key={i}>
                          <Tag>{c.action}</Tag>
                          {c.column?.name || c.index?.name || (c.action === 'TABLE_COMMENT' ? '表注释' : '')}
                        </li>
                      ))}
                    </ul>
                  }
                  action={<Button size="small" onClick={handleApply}>应用</Button>}
                  style={{ marginBottom: 16 }}
                />
              )}
              <Tabs defaultActiveKey="columns" items={tabItems} />
            </>
          )}
        </Spin>
      </Drawer>

      <ColumnFormModal
        open={columnModal.open}
        mode={columnModal.mode}
        dbType={dbType}
        dataSourceId={dataSourceId}
        initialValues={columnModal.init}
        existingColumns={structure?.columns?.map((c) => c.name) || []}
        onCancel={() => setColumnModal({ open: false, mode: 'add' })}
        onSubmit={columnModal.mode === 'add' ? handleAddColumn : handleEditColumn}
      />

      <Modal
        title={indexModal.mode === 'edit' ? '编辑索引' : tr('dbManage.addIndex')}
        open={indexModal.open}
        onOk={handleAddIndex}
        onCancel={() => { setIndexModal({ open: false, mode: 'add' }); indexForm.resetFields(); }}
        zIndex={1050}
      >
        <Form form={indexForm} layout="vertical">
          <Form.Item name="name" label={tr('dbManage.indexName')} rules={[{ required: true }]}>
            <Input placeholder="idx_xxx" />
          </Form.Item>
          <Form.Item name="type" label={tr('dbManage.indexType')} initialValue="INDEX">
            <IndexTypeSelect dbType={dbType} dataSourceId={dataSourceId} />
          </Form.Item>
          <Form.Item name="columns" label={tr('dbManage.columnsComma')} rules={[{ required: true }]}>
            <Input placeholder="col1, col2" />
          </Form.Item>
          {dialectOf(dbType) !== 'oracle' && (
          <Form.Item name="comment" label={tr('dbManage.comment')}>
            <Input placeholder="索引备注" />
          </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title="修改表注释"
        open={commentModal}
        onOk={handleTableComment}
        onCancel={() => setCommentModal(false)}
        zIndex={1050}
      >
        <Form form={commentForm} layout="vertical">
          <Form.Item name="comment" label="注释">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>

      <AlterTableModal
        open={alterModal}
        dataSourceId={dataSourceId}
        schema={schema}
        database={database}
        table={table}
        changes={pendingChanges}
        onCancel={() => setAlterModal(false)}
        onSuccess={handleAlterSuccess}
      />
    </>
  );
};

// IndexTypeSelect fetches index types from the API based on data source
const IndexTypeSelect: React.FC<{ dbType: string; dataSourceId: string; value?: string; onChange?: (v: string) => void }> = ({ dbType: _dbType, dataSourceId, value, onChange }) => {
  const [types, setTypes] = useState<{ label: string; value: string }[]>([]);
  useEffect(() => {
    if (dataSourceId) {
      dsAPI.indexTypes(dataSourceId).then(res => {
        const data = res.data?.data;
        if (Array.isArray(data) && data.length > 0) {
          setTypes(data.map((t: any) => ({ label: t.description ? `${t.name} - ${t.description}` : t.name, value: t.name })));
        } else {
          // Fallback defaults
          setTypes([
            { label: '普通索引', value: 'INDEX' },
            { label: '唯一索引', value: 'UNIQUE' },
          ]);
        }
      }).catch(() => setTypes([{ label: 'INDEX', value: 'INDEX' }, { label: 'UNIQUE', value: 'UNIQUE' }]));
    }
  }, [dataSourceId]);
  return <Select options={types} value={value} onChange={onChange} />;
};

export default TableStructureDrawer;
