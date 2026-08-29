import React, { useEffect, useState, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Table, Tag, Select, Tooltip, Space } from 'antd';
import { auditAPI } from '../api';

const operationTag = (_mod: string, op: string, t: (key: string) => string) => {
  const map: Record<string, { color: string; label: string }> = {
    user_login: { color: 'green', label: t('audit.opUserLogin') },
    ds_create: { color: 'blue', label: t('audit.opDsCreate') },
    ds_update: { color: 'blue', label: t('audit.opDsUpdate') },
    ds_delete: { color: 'red', label: t('audit.opDsDelete') },
    query_execute: { color: 'cyan', label: t('audit.opQueryExecute') },
    ddl_execute: { color: 'purple', label: 'DDL' },
    sync_start: { color: 'blue', label: t('audit.opSyncStart') },
    sync_complete: { color: 'green', label: t('audit.opSyncComplete') },
    ai_chat: { color: 'magenta', label: t('audit.opAiChat') },
    ai_approve: { color: 'green', label: t('audit.opAiApprove') },
    ai_reject: { color: 'orange', label: t('audit.opAiReject') },
    sync_structure: { color: 'blue', label: t('audit.syncStructure') },
    sync_data: { color: 'green', label: t('audit.syncData') },
    alter_table: { color: 'purple', label: t('audit.alterTable') },
  };
  const info = map[op] || { color: 'default', label: op };
  return <Tag color={info.color}>{info.label}</Tag>;
};

const _summarizeAlter = (d: any, t: (key: string, opts?: any) => string) => {
  const ds = d.data_source_name || d.data_source_id || '';
  const schema = d.schema ? `(${d.schema})` : '';
  const sub = Array.isArray(d.sub_actions) ? d.sub_actions.join(', ') : '';
  const status = d.success ? t('audit.success') : (d.error ? t('audit.failedDetail', { error: d.error }) : '');
  return `${ds}${schema} · ${d.table || ''}${sub ? ` · ${sub}` : ''}${status ? ` · ${status}` : ''}`;
};
void _summarizeAlter;

const parseDetails = (raw: string) => {
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
};

