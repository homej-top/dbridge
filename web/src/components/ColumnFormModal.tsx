import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal, Form, Input, Select, InputNumber, Switch, Alert, Space } from 'antd';
import { dsAPI } from '../api';
import { dialectOf } from '../utils/dialect';

// Fallback types if API call fails
const FALLBACK_TYPES: Record<string, string[]> = {
  mysql: ['int','bigint','smallint','tinyint','varchar','char','text','decimal','float','double','date','datetime','timestamp','time','json','boolean','blob','enum','set'],
  postgres: ['integer','bigint','smallint','serial','bigserial','numeric','decimal','real','double precision','varchar','char','text','bytea','timestamp','timestamptz','date','time','boolean','json','jsonb','uuid','inet'],
  oracle: ['NUMBER','INTEGER','VARCHAR2','NVARCHAR2','CHAR','NCHAR','CLOB','BLOB','DATE','TIMESTAMP'],
  sqlserver: ['int','bigint','smallint','tinyint','bit','decimal','float','varchar','nvarchar','char','nchar','text','varbinary','datetime','datetime2','date','time','uniqueidentifier','xml'],
};

interface ColumnTypeInfo {
  name: string;
  needs_length: boolean;
  needs_scale: boolean;
  description?: string;
}

function getFallbackTypes(dbType: string): ColumnTypeInfo[] {
  const names = FALLBACK_TYPES[dbType] || FALLBACK_TYPES['mysql'];
  return names.map(n => ({ name: n, needs_length: false, needs_scale: false }));
}

const RESERVED_WORDS = new Set([
  'select', 'from', 'where', 'insert', 'update', 'delete', 'drop', 'table',
  'index', 'column', 'database', 'schema', 'view', 'trigger', 'procedure',
  'function', 'group', 'order', 'having', 'limit', 'offset', 'join', 'union',
  'values', 'set', 'key', 'user', 'role', 'grant', 'revoke', 'primary',
  'foreign', 'unique', 'check', 'default', 'null', 'not', 'and', 'or', 'like',
  'in', 'between', 'exists', 'case', 'when', 'then', 'else', 'end', 'create',
  'alter', 'add', 'constraint', 'references', 'on', 'asc', 'desc',
]);

export type ColumnFormValues = {
  name: string;
  type: string;
  length?: string;
  nullable?: boolean;
  default?: string;
  has_default?: boolean;
  comment?: string;
  after?: string;
  key?: boolean;
};

type Props = {
  open: boolean;
  mode: 'add' | 'edit';
  dbType: string;
  dataSourceId?: string;
  initialValues?: ColumnFormValues;
  existingColumns?: string[];
  onCancel: () => void;
  onSubmit: (values: ColumnFormValues) => void;
};

