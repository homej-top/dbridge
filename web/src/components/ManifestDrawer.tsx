import React, { useEffect, useState } from 'react';
import { Drawer, Descriptions, Table, Tag, Alert } from 'antd';
import { exportTaskAPI } from '../api';
import { useTranslation } from 'react-i18next';

interface Manifest {
  version: number;
  source: {
    data_source: string;
    data_source_type: string;
    schema: string;
    export_time: string;
    format: string;
    target_db_type: string;
  };
  export_type: string;
  query?: string;
  tables: Array<{
    name: string;
    files: string[];
    row_count: number;
    batch_size: number;
    structure: Array<{
      name: string;
      type: string;
      nullable: string;
      key: string;
      default: string;
      comment: string;
    }>;
  }>;
  skipped: Array<{
    table: string;
    reason: string;
    skipped_parts: number[];
  }>;
}

interface Props {
  open: boolean;
  execId: string;
  onClose: () => void;
}

const ManifestDrawer: React.FC<Props> = ({ open, execId, onClose }) => {
  const { t } = useTranslation('exportManifest');
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (open && execId) {
      setLoading(true);
      exportTaskAPI.getManifest(execId)
        .then((res: any) => setManifest(res.data.data))
        .catch(() => {})
        .finally(() => setLoading(false));
    }
  }, [open, execId]);

  const tableColumns = [
    { title: t('tableName'), dataIndex: 'name', key: 'name', width: 150 },
    {
      title: t('files'), dataIndex: 'files', key: 'files', width: 200,
      render: (files: string[]) => files.join(', '),
    },
    { title: t('rowCount'), dataIndex: 'row_count', key: 'row_count', width: 80 },
    { title: t('batchSize'), dataIndex: 'batch_size', key: 'batch_size', width: 80 },
  ];

  const structureColumns = [
    { title: t('columnName'), dataIndex: 'name', key: 'name', width: 150 },
    { title: t('type'), dataIndex: 'type', key: 'type', width: 120 },
    { title: t('nullable'), dataIndex: 'nullable', key: 'nullable', width: 60 },
    { title: t('primaryKey'), dataIndex: 'key', key: 'key', width: 60, render: (k: string) => k === 'PRI' ? <Tag color="blue">PRI</Tag> : '' },
    { title: t('defaultValue'), dataIndex: 'default', key: 'default', width: 100 },
    { title: t('comment'), dataIndex: 'comment', key: 'comment', width: 150 },
  ];

  const [expandedTable, setExpandedTable] = useState<string | null>(null);

  return (
    <Drawer
      title={t('exportDetails')}
      open={open}
      onClose={onClose}
      width={800}
    >
      {loading ? (
        <div>{t('loading')}</div>
      ) : manifest ? (
        <>
          <Descriptions bordered size="small" column={2} style={{ marginBottom: 16 }}>
            {manifest.source ? (
              <>
                <Descriptions.Item label={t('dataSource')}>{manifest.source.data_source} ({manifest.source.data_source_type})</Descriptions.Item>
                <Descriptions.Item label={t('schemaLabel')}>{manifest.source.schema}</Descriptions.Item>
                <Descriptions.Item label={t('exportTime')}>{manifest.source.export_time}</Descriptions.Item>
                <Descriptions.Item label={t('format')}>{manifest.source.format}</Descriptions.Item>
                {manifest.source.target_db_type && (
                  <Descriptions.Item label={t('targetDatabase')}>{manifest.source.target_db_type}</Descriptions.Item>
                )}
              </>
            ) : (
              <Descriptions.Item label={t('info')}>{t('incompleteManifest')}</Descriptions.Item>
            )}
            <Descriptions.Item label={t('exportType')}>
              <Tag color={manifest.export_type === 'tables' ? 'blue' : 'green'}>
                {manifest.export_type === 'tables' ? t('tableExport') : t('queryResult')}
              </Tag>
            </Descriptions.Item>
          </Descriptions>

          {manifest.query && (
            <Alert
              message={t('querySQL')}
              description={<code>{manifest.query}</code>}
              type="info"
              style={{ marginBottom: 16 }}
            />
          )}

          {manifest.tables && manifest.tables.length > 0 && (
            <>
              <h4>{t('exportTables')} ({manifest.tables.length} {t('count')})</h4>
              <Table
                columns={tableColumns}
                dataSource={manifest.tables}
                rowKey="name"
                pagination={false}
                size="small"
                style={{ marginBottom: 16 }}
                onRow={(record) => ({
                  onClick: () => setExpandedTable(expandedTable === record.name ? null : record.name),
                  style: { cursor: 'pointer' },
                })}
              />
              {expandedTable && manifest.tables.find(item => item.name === expandedTable) && (
                <div style={{ marginBottom: 16 }}>
                  <h4>{expandedTable} {t('tableStructure')}</h4>
                  <Table
                    columns={structureColumns}
                    dataSource={manifest.tables.find(item => item.name === expandedTable)!.structure}
                    rowKey="name"
                    pagination={false}
                    size="small"
                  />
                </div>
              )}
            </>
          )}

          {manifest.skipped && manifest.skipped.length > 0 && (
            <>
              <h4>{t('skippedTables')} ({manifest.skipped.length} {t('count')})</h4>
              {manifest.skipped.map((skip, idx) => (
                <Alert
                  key={idx}
                  message={skip.table}
                  description={
                    <>
                      <div>{t('reason')}: {skip.reason}</div>
                      {skip.skipped_parts.length > 0 && (
                        <div>{t('skippedBatches')}: {skip.skipped_parts.join(', ')}</div>
                      )}
                    </>
                  }
                  type="warning"
                  style={{ marginBottom: 8 }}
                />
              ))}
            </>
          )}
        </>
      ) : (
        <div>{t('unableLoadManifest')}</div>
      )}
    </Drawer>
  );
};

export default ManifestDrawer;
