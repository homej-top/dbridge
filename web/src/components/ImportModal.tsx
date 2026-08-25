import React, { useState, useEffect } from 'react';
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
        message.success(`已加载文件: ${file.name}`);
      }
    };
    reader.readAsText(file);
    return false;
  };

  const handleExecute = async () => {
    if (!targetDS) {
      message.warning('请选择目标数据源');
      return;
    }
    if (!sql.trim()) {
      message.warning('请输入 SQL 内容');
      return;
    }

    Modal.confirm({
      title: '确认执行导入',
      content: (
        <div>
          <p>将在目标数据库上执行 SQL，此操作可能修改数据库结构和数据。</p>
          <p style={{ color: '#e74c3c', fontWeight: 500 }}>请确保已备份目标数据库！</p>
        </div>
      ),
      okText: '确认执行',
      okButtonProps: { danger: true },
      cancelText: '取消',
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
            message.success(`导入成功: 执行了 ${d?.executed_count} 条语句`);
          } else {
            message.warning(`导入部分完成: ${d?.executed_count} 条成功, ${d?.errors.length} 条失败`);
          }
        } catch { /* handled */ }
        finally { setExecuting(false); }
      },
    });
  };

  return (
    <Modal
      title="导入 SQL"
      open={open}
      onCancel={onClose}
      width={800}
      footer={
        <Space>
          <Button onClick={onClose}>关闭</Button>
          <Button
            type="primary"
            danger
            icon={<PlayCircleOutlined />}
            loading={executing}
            disabled={!sql.trim() || !targetDS}
            onClick={handleExecute}
          >
            执行导入
          </Button>
        </Space>
      }
      destroyOnHidden
    >
      <Spin spinning={loading || executing}>
        <div style={{ marginBottom: 12 }}>
          <div style={{ marginBottom: 8, fontWeight: 500 }}>目标数据源</div>
          <Select
            value={targetDS || undefined}
            onChange={setTargetDS}
            placeholder="选择目标数据源"
            style={{ width: '100%' }}
            options={dataSources.map(ds => ({
              label: `${ds.name} (${ds.type} - ${ds.host}:${ds.port})`,
              value: ds.id,
            }))}
          />
        </div>

        <div style={{ marginBottom: 8, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span style={{ fontWeight: 500 }}>SQL 内容</span>
          <Upload
            accept=".sql,.txt"
            showUploadList={false}
            beforeUpload={handleUpload}
          >
            <Button size="small" icon={<UploadOutlined />}>上传 .sql 文件</Button>
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
                message={`成功执行 ${result.executed_count} 条语句`}
                showIcon
                style={{ marginBottom: 8 }}
              />
            ) : (
              <Alert
                type="warning"
                message={`${result.executed_count} 条成功, ${result.errors.length} 条失败`}
                showIcon
                style={{ marginBottom: 8 }}
              />
            )}

            {result.executed.length > 0 && (
              <div style={{ marginBottom: 8 }}>
                <Text strong>已执行:</Text>
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
                  <Text type="secondary">... 还有 {result.executed.length - 20} 条</Text>
                )}
              </div>
            )}

            {result.errors.length > 0 && (
              <div>
                <Text strong type="danger">错误:</Text>
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