const ColumnFormModal: React.FC<Props> = ({
  open, mode, dbType, dataSourceId, initialValues, existingColumns = [], onCancel, onSubmit,
}) => {
  const { t: tr } = useTranslation();
  const [form] = Form.useForm<ColumnFormValues>();
  const [columnTypes, setColumnTypes] = useState<ColumnTypeInfo[]>([]);
  const [typesLoading, setTypesLoading] = useState(false);

  // Fetch column types from API when modal opens
  useEffect(() => {
    if (!open) return;
    if (dataSourceId) {
      setTypesLoading(true);
      dsAPI.columnTypes(dataSourceId)
        .then(res => {
          const data = res.data?.data;
          if (Array.isArray(data) && data.length > 0) {
            setColumnTypes(data);
          } else {
            setColumnTypes(getFallbackTypes(dbType));
          }
        })
        .catch(() => setColumnTypes(getFallbackTypes(dbType)))
        .finally(() => setTypesLoading(false));
    } else {
      setColumnTypes(getFallbackTypes(dbType));
    }
  }, [open, dataSourceId, dbType]);

  useEffect(() => {
    if (open) {
      if (mode === 'edit' && initialValues) {
        form.setFieldsValue(initialValues);
      } else {
        form.resetFields();
        form.setFieldsValue({ nullable: true });
      }
    }
  }, [open, mode, initialValues, form]);

  const nameValue = Form.useWatch('name', form);
  const typeValue = Form.useWatch('type', form);
  const isReserved = nameValue && RESERVED_WORDS.has(nameValue.toLowerCase());
  const selectedTypeInfo = columnTypes.find(ct => ct.name === typeValue);
  const needsLength = selectedTypeInfo?.needs_length || (typeValue && ['varchar', 'char', 'varbinary', 'binary', 'varchar2', 'nvarchar2', 'nchar', 'nvarchar', 'raw'].includes(typeValue.toLowerCase()));
  const needsScale = selectedTypeInfo?.needs_scale || false;

  return (
    <Modal
      title={mode === 'add' ? tr('dbManage.addColumn') : tr('dbManage.editColumn')}
      open={open}
      onCancel={onCancel}
      onOk={() => form.submit()}
      zIndex={1050}
      width={560}
      destroyOnHidden
    >
      {isReserved && (
        <Alert
          type="warning"
          showIcon
          message={`"${nameValue}" 是 SQL 保留字, 使用时会自动转义, 但建议避免作为列名`}
          style={{ marginBottom: 16 }}
        />
      )}
      <Form form={form} layout="vertical" onFinish={onSubmit} autoComplete="off">
        <Form.Item
          name="name"
          label={tr('dbManage.fieldName')}
          rules={[
            { required: true, message: tr('dbManage.requireFieldName') },
            { pattern: /^[a-zA-Z_][a-zA-Z0-9_]*$/, message: tr('dbManage.fieldNameHint') },
            {
              validator: (_, value) => {
                if (mode === 'add' && value && existingColumns.includes(value)) {
                  return Promise.reject('字段已存在');
                }
                return Promise.resolve();
              },
            },
          ]}
        >
          <Input placeholder="column_name" disabled={mode === 'edit'} />
        </Form.Item>
        <Space style={{ display: 'flex' }} align="start">
          <Form.Item
            name="type"
            label={tr('dbManage.tableType')}
            rules={[{ required: true, message: tr('dbManage.selectType') }]}
            style={{ minWidth: 200 }}
          >
            <Select
              showSearch
              placeholder={tr('dbManage.tableType')}
              loading={typesLoading}
              options={columnTypes.map((t) => ({
                label: t.description ? `${t.name} - ${t.description}` : t.name,
                value: t.name,
              }))}
            />
          </Form.Item>
          {needsLength && (
            <Form.Item
              name="length"
              label={tr('dbManage.length')}
              rules={[{ pattern: /^\d+$/, message: '正整数' }]}
            >
              <InputNumber min={1} max={65535} placeholder={tr("dbManage.length")} />
            </Form.Item>
          )}
          {needsScale && (
            <Form.Item
              name="scale"
              label={tr("dbManage.scale")}
              rules={[{ pattern: /^\d+$/, message: '正整数' }]}
            >
              <InputNumber min={0} max={38} placeholder={tr("dbManage.scalePlaceholder")} />
            </Form.Item>
          )}
        </Space>
        <Space style={{ display: 'flex' }} align="start">
          <Form.Item name="nullable" label={tr('dbManage.nullable')} valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="has_default" label={tr('dbManage.hasDefault')} valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, cur) => prev.has_default !== cur.has_default}>
            {({ getFieldValue }) =>
              getFieldValue('has_default') ? (
                <Form.Item name="default" label={tr('dbManage.defaultValue')} style={{ minWidth: 200 }}>
                  <Input placeholder={tr("dbManage.defaultValueHint")} />
                </Form.Item>
              ) : null
            }
          </Form.Item>
        </Space>
        {mode === 'edit' && (
          <Form.Item name="key" label={tr("dbManage.primaryKey")} valuePropName="checked">
            <Switch checkedChildren={tr("common.yes")} unCheckedChildren={tr("common.no")} onChange={(checked) => { if (checked) form.setFieldsValue({ nullable: false }); }} />
          </Form.Item>
        )}
        <Form.Item name="comment" label={tr('dbManage.comment')}>
          <Input placeholder={tr('dbManage.fieldComment')} />
        </Form.Item>
        {dialectOf(dbType) === 'mysql' && mode === 'add' && (
          <Form.Item name="after" label={tr('dbManage.positionAfter')}>
            <Select
              allowClear
              showSearch
              placeholder={tr('dbManage.appendToEnd')}
              options={existingColumns.map((c) => ({ label: c, value: c }))}
            />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
};

export default ColumnFormModal;
