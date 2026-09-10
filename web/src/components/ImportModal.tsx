import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Modal, Select, Button, Spin, message, Space, Alert, Upload, List, Typography,
} from 'antd';
import {
  UploadOutlined, PlayCircleOutlined,
} from '@ant-design/icons';
import Editor from '@monaco-editor/react';
import { dbTransferAPI, dsAPI } from '../api';
import type { DataSource } from '../types';

const { Text } = Typography;

interface Props {
  open: boolean;
  onClose: () => void;
}

const ImportModal: React.FC<Props> = ({ open, onClose }) => {
  const { t } = useTranslation('importModal');
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  const [targetDS, setTargetDS] = useState<string>('');
  const [sql, setSql] = useState('');
  const [loading, setLoading] = useState(false);
  const [executing, setExecuting] = useState(false);
  const [result, setResult] = useState<{
    executed_count: number;
    executed: string[];
    errors: string[];
  } | null>(null);

  useEffect(() => {
    if (open) {
      setLoading(true);
      setResult(null);
      setSql('');
      setTargetDS('');
      dsAPI.list().then(res => {
        const list = res.data?.data?.list || [];
        setDataSources(list);
        if (list.length > 0) setTargetDS(list[0].id);
      }).catch(() => {}).finally(() => setLoading(false));
    }
  }, [open]);

  const handleUpload = (file: File) => {
    const reader = new FileReader();
    reader.onload = (e) => {
      const text = e.target?.result;
      if (typeof text === 'string') {
        setSql(text);
        message.success(t('fileLoaded', { fileName: file.name }));
      }
    };
    reader.readAsText(file);
    return false;
  };

  const handleExecute = async () => {
    if (!targetDS) {
      message.warning(t('selectTargetDatasource'));
      return;
    }
    if (!sql.trim()) {
      message.warning(t('enterSqlContent'));
      return;
    }

    Modal.confirm({
      title: t('confirmExecuteImport'),
      content: (
        <div>
          <p>{t('willExecuteOnTarget')}</p>
          <p style={{ color: '#e74c3c', fontWeight: 500 }}>{t('ensureBackup')}</p>
        </div>
      ),
      okText: t('confirmExecute'),
      okButtonProps: { danger: true },
      cancelText: t('cancel'),
      onOk: async () => {
        setExecuting(true);
        setResult(null);
        try {
          const res = await dbTransferAPI.import({ ds_id: targetDS, sql });
          const d = res.data?.data;
          setResult({
            executed_count: d?.executed_count || 0,
            executed: d?.executed || [],
            errors: d?.errors || [],
          });
          if ((d?.errors || []).length === 0) {
            message.success(t('importSuccess', { count: d?.executed_count }));
          } else {
            message.warning(t('importPartial', { executed: d?.executed_count, failed: d?.errors.length }));
          }
        } catch { /* handled */ }
        finally { setExecuting(false); }
      },
    });
  };

  return (
    <Modal
      title={t('importSQL')}
      open={open}
      onCancel={onClose}
      width={800}
      footer={
        <Space>
          <Button onClick={onClose}>{t('close')}</Button>
          <Button
            type="primary"
            danger
            icon={<PlayCircleOutlined />}
            loading={executing}
            disabled={!sql.trim() || !targetDS}
            onClick={handleExecute}
          >
            {t('executeImport')}
          </Button>
        </Space>
      }
      destroyOnHidden
    >
      <Spin spinning={loading || executing}>
        <div style={{ marginBottom: 12 }}>
          <div style={{ marginBottom: 8, fontWeight: 500 }}>{t('targetDatasource')}</div>
          <Select
            value={targetDS || undefined}
            onChange={setTargetDS}
            placeholder={t('selectTargetDatasource')}
            style={{ width: '100%' }}
            options={dataSources.map(ds => ({
              label: `${ds.name} (${ds.type} - ${ds.host}:${ds.port})`,
              value: ds.id,
            }))}
          />
        </div>

        <div style={{ marginBottom: 8, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span style={{ fontWeight: 500 }}>{t('sqlContent')}</span>
          <Upload
            accept=".sql,.txt"
            showUploadList={false}
            beforeUpload={handleUpload}
          >
            <Button size="small" icon={<UploadOutlined />}>{t('uploadSqlFile')}</Button>
          </Upload>
        </div>
        <div style={{ border: '1px solid #d9d9d9', borderRadius: 4, marginBottom: 12 }}>
          <Editor
            height={350}
            defaultLanguage="sql"
            value={sql}
            onChange={v => setSql(v || '')}
            options={{
              minimap: { enabled: false },
              fontSize: 13,
              wordWrap: 'on',
              scrollBeyondLastLine: false,
            }}
          />
        </div>

        {result && (
          <div>
            {result.errors.length === 0 ? (
              <Alert
                type="success"
                message={t('successExecuted', { count: result.executed_count })}
                showIcon
                style={{ marginBottom: 8 }}
              />
            ) : (
              <Alert
                type="warning"
                message={t('successFailed', { success: result.executed_count, failed: result.errors.length })}
                showIcon
                style={{ marginBottom: 8 }}
              />
            )}

            {result.executed.length > 0 && (
              <div style={{ marginBottom: 8 }}>
                <Text strong>{t('executed')}:</Text>
                <List
                  size="small"
                  dataSource={result.executed.slice(0, 20)}
                  renderItem={(item, i) => (
                    <List.Item key={`exec-${i}`} style={{ padding: '4px 0' }}>
                      <Text code style={{ fontSize: 12 }}>{item}</Text>
                    </List.Item>
                  )}
                />
                {result.executed.length > 20 && (
                  <Text type="secondary">{t('remaining', { count: result.executed.length - 20 })}</Text>
                )}
              </div>
            )}

            {result.errors.length > 0 && (
              <div>
                <Text strong type="danger">{t('errors')}:</Text>
                <List
                  size="small"
                  dataSource={result.errors}
                  renderItem={(item, i) => (
                    <List.Item key={`err-${i}`} style={{ padding: '4px 0' }}>
                      <Text type="danger" style={{ fontSize: 12 }}>{item}</Text>
                    </List.Item>
                  )}
                />
              </div>
            )}
          </div>
        )}
      </Spin>
    </Modal>
  );
};

export default ImportModal;
