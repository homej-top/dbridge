import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Table, Button, Space, Modal, Input, Select, message, Tag,
  Popconfirm, Typography, Switch, Tabs,
} from 'antd';
import {
  PlusOutlined, EditOutlined, DeleteOutlined, CloudOutlined,
  HddOutlined, ReloadOutlined, CheckCircleOutlined, CloseCircleOutlined,
  QuestionCircleOutlined, StarFilled,
} from '@ant-design/icons';
import { filesAPI } from '../api';
import type { StorageProfile, StorageBinding } from '../types';

const { Title, Text } = Typography;

const StorageManagement: React.FC = () => {
  const { t } = useTranslation();
  const [profiles, setProfiles] = useState<StorageProfile[]>([]);
  const [loading, setLoading] = useState(false);
  const [connStatus, setConnStatus] = useState<Record<string, string>>({});
  const [modalOpen, setModalOpen] = useState(false);
  const [editingCode, setEditingCode] = useState<string | null>(null);
  const [formName, setFormName] = useState('');
  const [formCode, setFormCode] = useState('');
  const [formBackend, setFormBackend] = useState<'local' | 's3'>('local');
  const [formRootDir, setFormRootDir] = useState('');
  const [formEndpoint, setFormEndpoint] = useState('');
  const [formBucket, setFormBucket] = useState('');
  const [formRegion, setFormRegion] = useState('us-east-1');
  const [formAK, setFormAK] = useState('');
  const [formSK, setFormSK] = useState('');
  const [saving, setSaving] = useState(false);

  // Bindings tab state
  const [bindings, setBindings] = useState<StorageBinding[]>([]);
  const [bindingsLoading, setBindingsLoading] = useState(false);
  const [bindModalOpen, setBindModalOpen] = useState(false);
  const [bindEditing, setBindEditing] = useState<StorageBinding | null>(null);
  const [bindProfile, setBindProfile] = useState('');
  const [bindBasePath, setBindBasePath] = useState('');
  const [bindSaving, setBindSaving] = useState(false);

  const fetchProfiles = async () => {
    setLoading(true);
    try {
      const res = await filesAPI.profiles();
      setProfiles(res.data.data || []);
    } catch {} finally { setLoading(false); }
  };

  useEffect(() => { fetchProfiles(); }, []);

  const openCreate = () => {
    setEditingCode(null);
    setFormName(''); setFormCode(''); setFormBackend('local');
    setFormRootDir(''); setFormEndpoint(''); setFormBucket('');
    setFormRegion('us-east-1'); setFormAK(''); setFormSK('');
    setModalOpen(true);
  };

  const openEdit = (p: StorageProfile) => {
    setEditingCode(p.code);
    setFormName(p.name);
    setFormCode(p.code);
    setFormBackend(p.backend as any);
    setFormRootDir(p.summary?.root_dir || '');
    setFormEndpoint(p.summary?.endpoint || '');
    setFormBucket(p.summary?.bucket || '');
    setFormRegion(p.summary?.region || 'us-east-1');
    setFormAK('');
    setFormSK('');
    setModalOpen(true);
  };

  const handleSave = async () => {
    if (!formName.trim()) { message.warning(t('storageMgmt.enterName')); return; }
    setSaving(true);
    try {
      const data: any = { backend: formBackend };
      if (formBackend === 'local') {
        if (!formRootDir) { message.warning(t('storageMgmt.enterRootDir')); return; }
        data.local = { root_dir: formRootDir };
      } else {
        if (!formBucket) { message.warning(t('storageMgmt.enterBucket')); return; }
        let ep = formEndpoint.trim();
        if (ep && !ep.startsWith('http://') && !ep.startsWith('https://')) ep = 'http://' + ep;
        const isMinIO = ep.includes(':9000') || ep.includes('/minio');
        data.s3 = {
          bucket: formBucket, endpoint: ep, region: formRegion || 'us-east-1',
          access_key_id: formAK, secret_access_key: formSK,
          use_path_style: isMinIO, disable_ssl: ep.startsWith('http://'),
        };
      }
      if (editingCode) {
        // 修改：调用 UpdateProfile API（原子替换）
        await filesAPI.updateProfile(editingCode, data);
        // 若修改了 local 的 root_dir，提示路径变更
        if (formBackend === 'local') {
          message.success(t('storageMgmt.pathUpdated'), 5);
        } else {
          message.success(t('storageMgmt.updated'));
        }
      } else {
        const createData: any = { ...data, name: formName.trim(), code: formCode.trim() };
        await filesAPI.createProfile(createData);
        message.success(t('storageMgmt.created'));
      }
      setModalOpen(false);
      fetchProfiles();
    } catch {} finally { setSaving(false); }
  };

  const handleDelete = async (code: string) => {
    await filesAPI.deleteProfile(code);
    message.success(t('storageMgmt.deleted'));
    fetchProfiles();
  };

  // const handleToggle = async (p: StorageProfile) => {
  //   // 通过删除+重建实现开关（简化实现）
  //   message.info(t('storageMgmt.toggleNotSupported'));
  // };

  const handleTest = async (code: string) => {
    try {
      const r = await filesAPI.testProfile(code);
      const s = r.data?.data?.status || 'unknown';
      setConnStatus(prev => ({ ...prev, [code]: s }));
      message.success(s === 'connected' ? t('storageMgmt.testConnNormal') : t('storageMgmt.testConnFailed'));
    } catch { message.error(t('storageMgmt.testFailed')); }
  };

  const fetchBindings = async () => {
    setBindingsLoading(true);
    try {
      const res = await filesAPI.bindings();
      setBindings(res.data.data || []);
    } catch {} finally { setBindingsLoading(false); }
  };

  useEffect(() => { fetchBindings(); }, []);

  const openBindEdit = (b: StorageBinding) => {
    setBindEditing(b);
    setBindProfile(b.profile_code);
    setBindBasePath(b.base_path);
    setBindModalOpen(true);
  };

  const handleBindSave = async () => {
    if (!bindEditing) return;
    setBindSaving(true);
    try {
      await filesAPI.updateBinding(bindEditing.module_code, {
        profile_code: bindProfile,
        base_path: bindBasePath,
      });
      message.success(t('common.saved'));
      setBindModalOpen(false);
      fetchBindings();
    } catch { message.error(t('storageMgmt.opFailed')); } finally { setBindSaving(false); }
  };

  const validateBasePath = (p: string): boolean => {
    if (!p) return true;
    if (p.includes('..') || p.startsWith('/')) return false;
    if (/[*?[\]<>:"|\\]/.test(p)) return false;
    return true;
  };

  const columns = [
    { title: t('storageMgmt.name'), dataIndex: 'name', key: 'name', width: 160,
      render: (n: string, r: StorageProfile) => (
        <Space>
          {r.is_default && <StarFilled style={{ color: '#faad14' }} />}
          {n}
          {r.is_default && <Tag color="green">{t('storageMgmt.default')}</Tag>}
        </Space>
      )
    },
    { title: t('storageMgmt.code'), dataIndex: 'code', key: 'code', width: 100 },
    {
      title: t('storageMgmt.backend'), dataIndex: 'backend', key: 'backend', width: 80,
      render: (b: string) => b === 's3' ? <Space><CloudOutlined />S3</Space> : <Space><HddOutlined />{t('storageMgmt.local')}</Space>,
    },
    {
      title: t('storageMgmt.connStatus'), key: 'conn', width: 100,
      render: (_: any, r: StorageProfile) => {
        const s = connStatus[r.code];
        if (!s) return <Tag icon={<QuestionCircleOutlined />}>{t('storageMgmt.notTested')}</Tag>;
        if (s === 'connected') return <Tag color="green" icon={<CheckCircleOutlined />}>{t('storageMgmt.connected')}</Tag>;
        return <Tag color="red" icon={<CloseCircleOutlined />}>{t('storageMgmt.disconnected')}</Tag>;
      },
    },
    {
      title: t('storageMgmt.status'), dataIndex: 'enabled', key: 'enabled', width: 80,
      render: (e: boolean, r: StorageProfile) => (
        <Switch checked={e} size="small" onChange={async () => {
          try {
            await filesAPI.toggleProfile(r.code);
            message.success(e ? t('storageMgmt.disabledStatus') : t('storageMgmt.enabled'));
            fetchProfiles();
          } catch { message.error(t('storageMgmt.opFailed')); }
        }} />
      ),
    },
    {
      title: t('common.actions'), key: 'actions', width: 220,
      render: (_: any, r: StorageProfile) => (
        <Space size="small">
          <Button size="small" icon={<ReloadOutlined />} onClick={() => handleTest(r.code)}>{t('storageMgmt.test')}</Button>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>{t('common.edit')}</Button>
          <Popconfirm
            title={r.is_default
              ? `${t('storageMgmt.deleteDefault', { name: r.name })}`
              : `${t('storageMgmt.deleteConfirm', { name: r.name })}`}
            onConfirm={() => handleDelete(r.code)}
          >
            <Button size="small" danger icon={<DeleteOutlined />}>{t('common.delete')}</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const bindingColumns = [
    { title: t('storageMgmt.moduleName'), dataIndex: 'module_name', key: 'module_name', width: 160 },
    {
      title: t('storageMgmt.storageInstance'), dataIndex: 'profile_code', key: 'profile_code', width: 160,
      render: (code: string, r: StorageBinding) => (
        <Space>
          {code}
          {!r.profile_available && <Tag color="red">{t('storageMgmt.unavailable')}</Tag>}
        </Space>
      ),
    },
    { title: t('storageMgmt.basePath'), dataIndex: 'base_path', key: 'base_path', width: 160 },
    {
      title: t('common.actions'), key: 'actions', width: 100,
      render: (_: any, r: StorageBinding) => (
        <Button size="small" icon={<EditOutlined />} onClick={() => openBindEdit(r)}>{t('common.edit')}</Button>
      ),
    },
  ];

  return (
    <div>
      <Title level={4} style={{ marginBottom: 16 }}>{t('storageMgmt.title')}</Title>

      <Tabs defaultActiveKey="instances" items={[
        {
          key: 'instances',
          label: t('storageMgmt.instancesTab'),
          children: (
            <>
              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
                <span />
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>{t('storageMgmt.addInstance')}</Button>
              </div>
              <Table
                columns={columns}
                dataSource={profiles}
                rowKey="code"
                loading={loading}
                size="middle"
                pagination={false}
                locale={{ emptyText: t('storageMgmt.noInstances') }}
              />
            </>
          ),
        },
        {
          key: 'bindings',
          label: t('storageMgmt.bindingsTab'),
          children: (
            <>
              <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
                {t('storageMgmt.bindingsDesc')}
              </Text>
              <Table
                columns={bindingColumns}
                dataSource={bindings}
                rowKey="module_code"
                loading={bindingsLoading}
                size="middle"
                pagination={false}
              />
            </>
          ),
        },
      ]} />

      <Modal
        title={editingCode ? `${t('storageMgmt.editInstance')}: ${editingCode}` : t('storageMgmt.newInstance')}
        open={modalOpen}
        onOk={handleSave}
        onCancel={() => setModalOpen(false)}
        confirmLoading={saving}
        width={480}
        okText={editingCode ? t('common.save') : t('common.create')}
      >
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Input placeholder={t('storageMgmt.namePlaceholder')} value={formName} onChange={e => setFormName(e.target.value)} />
          <Input placeholder={t('storageMgmt.codePlaceholder')} value={formCode} onChange={e => setFormCode(e.target.value)}
            disabled={!!editingCode} />
          <Select value={formBackend} onChange={setFormBackend} style={{ width: '100%' }}
            disabled={!!editingCode}
            options={[{ label: t('storageMgmt.localFS'), value: 'local' }, { label: t('storageMgmt.s3Object'), value: 's3' }]} />
          {formBackend === 'local' ? (
            <Input placeholder={t('storageMgmt.rootDirPlaceholder')} value={formRootDir}
              onChange={e => setFormRootDir(e.target.value)} />
          ) : (
            <>
              <Input placeholder="Bucket" value={formBucket} onChange={e => setFormBucket(e.target.value)} />
              <Input placeholder="Endpoint" value={formEndpoint} onChange={e => setFormEndpoint(e.target.value)} />
              <Input placeholder="Region" value={formRegion} onChange={e => setFormRegion(e.target.value)} />
              <Input placeholder="Access Key" value={formAK} onChange={e => setFormAK(e.target.value)} />
              <Input.Password placeholder="Secret Key" value={formSK} onChange={e => setFormSK(e.target.value)} />
            </>
          )}
        </Space>
      </Modal>

      <Modal
        title={`${t('storageMgmt.editBinding')}: ${bindEditing?.module_name || ''}`}
        open={bindModalOpen}
        onOk={handleBindSave}
        onCancel={() => setBindModalOpen(false)}
        confirmLoading={bindSaving}
        width={480}
        okText={t('common.save')}
      >
        <Space orientation="vertical" style={{ width: '100%' }}>
          <div>
            <Text type="secondary">{t('storageMgmt.storageInstance')}</Text>
            <Select
              value={bindProfile}
              onChange={setBindProfile}
              style={{ width: '100%' }}
              options={profiles.filter(p => p.enabled).map(p => ({
                label: `${p.name} (${p.backend})`,
                value: p.code,
              }))}
            />
          </div>
          <div>
            <Text type="secondary">{t('storageMgmt.basePath')}</Text>
            <Input
              value={bindBasePath}
              onChange={e => setBindBasePath(e.target.value)}
              placeholder={t('storageMgmt.basePathPlaceholder')}
              status={!validateBasePath(bindBasePath) ? 'error' : undefined}
            />
            {!validateBasePath(bindBasePath) && (
              <Text type="danger" style={{ fontSize: 12 }}>{t('storageMgmt.basePathInvalid')}</Text>
            )}
          </div>
        </Space>
      </Modal>
    </div>
  );
};

export default StorageManagement;
