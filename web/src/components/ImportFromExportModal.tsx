import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal, Form, Input, Select, message } from 'antd';
import { exportTaskAPI, dsAPI } from '../api';
import type { DataSource, ExportTask } from '../types';

interface Props {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  preselectTaskId?: string;
}

const ImportFromExportModal: React.FC<Props> = ({ open, onClose, onSuccess, preselectTaskId }) => {
  const { t } = useTranslation('importFromExport');
  const [form] = Form.useForm();
  const [dataSources, setDataSources] = useState<DataSource[]>([]);
  const [exportTasks, setExportTasks] = useState<ExportTask[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (open) {
      form.resetFields();
      dsAPI.list().then(res => setDataSources(res.data.data?.list || [])).catch(() => {});
      exportTaskAPI.list({ task_type: 'export', status: 'completed', page_size: 100 })
        .then(res => {
          const list = res.data.data?.list || [];
          setExportTasks(list);
          // Set preselect value after state update renders
          if (preselectTaskId) {
            requestAnimationFrame(() => form.setFieldValue('export_task_id', preselectTaskId));
          }
        })
        .catch(() => {});
    }
  }, [open, preselectTaskId]);

  const handleTargetDsChange = (dsId: string) => {
    const ds = dataSources.find(d => d.id === dsId);
    if (ds?.database) {
      form.setFieldValue('import_target_schema', ds.database);
    }
  };

  const handleSubmit = async () => {
    const values = await form.validateFields();
    // Extract ID from labelInValue object
    if (values.export_task_id && typeof values.export_task_id === 'object') {
      values.export_task_id = values.export_task_id.value;
    }
    setLoading(true);
    try {
      await exportTaskAPI.createImportFromExport(values);
      message.success(t('taskCreated'));
      form.resetFields();
      onSuccess();
      onClose();
    } catch { /* handled */ }
    finally { setLoading(false); }
  };

  return (
    <Modal
      title={t('importFromExport')}
      open={open}
      onOk={handleSubmit}
      onCancel={onClose}
      confirmLoading={loading}
      okText={t('create')}
      cancelText={t('cancel')}
      width={500}
    >
      <Form form={form} layout="vertical">
        <Form.Item name="name" label={t('taskName')} rules={[{ required: true }]}>
          <Input placeholder={t('enterTaskName')} />
        </Form.Item>
        <Form.Item name="export_task_id" label={t('selectExportTask')} rules={[{ required: true }]}>
          <Select
            showSearch
            allowClear
            optionFilterProp="label"
            placeholder={t('selectCompletedExportTask')}
            options={exportTasks.map(t => ({
              label: `${t.name} (${t.schema_name}) - ${t.export_format}`,
              value: t.id,
            }))}
          />
        </Form.Item>
        <Form.Item name="import_target_ds_id" label={t('targetDatasource')} rules={[{ required: true }]}>
          <Select
            options={dataSources.map(ds => ({ label: `${ds.name} (${ds.type})`, value: ds.id }))}
            placeholder={t('selectTargetDatasource')}
            onChange={handleTargetDsChange}
          />
        </Form.Item>
        <Form.Item name="import_target_schema" label={t('targetSchema')}>
          <Input placeholder={t('targetSchemaOptional')} />
        </Form.Item>
        <Form.Item name="import_strategy" label={t('importStrategy')} initialValue="fail" tooltip={t('pkConflictHandling')}>
          <Select
            options={[
              { label: t('strategyFail'), value: 'fail' },
              { label: t('strategySkip'), value: 'skip' },
              { label: t('strategyUpdate'), value: 'update' },
              { label: t('strategyReplace'), value: 'replace' },
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
};

export default ImportFromExportModal;
