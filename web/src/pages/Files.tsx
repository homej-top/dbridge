import React, { useEffect, useState, useCallback, useRef } from 'react';
import {
  Table, Button, Upload, Space, Modal, Input, message, Breadcrumb,
  Tree, Card, Row, Col, Select, Typography, Empty, Tooltip,
  Popconfirm,
} from 'antd';
import type { DataNode, EventDataNode } from 'antd/es/tree';
import {
  UploadOutlined, DownloadOutlined, DeleteOutlined, FolderAddOutlined,
  EditOutlined, ReloadOutlined, HomeOutlined, FileOutlined,
  FolderOutlined,
  FileZipOutlined, CopyOutlined, SwapOutlined,
} from '@ant-design/icons';
import { filesAPI } from '../api';
import { useTranslation } from 'react-i18next';
import type { FileInfo, FileListResult, StorageProfile, StorageQuota, TransferResult } from '../types';
import StoragePicker from '../components/StoragePicker';
import SyncDialog from '../components/SyncDialog';

const { Text } = Typography;

const fmtSize = (b: number) => b < 1024 ? b + ' B' : b < 1048576 ? (b / 1024).toFixed(1) + ' KB' : b < 1073741824 ? (b / 1048576).toFixed(1) + ' MB' : (b / 1073741824).toFixed(2) + ' GB';
const fmtTime = (t: string) => t ? new Date(t).toLocaleString() : '-';

// 树缓存：path -> { dirs, files, loaded }
interface TreeNodeCache {
  dirs: FileInfo[];
  files: FileInfo[];
  loaded: boolean;
}

