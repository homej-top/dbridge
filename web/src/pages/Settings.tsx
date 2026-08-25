import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Form, Input, InputNumber, Button, Card, message, Spin, Switch } from 'antd';
import { settingsAPI } from '../api';

const Settings: React.FC = () => {
  const { t: tr } = useTranslation();
  const [_loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(true);
  const [form] = Form.useForm();
  const [retention, setRetention] = useState({ enabled: true, days: 90, cron: '0 3 * * *', batchSize: 1000 });
  const [savingRetention, setSavingRetention] = useState(false);

  const loadSettings = useCallback(async () => {
    try {
      await settingsAPI.get();
      setCurrentValues({});
      form.setFieldsValue({});
    } catch {
      message.error(tr('settings.loadFailed'));
    } finally {
      setFetching(false);
    }
  }, [form]);

  useEffect(() => {
    setFetching(true);
    loadSettings();
  }, [loadSettings]);

  const [_currentValues, setCurrentValues] = useState<Record<string,any>>({});

  const _handleSave = async () => {
    await form.validateFields();
    setLoading(true);
    try {
      const payload: Record<string, any> = {};

      await settingsAPI.update(payload);
      message.success(tr('common.updateSuccess'));
      await loadSettings();
    } catch {
      // handled
    } finally {
      setLoading(false);
    }
  };
  void _handleSave;

  const handleSaveRetention = async () => {
    setSavingRetention(true);
    try {
      await fetch('/api/v1/settings', {
        method: 'PUT', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${localStorage.getItem('token') || ''}` },
        body: JSON.stringify({
          'audit.retention.enabled': retention.enabled ? 'true' : 'false',
          'audit.retention.days': String(retention.days),
          'audit.retention.cron': retention.cron,
          'audit.retention.batch_size': String(retention.batchSize),
        }),
      });
      message.success('审计保留策略已保存');
    } catch { message.error('保存失败'); }
    finally { setSavingRetention(false); }
  };

  return (
    <div>
      <h2 style={{ fontSize: 18, marginBottom: 16 }}>{tr('nav.settings')}</h2>
      <Spin spinning={fetching}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 700 }}>
        </div>
      </Spin>

      <Card title="📋 审计日志保留策略" size="small" style={{ maxWidth: 700, marginTop: 16 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div>
              <div style={{ fontWeight: 500 }}>启用自动清理</div>
              <div style={{ fontSize: 12, color: '#999' }}>定时删除超过保留天数的审计日志</div>
            </div>
            <Switch checked={retention.enabled} onChange={(v) => setRetention(prev => ({ ...prev, enabled: v }))} />
          </div>
          <div style={{ display: 'flex', gap: 16 }}>
            <div>
              <div style={{ fontSize: 12, color: '#999', marginBottom: 4 }}>保留天数（1-365）</div>
              <InputNumber min={1} max={365} value={retention.days} disabled={!retention.enabled}
                onChange={(v) => setRetention(prev => ({ ...prev, days: v || 90 }))} style={{ width: 120 }} />
            </div>
            <div>
              <div style={{ fontSize: 12, color: '#999', marginBottom: 4 }}>清理时间（cron）</div>
              <Input placeholder="0 3 * * *" value={retention.cron} disabled={!retention.enabled}
                onChange={(e) => setRetention(prev => ({ ...prev, cron: e.target.value }))} style={{ width: 130 }} />
            </div>
            <div>
              <div style={{ fontSize: 12, color: '#999', marginBottom: 4 }}>批量删除数（100-5000）</div>
              <InputNumber min={100} max={5000} value={retention.batchSize} disabled={!retention.enabled}
                onChange={(v) => setRetention(prev => ({ ...prev, batchSize: v || 1000 }))} style={{ width: 120 }} />
            </div>
          </div>
          <Button type="primary" loading={savingRetention} onClick={handleSaveRetention} style={{ alignSelf: 'flex-start' }}>保存</Button>
        </div>
      </Card>
    </div>
  );
};

export default Settings;
