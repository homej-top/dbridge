import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
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
  const { t: tr } = useTranslation();
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
      message.success(tr('syncTasks.createdSuccess'));
      setModalOpen(false);
      fetchData();
    } catch {
      // handled
    }
  };

  const handleStart = async (id: string) => {
    try {
      await syncAPI.start(id);
      message.success(tr('syncTasks.taskStarted'));
      fetchData();
    } catch {
      // handled
    }
  };

  const handleStop = async (id: string) => {
    try {
      await syncAPI.stop(id);
      message.success(tr('syncTasks.taskStopped'));
      fetchData();
    } catch {
      // handled
    }
  };

  const statusMap: Record<string, { color: string; text: string }> = {
    pending: { color: 'default', text: tr('syncTasks.statusPending') },
    running: { color: 'processing', text: tr('syncTasks.statusRunning') },
    completed: { color: 'success', text: tr('syncTasks.statusCompleted') },
    failed: { color: 'error', text: tr('syncTasks.statusFailed') },
    stopped: { color: 'warning', text: tr('syncTasks.statusStopped') },
  };

  const columns = [
    { title: tr('syncTasks.taskName'), dataIndex: 'name', key: 'name' },
    { title: tr('syncTasks.sourceTable'), dataIndex: 'source_table', key: 'source_table' },
    { title: tr('syncTasks.targetTable'), dataIndex: 'target_table', key: 'target_table' },
    {
      title: tr('syncTasks.status'),
      dataIndex: 'status',
      key: 'status',
      render: (s: string) => {
        const info = statusMap[s] || { color: 'default', text: s };
        return <Tag color={info.color}>{info.text}</Tag>;
      },
    },
    {
      title: tr('syncTasks.progress'),
      dataIndex: 'progress',
      key: 'progress',
      width: 120,
      render: (p: number) => <Progress percent={Math.round(p)} size="small" strokeColor="#20a53a" />,
    },
    { title: tr('syncTasks.lastSync'), dataIndex: 'last_sync_time', key: 'last_sync_time' },
    {
      title: tr('syncTasks.action'),
      key: 'action',
      width: 120,
      render: (_: any, record: SyncTask) => (
        <Space size={8}>
          {record.status === 'pending' || record.status === 'failed' ? (
            <a
              style={{ color: '#20a53a' }}
              onClick={() => handleStart(record.id)}
            >
              {tr('syncTasks.start')}
            </a>
          ) : (
            <a
              style={{ color: '#e74c3c' }}
              onClick={() => handleStop(record.id)}
            >
              {tr('syncTasks.stop')}
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
        <h2 style={{ margin: 0 }}>{tr('syncTasks.title')}</h2>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => setModalOpen(true)}
        >
          {tr('syncTasks.addTask')}
        </Button>
      </div>

      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        pagination={{
          showSizeChanger: true,
          showTotal: (total) => tr('syncTasks.totalItems', { total }),
          pageSizeOptions: ['10', '20', '50'],
        }}
      />

      <Modal
        title={tr('syncTasks.createTask')}
        open={modalOpen}
        onOk={handleCreate}
        onCancel={() => setModalOpen(false)}
        okText={tr('syncTasks.create')}
        cancelText={tr('common.cancel')}
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="name"
            label={tr('syncTasks.taskName')}
            rules={[{ required: true }]}
          >
            <Input placeholder={tr('syncTasks.taskNamePlaceholder')} />
          </Form.Item>
          <Form.Item
            name="source_ds"
            label={tr('syncTasks.sourceDS')}
            rules={[{ required: true }]}
          >
            <Select
              options={dataSources.map((ds) => ({
                label: ds.name,
                value: ds.id,
              }))}
              placeholder={tr('syncTasks.selectSourceDS')}
            />
          </Form.Item>
          <Form.Item
            name="target_ds"
            label={tr('syncTasks.targetDS')}
            rules={[{ required: true }]}
          >
            <Select
              options={dataSources.map((ds) => ({
                label: ds.name,
                value: ds.id,
              }))}
              placeholder={tr('syncTasks.selectTargetDS')}
            />
          </Form.Item>
          <Form.Item
            name="source_table"
            label={tr('syncTasks.sourceTableName')}
            rules={[{ required: true }]}
          >
            <Input placeholder={tr('syncTasks.sourceTablePlaceholder')} />
          </Form.Item>
          <Form.Item
            name="target_table"
            label={tr('syncTasks.targetTableName')}
            rules={[{ required: true }]}
          >
            <Input placeholder={tr('syncTasks.targetTablePlaceholder')} />
          </Form.Item>
          <Form.Item name="sync_mode" label={tr('syncTasks.syncMode')} initialValue="full">
            <Select
              options={[
                { label: tr('syncTasks.modeFull'), value: 'full' },
                { label: tr('syncTasks.modeIncremental'), value: 'incremental' },
                { label: tr('syncTasks.modeDDL'), value: 'ddl' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default SyncTasks;
