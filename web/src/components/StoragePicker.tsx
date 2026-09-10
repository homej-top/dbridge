import React, { useEffect, useState, useCallback } from 'react';
import { Select, Tree, Input, Space, Checkbox, Typography } from 'antd';
import { FolderOutlined, HddOutlined, CloudOutlined } from '@ant-design/icons';
import type { DataNode } from 'antd/es/tree';
import { filesAPI } from '../api';
import { useTranslation } from 'react-i18next';
import type { StorageProfile, FileInfo } from '../types';

const { Text } = Typography;

interface StoragePickerProps {
  profiles: StorageProfile[];
  value: {
    profile: string;
    path: string;
    overwrite: boolean;
  };
  onChange: (value: { profile: string; path: string; overwrite: boolean }) => void;
  sourceProfile?: string;
}

const MAX_DEPTH = 5;

const StoragePicker: React.FC<StoragePickerProps> = ({ profiles, value, onChange }) => {
  const { t } = useTranslation();
  const [treeData, setTreeData] = useState<DataNode[]>([]);
  const [loadedKeys, setLoadedKeys] = useState<Set<string>>(new Set());
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);

  const profileOptions = profiles
    .filter(p => p.enabled)
    .map(p => ({
      label: (
        <Space>
          {p.backend === 'local' ? <HddOutlined /> : <CloudOutlined />}
          <span>{p.name}</span>
          <Text type="secondary" style={{ fontSize: 12 }}>({p.code})</Text>
        </Space>
      ),
      value: p.code,
    }));

  const loadChildren = useCallback(async (parentPath: string, depth: number): Promise<DataNode[]> => {
    if (depth >= MAX_DEPTH) return [];
    try {
      const res = await filesAPI.list({ dir: parentPath, page_size: 200 });
      const files: FileInfo[] = res.data?.data?.files || [];
      return files
        .filter(f => f.is_dir)
        .map(d => ({
          title: <Space><FolderOutlined style={{ color: '#faad14' }} /><span>{d.name}</span></Space>,
          key: d.path,
          isLeaf: depth + 1 >= MAX_DEPTH,
        }));
    } catch {
      return [];
    }
  }, []);

  const loadRootDirs = useCallback(async () => {
    const nodes = await loadChildren('', 0);
    setTreeData(nodes);
    setLoadedKeys(new Set(['__root__']));
  }, [loadChildren]);

  useEffect(() => {
    if (value.profile) {
      setTreeData([]);
      setLoadedKeys(new Set());
      setExpandedKeys([]);
      loadRootDirs();
    }
  }, [value.profile, loadRootDirs]);

  const onLoadData = async (node: DataNode) => {
    const key = node.key as string;
    if (loadedKeys.has(key)) return;

    const depth = key.split('/').filter(Boolean).length;
    const children = await loadChildren(key, depth);
    setLoadedKeys(prev => new Set([...prev, key]));
    setTreeData(prev => {
      const updateChildren = (nodes: DataNode[]): DataNode[] => {
        return nodes.map(n => {
          if (n.key === key) {
            return { ...n, children };
          }
          if (n.children) {
            return { ...n, children: updateChildren(n.children) };
          }
          return n;
        });
      };
      return updateChildren(prev);
    });
  };

  const handleProfileChange = (code: string) => {
    onChange({ ...value, profile: code, path: '' });
  };

  const handlePathChange = (path: string) => {
    onChange({ ...value, path });
  };

  const handleOverwriteChange = (checked: boolean) => {
    onChange({ ...value, overwrite: checked });
  };

  const handleTreeNodeSelect = (keys: React.Key[]) => {
    if (keys.length > 0) {
      handlePathChange(keys[0] as string);
    }
  };

  return (
    <div>
      <div style={{ marginBottom: 12 }}>
        <Text strong>{t('transfer.targetProfile')}</Text>
        <Select
          style={{ width: '100%', marginTop: 4 }}
          value={value.profile || undefined}
          placeholder={t('transfer.selectProfile')}
          onChange={handleProfileChange}
          options={profileOptions}
        />
      </div>

      {value.profile && (
        <>
          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('transfer.targetDir')}</Text>
            <div style={{
              border: '1px solid #d9d9d9',
              borderRadius: 6,
              padding: 8,
              marginTop: 4,
              maxHeight: 240,
              overflow: 'auto',
            }}>
              {treeData.length > 0 ? (
                <Tree
                  treeData={treeData}
                  loadData={onLoadData}
                  expandedKeys={expandedKeys}
                  onExpand={(keys) => setExpandedKeys(keys)}
                  selectedKeys={[value.path]}
                  onSelect={handleTreeNodeSelect}
                />
              ) : (
                <Text type="secondary">{t('transfer.noSubdirs')}</Text>
              )}
            </div>
          </div>

          <div style={{ marginBottom: 12 }}>
            <Text strong>{t('transfer.targetPath')}</Text>
            <Input
              style={{ marginTop: 4 }}
              placeholder={t('transfer.targetPathPlaceholder')}
              value={value.path}
              onChange={e => handlePathChange(e.target.value)}
            />
          </div>
        </>
      )}

      <Checkbox
        checked={value.overwrite}
        onChange={e => handleOverwriteChange(e.target.checked)}
      >
        {t('transfer.overwriteExisting')}
      </Checkbox>
    </div>
  );
};

export default StoragePicker;