const FilesPage: React.FC = () => {
  const { t } = useTranslation();
  const [currentDir, setCurrentDir] = useState('');
  const [fileList, setFileList] = useState<FileListResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');
  const [treeNodes, setTreeNodes] = useState<DataNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [mkdirVisible, setMkdirVisible] = useState(false);
  const [mkdirName, setMkdirName] = useState('');
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameTarget, setRenameTarget] = useState<FileInfo | null>(null);
  const [renameNewName, setRenameNewName] = useState('');
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [profiles, setProfiles] = useState<StorageProfile[]>([]);
  const [quota, setQuota] = useState<StorageQuota | null>(null);
  const [currentProfile, setCurrentProfile] = useState('');
  const [, setPage] = useState(1);
  const treeCache = useRef<Record<string, TreeNodeCache>>({});
  const [transferVisible, setTransferVisible] = useState(false);
  const [transferMode, setTransferMode] = useState<'copy' | 'move'>('copy');
  const [transferSource, setTransferSource] = useState<string[]>([]);
  const [transferTarget, setTransferTarget] = useState({ profile: '', path: '', overwrite: false });
  const [transferLoading, setTransferLoading] = useState(false);
  const [_transferResult, setTransferResult] = useState<TransferResult | null>(null);
  const [syncVisible, setSyncVisible] = useState(false);
  const [syncSources, setSyncSources] = useState<string[]>([]);

  // 加载指定目录，更新树缓存 + 右侧文件列表
  const loadDir = useCallback(async (dir: string) => {
    setLoading(true);
    try {
      const res = await filesAPI.list({ dir, page_size: 200 });
      const raw = res.data?.data;
      const fileList = raw?.files || []; // null/undefined → []
      const dirs = fileList.filter((f: FileInfo) => f.is_dir);
      const files = fileList.filter((f: FileInfo) => !f.is_dir);

      // 还原原始响应字段（total 等）
      const data: FileListResult = {
        files: fileList,
        total: raw?.total ?? fileList.length,
        total_mode: raw?.total_mode ?? 'exact',
        page: raw?.page ?? 1,
        page_size: raw?.page_size ?? 200,
      };

      treeCache.current[dir] = { dirs, files, loaded: true };

      // 为新发现的子目录预填充缓存（标记未加载）
      for (const d of dirs) {
        if (!treeCache.current[d.path]) {
          treeCache.current[d.path] = { dirs: [], files: [], loaded: false };
        }
      }

      // 如果当前显示的就是这个目录，更新文件列表
      if (dir === currentDir) {
        setFileList({ ...data, files: data.files });
      }

      // 重建树节点
      rebuildTree(dir === '' ? '' : (dir.includes('/') ? dir.substring(0, dir.lastIndexOf('/')) : ''));
    } finally { setLoading(false); }
  }, [currentDir]);

  // 从缓存重建树节点（递归，仅目录）
  const buildChildren = useCallback((parentPath: string): DataNode[] => {
    const cached = treeCache.current[parentPath];
    if (!cached) return [];
    return cached.dirs.map(d => {
      const childLoaded = treeCache.current[d.path]?.loaded;
      return {
        title: <Space><FolderOutlined style={{ color: '#faad14' }} /><span>{d.name}</span></Space>,
        key: d.path,
        isLeaf: false,  // 目录永远不是叶子，确保可点击
        selectable: true,
        children: childLoaded ? buildChildren(d.path) : undefined,
      };
    });
  }, []);

  const rebuildTree = useCallback((_parentPath?: string) => {
    // 确保根节点已初始化
    if (!treeCache.current['']) {
      treeCache.current[''] = { dirs: [], files: [], loaded: false };
    }
    const roots = buildChildren('');
    setTreeNodes(roots);
    // 如果在子目录中，展开对应路径
    if (currentDir) {
      const parts = currentDir.split('/');
      const keys: string[] = [];
      let p = '';
      for (const part of parts) {
        p = p ? p + '/' + part : part;
        keys.push(p);
      }
      setExpandedKeys(prev => [...new Set([...prev, ...keys])]);
    }
  }, [buildChildren, currentDir]);

  // 树节点点击 → 懒加载 + 展开节点
  const handleTreeSelect = useCallback(async (_keys: React.Key[], info: { node: EventDataNode<DataNode> }) => {
    const dirPath = info.node.key as string;
    const cached = treeCache.current[dirPath];
    if (!cached?.loaded) {
      await loadDir(dirPath);
    }
    setCurrentDir(dirPath);
    // 展开当前节点及其所有父节点
    const parts = dirPath.split('/');
    const keys: string[] = [];
    let p = '';
    for (const part of parts) {
      p = p ? p + '/' + part : part;
      keys.push(p);
    }
    setExpandedKeys(prev => [...new Set([...prev, ...keys])]);
  }, [loadDir]);

  // 树节点展开 → 懒加载 + 展开
  const handleTreeExpand = useCallback(async (keys: React.Key[], info: { node: EventDataNode<DataNode>; expanded: boolean }) => {
    setExpandedKeys(keys);
    if (info.expanded) {
      const dirPath = info.node.key as string;
      const cached = treeCache.current[dirPath];
      if (!cached?.loaded) {
        await loadDir(dirPath);
      }
    }
  }, [loadDir]);

  // 右侧目录点击 → 加载 + 同步左侧树
  const handleDirClick = useCallback(async (dirPath: string) => {
    const cached = treeCache.current[dirPath];
    if (!cached?.loaded) {
      await loadDir(dirPath);
    }
    setCurrentDir(dirPath);
    // 展开对应节点
    const parts = dirPath.split('/');
    const keys: string[] = [];
    let p = '';
    for (const part of parts) {
      p = p ? p + '/' + part : part;
      keys.push(p);
      if (!treeCache.current[p]) {
        treeCache.current[p] = { dirs: [], files: [], loaded: false };
      }
    }
    setExpandedKeys(prev => [...new Set([...prev, ...keys])]);
  }, [loadDir]);

  // 面包屑点击
  const navigateTo = useCallback((dir: string) => {
    handleDirClick(dir);
    setPage(1);
  }, [handleDirClick]);

  // 初始加载 + 切换目录时刷新
  useEffect(() => {
    if (!treeCache.current[currentDir]?.loaded) {
      loadDir(currentDir);
    } else {
      // 从缓存恢复
      const cached = treeCache.current[currentDir];
      if (cached) {
        setFileList({
          files: [...cached.dirs, ...cached.files],
          total: cached.dirs.length + cached.files.length,
          total_mode: 'exact',
          page: 1,
          page_size: 200,
        });
      }
    }
  }, [currentDir]);

  useEffect(() => {
    filesAPI.profiles().then(r => {
      const list = r.data?.data || [];
      setProfiles(list);
      const def = list.find((p: StorageProfile) => p.is_default) || list.find((p: StorageProfile) => p.enabled);
      if (def && !currentProfile) setCurrentProfile(def.code);
    }).catch(() => {});
    filesAPI.quota().then(r => setQuota(r.data.data)).catch(() => {});
  }, []);

  const refresh = useCallback(() => {
    delete treeCache.current[currentDir];
    loadDir(currentDir);
  }, [currentDir, loadDir]);

  const handleDownload = async (f: FileInfo) => {
    const res = await filesAPI.download(f.path);
    const url = window.URL.createObjectURL(res.data as any);
    const a = document.createElement('a'); a.href = url; a.download = f.name; a.click();
    window.URL.revokeObjectURL(url);
  };

  const handleBatchDownload = async () => {
    if (!selectedRowKeys.length) { message.warning(t('files.selectFilesFirst')); return; }
    const res = await filesAPI.batchDownload(selectedRowKeys as string[]);
    const url = window.URL.createObjectURL(res.data as any);
    const a = document.createElement('a'); a.href = url; a.download = 'batch-download.zip'; a.click();
    window.URL.revokeObjectURL(url);
  };

  const handleMkdir = async () => {
    if (!mkdirName.trim()) return;
    const path = currentDir ? `${currentDir}/${mkdirName.trim()}` : mkdirName.trim();
    await filesAPI.mkdir(path);
    message.success(t('files.dirCreated'));
    setMkdirVisible(false); setMkdirName('');
    delete treeCache.current[currentDir];
    loadDir(currentDir);
  };

  const handleRename = async () => {
    if (!renameTarget || !renameNewName.trim()) return;
    const oldPath = renameTarget.path.replace(/\/$/, '');
    const parentDir = oldPath.includes('/') ? oldPath.substring(0, oldPath.lastIndexOf('/')) : '';
    const newPath = parentDir ? `${parentDir}/${renameNewName.trim()}` : renameNewName.trim();
    await filesAPI.rename(renameTarget.path, newPath);
    message.success(t('files.renameSuccess'));
    setRenameVisible(false); setRenameTarget(null);
    // 清除相关缓存
    delete treeCache.current[currentDir];
    delete treeCache.current[renameTarget.path];
    if (parentDir) delete treeCache.current[parentDir];
    loadDir(currentDir);
  };

  const handleDelete = async (f: FileInfo) => {
    if (f.is_dir) { await filesAPI.removeDir(f.path); }
    else { await filesAPI.delete(f.path); }
    message.success(t('files.deleteSuccess'));
    delete treeCache.current[currentDir];
    delete treeCache.current[f.path];
    loadDir(currentDir);
  };

  const handleBatchDelete = async () => {
    if (!selectedRowKeys.length) { message.warning(t('files.selectFilesFirst')); return; }
    Modal.confirm({
      title: t('files.batchDelete'),
      content: t('files.confirmBatchDelete', {n: selectedRowKeys.length}),
      onOk: async () => {
        await filesAPI.deleteBatch(selectedRowKeys as string[]);
        message.success(t('files.batchDeleteSuccess'));
        setSelectedRowKeys([]);
        delete treeCache.current[currentDir];
        loadDir(currentDir);
      },
    });
  };

  const handleSwitchProfile = async (code: string) => {
    setCurrentProfile(code);
    try { await filesAPI.setDefaultProfile(code); } catch {}
    treeCache.current = {};
    setTreeNodes([]);
    setFileList(null);
    setCurrentDir('');
    setPage(1);
    filesAPI.quota().then(r => setQuota(r.data.data)).catch(() => {});
    loadDir('');
  };

  const openTransfer = (mode: 'copy' | 'move', paths: string[]) => {
    setTransferMode(mode);
    setTransferSource(paths);
    setTransferTarget({ profile: currentProfile, path: '', overwrite: false });
    setTransferResult(null);
    setTransferVisible(true);
  };

  const handleTransfer = async () => {
    if (!transferTarget.profile || !transferTarget.path) {
      message.warning(t('transfer.fillTarget'));
      return;
    }
    setTransferLoading(true);
    try {
      const apiFn = transferMode === 'copy' ? filesAPI.copyFiles : filesAPI.moveFiles;
      const promises = transferSource.map(srcPath => {
        const fileName = srcPath.split('/').pop() || srcPath;
        const targetPath = transferTarget.path.endsWith('/')
          ? transferTarget.path + fileName
          : transferTarget.path + '/' + fileName;
        return apiFn({
          source_profile: currentProfile,
          source_path: srcPath,
          target_profile: transferTarget.profile,
          target_path: targetPath,
          overwrite: transferTarget.overwrite,
        });
      });
      const results = await Promise.all(promises);
      const merged: TransferResult = {
        total_files: 0, transferred: 0, skipped: 0, failed: 0,
        total_bytes: 0, transferred_bytes: 0, total_errors: 0,
        errors_truncated: false, errors: [], duration: 0,
      };
      for (const r of results) {
        const d = r.data?.data as TransferResult;
        if (d) {
          merged.total_files += d.total_files;
          merged.transferred += d.transferred;
          merged.skipped += d.skipped;
          merged.failed += d.failed;
          merged.total_bytes += d.total_bytes;
          merged.transferred_bytes += d.transferred_bytes;
          merged.total_errors += d.total_errors;
          if (d.errors) merged.errors = [...(merged.errors || []), ...d.errors];
        }
      }
      setTransferResult(merged);
      if (merged.failed === 0) {
        message.success(t('transfer.success', { n: merged.transferred }));
        setTransferVisible(false);
        refresh();
      } else {
        message.warning(t('transfer.partialSuccess', { ok: merged.transferred, fail: merged.failed }));
      }
    } catch (err: any) {
      message.error(err?.response?.data?.message || t('transfer.failed'));
    } finally {
      setTransferLoading(false);
    }
  };

  const breadcrumbItems = [
    { title: <span onClick={() => navigateTo('')} style={{ cursor: 'pointer', color: '#1677ff' }}><HomeOutlined style={{ marginRight: 2 }} />{t('files.rootDir')}</span> },
    ...currentDir.split('/').filter(Boolean).map((part, idx, arr) => ({
      title: <span onClick={() => navigateTo(arr.slice(0, idx + 1).join('/'))} style={{ cursor: 'pointer', color: '#1677ff' }}>{part}</span>,
    })),
  ];

  const columns = [
    {
      title: t('files.name'), dataIndex: 'name', key: 'name', ellipsis: true,
      render: (name: string, r: FileInfo) => (
        <Space>
          {r.is_dir ? <FolderOutlined style={{ color: '#faad14', fontSize: 16 }} /> : <FileOutlined style={{ color: '#999', fontSize: 16 }} />}
          <Tooltip title={name}>
            {r.is_dir ? <a onClick={() => handleDirClick(r.path)}>{name}</a> : <span>{name}</span>}
          </Tooltip>
        </Space>
      ),
    },
    { title: t('files.size'), dataIndex: 'size', key: 'size', width: 120, render: (_: number, r: FileInfo) => r.is_dir ? '-' : fmtSize(r.size) },
    { title: t('files.modTime'), dataIndex: 'mod_time', key: 'mod_time', width: 180, render: (t: string) => fmtTime(t) },
    { title: t('files.type'), dataIndex: 'content_type', key: 'type', width: 150, render: (t: string) => t || '-' },
    {
      title: t('files.action'), key: 'actions', width: 260,
      render: (_: any, r: FileInfo) => (
        <Space size="small">
          {!r.is_dir && <Tooltip title={t('files.download')}><Button size="small" icon={<DownloadOutlined />} onClick={() => handleDownload(r)} /></Tooltip>}
          <Tooltip title={t('transfer.copyTo')}><Button size="small" icon={<CopyOutlined />} onClick={() => openTransfer('copy', [r.path])} /></Tooltip>
          <Tooltip title={t('transfer.moveTo')}><Button size="small" icon={<SwapOutlined />} onClick={() => openTransfer('move', [r.path])} /></Tooltip>
          <Tooltip title={t('files.rename')}><Button size="small" icon={<EditOutlined />} onClick={() => { setRenameTarget(r); setRenameNewName(r.name); setRenameVisible(true); }} /></Tooltip>
          <Popconfirm title={t('files.confirmDelete', {name: r.name})} onConfirm={() => handleDelete(r)}>
            <Tooltip title={t('files.delete')}><Button size="small" danger icon={<DeleteOutlined />} /></Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div style={{ padding: '0 0 24px' }}>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Row justify="space-between" align="middle">
          <Col>
            <Space>
              <Upload customRequest={({ file, onSuccess }: any) => {
                filesAPI.upload(currentDir, file as File).then(() => { onSuccess?.('ok'); refresh(); });
              }} showUploadList={false}>
                <Button type="primary" icon={<UploadOutlined />}>{t('files.uploadFile')}</Button>
              </Upload>
              <Button icon={<FolderAddOutlined />} onClick={() => setMkdirVisible(true)}>{t('files.mkdir')}</Button>
              {selectedRowKeys.length > 0 && (
                <Space>
                  <Button icon={<FileZipOutlined />} onClick={handleBatchDownload}>{t('files.batchDownload', {n: selectedRowKeys.length})}</Button>
                  <Button icon={<CopyOutlined />} onClick={() => openTransfer('copy', selectedRowKeys as string[])}>{t('transfer.batchCopy', {n: selectedRowKeys.length})}</Button>
                  <Button icon={<SwapOutlined />} onClick={() => openTransfer('move', selectedRowKeys as string[])}>{t('transfer.batchMove', {n: selectedRowKeys.length})}</Button>
                  <Button icon={<SwapOutlined />} onClick={() => { setSyncSources(selectedRowKeys as string[]); setSyncVisible(true); }}>{t('sync.syncTo')}</Button>
                  <Popconfirm title={t('files.confirmBatchDelete', {n: selectedRowKeys.length})} onConfirm={handleBatchDelete}>
                    <Button danger icon={<DeleteOutlined />}>{t('files.batchDelete')}</Button>
                  </Popconfirm>
                </Space>
              )}
            </Space>
          </Col>
          <Col>
            <Space>
              <Input.Search placeholder={t('files.search')} value={search} onChange={e => setSearch(e.target.value)}
                onSearch={() => { loadDir(currentDir); }} allowClear style={{ width: 180 }} />
              <Button icon={<ReloadOutlined />} onClick={refresh}>{t('files.refresh')}</Button>
            </Space>
          </Col>
        </Row>
      </Card>

      <Row gutter={12}>
        <Col span={6} style={{ display: 'flex', flexDirection: 'column', height: 'calc(100vh - 180px)' }}>
          {/* 存储实例切换 + 概览 */}
          <Card size="small" style={{ marginBottom: 12 }}>
            <Select
              style={{ width: '100%' }}
              value={currentProfile}
              onChange={(val) => handleSwitchProfile(val)}
              options={profiles.filter(p => p.enabled).map(p => ({
                label: `${p.name} (${p.code})`,
                value: p.code,
              }))}
            />
            <Row style={{ marginTop: 8 }} gutter={8}>
              <Col flex="auto">
                <Text type="secondary" style={{ fontSize: 12 }}>{t('files.used')}: {quota ? fmtSize(quota.usage_bytes) : '-'}</Text>
              </Col>
              <Col flex="auto">
                <Text type="secondary" style={{ fontSize: 12 }}>{t('files.quota')}: {quota ? (quota.unlimited ? t('files.unlimited') : fmtSize(quota.quota_bytes)) : '-'}</Text>
              </Col>
              <Col>
                <Button size="small" type="link" onClick={() => window.location.href = '/settings/storage'} style={{ padding: 0 }}>{t('files.manage')}</Button>
              </Col>
            </Row>
          </Card>

          {/* 目录树 */}
          <Card size="small" style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
            styles={{ body: { flex: 1, overflow: 'auto', padding: 8 } }}>
            <Tree showLine
              treeData={treeNodes}
              expandedKeys={expandedKeys}
              selectedKeys={currentDir ? [currentDir] : []}
              onSelect={handleTreeSelect}
              onExpand={handleTreeExpand}
            />
          </Card>
        </Col>

        <Col span={18}>
          <Card size="small" title={
            <Breadcrumb items={breadcrumbItems} separator={<span style={{ color: '#999', fontSize: 13, lineHeight: '20px', display: 'inline-flex', alignItems: 'center' }}>/</span>}
              style={{ fontSize: 14 }} />}>
            <Table columns={columns}
              dataSource={fileList?.files || []}
              rowKey="path" loading={loading} size="small"
              rowSelection={{ selectedRowKeys, onChange: setSelectedRowKeys, getCheckboxProps: (r: FileInfo) => ({ disabled: r.is_dir }) }}
              pagination={false}
              locale={{ emptyText: <Empty description={t('files.emptyDir')} /> }}
            />
          </Card>
        </Col>
      </Row>

      <Modal title={t('files.mkdirTitle')} open={mkdirVisible} onOk={handleMkdir} onCancel={() => setMkdirVisible(false)}>
        <Input placeholder={t('files.dirNamePlaceholder')} value={mkdirName} onChange={e => setMkdirName(e.target.value)} onPressEnter={handleMkdir} />
      </Modal>
      <Modal title={t('files.renameTitle')} open={renameVisible} onOk={handleRename} onCancel={() => setRenameVisible(false)}>
        <Input placeholder={t('files.newNamePlaceholder')} value={renameNewName} onChange={e => setRenameNewName(e.target.value)} onPressEnter={handleRename} />
      </Modal>

      <Modal
        title={transferMode === 'copy' ? t('transfer.copyTo') : t('transfer.moveTo')}
        open={transferVisible}
        onOk={handleTransfer}
        onCancel={() => setTransferVisible(false)}
        confirmLoading={transferLoading}
        okText={transferMode === 'copy' ? t('transfer.copy') : t('transfer.move')}
        width={560}
      >
        <div style={{ marginBottom: 12 }}>
          <Text type="secondary">
            {t('transfer.sourceCount', { n: transferSource.length })}
          </Text>
        </div>
        <StoragePicker
          profiles={profiles}
          value={transferTarget}
          onChange={setTransferTarget}
          sourceProfile={currentProfile}
        />
      </Modal>

      <SyncDialog
        open={syncVisible}
        profiles={profiles}
        sourceProfile={currentProfile}
        sourcePaths={syncSources}
        onClose={() => setSyncVisible(false)}
        onSuccess={() => { setSyncVisible(false); refresh(); }}
      />
    </div>
  );
};

export default FilesPage;
