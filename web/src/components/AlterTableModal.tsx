import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal, Alert, Space, Button, Switch, Typography, Divider, message } from 'antd';
import Editor from '@monaco-editor/react';
import type { AlterChange } from '../api';
import { tableAPI } from '../api';

const { Text } = Typography;

type Props = {
  open: boolean;
  dataSourceId: string;
  schema: string;
  database?: string;
  table: string;
  changes: AlterChange[];
  onCancel: () => void;
  onSuccess: (result: any) => void;
};

const AlterTableModal: React.FC<Props> = ({
  open, dataSourceId, schema, database, table, changes, onCancel, onSuccess,
}) => {
  const { t: tr } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [executing, setExecuting] = useState(false);
  const [ddl, setDdl] = useState('');
  const [rollbackDdl, setRollbackDdl] = useState('');
  const [warnings, setWarnings] = useState<string[]>([]);
  const [highRisk, setHighRisk] = useState(false);
  const [editable, setEditable] = useState(false);
  const [confirmed, setConfirmed] = useState(false);

  useEffect(() => {
    if (!open) {
      setDdl('');
      setRollbackDdl('');
      setWarnings([]);
      setHighRisk(false);
      setEditable(false);
      setConfirmed(false);
      return;
    }
    const load = async () => {
      setLoading(true);
      try {
        const res = await tableAPI.previewAlter({
          data_source_id: dataSourceId,
          schema: schema || undefined,
          database: database || undefined,
          table,
          changes,
        });
        const data = res.data.data;
        setDdl(data?.ddl || '');
        setRollbackDdl(data?.rollback_ddl || '');
        setWarnings(data?.warnings || []);
        setHighRisk(!!data?.high_risk);
      } catch {
        // handled by interceptor
      } finally {
        setLoading(false);
      }
    };
    load();
  }, [open, dataSourceId, schema, table, changes]);

  const handleExecute = async () => {
    if (highRisk && !confirmed) {
      message.warning('请先勾选"我已知晓风险, 确认执行"');
      return;
    }
    setExecuting(true);
    try {
      const payload: any = {
        data_source_id: dataSourceId,
        schema: schema || undefined,
        database: database || undefined,
        table,
        changes: [],
      };
      if (editable) {
        payload.override_ddl = ddl;
      } else {
        payload.changes = changes;
      }
      const res = await tableAPI.alter(payload);
      const result = res.data.data;
      if (result?.success) {
        message.success(`结构变更成功, 耗时 ${result.duration || 0}ms`);
        onSuccess(result);
      } else {
        Modal.error({
          title: '执行失败',
          width: 700,
          content: (
            <div style={{ fontSize: 13 }}>
              <div style={{ color: '#ff4d4f', marginBottom: 8 }}>{result?.error}</div>
              {result?.executed?.length > 0 && (
                <div>
                  <Text strong>已执行:</Text>
                  <pre style={{ fontSize: 11, background: '#f5f5f5', padding: 8, maxHeight: 120, overflow: 'auto' }}>
                    {result.executed.join(';\n')}
                  </pre>
                </div>
              )}
              {result?.not_executed?.length > 0 && (
                <div>
                  <Text strong>未执行:</Text>
                  <pre style={{ fontSize: 11, background: '#f5f5f5', padding: 8, maxHeight: 120, overflow: 'auto' }}>
                    {result.not_executed.join(';\n')}
                  </pre>
                </div>
              )}
              {result?.rollback_script_path && (
                <div style={{ marginTop: 8, color: '#666' }}>
                  回退脚本: {result.rollback_script_path}
                </div>
              )}
            </div>
          ),
        });
      }
    } catch {
      // handled
    } finally {
      setExecuting(false);
    }
  };

  const footer = (
    <Space>
      <Button onClick={onCancel} disabled={executing}>取消</Button>
      {highRisk && (
        <Space size={4}>
          <Switch size="small" checked={confirmed} onChange={setConfirmed} />
          <Text type="danger" style={{ fontSize: 13 }}>我已知晓风险, 确认执行</Text>
        </Space>
      )}
      <Button type="primary" danger={highRisk} loading={executing} onClick={handleExecute}>
        确认执行
      </Button>
    </Space>
  );

  return (
    <Modal
      title={`${tr('dbManage.alterPreview')} · ${schema ? schema + '.' : ''}${table}`}
      open={open}
      onCancel={onCancel}
      zIndex={1050}
      width={900}
      footer={footer}
      destroyOnHidden
      confirmLoading={loading || executing}
    >
      <div style={{ marginBottom: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Text strong>{tr('dbManage.pendingDDL')}:</Text>
        <Space size={4}>
          <Switch size="small" checked={editable} onChange={setEditable} />
          <Text style={{ fontSize: 12 }}>编辑模式 (微调 DDL)</Text>
        </Space>
      </div>
      <div style={{ border: '1px solid #d9d9d9', borderRadius: 4, marginBottom: 12 }}>
        <Editor
          height={180}
          defaultLanguage="sql"
          value={ddl}
          onChange={(v) => setDdl(v || '')}
          loading={loading ? '生成中...' : undefined}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            wordWrap: 'on',
            readOnly: !editable,
            scrollBeyondLastLine: false,
          }}
        />
      </div>

      <Text strong style={{ display: 'block', marginBottom: 8 }}>回退脚本 (自动生成, 不执行):</Text>
      <div style={{ border: '1px solid #d9d9d9', borderRadius: 4, background: '#fafafa', marginBottom: 12 }}>
        <Editor
          height={120}
          defaultLanguage="sql"
          value={rollbackDdl}
          options={{
            minimap: { enabled: false },
            fontSize: 12,
            wordWrap: 'on',
            readOnly: true,
            scrollBeyondLastLine: false,
          }}
        />
      </div>

      {warnings.length > 0 && (
        <>
          <Divider style={{ margin: '8px 0' }} />
          <Alert
            type={highRisk ? 'error' : 'warning'}
            showIcon
            message={highRisk ? '高危操作警告' : '注意事项'}
            description={
              <ul style={{ margin: 0, paddingLeft: 20 }}>
                {warnings.map((w, i) => (
                  <li key={i} style={{ color: highRisk ? '#ff4d4f' : '#faad14' }}>{w}</li>
                ))}
              </ul>
            }
          />
        </>
      )}
    </Modal>
  );
};

export default AlterTableModal;
