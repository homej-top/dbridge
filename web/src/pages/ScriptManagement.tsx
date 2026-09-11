import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button, Input, Space, Typography, message,
  Modal, Card, Tree, Dropdown, Empty, Spin, Breadcrumb,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, FileTextOutlined,
  SaveOutlined, FolderOutlined, MoreOutlined,
  FileAddOutlined, FolderAddOutlined, EditOutlined,
  SearchOutlined, HomeOutlined, ReloadOutlined,
} from '@ant-design/icons';
import type { MenuProps, TreeDataNode } from 'antd';
import Editor from '@monaco-editor/react';
import { scriptFsAPI } from '../api';
import type { ScriptFileInfo } from '../api';

const { Text } = Typography;

interface DirCache {
  dirs: ScriptFileInfo[];
  files: ScriptFileInfo[];
  loaded: boolean;
}

const ScriptManagement: React.FC = () => {
  const { t: tr } = useTranslation();

  const [currentDir, setCurrentDir] = useState('');
  const [treeNodes, setTreeNodes] = useState<TreeDataNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [loading, setLoading] = useState(false);

  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [selectedName, setSelectedName] = useState('');
  const [content, setContent] = useState('');
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle');
  const [editorLoading, setEditorLoading] = useState(false);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const dirtyRef = useRef(false);
  const treeCache = useRef<Record<string, DirCache>>({});

  const [createFileOpen, setCreateFileOpen] = useState(false);
  const [createFileName, setCreateFileName] = useState('');
  const [createFolderOpen, setCreateFolderOpen] = useState(false);
  const [createFolderName, setCreateFolderName] = useState('');
  const [createParentDir, setCreateParentDir] = useState('');

  const [renameOpen, setRenameOpen] = useState(false);
  const [renameTarget, setRenameTarget] = useState<ScriptFileInfo | null>(null);
  const [renameNewName, setRenameNewName] = useState('');

  const [submitting, setSubmitting] = useState(false);

  const loadDir = useCallback(async (dir: string) => {
    setLoading(true);
    try {
      const res = await scriptFsAPI.list(dir);
      const raw = res.data?.data;
      const allFiles: ScriptFileInfo[] = raw?.files || [];
      const dirs = allFiles.filter(f => f.is_dir);
      const files = allFiles.filter(f => !f.is_dir);

      treeCache.current[dir] = { dirs, files, loaded: true };

      for (const d of dirs) {
        if (!treeCache.current[d.path]) {
          treeCache.current[d.path] = { dirs: [], files: [], loaded: false };
        }
      }

      rebuildTree();
    } catch {
      // handled by interceptor
    } finally {
      setLoading(false);
    }
  }, []);

  const buildChildren = useCallback((parentPath: string): TreeDataNode[] => {
    const cached = treeCache.current[parentPath];
    if (!cached) return [];
    const nodes: TreeDataNode[] = [];

    for (const d of cached.dirs) {
      const childLoaded = treeCache.current[d.path]?.loaded;
      nodes.push({
        title: <Space><FolderOutlined style={{ color: '#faad14' }} /><span>{d.name}</span></Space>,
        key: d.path,
        isLeaf: false,
        selectable: true,
        _isDir: true,
        _fileInfo: d,
        children: childLoaded
          ? buildChildren(d.path)
          : [{ title: '...', key: d.path + '/__ph', isLeaf: true, disabled: true, selectable: false }],
      } as TreeDataNode & { _isDir: boolean; _fileInfo: ScriptFileInfo });
    }

    for (const f of cached.files) {
      nodes.push({
        title: <Space><FileTextOutlined style={{ color: '#1677ff' }} /><span>{f.name}</span></Space>,
        key: f.path,
        isLeaf: true,
        selectable: true,
        _isDir: false,
        _fileInfo: f,
      } as TreeDataNode & { _isDir: boolean; _fileInfo: ScriptFileInfo });
    }

    return nodes;
  }, []);

  const rebuildTree = useCallback(() => {
    if (!treeCache.current['']) {
      treeCache.current[''] = { dirs: [], files: [], loaded: false };
    }
    const roots = buildChildren('');
    setTreeNodes(roots);
  }, [buildChildren]);

  const handleSelectFileRef = useRef<(file: ScriptFileInfo) => void>(() => {});

  const handleTreeSelect = useCallback(async (_keys: React.Key[], info: { node: any }) => {
    const node = info.node as TreeDataNode & { _isDir?: boolean; _fileInfo?: ScriptFileInfo };
    if (node._isDir === false && node._fileInfo) {
      handleSelectFileRef.current(node._fileInfo);
      setSelectedPath(node._fileInfo.path);
      const parentDir = node._fileInfo.path.includes('/')
        ? node._fileInfo.path.substring(0, node._fileInfo.path.lastIndexOf('/'))
        : '';
      setCurrentDir(parentDir);
      return;
    }
    const dirPath = info.node.key as string;
    const cached = treeCache.current[dirPath];
    if (!cached?.loaded) {
      await loadDir(dirPath);
    }
    setCurrentDir(dirPath);
  }, [loadDir]);

  const handleTreeExpand = useCallback(async (keys: React.Key[], info: { node: any; expanded: boolean }) => {
    setExpandedKeys(keys);
    if (info.expanded) {
      const dirPath = info.node.key as string;
      const cached = treeCache.current[dirPath];
      if (!cached?.loaded) {
        await loadDir(dirPath);
      }
    }
  }, [loadDir]);

  useEffect(() => {
    if (!treeCache.current['']?.loaded) {
      loadDir('');
    } else {
      rebuildTree();
    }
  }, []);

  const refresh = useCallback(() => {
    delete treeCache.current[currentDir];
    loadDir(currentDir);
  }, [currentDir, loadDir]);

  const handleSelectFile = useCallback(async (file: ScriptFileInfo) => {
    if (dirtyRef.current && selectedPath) {
      doSave(selectedPath, content);
    }
    setEditorLoading(true);
    try {
      const res = await scriptFsAPI.read(file.path);
      const data = res.data?.data;
      setSelectedPath(file.path);
      setSelectedName(file.name);
      setContent(data?.content || '');
      setSaveStatus('idle');
      dirtyRef.current = false;
    } catch {
      // handled
    } finally {
      setEditorLoading(false);
    }
  }, [selectedPath, content]);
  handleSelectFileRef.current = handleSelectFile;

  const handleContentChange = (val: string | undefined) => {
    const newContent = val || '';
    setContent(newContent);
    dirtyRef.current = true;
    setSaveStatus('idle');

    if (saveTimerRef.current) clearTimeout(saveTimerRef.current);
    saveTimerRef.current = setTimeout(() => {
      if (selectedPath) doSave(selectedPath, newContent);
    }, 1000);
  };

  const doSave = async (path: string, sql: string) => {
    setSaveStatus('saving');
    try {
      await scriptFsAPI.save(path, sql);
      setSaveStatus('saved');
      dirtyRef.current = false;
    } catch {
      setSaveStatus('error');
    }
  };

  const handleManualSave = () => {
    if (selectedPath && dirtyRef.current) doSave(selectedPath, content);
  };

  const handleDelete = async (file: ScriptFileInfo) => {
    try {
      await scriptFsAPI.remove(file.path, file.is_dir);
      message.success(tr('scripts.deleteSuccess'));
      if (selectedPath === file.path) {
        setSelectedPath(null);
        setSelectedName('');
        setContent('');
      }
      const parentDir = file.path.includes('/') ? file.path.substring(0, file.path.lastIndexOf('/')) : '';
      delete treeCache.current[parentDir];
      delete treeCache.current[file.path];
      if (parentDir === currentDir) {
        loadDir(currentDir);
      } else {
        rebuildTree();
      }
    } catch {
      // handled
    }
  };

  const openCreateFile = (parentDir: string) => {
    setCreateParentDir(parentDir);
    setCreateFileName('');
    setCreateFileOpen(true);
  };

  const handleCreateFile = async () => {
    if (!createFileName.trim()) return;
    setSubmitting(true);
    try {
      let fileName = createFileName.trim();
      if (!fileName.endsWith('.sql')) fileName += '.sql';
      const filePath = createParentDir ? `${createParentDir}/${fileName}` : fileName;
      await scriptFsAPI.save(filePath, '-- SQL\n');
      message.success(tr('scripts.createSuccess'));
      setCreateFileOpen(false);
      delete treeCache.current[createParentDir];
      loadDir(createParentDir);
      setSelectedPath(filePath);
      setSelectedName(fileName);
      setContent('-- SQL\n');
      setSaveStatus('idle');
      dirtyRef.current = false;
    } catch {
      // handled
    } finally {
      setSubmitting(false);
    }
  };

  const openCreateFolder = (parentDir: string) => {
    setCreateParentDir(parentDir);
    setCreateFolderName('');
    setCreateFolderOpen(true);
  };

  const handleCreateFolder = async () => {
    if (!createFolderName.trim()) return;
    setSubmitting(true);
    try {
      const folderPath = createParentDir ? `${createParentDir}/${createFolderName.trim()}` : createFolderName.trim();
      await scriptFsAPI.mkdir(folderPath);
      message.success(tr('scripts.createSuccess'));
      setCreateFolderOpen(false);
      delete treeCache.current[createParentDir];
      loadDir(createParentDir);
    } catch {
      // handled
    } finally {
      setSubmitting(false);
    }
  };

  const openRename = (file: ScriptFileInfo) => {
    setRenameTarget(file);
    setRenameNewName(file.name);
    setRenameOpen(true);
  };

  const handleRename = async () => {
    if (!renameTarget || !renameNewName.trim()) return;
    setSubmitting(true);
    try {
      const oldPath = renameTarget.path;
      const parentDir = oldPath.includes('/') ? oldPath.substring(0, oldPath.lastIndexOf('/')) : '';
      const newPath = parentDir ? `${parentDir}/${renameNewName.trim()}` : renameNewName.trim();
      await scriptFsAPI.rename(oldPath, newPath);
      message.success(tr('scripts.updateSuccess'));
      setRenameOpen(false);
      if (selectedPath === oldPath) {
        setSelectedPath(newPath);
        setSelectedName(renameNewName.trim());
      }
      delete treeCache.current[currentDir];
      delete treeCache.current[oldPath];
      loadDir(currentDir);
    } catch {
      // handled
    } finally {
      setSubmitting(false);
    }
  };

  const fileMenu = (file: ScriptFileInfo): MenuProps['items'] => {
    if (file.is_dir) {
      return [
        { key: 'add-file', icon: <FileAddOutlined />, label: tr('scripts.addScript'), onClick: () => openCreateFile(file.path) },
        { key: 'add-folder', icon: <FolderAddOutlined />, label: tr('scripts.addSubFolder'), onClick: () => openCreateFolder(file.path) },
        { key: 'rename', icon: <EditOutlined />, label: tr('files.rename'), onClick: () => openRename(file) },
        {
          key: 'delete', icon: <DeleteOutlined />, label: tr('scripts.delete'), danger: true,
          onClick: () => Modal.confirm({
            title: tr('scripts.deleteFolderConfirm'),
            content: tr('scripts.deleteFolderDesc'),
            okText: tr('scripts.delete'),
            okType: 'danger',
            onOk: () => handleDelete(file),
          }),
        },
      ];
    }
    return [
      { key: 'open', icon: <FileTextOutlined />, label: tr('common.view'), onClick: () => handleSelectFile(file) },
      { key: 'rename', icon: <EditOutlined />, label: tr('files.rename'), onClick: () => openRename(file) },
      {
        key: 'delete', icon: <DeleteOutlined />, label: tr('scripts.delete'), danger: true,
        onClick: () => Modal.confirm({
          title: tr('scripts.deleteScriptConfirm'),
          okText: tr('scripts.delete'),
          okType: 'danger',
          onOk: () => handleDelete(file),
        }),
      },
    ];
  };

  const renderTreeTitle = (nodeData: TreeDataNode) => {
    const node = nodeData as TreeDataNode & { _isDir?: boolean; _fileInfo?: ScriptFileInfo };
    let menu: MenuProps['items'];

    if (node._fileInfo) {
      menu = fileMenu(node._fileInfo);
    } else {
      const dirPath = nodeData.key as string;
      const dirName = dirPath.includes('/') ? dirPath.split('/').pop()! : dirPath;
      const dirInfo: ScriptFileInfo = {
        name: dirName,
        path: dirPath,
        is_dir: true,
        size: 0,
        mod_time: '',
        content_type: '',
      };
      menu = fileMenu(dirInfo);
    }

    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
        <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {nodeData.title as React.ReactNode}
        </span>
        <Dropdown menu={{ items: menu }} trigger={['click']}>
          <Button
            type="text"
            size="small"
            icon={<MoreOutlined style={{ fontSize: 14 }} />}
            onClick={(e) => e.stopPropagation()}
            style={{ flexShrink: 0, opacity: 0.5 }}
          />
        </Dropdown>
      </div>
    );
  };

  const breadcrumbItems = [
    {
      title: (
        <span onClick={() => { setCurrentDir(''); if (!treeCache.current['']?.loaded) loadDir(''); }}
          style={{ cursor: 'pointer', color: '#1677ff' }}>
          <HomeOutlined style={{ marginRight: 2 }} />{tr('scripts.rootDir')}
        </span>
      ),
    },
    ...currentDir.split('/').filter(Boolean).map((part, idx, arr) => ({
      title: (
        <span onClick={() => {
          const target = arr.slice(0, idx + 1).join('/');
          setCurrentDir(target);
          if (!treeCache.current[target]?.loaded) loadDir(target);
        }} style={{ cursor: 'pointer', color: '#1677ff' }}>
          {part}
        </span>
      ),
    })),
  ];

  return (
    <div style={{ display: 'flex', gap: 16, height: 'calc(100vh - 160px)' }}>
      <style>{`
        .script-tree .ant-tree-treenode {
          width: 100%;
        }
        .script-tree .ant-tree-node-content-wrapper {
          flex: 1 !important;
        }
        .script-tree .ant-tree-title {
          flex: 1;
          display: block !important;
        }
      `}</style>

      {/* Left: directory tree */}
      <div style={{ width: 320, flexShrink: 0, display: 'flex', flexDirection: 'column' }}>
        <div style={{ marginBottom: 12, display: 'flex', gap: 8 }}>
          <Input
            placeholder={tr('scripts.searchScript')}
            prefix={<SearchOutlined />}
            allowClear
            style={{ flex: 1 }}
            disabled
          />
          <Dropdown
            menu={{
              items: [
                { key: 'file', icon: <FileAddOutlined />, label: tr('scripts.newScript'), onClick: () => openCreateFile(currentDir) },
                { key: 'folder', icon: <FolderAddOutlined />, label: tr('scripts.newFolder'), onClick: () => openCreateFolder(currentDir) },
              ],
            }}
          >
            <Button type="primary" icon={<PlusOutlined />}>
              {tr('scripts.newScriptBtn')}
            </Button>
          </Dropdown>
          <Button icon={<ReloadOutlined />} onClick={refresh} />
        </div>
        <div style={{ flex: 1, overflow: 'auto' }}>
          <Spin spinning={loading}>
            <Tree
              className="script-tree"
              showIcon
              blockNode
              showLine
              treeData={treeNodes}
              titleRender={renderTreeTitle}
              onSelect={handleTreeSelect}
              onExpand={handleTreeExpand}
              expandedKeys={expandedKeys}
              selectedKeys={[...(currentDir ? [currentDir] : []), ...(selectedPath ? [selectedPath] : [])]}
              style={{ padding: '4px 0' }}
            />
          </Spin>
        </div>
      </div>

      {/* Right: editor */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        {/* Breadcrumb + file info (single row) */}
        <Card size="small" style={{ marginBottom: 8 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <Space size={4}>
              <Breadcrumb items={breadcrumbItems} separator={<span style={{ color: '#999', fontSize: 13 }}>/</span>} style={{ fontSize: 14 }} />
              {selectedPath && <Text type="secondary" style={{ fontSize: 13 }}>/ {selectedName}</Text>}
            </Space>
            {selectedPath && (
              <Space>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {saveStatus === 'saving' && tr('scripts.saving')}
                  {saveStatus === 'saved' && tr('scripts.saved')}
                  {saveStatus === 'error' && <Text type="danger">{tr('scripts.saveFailed')}</Text>}
                </Text>
                <Button
                  size="small"
                  icon={<SaveOutlined />}
                  onClick={handleManualSave}
                  disabled={!dirtyRef.current}
                >
                  {tr('scripts.save')}
                </Button>
              </Space>
            )}
          </div>
        </Card>

        {/* Editor */}
        {selectedPath ? (
            <div style={{ flex: 1, border: '1px solid #d9d9d9', borderRadius: 4 }}>
              {editorLoading ? (
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
                  <Spin />
                </div>
              ) : (
                <Editor
                  height="100%"
                  defaultLanguage="sql"
                  value={content}
                  onChange={handleContentChange}
                  options={{
                    minimap: { enabled: false },
                    fontSize: 13,
                    wordWrap: 'on',
                    scrollBeyondLastLine: false,
                  }}
                />
              )}
            </div>
        ) : (
          <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Empty description={tr('scripts.selectOrCreate')} />
          </div>
        )}
      </div>

      {/* Create File Modal */}
      <Modal
        title={tr('scripts.newScript')}
        open={createFileOpen}
        onCancel={() => setCreateFileOpen(false)}
        onOk={handleCreateFile}
        confirmLoading={submitting}
        okText={tr('scripts.create')}
        cancelText={tr('common.cancelText')}
        width={400}
        destroyOnHidden
      >
        <div style={{ marginTop: 16 }}>
          <div style={{ marginBottom: 8 }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {tr('scripts.rootDir')}: {createParentDir || '/'}
            </Text>
          </div>
          <Input
            placeholder={tr('scripts.scriptNamePlaceholder')}
            value={createFileName}
            onChange={e => setCreateFileName(e.target.value)}
            addonAfter=".sql"
            onPressEnter={handleCreateFile}
          />
        </div>
      </Modal>

      {/* Create Folder Modal */}
      <Modal
        title={tr('scripts.newFolder')}
        open={createFolderOpen}
        onCancel={() => setCreateFolderOpen(false)}
        onOk={handleCreateFolder}
        confirmLoading={submitting}
        okText={tr('scripts.create')}
        cancelText={tr('common.cancelText')}
        width={400}
        destroyOnHidden
      >
        <div style={{ marginTop: 16 }}>
          <div style={{ marginBottom: 8 }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {tr('scripts.rootDir')}: {createParentDir || '/'}
            </Text>
          </div>
          <Input
            placeholder={tr('scripts.folderNamePlaceholder')}
            value={createFolderName}
            onChange={e => setCreateFolderName(e.target.value)}
            onPressEnter={handleCreateFolder}
          />
        </div>
      </Modal>

      {/* Rename Modal */}
      <Modal
        title={tr('files.rename')}
        open={renameOpen}
        onCancel={() => setRenameOpen(false)}
        onOk={handleRename}
        confirmLoading={submitting}
        okText={tr('scripts.save')}
        cancelText={tr('common.cancelText')}
        width={400}
        destroyOnHidden
      >
        <div style={{ marginTop: 16 }}>
          <Input
            placeholder={tr('files.newNamePlaceholder')}
            value={renameNewName}
            onChange={e => setRenameNewName(e.target.value)}
            onPressEnter={handleRename}
          />
        </div>
      </Modal>
    </div>
  );
};

export default ScriptManagement;