const AuditLogs: React.FC = () => {
  const { t: tr } = useTranslation();
  const [data, setData] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [operation, setOperation] = useState('');
  const [filterModule, setFilterModule] = useState('');
  const [filterResult, setFilterResult] = useState('');
  const moduleOptions = useMemo(() => [
    { label: tr('audit.moduleAll'), value: '' },
    { label: tr('audit.moduleSystem'), value: 'system' },
    { label: tr('audit.moduleDatasource'), value: 'datasource' },
    { label: tr('audit.moduleQuery'), value: 'query' },
    { label: tr('audit.moduleSync'), value: 'sync' },
    { label: tr('audit.moduleAi'), value: 'ai' },
    { label: tr('audit.moduleSecurity'), value: 'security' },
    { label: tr('audit.moduleReport'), value: 'report' },
    { label: tr('audit.moduleExport'), value: 'export' },
    { label: tr('audit.moduleCompare'), value: 'compare' },
  ], [tr]);

  const fetchData = useCallback(async (p: number, ps: number, op: string, mod: string, res: string) => {
    setLoading(true);
    try {
      const params: any = { page: p, page_size: ps };
      if (op) params.operation = op;
      if (mod) params.module = mod;
      if (res) params.result = res;
      const resp = await auditAPI.list(params);
      setData(resp.data.data?.list || []);
      setTotal(resp.data.data?.total || 0);
    } catch {
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData(page, pageSize, operation, filterModule, filterResult);
  }, [page, pageSize, operation, filterModule, filterResult, fetchData]);

  const handlePageChange = (p: number, ps?: number) => {
    setPage(p);
    if (ps && ps !== pageSize) setPageSize(ps);
  };

  const _handleOperationChange = (val: string) => {
    setOperation(val);
    setPage(1);
  };
  void _handleOperationChange;

  const columns = [
    { title: tr('audit.moduleLabel'), dataIndex: 'module', key: 'module', width: 90,
      render: (v: string) => {
        const labels: Record<string, { color: string; label: string }> = {
          system: { color: 'blue', label: tr('audit.moduleSystem') },
          datasource: { color: 'cyan', label: tr('audit.moduleDatasource') },
          query: { color: 'green', label: tr('audit.moduleQuery') },
          sync: { color: 'purple', label: tr('audit.moduleSync') },
          ai: { color: 'magenta', label: 'AI' },
          security: { color: 'red', label: tr('audit.moduleSecurity') },
          report: { color: 'orange', label: tr('audit.moduleReport') },
          export: { color: 'gold', label: tr('audit.moduleExport') },
          compare: { color: 'geekblue', label: tr('audit.moduleCompare') },
        };
        const info = labels[v] || { color: 'default', label: v || '-' };
        return <Tag color={info.color}>{info.label}</Tag>;
      } },
    { title: tr('common.actions'), dataIndex: 'operation', key: 'operation', width: 110,
      render: (op: string, r: any) => operationTag(r.module, op, tr) },
    {
      title: tr('audit.detail'),
      dataIndex: 'details',
      key: 'details',
      ellipsis: { showTitle: false },
      render: (raw: string, record: any) => {
        const d = parseDetails(raw);
        if (!d) return raw || '-';
        const parts: string[] = [];
        if (d.sql) parts.push(`SQL: ${typeof d.sql === 'string' ? d.sql.slice(0, 80) + (d.sql.length > 80 ? '...' : '') : ''}`);
        if (d.target && d.target !== record.target_id) parts.push(`${tr('audit.objectLabel')}: ${d.target}`);
        if (d.error) parts.push(`${tr('audit.errorLabel')}: ${d.error}`);
        if (d.rows_affected != null) parts.push(tr('audit.rowsAffected', { n: d.rows_affected }));
        if (d.duration) parts.push(`${d.duration}ms`);
        if (d.ds_type) parts.push(`${tr('audit.typeLabel')}: ${d.ds_type}`);
        const summary = parts.join(' | ');
        return (
          <Tooltip title={raw} placement="topLeft">
            <div style={{ maxWidth: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {summary || raw}
            </div>
          </Tooltip>
        );
      },
    },
    { title: tr('audit.resultLabel'), dataIndex: 'result', key: 'result', width: 80,
      render: (v: string) => v === 'success' ? <Tag color="success">{tr('audit.resultSuccess')}</Tag> : v === 'failure' ? <Tag color="error">{tr('audit.resultFailure')}</Tag> : '-' },
    { title: tr('audit.operator'), dataIndex: 'username', key: 'username', width: 100, ellipsis: true,
      render: (v: string, r: any) => v || r.user_id?.slice(0, 8) || '-' },
    { title: tr('audit.targetLabel'), dataIndex: 'target_name', key: 'target', width: 120, ellipsis: true,
      render: (v: string, r: any) => v || r.target_id?.slice(0, 8) || '-' },
    {
      title: 'IP',
      dataIndex: 'ip',
      key: 'ip',
      width: 130,
    },
    {
      title: tr('audit.time'),
      dataIndex: 'created_at',
      key: 'created_at',
      width: 180,
      render: (v: string) => v ? new Date(v).toLocaleString('zh-CN') : '',
    },
  ];

  const expandedRowRender = (record: any) => {
    const d = parseDetails(record.details);
    return (
      <pre style={{ margin: 0, fontSize: 12, maxHeight: 300, overflow: 'auto', background: '#f5f5f5', padding: 12, borderRadius: 4 }}>
        {d ? JSON.stringify(d, null, 2) : record.details}
      </pre>
    );
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <h2 style={{ fontSize: 18, margin: 0 }}>{tr('nav.auditLogs')}</h2>
        <Space>
          <Select style={{ width: 110 }} value={filterModule || undefined} onChange={v => { setFilterModule(v || ''); setPage(1); }} options={moduleOptions} placeholder={tr('audit.moduleLabel')} />
          <Select style={{ width: 90 }} value={filterResult || undefined} onChange={v => { setFilterResult(v || ''); setPage(1); }}
            options={[{ label: tr('audit.resultAll'), value: '' }, { label: tr('audit.resultSuccess'), value: 'success' }, { label: tr('audit.resultFailure'), value: 'failure' }]} placeholder={tr('audit.resultLabel')} />
        </Space>
      </div>
      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        expandable={{
          expandedRowRender,
          rowExpandable: (record) => !!record.details,
        }}
        pagination={{
          current: page,
          pageSize: pageSize,
          total: total,
          showSizeChanger: true,
          showTotal: (total) => `${tr('common.total')} ${total} ${tr('common.rows')}`,
          pageSizeOptions: ['10', '20', '50'],
          onChange: handlePageChange,
        }}
      />
    </div>
  );
};

export default AuditLogs;
