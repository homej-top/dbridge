import React, { useState, useMemo, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal, Form, Input, Select, Button, Space, Table, Checkbox, message } from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import Editor from '@monaco-editor/react';
import { queryAPI, dsAPI } from '../api';
import { dialectOf, getDialect } from '../utils/dialect';

interface ColumnTypeInfo {
  name: string;
  needs_length: boolean;
  needs_scale: boolean;
  description?: string;
}

const FALLBACK_TYPES: Record<string, ColumnTypeInfo[]> = {
  mysql: [{name:'BIGINT',needs_length:false,needs_scale:false},{name:'INT',needs_length:false,needs_scale:false},{name:'VARCHAR',needs_length:true,needs_scale:false},{name:'TEXT',needs_length:false,needs_scale:false},{name:'DECIMAL',needs_length:true,needs_scale:true},{name:'DATE',needs_length:false,needs_scale:false},{name:'DATETIME',needs_length:false,needs_scale:false},{name:'JSON',needs_length:false,needs_scale:false},{name:'BOOLEAN',needs_length:false,needs_scale:false}],
  postgres: [{name:'bigint',needs_length:false,needs_scale:false},{name:'integer',needs_length:false,needs_scale:false},{name:'serial',needs_length:false,needs_scale:false},{name:'varchar',needs_length:true,needs_scale:false},{name:'text',needs_length:false,needs_scale:false},{name:'numeric',needs_length:true,needs_scale:true},{name:'timestamp',needs_length:false,needs_scale:false},{name:'jsonb',needs_length:false,needs_scale:false},{name:'boolean',needs_length:false,needs_scale:false}],
  oracle: [{name:'NUMBER',needs_length:true,needs_scale:true},{name:'INTEGER',needs_length:false,needs_scale:false},{name:'VARCHAR2',needs_length:true,needs_scale:false},{name:'CLOB',needs_length:false,needs_scale:false},{name:'DATE',needs_length:false,needs_scale:false},{name:'TIMESTAMP',needs_length:false,needs_scale:false},{name:'BLOB',needs_length:false,needs_scale:false}],
  sqlserver: [{name:'int',needs_length:false,needs_scale:false},{name:'bigint',needs_length:false,needs_scale:false},{name:'varchar',needs_length:true,needs_scale:false},{name:'nvarchar',needs_length:true,needs_scale:false},{name:'text',needs_length:false,needs_scale:false},{name:'decimal',needs_length:true,needs_scale:true},{name:'datetime2',needs_length:false,needs_scale:false},{name:'date',needs_length:false,needs_scale:false},{name:'bit',needs_length:false,needs_scale:false}],
};

function getDefaultType(dbType: string): string {
  switch (dbType) {
    case 'mysql': case 'mariadb': case 'oceanbase': return 'BIGINT';
    case 'postgres': case 'postgresql': return 'bigserial';
    case 'oracle': return 'NUMBER';
    case 'sqlserver': return 'int';
    default: return 'BIGINT';
  }
}

function getDefaultStringType(dbType: string): string {
  switch (dbType) {
    case 'mysql': case 'mariadb': case 'oceanbase': return 'VARCHAR';
    case 'postgres': case 'postgresql': return 'varchar';
    case 'oracle': return 'VARCHAR2';
    case 'sqlserver': return 'nvarchar';
    default: return 'VARCHAR';
  }
}

type ColumnDef = {
  name: string;
  type: string;
  length: string;
  nullable: boolean;
  defaultValue: string;
  comment: string;
  _key: number;
};

type CreateTableModalProps = {
  open: boolean;
  dataSourceId: string;
  schema: string;
  dbType: string;
  database?: string;
  onClose: () => void;
  onSuccess: () => void;
};

