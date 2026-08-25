import React, { useEffect, useState } from 'react';
import {
  Table,
  Button,
  Modal,
  Form,
  Input,
  Select,
  Space,
  Tag,
  Progress,
  message,
} from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { syncAPI, dsAPI } from '../api';
import type { SyncTask, DataSource } from '../types';

const SyncTasks: React.FC = () => {
  const [data, setData] = useState<SyncTask[]>([]);
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = await syncAPI.list();
      setData(res.data.data?.list || []);
    } catch {
      // handled
    } finally {
      setLoading(false);
    }
  };

  const fetchDS = async () => {
    try {
      const res = await dsAPI.list();
      setDataSources(res.data.data?.list || []);
    } catch {
      // handled
    }
  };

  useEffect(() => {
    fetchData();
    fetchDS();
  }, []);

  const handleCreate = async () => {
    const values = await form.validateFields();
    try {
      await syncAPI.create(values);
      message.success('创建成功');
      setModalOpen(false);
      fetchData();
    } catch {
      // handled
    }
  };

  const handleStart = async (id: string) => {
    try {
      await syncAPI.start(id);
      message.success('任务已启动');
      fetchData();
    } catch {
      // handled
    }
  };

  const handleStop = async (id: string) => {
    try {
      await syncAPI.stop(id);
      message.success('任务已停止');
      fetchData();
    } catch {
      // handled
    }
  };

  const statusMap: Record<string, { color: string; text: string }> = {
    pending: { color: 'default', text: '等待中' },
    running: { color: 'processing', text: '运行中' },
    completed: { color: 'success', text: '已完成' },
    failed: { color: 'error', text: '失败' },
    stopped: { color: 'warning', text: '已停止' },
  };

  const columns = [
    { title: '任务名称', dataIndex: 'name', key: 'name' },
    { title: '源表', dataIndex: 'source_table', key: 'source_table' },
    { title: '目标表', dataIndex: 'target_table', key: 'target_table' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (s: string) => {
        const info = statusMap[s] || { color: 'default', text: s };
        return <Tag color={info.color}>{info.text}</Tag>;
      },
    },
    {
      title: '进度',
      dataIndex: 'progress',
      key: 'progress',
      width: 120,
      render: (p: number) => <Progress percent={Math.round(p)} size="small" strokeColor="#20a53a" />,
    },
    { title: '最后同步', dataIndex: 'last_sync_time', key: 'last_sync_time' },
    {
      title: '操作',
      key: 'action',
      width: 120,
      render: (_: any, record: SyncTask) => (
        <Space size={8}>
          {record.status === 'pending' || record.status === 'failed' ? (
            <a
              style={{ color: '#20a53a' }}
              onClick={() => handleStart(record.id)}
            >
              启动
            </a>
          ) : (
            <a
              style={{ color: '#e74c3c' }}
              onClick={() => handleStop(record.id)}
            >
              停止
            </a>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div
        style={{
          marginBottom: 16,
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
        }}
      >
        <h2 style={{ margin: 0 }}>同步任务</h2>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => setModalOpen(true)}
        >
          添加任务
        </Button>
      </div>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        pagination={{
          showSizeChanger: true,
          showTotal: (total) => `共 ${total} 条`,
          pageSizeOptions: ['10', '20', '50'],
        }}
      />

      <Modal
        title="添加同步任务"
        open={modalOpen}
        onOk={handleCreate}
        onCancel={() => setModalOpen(false)}
        okText="创建"
        cancelText="取消"
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="name"
            label="任务名称"
            rules={[{ required: true }]}
          >
            <Input placeholder="请输入任务名称" />
          </Form.Item>
          <Form.Item
            name="source_ds"
            label="源数据源"
            rules={[{ required: true }]}
          >
            <Select
              options={dataSources.map((ds) => ({
                label: ds.name,
                value: ds.id,
              }))}
              placeholder="请选择源数据源"
            />
          </Form.Item>
          <Form.Item
            name="target_ds"
            label="目标数据源"
            rules={[{ required: true }]}
          >
            <Select
              options={dataSources.map((ds) => ({
                label: ds.name,
                value: ds.id,
              }))}
              placeholder="请选择目标数据源"
            />
          </Form.Item>
          <Form.Item
            name="source_table"
            label="源表名"
            rules={[{ required: true }]}
          >
            <Input placeholder="请输入源表名" />
          </Form.Item>
          <Form.Item
            name="target_table"
            label="目标表名"
            rules={[{ required: true }]}
          >
            <Input placeholder="请输入目标表名" />
          </Form.Item>
          <Form.Item name="sync_mode" label="同步模式" initialValue="full">
            <Select
              options={[
                { label: '全量同步', value: 'full' },
                { label: '增量同步', value: 'incremental' },
                { label: 'DDL 同步', value: 'ddl' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default SyncTasks;
