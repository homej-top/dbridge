import React, { useEffect, useState } from 'react';
import { Drawer, Button, message, Progress } from 'antd';
import { DownloadOutlined } from '@ant-design/icons';
import { exportTaskAPI } from '../api';
import { useTranslation } from 'react-i18next';

interface Props {
  open: boolean;
  taskId: string;
  execId: string;
  onClose: () => void;
}

const ExecutionLogDrawer: React.FC<Props> = ({ open, taskId, execId, onClose }) => {
  const { t } = useTranslation('executionLog');
  const [logText, setLogText] = useState<string>('');
  const [progress, setProgress] = useState<number>(0);
  const [logFilePath, setLogFilePath] = useState<string>('');
  const [loading, setLoading] = useState(false);

  const [status, setStatus] = useState<string>('');

  const fetchLogs = async () => {
    setLoading(true);
    try {
      const res = await exportTaskAPI.getExecutionLogs(taskId, execId);
      const execStatus = res.data.data?.status || '';
      setStatus(execStatus);
      const text = res.data.data?.log_text || res.data.data?.logs || '';
      setProgress(res.data.data?.progress || 0);
      const lfp = res.data.data?.log_file_path || '';
      setLogFilePath(lfp);
      setLogText(text);
    } catch { /* handled */ }
    finally { setLoading(false); }
  };

  const isActive = status === 'running' || status === 'pending';

  useEffect(() => {
    if (open && execId) {
      fetchLogs();
    }
  }, [open, execId]);

  useEffect(() => {
    if (open && isActive) {
      const interval = setInterval(fetchLogs, 2000);
      return () => clearInterval(interval);
    }
  }, [open, isActive]);

  const handleDownloadLog = async () => {
    try {
      const text = logText || '';
      const blob = new Blob([text], { type: 'text/plain' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${execId}.log`;
      a.click();
      URL.revokeObjectURL(url);
      message.success(t('logFileDownloaded'));
    } catch { /* handled */ }
  };

  const renderLogLines = () => {
    if (!logText) return <div style={{ color: '#999' }}>{t('noLogs')}</div>;

    const lines = logText.split('\n').filter(line => line.trim());
    return lines.map((line, idx) => {
      let color = '#333';
      if (line.includes('[ERROR]')) color = '#e74c3c';
      else if (line.includes('[WARN]')) color = '#fa8c16';
      else if (line.includes('[INFO]')) color = '#333';

      return (
        <div key={idx} style={{ color, fontFamily: 'monospace', fontSize: 12, marginBottom: 4 }}>
          {line}
        </div>
      );
    });
  };

  return (
    <Drawer
      title={`${t('executionLog')} #${execId.slice(0, 8)}`}
      open={open}
      onClose={onClose}
      width={700}
      footer={
        <div style={{ textAlign: 'right' }}>
          {logFilePath && (
            <Button icon={<DownloadOutlined />} onClick={handleDownloadLog}>
              {t('downloadLogFile')}
            </Button>
          )}
        </div>
      }
    >
      {loading ? (
        <div>{t('loading')}</div>
      ) : (
        <>
          {progress > 0 && (
            <div style={{ marginBottom: 16 }}>
              <Progress percent={progress} />
            </div>
          )}
          <div style={{ maxHeight: 'calc(100vh - 200px)', overflowY: 'auto' }}>
            {renderLogLines()}
          </div>
        </>
      )}
    </Drawer>
  );
};

export default ExecutionLogDrawer;
