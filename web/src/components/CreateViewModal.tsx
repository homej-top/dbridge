import React, { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal, Form, Input, message, Alert } from 'antd';
import Editor from '@monaco-editor/react';
import { queryAPI } from '../api';
import { getDialect } from '../utils/dialect';

type CreateViewModalProps = {
  open: boolean;
  mode?: 'create' | 'edit';
  dataSourceId: string;
  schema: string;
  dbType: string;
  database?: string;
  initialViewName?: string;
  initialSql?: string;
  onClose: () => void;
  onSuccess: () => void;
};

const CreateViewModal: React.FC<CreateViewModalProps> = ({
  open, mode = 'create', dataSourceId, schema, dbType, database,
  initialViewName, initialSql, onClose, onSuccess,
}) => {
  const { t: tr } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [sql, setSql] = useState('SELECT ...');
  const viewName = Form.useWatch('viewName', form);
  const isEdit = mode === 'edit';

  // Init form with existing view data when editing
  React.useEffect(() => {
    if (open && isEdit && initialViewName) {
      form.setFieldsValue({ viewName: initialViewName });
      setSql(initialSql || 'SELECT ...');
    } else if (open && !isEdit) {
      form.resetFields();
      setSql('SELECT ...');
    }
  }, [open, isEdit, initialViewName, initialSql, form]);

  const ddl = useMemo(() => {
    if (!viewName) return '';
    const gen = getDialect(dbType);
    const orReplace = isEdit || dbType === 'postgres' || dbType === 'postgresql' || dbType === 'oracle';
    return gen.createViewDDL(schema, viewName, sql, orReplace);
  }, [schema, sql, dbType, viewName, isEdit]);


  const handleSubmit = async () => {
    try {
      await form.validateFields();
      if (!sql || sql === 'SELECT ...') {
        message.warning(tr('dbManage.requireSelect'));
        return;
      }
      setLoading(true);
      await queryAPI.executeDDL({ data_source_id: dataSourceId, sql: ddl, schema, database });
      message.success(isEdit ? '视图更新成功' : '视图创建成功');
      onSuccess();
      onClose();
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal
      title={isEdit ? tr('dbManage.editView') : tr('dbManage.createView')}
      open={open}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={loading}
      okText={isEdit ? tr('dbManage.updateView') : tr('common.create')}
      cancelText={tr("common.cancel")}
      width={700}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
        <Form.Item
          name="viewName"
          label={tr("dbManage.viewName")}
          rules={[
            { required: true, message: tr('dbManage.requireViewName') },
            { pattern: /^[a-zA-Z_][a-zA-Z0-9_]*$/, message: tr('dbManage.invalidName') },
          ]}
        >
          <Input placeholder={tr("dbManage.viewNamePlaceholder")} disabled={isEdit} />
        </Form.Item>

        <div style={{ marginBottom: 8, fontWeight: 500 }}>{tr("dbManage.selectStatement")}</div>
        <div style={{ marginBottom: 8 }}>
          {(dbType === 'postgres' || dbType === 'postgresql' || dbType === 'oracle') && (
            <Alert
              type="warning"
              showIcon
              message={tr("dbManage.quoteWarning")}
              description={`示例: SELECT * FROM ${getDialect(dbType).qualifiedTable(schema, 'TABLE_NAME')}`}
              style={{ marginBottom: 8 }}
            />
          )}
        </div>
        <div style={{ border: '1px solid #d9d9d9', borderRadius: 4 }}>
          <Editor
            height={250}
            defaultLanguage="sql"
            value={sql}
            onChange={(v) => setSql(v || '')}
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              wordWrap: 'on',
              scrollBeyondLastLine: false,
            }}
          />
        </div>
      </Form>
    </Modal>
  );
};

export default CreateViewModal;
