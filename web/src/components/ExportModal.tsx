import React, { useState, useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Modal, Checkbox, Select, Button, Spin, message, Space, Radio, Statistic, Row, Col,
} from 'antd';
import {
  DownloadOutlined, CopyOutlined, EyeOutlined,
} from '@ant-design/icons';
import Editor from '@monaco-editor/react';
import { dbTransferAPI, dsAPI } from '../api';
import type { TableListItem } from '../types';

interface Props {
  open: boolean;
  dataSourceId: string;
  schema: string;
  dbType: string;
  onClose: () => void;
}

const ExportModal: React.FC<Props> = ({ open, dataSourceId, schema, dbType, onClose }) => {
  const { t } = useTranslation('exportModal');
  const [tables, setTables] = useState<TableListItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedTables, setSelectedTables] = useState<string[]>([]);
  const [targetDbType, setTargetDbType] = useState<string>(dbType === 'postgres' ? 'postgres' : dbType === 'sqlserver' ? 'sqlserver' : 'mysql');
  const [includeStructure, setIncludeStructure] = useState(true);
  const [includeData, setIncludeData] = useState(true);
  const [batchSize, setBatchSize] = useState(500);

  const [generating, setGenerating] = useState(false);
  const [generatedSQL, setGeneratedSQL] = useState('');
  const [stats, setStats] = useState<{ tableCount: number; rowCount: number; durationMs: number } | null>(null);
  const [step, setStep] = useState<'config' | 'preview'>('config');

  useEffect(() => {
    if (open && dataSourceId && schema) {
      setLoading(true);
      setStep('config');
      setGeneratedSQL('');
      setStats(null);
      setSelectedTables([]);
      setTargetDbType(dbType);
      dsAPI.tableList(dataSourceId, schema).then(res => {
        const list: TableListItem[] = (res.data?.data?.list || []).filter((t: TableListItem) => t.type === 'table');
        setTables(list);
        setSelectedTables(list.map((t: TableListItem) => t.name));
      }).catch(() => {}).finally(() => setLoading(false));
    }
  }, [open, dataSourceId, schema, dbType]);

  const handleGenerate = async () => {
    if (!includeStructure && !includeData) {
      message.warning(t('selectAtLeastOne'));
      return;
    }
    setGenerating(true);
    try {
      const res = await dbTransferAPI.export({
        ds_id: dataSourceId,
        schema,
        target_db_type: targetDbType,
        tables: selectedTables,
        include_structure: includeStructure,
        include_data: includeData,
        batch_size: batchSize,
      });
      const d = res.data?.data;
      setGeneratedSQL(d?.sql || '');
      setStats({
        tableCount: d?.table_count || 0,
        rowCount: d?.row_count || 0,
        durationMs: d?.duration_ms || 0,
      });
      setStep('preview');
    } catch { /* handled */ }
    finally { setGenerating(false); }
  };

  const handleDownload = () => {
    const blob = new Blob([generatedSQL], { type: 'text/sql;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${schema}_${targetDbType}_${new Date().toISOString().slice(0, 10)}.sql`;
    a.click();
    URL.revokeObjectURL(url);
    message.success(t('fileDownloaded'));
  };

  const handleCopy = () => {
    navigator.clipboard.writeText(generatedSQL).then(
      () => message.success(t('copiedToClipboard')),
      () => message.error(t('copyFailed')),
    );
  };

  const allSelected = selectedTables.length === tables.length && tables.length > 0;
  const indeterminate = selectedTables.length > 0 && selectedTables.length < tables.length;

  const toggleAll = (checked: boolean) => {
    setSelectedTables(checked ? tables.map(t => t.name) : []);
  };

  const sqlSize = useMemo(() => {
    const bytes = new Blob([generatedSQL]).size;
    if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
    if (bytes > 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${bytes} B`;
  }, [generatedSQL]);

  return (
    <Modal
      title={step === 'config' ? `${t('exportSQL')} · ${schema}` : `${t('previewExport')} · ${schema}`}
      open={open}
      onCancel={onClose}
      width={step === 'preview' ? 900 : 600}
      footer={step === 'config' ? (
        <Space>
          <Button onClick={onClose}>{t('cancel')}</Button>
          <Button
            type="primary"
            icon={<EyeOutlined />}
            loading={generating}
            disabled={selectedTables.length === 0}
            onClick={handleGenerate}
          >
            {t('generateSQL')}
          </Button>
        </Space>
      ) : (
        <Space>
          <Button onClick={() => setStep('config')}>{t('backToModify')}</Button>
          <Button icon={<CopyOutlined />} onClick={handleCopy}>{t('copy')}</Button>
          <Button type="primary" icon={<DownloadOutlined />} onClick={handleDownload}>{t('downloadSql')}</Button>
        </Space>
      )}
      destroyOnHidden
    >
      {step === 'config' ? (
        <Spin spinning={loading || generating}>
          <div style={{ marginBottom: 16 }}>
            <div style={{ marginBottom: 8, fontWeight: 500 }}>{t('targetDbType')}</div>
            <Radio.Group value={targetDbType} onChange={e => setTargetDbType(e.target.value)}>
              <Radio.Button value="mysql">MySQL</Radio.Button>
              <Radio.Button value="postgres">PostgreSQL</Radio.Button>
              <Radio.Button value="sqlserver">SQL Server</Radio.Button>
            </Radio.Group>
            {targetDbType !== dbType && (
              <span style={{ marginLeft: 8, color: '#fa8c16', fontSize: 12 }}>
                {t('typeConversion')}: {dbType} → {targetDbType}
              </span>
            )}
          </div>

          <div style={{ marginBottom: 16 }}>
            <div style={{ marginBottom: 8, fontWeight: 500 }}>{t('exportContent')}</div>
            <Space>
              <Checkbox checked={includeStructure} onChange={e => setIncludeStructure(e.target.checked)}>
                {t('tableStructureDDL')}
              </Checkbox>
              <Checkbox checked={includeData} onChange={e => setIncludeData(e.target.checked)}>
                {t('dataINSERT')}
              </Checkbox>
              {includeData && (
                <Select
                  size="small"
                  value={batchSize}
                  onChange={setBatchSize}
                  style={{ width: 120 }}
                  options={[
                    { label: t('rowsPerBatch100'), value: 100 },
                    { label: t('rowsPerBatch500'), value: 500 },
                    { label: t('rowsPerBatch1000'), value: 1000 },
                    { label: t('rowsPerBatch5000'), value: 5000 },
                  ]}
                />
              )}
            </Space>
          </div>

          <div style={{ marginBottom: 8, fontWeight: 500 }}>
            {t('selectTables')} ({selectedTables.length}/{tables.length})
          </div>
          <div style={{ marginBottom: 8 }}>
            <Checkbox
              checked={allSelected}
              indeterminate={indeterminate}
              onChange={e => toggleAll(e.target.checked)}
            >
              {t('selectAll')}
            </Checkbox>
          </div>
          <div style={{ maxHeight: 300, overflow: 'auto', border: '1px solid #f0f0f0', borderRadius: 4, padding: 8 }}>
            <Checkbox.Group
              value={selectedTables}
              onChange={vals => setSelectedTables(vals as string[])}
              style={{ width: '100%' }}
            >
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '4px 16px' }}>
                {tables.map(t => (
                  <Checkbox key={t.name} value={t.name}>
                    {t.name}
                    {t.row_count !== null && (
                      <span style={{ color: '#999', fontSize: 11, marginLeft: 4 }}>({t.row_count})</span>
                    )}
                  </Checkbox>
                ))}
              </div>
            </Checkbox.Group>
          </div>
        </Spin>
      ) : (
        <div>
          {stats && (
            <Row gutter={16} style={{ marginBottom: 12 }}>
              <Col span={8}>
                <Statistic title={t('tablesExported')} value={stats.tableCount} />
              </Col>
              <Col span={8}>
                <Statistic title={t('dataRows')} value={stats.rowCount} />
              </Col>
              <Col span={4}>
                <Statistic title={t('duration')} value={stats.durationMs} suffix="ms" />
              </Col>
              <Col span={4}>
                <Statistic title={t('size')} value={sqlSize} />
              </Col>
            </Row>
          )}
          <div style={{ border: '1px solid #d9d9d9', borderRadius: 4 }}>
            <Editor
              height={450}
              defaultLanguage="sql"
              value={generatedSQL}
              options={{
                minimap: { enabled: false },
                fontSize: 13,
                wordWrap: 'on',
                readOnly: true,
                scrollBeyondLastLine: false,
              }}
            />
          </div>
        </div>
      )}
    </Modal>
  );
};

export default ExportModal;