const CreateTableModal: React.FC<CreateTableModalProps> = ({
  open, dataSourceId, schema, dbType, database, onClose, onSuccess,
}) => {
  const { t: tr } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [columnTypes, setColumnTypes] = useState<ColumnTypeInfo[]>([]);
  const defaultType = getDefaultType(dbType);
  const defaultStrType = getDefaultStringType(dbType);
  const tableName = Form.useWatch('tableName', form);
  const [columns, setColumns] = useState<ColumnDef[]>([
    { name: 'id', type: defaultType, length: '', nullable: false, defaultValue: '', comment: '', _key: Date.now() },
  ]);
  const [primaryKey, setPrimaryKey] = useState('id');
  const [autoIncrement, setAutoIncrement] = useState(true);

  // Fetch column types from API on open
  useEffect(() => {
    if (!open) return;
    if (dataSourceId) {
      dsAPI.columnTypes(dataSourceId)
        .then(res => {
          const data = res.data?.data;
          if (Array.isArray(data) && data.length > 0) {
            setColumnTypes(data);
          } else {
            setColumnTypes(FALLBACK_TYPES[dbType] || FALLBACK_TYPES['mysql']);
          }
        })
        .catch(() => setColumnTypes(FALLBACK_TYPES[dbType] || FALLBACK_TYPES['mysql']));
    }
  }, [open, dataSourceId, dbType]);

  // Reset columns when modal opens
  useEffect(() => {
    if (open) {
      setColumns([{ name: 'id', type: defaultType, length: '', nullable: false, defaultValue: '', comment: '', _key: Date.now() }]);
      setPrimaryKey('id');
      setAutoIncrement(true);
      form.resetFields();
    }
  }, [open]);

  const addColumn = () => {
    setColumns([...columns, { name: '', type: defaultStrType, length: '255', nullable: true, defaultValue: '', comment: '', _key: Date.now() }]);
  };

  const removeColumn = (idx: number) => {
    const next = columns.filter((_, i) => i !== idx);
    setColumns(next);
    if (primaryKey && !next.find(c => c.name === primaryKey)) {
      setPrimaryKey(next[0]?.name || '');
    }
  };

  const updateColumn = (idx: number, field: keyof ColumnDef, value: string | boolean) => {
    const next = [...columns];
    (next[idx] as any)[field] = value;
    // When type changes, clear length if the new type doesn't need it
    if (field === 'type') {
      const types = columnTypes.length > 0 ? columnTypes : FALLBACK_TYPES[dbType];
      const ti = types?.find(t => t.name.toLowerCase() === String(value).toLowerCase());
      if (ti && !ti.needs_length) {
        next[idx].length = '';
      }
    }
    setColumns(next);
  };

  const ddl = useMemo(() => {
    if (!tableName || columns.length === 0 || columns.some(c => !c.name)) return '';

    const gen = getDialect(dbType);
    const dialect = dialectOf(dbType);

    return gen.createTableDDL({
      schema,
      table: tableName,
      columns: columns.map(c => ({
        name: c.name,
        type: c.type,
        length: c.length || undefined,
        nullable: c.nullable,
        defaultValue: c.defaultValue || undefined,
        comment: c.comment || undefined,
      })),
      primaryKey: primaryKey || undefined,
      autoIncrement: dialect === 'mysql' ? autoIncrement : undefined,
      comment: form.getFieldValue('comment') || undefined,
      engine: dialect === 'mysql' ? (form.getFieldValue('engine') || 'InnoDB') : undefined,
      charset: dialect === 'mysql' ? (form.getFieldValue('charset') || 'utf8mb4') : undefined,
    });
  }, [columns, primaryKey, autoIncrement, schema, dbType, tableName, form]);

  const handleSubmit = async () => {
    try {
      await form.validateFields();
      if (!ddl) {
        message.warning('请填写表名和字段');
        return;
      }
      setLoading(true);
      await queryAPI.executeDDL({ data_source_id: dataSourceId, sql: ddl, schema, database });
      message.success('表创建成功');
      onSuccess();
      onClose();
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  };

  const typeOptions = columnTypes.map(t => ({ label: t.description ? `${t.name} - ${t.description}` : t.name, value: t.name }));

  const colColumns = [
    {
      title: tr('dbManage.fieldName'), dataIndex: 'name', width: 140,
      render: (_: any, __: any, idx: number) => (
        <Input size="small" value={columns[idx].name} onChange={e => updateColumn(idx, 'name', e.target.value)} />
      ),
    },
    {
      title: tr('dbManage.fieldType'), dataIndex: 'type', width: 130,
      render: (_: any, __: any, idx: number) => (
        <Select size="small" value={columns[idx].type} onChange={v => updateColumn(idx, 'type', v)}
          options={typeOptions} style={{ width: '100%' }}
          showSearch filterOption={(input, option) => (option?.label as string)?.toLowerCase().includes(input.toLowerCase())}
        />
      ),
    },
    {
      title: tr('dbManage.length'), dataIndex: 'length', width: 80,
      render: (_: any, __: any, idx: number) => {
        const types = columnTypes.length > 0 ? columnTypes : FALLBACK_TYPES[dbType];
        const ti = types?.find(t => t.name.toLowerCase() === columns[idx].type.toLowerCase());
        const needsLen = ti ? ti.needs_length : true;
        return (
          <Input size="small" value={columns[idx].length} disabled={!needsLen}
            onChange={e => updateColumn(idx, 'length', e.target.value)}
            placeholder={needsLen ? '' : '—'} />
        );
      },
    },
    {
      title: tr('dbManage.fieldComment') || 'Comment', dataIndex: 'comment', width: 100,
      render: (_: any, __: any, idx: number) => (
        <Input size="small" value={columns[idx].comment} onChange={e => updateColumn(idx, 'comment', e.target.value)} placeholder="" />
      ),
    },
    {
      title: tr('dbManage.nullable'), dataIndex: 'nullable', width: 50, align: 'center' as const,
      render: (_: any, __: any, idx: number) => (
        <Checkbox checked={columns[idx].nullable} onChange={e => updateColumn(idx, 'nullable', e.target.checked)} />
      ),
    },
    {
      title: tr('dbManage.defaultValue'), dataIndex: 'defaultValue', width: 100,
      render: (_: any, __: any, idx: number) => (
        <Input size="small" value={columns[idx].defaultValue} onChange={e => updateColumn(idx, 'defaultValue', e.target.value)} placeholder="如 NOW()" />
      ),
    },
    {
      title: '', dataIndex: 'action', width: 40, align: 'center' as const,
      render: (_: any, __: any, idx: number) => (
        columns.length > 1 ? (
          <Button type="text" danger size="small" icon={<DeleteOutlined />} onClick={() => removeColumn(idx)} />
        ) : null
      ),
    },
  ];

  return (
    <Modal
      title={tr('dbManage.createTable')}
      open={open}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={loading}
      okText={tr("common.create")}
      cancelText={tr("common.cancel")}
      width={800}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
        <Space style={{ width: '100%' }} size={16}>
          <Form.Item name="tableName" label={tr('dbManage.tableName')} rules={[{ required: true, message: tr('dbManage.tableNamePlaceholder') }, { pattern: /^[a-zA-Z_][a-zA-Z0-9_]*$/, message: tr('dbManage.invalidName') }]} style={{ flex: 1 }}>
            <Input placeholder={tr('dbManage.tableNamePlaceholder')} />
          </Form.Item>
          {dialectOf(dbType) === 'mysql' && (
            <>
              <Form.Item name="engine" label={tr('dbManage.engine')} initialValue="InnoDB" style={{ width: 120 }}>
                <Select options={[{ label: 'InnoDB', value: 'InnoDB' }, { label: 'MyISAM', value: 'MyISAM' }, { label: 'MEMORY', value: 'MEMORY' }]} />
              </Form.Item>
              <Form.Item name="charset" label={tr('dbManage.charset')} initialValue="utf8mb4" style={{ width: 120 }}>
                <Select options={[{ label: 'utf8mb4', value: 'utf8mb4' }, { label: 'utf8', value: 'utf8' }, { label: 'latin1', value: 'latin1' }]} />
              </Form.Item>
            </>
          )}
        </Space>
        {(dialectOf(dbType) === 'mysql' || dialectOf(dbType) === 'postgres') && (
          <Form.Item name="comment" label={tr('dbManage.comment') || tr('dbManage.tableComment') || 'Comment'}>
            <Input placeholder={tr('dbManage.tableCommentPlaceholder') || 'Table comment (optional)'} />
          </Form.Item>
        )}
      </Form>

      <div style={{ marginBottom: 8, fontWeight: 500 }}>{tr("dbManage.fields")}</div>
      <Table
        size="small"
        dataSource={columns}
        columns={colColumns}
        pagination={false}
        rowKey="_key"
        bordered
        style={{ marginBottom: 12 }}
      />
      <Space style={{ marginBottom: 12 }}>
        <Button type="dashed" icon={<PlusOutlined />} size="small" onClick={addColumn}>{tr("dbManage.addColumn")}</Button>
        <span>{tr('dbManage.primaryKey')}:</span>
        <Select
          size="small"
          value={primaryKey}
          onChange={setPrimaryKey}
          style={{ width: 120 }}
          options={columns.filter(c => c.name).map(c => ({ label: c.name, value: c.name }))}
        />
        {dialectOf(dbType) === 'mysql' && (
          <Checkbox checked={autoIncrement} onChange={e => setAutoIncrement(e.target.checked)}>
            AUTO_INCREMENT
          </Checkbox>
        )}
      </Space>

      {ddl && (
        <>
          <div style={{ marginBottom: 8, fontWeight: 500 }}>DDL 预览</div>
          <div style={{ border: '1px solid #d9d9d9', borderRadius: 4 }}>
            <Editor
              height={180}
              defaultLanguage="sql"
              value={ddl}
              options={{
                minimap: { enabled: false },
                fontSize: 13,
                wordWrap: 'on',
                readOnly: true,
                scrollBeyondLastLine: false,
                lineNumbers: 'off',
              }}
            />
          </div>
        </>
      )}
    </Modal>
  );
};

export default CreateTableModal;
