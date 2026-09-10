import React, { useEffect, useState } from 'react';
import { Table, Tag, Space, Tooltip, Progress, message } from 'antd';
import { DownloadOutlined, StopOutlined, EyeOutlined, ImportOutlined, PlayCircleOutlined } from '@ant-design/icons';
import { exportTaskAPI } from '../api';
import { useTranslation } from 'react-i18next';

interface Execution {
  id: string;
  task_id: string;
  run_number: number;
  status: string;
  started_at?: string;
  finished_at?: string;
  progress: number;
  result_file_path?: string;
  result_file_name?: string;
  result_file_size?: number;
  result_summary?: string;
  log_file_path?: string;
  log_text?: string;
  error_msg?: string;
  created_at: string;
}

// 格式化为 yyyy-MM-dd hh:mm:ss
const fmtDateTime = (text?: string) => {
  if (!text) return '';
  const d = new Date(text);
  if (isNaN(d.getTime())) return text;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

interface Props {
  taskId: string;
  refreshTrigger?: number;
  onViewLogs: (taskId: string, execId: string) => void;
  onViewManifest: (execId: string) => void;
  onImportFromExecution?: (execId: string) => void;
  onRetry?: () => void;
}

const ExecutionSubTable: React.FC<Props> = ({ taskId, refreshTrigger, onViewLogs, onImportFromExecution, onRetry }) => {
  const { t } = useTranslation('executionLog');
  const [executions, setExecutions] = useState<Execution[]>([]);
  const [loading, setLoading] = useState(false);

  const statusMap: Record<string, { color: string; text: string }> = {
    pending: { color: 'default', text: t('pending') },
    running: { color: 'processing', text: t('running') },
    completed: { color: 'success', text: t('completed') },
    failed: { color: 'error', text: t('failed') },
    cancelled: { color: 'warning', text: t('cancelled') },
  };

  const fetchExecutions = async () => {
    setLoading(true);
    try {
      const res = await exportTaskAPI.listExecutions(taskId);
      setExecutions(res.data.data?.list || []);
    } catch { /* handled */ }
    finally { setLoading(false); }
  };

  useEffect(() => {
    fetchExecutions();
  }, [taskId]);

  // 父组件触发刷新（如启动任务后延迟刷新）
  useEffect(() => {
    if (refreshTrigger && refreshTrigger > 0) {
      fetchExecutions();
    }
  }, [refreshTrigger]);

  const hasActive = executions.some(e => e.status === 'running' || e.status === 'pending');

  useEffect(() => {
    if (!hasActive) return;
    const interval = setInterval(fetchExecutions, 3000);
    return () => clearInterval(interval);
  }, [hasActive, taskId]);

  const handleDownload = async (exec: Execution) => {
    try {
      const res = await exportTaskAPI.downloadExecution(taskId, exec.id);
      const blob = new Blob([res.data], { type: 'application/octet-stream' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = exec.result_file_name || `${taskId}-${exec.run_number}.zip`;
      a.click();
      URL.revokeObjectURL(url);
      message.success(t('fileDownloaded'));
    } catch { /* handled */ }
  };

  const handleCancel = async () => {
    try {
      await exportTaskAPI.cancel(taskId);
      message.success(t('cancelledMsg'));
      fetchExecutions();
    } catch { /* handled */ }
  };

  const handleRetry = async () => {
    if (onRetry) { onRetry(); return; }
    try {
      await exportTaskAPI.start(taskId);
      message.success(t('restarted'));
      fetchExecutions();
    } catch { /* handled */ }
  };

  const columns = [
    {
      title: '#', dataIndex: 'run_number', key: 'run_number', width: 50,
      render: (n: number) => `#${n}`,
    },
    {
      title: t('progress'), dataIndex: 'progress', key: 'progress',
      render: (p: number, record: Execution) => {
        if (record.status === 'running') {
          return <Progress percent={p} size="small" />;
        }
        return <Tag color={statusMap[record.status]?.color}>{statusMap[record.status]?.text || record.status}</Tag>;
      },
    },
    {
      title: t('startTime'), dataIndex: 'started_at', key: 'started_at', width: 180,
      render: (val: string, record: Execution) => fmtDateTime(val || record.created_at),
    },
    {
      title: t('duration'), key: 'duration', width: 110,
      render: (_: any, record: Execution) => {
        if (!record.started_at || !record.finished_at) return '-';
        const start = new Date(record.started_at).getTime();
        const end = new Date(record.finished_at).getTime();
        const ms = end - start;
        if (ms < 1000) return `${ms}ms`;
        if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
        return `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`;
      },
    },
    {
      title: <span style={{ paddingLeft: 8 }}>{t('action')}</span>, key: 'action', width: 160,
      render: (_: any, record: Execution) => (
        <Space size={8} style={{ paddingLeft: 8 }}>
          <Tooltip title={t('viewLog')}><a style={{ color: '#1890ff' }} onClick={() => onViewLogs(taskId, record.id)}><EyeOutlined /></a></Tooltip>
          {record.status === 'running' && (
            <Tooltip title={t('cancel')}><a style={{ color: '#fa8c16' }} onClick={() => handleCancel()}><StopOutlined /></a></Tooltip>
          )}
          {(record.status === 'failed' || record.status === 'cancelled' || record.status === 'pending') && (
            <Tooltip title={t('retry')}><a style={{ color: '#20a53a' }} onClick={() => handleRetry()}><PlayCircleOutlined /></a></Tooltip>
          )}
          {record.status === 'completed' && record.result_file_path && (
            <Tooltip title={t('download')}><a style={{ color: '#20a53a' }} onClick={() => handleDownload(record)}><DownloadOutlined /></a></Tooltip>
          )}
          {record.status === 'completed' && record.result_file_path && onImportFromExecution && (
            <Tooltip title={t('importTo')}><a style={{ color: '#fa8c16' }} onClick={() => onImportFromExecution(taskId)}><ImportOutlined /></a></Tooltip>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div style={{ margin: '0 -16px' }}>
      <Table
        columns={columns}
        dataSource={executions}
        rowKey="id"
        loading={loading}
        pagination={false}
        size="small"
        scroll={{ x: 680 }}
      />
    </div>
  );
};

export default ExecutionSubTable;
