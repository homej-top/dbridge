import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Spin, Empty, Button, Space, message } from 'antd';
import { EditOutlined, CopyOutlined, ReloadOutlined } from '@ant-design/icons';
import Editor from '@monaco-editor/react';
import { dsAPI } from '../api';

interface ObjectDefinitionPanelProps {
  dataSourceId: string;
  schema: string;
  objectType: string;
  objectName: string;
  database?: string;
  onEdit?: () => void;
}

const ObjectDefinitionPanel: React.FC<ObjectDefinitionPanelProps> = ({
  dataSourceId,
  schema,
  objectType,
  objectName,
  database,
  onEdit,
}) => {
  const { t: tr } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [definition, setDefinition] = useState('');

  const loadDefinition = async () => {
    setLoading(true);
    try {
      const res = await dsAPI.getObjectDetail(dataSourceId, schema, objectType, objectName, database);
      const detail = res.data?.data || res.data;
      setDefinition(detail?.definition || detail?.ddl || '');
    } catch {
      message.error(tr('objects.noDefinition'));
      setDefinition('');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (dataSourceId && schema && objectType && objectName) {
      loadDefinition();
    }
  }, [dataSourceId, schema, objectType, objectName, database]);

  const handleCopy = () => {
    if (definition) {
      navigator.clipboard.writeText(definition);
      message.success(tr('query.copied'));
    }
  };

  if (loading) {
    return <Spin style={{ display: 'block', margin: '40px auto' }} />;
  }

  if (!definition) {
    return <Empty description={tr('objects.noDefinition')} />;
  }

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <div style={{ padding: '4px 8px', borderBottom: '1px solid #f0f0f0', display: 'flex', alignItems: 'center', gap: 4 }}>
        <span style={{ flex: 1, fontWeight: 500 }}>{objectName}</span>
        <Space size={4}>
          <Button size="small" icon={<ReloadOutlined />} onClick={loadDefinition} title={tr('common.refresh')} />
          <Button size="small" icon={<CopyOutlined />} onClick={handleCopy} title={tr('common.copy')} />
          {onEdit && (
            <Button size="small" type="primary" icon={<EditOutlined />} onClick={onEdit}>
              {tr('common.edit')}
            </Button>
          )}
        </Space>
      </div>
      <div style={{ flex: 1, overflow: 'hidden' }}>
        <Editor
          height="100%"
          language="sql"
          value={definition}
          loading={<Spin style={{ display: 'block', margin: '40px auto' }} />}
          options={{
            readOnly: true,
            minimap: { enabled: false },
            fontSize: 14,
            wordWrap: 'on',
            scrollBeyondLastLine: false,
            lineNumbers: 'on',
            renderLineHighlight: 'none',
            overviewRulerBorder: false,
          }}
        />
      </div>
    </div>
  );
};

export default ObjectDefinitionPanel;
