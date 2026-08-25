import React, { useEffect, useState, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Table, Tag, Select, Tooltip, Space } from 'antd';
import { auditAPI } from '../api';

const operationTag = (_mod: string, op: string) => {
  const map: Record<string, { color: string; label: string }> = {
    user_login: { color: 'green', label: '登录' },
    ds_create: { color: 'blue', label: '创建数据源' },
    ds_update: { color: 'blue', label: '修改数据源' },
    ds_delete: { color: 'red', label: '删除数据源' },
    query_execute: { color: 'cyan', label: '查询' },
    ddl_execute: { color: 'purple', label: 'DDL' },
    sync_start: { color: 'blue', label: '开始同步' },
    sync_complete: { color: 'green', label: '同步完成' },
    ai_chat: { color: 'magenta', label: 'AI 对话' },
    ai_approve: { color: 'green', label: 'AI 审批通过' },
    ai_reject: { color: 'orange', label: 'AI 审批拒绝' },
    sync_structure: { color: 'blue', label: '结构同步' },
    sync_data: { color: 'green', label: '数据同步' },
    alter_table: { color: 'purple', label: '表结构变更' },
  };
  const info = map[op] || { color: 'default', label: op };
  return <Tag color={info.color}>{info.label}</Tag>;
};

const _summarizeAlter = (d: any) => {
  const ds = d.data_source_name || d.data_source_id || '';
  const schema = d.schema ? `(${d.schema})` : '';
  const sub = Array.isArray(d.sub_actions) ? d.sub_actions.join(', ') : '';
  const status = d.success ? '成功' : (d.error ? `失败: ${d.error}` : '');
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
    { label: '全部模块', value: '' },
    { label: '系统', value: 'system' },
    { label: '数据连接', value: 'datasource' },
    { label: '数据操作', value: 'query' },
    { label: '数据迁移', value: 'sync' },
    { label: 'AI 中心', value: 'ai' },
    { label: '安全', value: 'security' },
    { label: '报表', value: 'report' },
    { label: '导出', value: 'export' },
    { label: '对比', value: 'compare' },
  ], []);

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
    { title: '模块', dataIndex: 'module', key: 'module', width: 90,
      render: (v: string) => {
        const labels: Record<string, { color: string; label: string }> = {
          system: { color: 'blue', label: '系统' },
          datasource: { color: 'cyan', label: '数据源' },
          query: { color: 'green', label: '查询' },
          sync: { color: 'purple', label: '同步' },
          ai: { color: 'magenta', label: 'AI' },
          security: { color: 'red', label: '安全' },
          report: { color: 'orange', label: '报表' },
          export: { color: 'gold', label: '导出' },
          compare: { color: 'geekblue', label: '对比' },
        };
        const info = labels[v] || { color: 'default', label: v || '-' };
        return <Tag color={info.color}>{info.label}</Tag>;
      } },
    { title: '操作', dataIndex: 'operation', key: 'operation', width: 110,
      render: (op: string, r: any) => operationTag(r.module, op) },
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
        if (d.target && d.target !== record.target_id) parts.push(`对象: ${d.target}`);
        if (d.error) parts.push(`错误: ${d.error}`);
        if (d.rows_affected != null) parts.push(`${d.rows_affected} 行`);
        if (d.duration) parts.push(`${d.duration}ms`);
        if (d.ds_type) parts.push(`类型: ${d.ds_type}`);
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
    { title: '结果', dataIndex: 'result', key: 'result', width: 80,
      render: (v: string) => v === 'success' ? <Tag color="success">成功</Tag> : v === 'failure' ? <Tag color="error">失败</Tag> : '-' },
    { title: '操作者', dataIndex: 'username', key: 'username', width: 100, ellipsis: true,
      render: (v: string, r: any) => v || r.user_id?.slice(0, 8) || '-' },
    { title: '目标', dataIndex: 'target_name', key: 'target', width: 120, ellipsis: true,
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
          <Select style={{ width: 110 }} value={filterModule || undefined} onChange={v => { setFilterModule(v || ''); setPage(1); }} options={moduleOptions} placeholder="模块" />
          <Select style={{ width: 90 }} value={filterResult || undefined} onChange={v => { setFilterResult(v || ''); setPage(1); }}
            options={[{ label: '全部结果', value: '' }, { label: '成功', value: 'success' }, { label: '失败', value: 'failure' }]} placeholder="结果" />
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
