import React, { useEffect, useMemo, useState } from 'react';
import { Modal, Form, Input, Select, message } from 'antd';
import Editor from '@monaco-editor/react';
import { useTranslation } from 'react-i18next';
import { queryAPI } from '../api';
import { dialectOf, getDialect } from '../utils/dialect';

type SchemaFormModalProps = {
  open: boolean;
  mode: 'create' | 'edit';
  dataSourceId: string;
  dbType: string;
  /** The tree level being created/edited: 'database', 'schema', or 'user' */
  level?: string;
  /** For PG/SQL Server, the parent database when creating a schema inside it */
  database?: string;
  initValues?: { name: string; charset: string; collation: string };
  onClose: () => void;
  onSuccess: () => void;
};

const MYSQL_CHARSETS = [
  'utf8mb4', 'utf8', 'utf8mb3', 'latin1', 'gbk', 'gb2312', 'big5', 'ascii',
];

const MYSQL_COLLATIONS: Record<string, string[]> = {
  utf8mb4: ['utf8mb4_unicode_ci', 'utf8mb4_general_ci', 'utf8mb4_bin', 'utf8mb4_0900_ai_ci'],
  utf8: ['utf8_general_ci', 'utf8_unicode_ci', 'utf8_bin'],
  utf8mb3: ['utf8mb3_general_ci', 'utf8mb3_unicode_ci', 'utf8mb3_bin'],
  latin1: ['latin1_swedish_ci', 'latin1_general_ci', 'latin1_bin'],
  gbk: ['gbk_chinese_ci', 'gbk_bin'],
  gb2312: ['gb2312_chinese_ci', 'gb2312_bin'],
  big5: ['big5_chinese_ci', 'big5_bin'],
  ascii: ['ascii_general_ci', 'ascii_bin'],
};

