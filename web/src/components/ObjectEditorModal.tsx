import React, { useEffect, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal, Spin, message } from 'antd';
import Editor from '@monaco-editor/react';
import { dsAPI } from '../api';

interface ObjectEditorModalProps {
  open: boolean;
  dataSourceId: string;
  schema: string;
  objectType: string;
  objectName?: string;
  database?: string;
  onClose: () => void;
  onSuccess: (info: { objectType: string; schema: string; database?: string }) => void;
}

const ObjectEditorModal: React.FC<ObjectEditorModalProps> = ({
  open,
  dataSourceId,
  schema,
  objectType,
  objectName,
  database,
  onClose,
  onSuccess,
}) => {
  const { t: tr } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [ddl, setDdl] = useState('');

  const isEdit = !!objectName;

  const loadInitial = useCallback(async () => {
    if (!open || !dataSourceId) return;
    setLoading(true);
    try {
      if (isEdit && objectName) {
        const res = await dsAPI.getObjectDetail(dataSourceId, schema, objectType, objectName, database);
        const detail = res.data?.data || res.data;
        setDdl(detail?.definition || detail?.ddl || '');
      } else {
        const res = await dsAPI.getCreateTemplate(dataSourceId, schema, objectType, { object_name: '', database });
        const template = res.data?.data || res.data;
        setDdl(template?.template || template?.ddl || '');
      }
    } catch {
      setDdl('');
    } finally {
      setLoading(false);
    }
  }, [open, dataSourceId, schema, objectType, objectName, database, isEdit]);

  useEffect(() => {
    if (open) {
      loadInitial();
    } else {
      setDdl('');
    }
  }, [open, loadInitial]);

  const handleSave = async () => {
    if (!ddl.trim()) {
      message.warning(tr('objects.noDefinition'));
      return;
    }
    setSaving(true);
    try {
      if (isEdit && objectName) {
        await dsAPI.alterObject(dataSourceId, schema, objectType, objectName, { ddl, database });
        message.success(tr('objects.alterSuccess'));
      } else {
        await dsAPI.createObject(dataSourceId, schema, objectType, { ddl, database });
        message.success(tr('objects.createSuccess'));
      }
      console.log('[ObjectEditorModal] Calling onSuccess with:', { objectType, schema, database });
      onSuccess({ objectType, schema, database });
      onClose();
    } catch {
      // Error handled by interceptor
    } finally {
      setSaving(false);
    }
  };

  const title = isEdit
    ? `${tr('objects.edit')} - ${objectName}`
    : `${tr('objects.create')} (${tr(`objects.${objectType}s`) || objectType})`;

  return (
    <Modal
      title={title}
      open={open}
      onCancel={onClose}
      onOk={handleSave}
      confirmLoading={saving}
      width={900}
      styles={{ body: { height: 500, padding: 0 } }}
      destroyOnClose
    >
      {loading ? (
        <Spin style={{ display: 'block', margin: '100px auto' }} />
      ) : (
        <Editor
          height="100%"
          language="sql"
          value={ddl}
          onChange={(v) => setDdl(v || '')}
          options={{
            minimap: { enabled: false },
            fontSize: 14,
            wordWrap: 'on',
            scrollBeyondLastLine: false,
            lineNumbers: 'on',
            automaticLayout: true,
          }}
        />
      )}
    </Modal>
  );
};

export default ObjectEditorModal;
