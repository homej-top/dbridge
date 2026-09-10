import React, { useState } from 'react';
import { Modal, Radio, Input, Alert, Space, Typography, Descriptions, Table, Button, message } from 'antd';
import { WarningOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { StorageProfile, SyncPreview, TransferResult } from '../types';
import StoragePicker from './StoragePicker';

const { Text } = Typography;

interface SyncDialogProps {
  open: boolean;
  profiles: StorageProfile[];
  sourceProfile: string;
  sourcePaths: string[];
  onClose: () => void;
  onSuccess: () => void;
}

const fmtSize = (b: number) => b < 1024 ? b + ' B' : b < 1048576 ? (b / 1024).toFixed(1) + ' KB' : b < 1073741824 ? (b / 1048576).toFixed(1) + ' MB' : (b / 1073741824).toFixed(2) + ' GB';

const SyncDialog: React.FC<SyncDialogProps> = ({ open, profiles, sourceProfile, sourcePaths, onClose, onSuccess }) => {
  const { t } = useTranslation();
  const [target, setTarget] = useState({ profile: '', path: '', overwrite: false });
  const [conflict, setConflict] = useState('skip');
  const [mode, setMode] = useState('copy');
  const [patterns, setPatterns] = useState('');
  const [preview, setPreview] = useState<SyncPreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [syncing, setSyncing] = useState(false);

  const handlePreview = async () => {
    if (!target.profile || !target.path) {
      message.warning(t('transfer.fillTarget'));
      return;
    }
    setLoading(true);
    try {
      const filter = patterns.trim()
        ? { patterns: patterns.split(',').map(p => p.trim()).filter(Boolean) }
        : undefined;
      const promises = sourcePaths.map(srcPath =>
        import('../api').then(({ filesAPI }) =>
          filesAPI.syncFiles({
            source_profile: sourceProfile,
            source_path: srcPath,
            target_profile: target.profile,
            target_path: target.path,
            conflict,
            mode,
            dry_run: true,
            filter,
          })
        )
      );
      const results = await Promise.all(promises);
      const merged: SyncPreview = {
        dry_run: true,
        mode,
        to_create: [],
        to_update: [],
        to_delete: [],
        summary: { new_files: 0, updated_files: 0, deleted_files: 0, source_files_to_delete: 0, total_bytes: 0, is_move_mode: mode === 'move' },
      };
      for (const r of results) {
        const d = r.data?.data as SyncPreview;
        if (d) {
          merged.to_create = [...merged.to_create, ...d.to_create];
          merged.to_update = [...merged.to_update, ...d.to_update];
          merged.summary.new_files += d.summary.new_files;
          merged.summary.updated_files += d.summary.updated_files;
          merged.summary.total_bytes += d.summary.total_bytes;
          merged.summary.source_files_to_delete += d.summary.source_files_to_delete;
        }
      }
      setPreview(merged);
    } catch (err: any) {
      message.error(err?.response?.data?.message || t('sync.previewFailed'));
    } finally {
      setLoading(false);
    }
  };

  const handleSync = async () => {
    setSyncing(true);
    try {
      const filter = patterns.trim()
        ? { patterns: patterns.split(',').map(p => p.trim()).filter(Boolean) }
        : undefined;
      const { filesAPI } = await import('../api');
      const promises = sourcePaths.map(srcPath =>
        filesAPI.syncFiles({
          source_profile: sourceProfile,
          source_path: srcPath,
          target_profile: target.profile,
          target_path: target.path,
          conflict,
          mode,
          dry_run: false,
          filter,
        })
      );
      const results = await Promise.all(promises);
      let totalTransferred = 0;
      let totalFailed = 0;
      for (const r of results) {
        const d = r.data?.data as TransferResult;
        if (d) {
          totalTransferred += d.transferred;
          totalFailed += d.failed;
        }
      }
      if (totalFailed === 0) {
        message.success(t('transfer.success', { n: totalTransferred }));
        onClose();
        onSuccess();
      } else {
        message.warning(t('transfer.partialSuccess', { ok: totalTransferred, fail: totalFailed }));
      }
    } catch (err: any) {
      message.error(err?.response?.data?.message || t('transfer.failed'));
    } finally {
      setSyncing(false);
    }
  };

  const previewColumns = [
    { title: t('sync.filePath'), dataIndex: 'path', key: 'path', ellipsis: true },
    { title: t('files.size'), dataIndex: 'size', key: 'size', width: 100, render: (s: number) => fmtSize(s) },
    { title: t('sync.conflict'), dataIndex: 'conflict', key: 'conflict', width: 100, render: (c: string) => c || '-' },
  ];

  return (
    <Modal
      title={t('sync.syncTo')}
      open={open}
      onCancel={onClose}
      width={680}
      footer={
        <Space>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button onClick={handlePreview} loading={loading}>{t('sync.preview')}</Button>
          <Button type="primary" onClick={handleSync} loading={syncing} disabled={!preview}>
            {t('sync.startSync')}
          </Button>
        </Space>
      }
    >
      <StoragePicker
        profiles={profiles}
        value={target}
        onChange={setTarget}
        sourceProfile={sourceProfile}
      />

      <div style={{ marginTop: 16 }}>
        <Text strong>{t('sync.conflictStrategy')}</Text>
        <div style={{ marginTop: 4 }}>
          <Radio.Group value={conflict} onChange={e => setConflict(e.target.value)}>
            <Space direction="vertical">
              <Radio value="skip">{t('sync.skipExisting')}</Radio>
              <Radio value="overwrite">{t('sync.overwriteExisting')}</Radio>
              <Radio value="rename">{t('sync.renameExisting')}</Radio>
            </Space>
          </Radio.Group>
        </div>
      </div>

      <div style={{ marginTop: 16 }}>
        <Text strong>{t('sync.syncMode')}</Text>
        <div style={{ marginTop: 4 }}>
          <Radio.Group value={mode} onChange={e => setMode(e.target.value)}>
            <Space direction="vertical">
              <Radio value="copy">{t('sync.modeCopy')}</Radio>
              <Radio value="move">
                <span>{t('sync.modeMove')} </span>
                <Text type="danger">{t('sync.irreversible')}</Text>
              </Radio>
            </Space>
          </Radio.Group>
        </div>
      </div>

      <div style={{ marginTop: 16 }}>
        <Text strong>{t('sync.fileFilter')}</Text>
        <Input
          style={{ marginTop: 4 }}
          placeholder={t('sync.filterPlaceholder')}
          value={patterns}
          onChange={e => setPatterns(e.target.value)}
        />
      </div>

      {preview && (
        <div style={{ marginTop: 16 }}>
          {preview.summary.is_move_mode && (
            <Alert
              type="error"
              showIcon
              icon={<WarningOutlined />}
              message={t('sync.moveWarning', { n: preview.summary.source_files_to_delete })}
              style={{ marginBottom: 12 }}
            />
          )}
          <Descriptions bordered size="small" column={2}>
            <Descriptions.Item label={t('sync.newFiles')}>{preview.summary.new_files}</Descriptions.Item>
            <Descriptions.Item label={t('sync.updatedFiles')}>{preview.summary.updated_files}</Descriptions.Item>
            <Descriptions.Item label={t('sync.totalBytes')}>{fmtSize(preview.summary.total_bytes)}</Descriptions.Item>
            {preview.summary.is_move_mode && (
              <Descriptions.Item label={t('sync.sourceToDelete')}>
                <Text type="danger">{preview.summary.source_files_to_delete}</Text>
              </Descriptions.Item>
            )}
          </Descriptions>

          {preview.to_create.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <Text strong>{t('sync.toCreate')}</Text>
              <Table
                size="small"
                dataSource={preview.to_create.slice(0, 50)}
                columns={previewColumns}
                rowKey="path"
                pagination={false}
                scroll={{ y: 150 }}
              />
            </div>
          )}

          {preview.to_update.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <Text strong>{t('sync.toUpdate')}</Text>
              <Table
                size="small"
                dataSource={preview.to_update.slice(0, 50)}
                columns={previewColumns}
                rowKey="path"
                pagination={false}
                scroll={{ y: 150 }}
              />
            </div>
          )}
        </div>
      )}
    </Modal>
  );
};

export default SyncDialog;
