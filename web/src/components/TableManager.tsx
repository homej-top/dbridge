import React, { useEffect, useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Table, Input, Button, Space, Tag, Popconfirm, Tooltip, Empty, message,
} from 'antd';
import {
  SearchOutlined, ReloadOutlined, PlusOutlined, PlayCircleOutlined,
  EditOutlined, DeleteOutlined, CodeOutlined,
} from '@ant-design/icons';
import type { TableListItem } from '../types';
import { dsAPI, queryAPI } from '../api';
import { getDialect } from '../utils/dialect';

type TableManagerProps = {
  dataSourceId: string;
  schema: string;
  database?: string;
  dbType: string;
  readOnly: boolean;
  onCreateTable: () => void;
  onCreateView: () => void;
  onEditTable: (table: string) => void;
  onEditView: (view: string) => void;
  onViewDefinition: (view: string) => void;
  onViewData: (table: string) => void;
  onRefreshSchemas: () => void;
};

const TableManager: React.FC<TableManagerProps> = ({
  dataSourceId, schema, database, dbType, readOnly,
  onCreateTable, onCreateView, onEditTable, onEditView, onViewDefinition, onViewData, onRefreshSchemas,
}) => {
  const { t: tr } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [tableList, setTableList] = useState<TableListItem[]>([]);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [currentPage, setCurrentPage] = useState(1);
  void currentPage;
  const [pageSize, ] = useState(20);
  void pageSize;

  const fetchList = async () => {
    if (!dataSourceId || !schema) return;
    setLoading(true);
    try {
      const res = await dsAPI.tableList(dataSourceId, schema, database || undefined);
      const data = res.data?.data;
      setTableList(Array.isArray(data) ? data : (data?.list || []));
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    setCurrentPage(1);
    setSearchKeyword('');
    fetchList();
  }, [dataSourceId, schema, database]);

  const filteredList = useMemo(() => {
    if (!searchKeyword) return tableList;
    const kw = searchKeyword.toLowerCase();
    return tableList.filter(t => t.name.toLowerCase().includes(kw));
  }, [tableList, searchKeyword]);

  const handleDelete = async (item: TableListItem) => {
    const gen = getDialect(dbType);
    const sql = item.type === 'view'
      ? gen.dropView(schema, item.name, gen.supportsIfExists())
      : gen.dropTable(schema, item.name, gen.supportsIfExists());
    try {
      await queryAPI.executeDDL({ data_source_id: dataSourceId, sql, database: database || undefined });
      message.success(`已删除 ${item.name}`);
      fetchList();
      onRefreshSchemas();
    } catch {
      // handled by interceptor
    }
  };

  const formatRowCount = (n: number | null) => {
    if (n === null || n === undefined) return '-';
    if (n >= 1000000) return `${(n / 1000000).toFixed(1)}M`;
    if (n >= 1000) return `${(n / 1000).toFixed(1)}K`;
    return String(n);
  };

  const columns = [
    {
      title: tr('dbManage.tableName'), dataIndex: 'name', width: 200, ellipsis: true,
      render: (name: string, record: TableListItem) => (
        <a onClick={() => {
          if (record.type === 'view') onEditView(name);
          else onEditTable(name);
        }}>{name}</a>
      ),
    },
    {
      title: tr('dbManage.tableType'), dataIndex: 'type', width: 70,
      render: (type: string) => (
        <Tag color={type === 'view' ? 'green' : 'blue'} style={{ fontSize: 12 }}>
          {type === 'view' ? tr('dbManage.views') : tr('dbManage.tables')}
        </Tag>
      ),
    },
    {
      title: tr('dbManage.engine'), dataIndex: 'engine', width: 90,
      render: (v: string | null) => v || '-',
    },
    {
      title: tr('dbManage.rows'), dataIndex: 'row_count', width: 90, align: 'right' as const,
      render: (v: number | null, record: TableListItem) => record.type === 'view' ? '-' : formatRowCount(v),
    },
    {
      title: tr('dbManage.comment'), dataIndex: 'comment', width: 180, ellipsis: true,
      render: (v: string) => v ? <Tooltip title={v}>{v}</Tooltip> : '-',
    },
    {
      title: tr('dbManage.createTime'), dataIndex: 'create_time', width: 150,
      render: (v: string | null) => v || '-',
    },
    {
      title: tr('dbManage.updateTime'), dataIndex: 'update_time', width: 150,
      render: (v: string | null) => v || '-',
    },
    {
      title: tr('dbManage.action'), dataIndex: 'action', width: 150, fixed: 'right' as const,
      render: (_: any, record: TableListItem) => (
        <Space size={4}>
          <Tooltip title={tr('dbManage.viewData')}>
            <Button type="text" size="small" icon={<PlayCircleOutlined />}
              onClick={() => onViewData(record.name)} />
          </Tooltip>
          {record.type === 'table' ? (
            <Tooltip title={tr('dbManage.editTable')}>
              <Button type="text" size="small" icon={<EditOutlined />}
                onClick={() => onEditTable(record.name)} />
            </Tooltip>
          ) : (
            <>
              <Tooltip title="编辑视图">
                <Button type="text" size="small" icon={<EditOutlined />}
                  onClick={() => onEditView(record.name)} />
              </Tooltip>
              <Tooltip title="查看视图定义">
                <Button type="text" size="small" icon={<CodeOutlined />}
                  onClick={() => onViewDefinition(record.name)} />
              </Tooltip>
            </>
          )}
          {!readOnly && (
            <Popconfirm
              title={`${tr('dbManage.confirmDelete')}${record.type === 'view' ? tr('dbManage.views') : tr('dbManage.tables')} "${record.name}" ?`}
              description={tr('dbManage.deleteWarning')}
              onConfirm={() => handleDelete(record)}
              okText={tr('dbManage.confirmDelete')}
              cancelText={tr('common.cancelText')}
              okButtonProps={{ danger: true }}
            >
              <Tooltip title={tr('dbManage.delete')}>
                <Button type="text" size="small" danger icon={<DeleteOutlined />} />
              </Tooltip>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <div style={{ marginBottom: 12 }}>
        <div style={{ fontWeight: 600, fontSize: 15, marginBottom: 8 }}>
          {schema} · {tr('dbManage.tableManagement')}
        </div>
        <Space wrap>
          <Input
            placeholder={tr('dbManage.searchTable')}
            prefix={<SearchOutlined />}
            value={searchKeyword}
            onChange={e => { setSearchKeyword(e.target.value); setCurrentPage(1); }}
            style={{ width: 220 }}
            allowClear
          />
          {!readOnly && (
            <>
              <Button icon={<PlusOutlined />} onClick={onCreateTable}>{tr('dbManage.createTable')}</Button>
              <Button icon={<PlusOutlined />} onClick={onCreateView}>{tr('dbManage.createView')}</Button>
            </>
          )}
          <Button icon={<ReloadOutlined />} onClick={fetchList}>{tr("dbManage.refresh")}</Button>
        </Space>
      </div>
      <div style={{ flex: 1, overflow: 'auto' }}>
        <Table
          size="small"
          dataSource={filteredList}
          columns={columns}
          rowKey="name"
          loading={loading}
          scroll={{ x: 900 }}
          pagination={{
            total: filteredList.length,
            showSizeChanger: true,
            pageSizeOptions: ['10', '20', '50', '100'],
            showTotal: (total) => `${tr('common.total')} ${total} ${tr('common.rows')}`,
          }}
          locale={{ emptyText: <Empty description="暂无表/视图" /> }}
        />
      </div>
    </div>
  );
};

export default TableManager;