const SchemaFormModal: React.FC<SchemaFormModalProps> = ({
  open, mode, dataSourceId, dbType, level, database, initValues, onClose, onSuccess,
}) => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [newName, setNewName] = useState('');

  useEffect(() => {
    if (open) {
      if (mode === 'edit' && initValues) {
        form.setFieldsValue({
          name: initValues.name,
          charset: initValues.charset || 'utf8mb4',
          collation: initValues.collation || 'utf8mb4_unicode_ci',
          new_name: '',
        });
        setNewName('');
      } else {
        form.setFieldsValue({
          name: '',
          charset: 'utf8mb4',
          collation: 'utf8mb4_unicode_ci',
        });
      }
    }
  }, [open, mode, initValues, form]);

  const charset = Form.useWatch('charset', form) || 'utf8mb4';
  const formName = Form.useWatch('name', form) || '';

  const ddl = useMemo(() => {
    const dialect = dialectOf(dbType);
    const gen = getDialect(dbType);
    if (mode === 'create') {
      const name = formName;
      if (!name) return '';

      // Oracle uses CREATE USER for schemas
      let createLevel = level || (
        dialect === 'mysql' ? 'database' :
        dialect === 'oracle' ? 'user' :
        'schema'
      );
      if (dialect === 'oracle' && createLevel === 'schema') {
        createLevel = 'user';
      }

      if (createLevel === 'database') {
        return gen.createDatabase(name, form.getFieldValue('charset'), form.getFieldValue('collation'));
      }
      if (createLevel === 'user') {
        if (dialect === 'oracle') return gen.createUser(name, name);
      }
      if (createLevel === 'schema') {
        return gen.createSchema(name);
      }
      return gen.createSchema(name);
    }
    // Edit mode — ALTER statements (dialect-specific, kept here for now)
    if (mode === 'edit' && dialect === 'postgres' && level === 'database' && newName) {
      return `ALTER DATABASE "${formName}" RENAME TO "${newName}";`;
    }
    if (mode === 'edit' && dialect === 'postgres' && newName) {
      return `ALTER SCHEMA "${formName}" RENAME TO "${newName}";`;
    }
    if (mode === 'edit' && dialect === 'sqlserver' && level === 'database' && newName) {
      return `ALTER DATABASE [${formName}] MODIFY NAME = [${newName}];`;
    }
    if (mode === 'edit' && dialect === 'sqlserver' && level === 'schema' && newName) {
      // SQL Server doesn't support direct schema renaming.
      // Need to create new schema and transfer objects manually.
      return `-- SQL Server does not support direct schema renaming.\n-- Please:\n-- 1. CREATE SCHEMA [${newName}];\n-- 2. Transfer objects: ALTER SCHEMA [${newName}] TRANSFER [${formName}].[object_name];\n-- 3. DROP SCHEMA [${formName}];`;
    }
    if (dialect === 'mysql') {
      const cs = form.getFieldValue('charset') || 'utf8mb4';
      const col = form.getFieldValue('collation') || 'utf8mb4_unicode_ci';
      return `ALTER DATABASE \`${formName}\`\n  CHARACTER SET ${cs}\n  COLLATE ${col};`;
    }
    if (mode === 'edit' && dialect === 'oracle') {
      const newPwd = form.getFieldValue('new_password') || '';
      if (newPwd) return `ALTER USER "${formName}" IDENTIFIED BY "${newPwd}"`;
    }
    return '';
  }, [mode, formName, newName, charset, form, dbType, level]);

  const handleSubmit = async () => {
    try {
      await form.validateFields();
      if (!ddl) {
        message.warning(t('schemaForm.noChanges'));
        return;
      }
      // Check if DDL is only comments (e.g., SQL Server schema rename warning)
      const ddlLines = ddl.split('\n').map(line => line.trim()).filter(line => line && !line.startsWith('--'));
      if (ddlLines.length === 0) {
        message.warning(t('schemaForm.manualExecWarning'));
        return;
      }
      setLoading(true);
      await queryAPI.executeDDL({ data_source_id: dataSourceId, sql: ddl, database });
      message.success(mode === 'create' ? `${unitLabel} ${t('schemaForm.createSuccess')}` : `${unitLabel} ${t('schemaForm.editSuccess')}`);
      onSuccess();
      onClose();
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  };

  // Determine the label and title based on level and DB type
  const unitLabel = (() => {
    if (level === 'user') return t('tree.user');
    if (level === 'database') {
      if (dialectOf(dbType) === 'mysql') return t('tree.database');
      return t('tree.database');
    }
    if (level === 'schema') return t('tree.schema');
    // Fallback
    if (dialectOf(dbType) === 'mysql') return t('tree.database');
    if (dialectOf(dbType) === 'oracle') return t('tree.user');
    if (dialectOf(dbType) === 'postgres' || dialectOf(dbType) === 'sqlserver') return t('tree.schema');
    return t('tree.schema');
  })();

  const collationOptions = dialectOf(dbType) === 'mysql'
    ? (MYSQL_COLLATIONS[charset] || MYSQL_COLLATIONS['utf8mb4'] || []).map((c: string) => ({ label: c, value: c }))
    : [];

  return (
    <Modal
      title={mode === 'create' ? `${t('schemaForm.create')} ${unitLabel}` : `${t('schemaForm.edit')} ${unitLabel} · ${initValues?.name || ''}`}
      open={open}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={loading}
      okText={t('schemaForm.execute')}
      cancelText={t('schemaForm.cancel')}
      width={560}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
        <Form.Item
          name="name"
          label={`${unitLabel} ${t('schemaForm.nameLabel')}`}
          rules={[
            { required: mode === 'create', message: t('schemaForm.nameRequired') },
            { pattern: /^[a-zA-Z_][a-zA-Z0-9_]*$/, message: t('schemaForm.namePattern') },
          ]}
        >
          <Input
            placeholder={t('schemaForm.namePlaceholder')}
            disabled={mode === 'edit' && (dialectOf(dbType) === 'mysql' || dialectOf(dbType) === 'oracle')}
          />
        </Form.Item>

        {mode === 'edit' && dialectOf(dbType) === 'oracle' && (
          <div style={{ color: '#faad14', fontSize: 12, marginBottom: 16 }}>
            {t('schemaForm.oracleRenameWarning')}
          </div>
        )}

        {mode === 'edit' && (dialectOf(dbType) === 'postgres' || dialectOf(dbType) === 'sqlserver') && (
          <Form.Item name="new_name" label={t('schemaForm.newNameLabel')}>
            <Input
              placeholder={t('schemaForm.newNamePlaceholder')}
              onChange={(e) => setNewName(e.target.value)}
            />
          </Form.Item>
        )}

        {mode === 'edit' && dialectOf(dbType) === 'oracle' && (
          <Form.Item name="new_password" label={t('schemaForm.newPasswordLabel')}>
            <Input.Password
              placeholder={t('schemaForm.newPasswordPlaceholder')}
              visibilityToggle
            />
          </Form.Item>
        )}

        {dialectOf(dbType) === 'mysql' && (
          <>
            <Form.Item name="charset" label={t('schemaForm.charsetLabel')}>
              <Select
                options={MYSQL_CHARSETS.map((c: string) => ({ label: c, value: c }))}
                onChange={() => {
                  const cs = form.getFieldValue('charset');
                  const cols = MYSQL_COLLATIONS[cs] || [];
                  if (cols.length > 0) {
                    form.setFieldValue('collation', cols[0]);
                  }
                }}
              />
            </Form.Item>
            <Form.Item name="collation" label={t('schemaForm.collationLabel')}>
              <Select options={collationOptions} />
            </Form.Item>
          </>
        )}

        {ddl && (
          <Form.Item label={t('schemaForm.ddlPreview')}>
            <div style={{ border: '1px solid #d9d9d9', borderRadius: 4 }}>
              <Editor
                height={120}
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
          </Form.Item>
        )}

        {mode === 'edit' && dialectOf(dbType) === 'mysql' && (
          <div style={{ color: '#faad14', fontSize: 12 }}>
            {t('schemaForm.charsetWarning')}
          </div>
        )}
      </Form>
    </Modal>
  );
};

export default SchemaFormModal;
