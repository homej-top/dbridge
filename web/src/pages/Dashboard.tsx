import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Row, Col, Card, Statistic, Spin } from 'antd';
import {
  DatabaseOutlined, SyncOutlined, CodeOutlined, CheckCircleOutlined,
} from '@ant-design/icons';
import { dashboardAPI } from '../api';

interface Stats {
  data_source_count: number;
  sync_task_count: number;
  query_count: number;
  success_rate: number;
  audit_log_count: number;
  running_syncs: number;
}

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);
  const { t: tr } = useTranslation();

  useEffect(() => {
    loadStats();
  }, []);

  const loadStats = async () => {
    try {
      const res = await dashboardAPI.stats();
      setStats(res.data.data);
    } catch {} finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <h2 style={{ fontSize: 18, marginBottom: 16 }}>{tr('nav.dashboard')}</h2>
      <Spin spinning={loading}>
        <Row gutter={[16, 16]}>
          <Col span={6}>
            <Card hoverable style={{ borderLeft: '3px solid #20a53a' }} styles={{ body: { padding: '20px 24px' } }}>
              <Statistic title={<span style={{ color: '#666' }}>{tr('dashboards.dataSources')}</span>}
                value={stats?.data_source_count ?? 0} prefix={<DatabaseOutlined style={{ color: '#20a53a' }} />}
                valueStyle={{ color: '#333', fontWeight: 600 }} />
            </Card>
          </Col>
          <Col span={6}>
            <Card hoverable style={{ borderLeft: '3px solid #1890ff' }} styles={{ body: { padding: '20px 24px' } }}>
              <Statistic title={<span style={{ color: '#666' }}>{tr('dashboards.syncTasks')}</span>}
                value={stats?.sync_task_count ?? 0} prefix={<SyncOutlined style={{ color: '#1890ff' }} />}
                valueStyle={{ color: '#333', fontWeight: 600 }} />
              {stats && stats.running_syncs > 0 && (
                <div style={{ fontSize: 12, color: '#1890ff', marginTop: 4 }}>{stats.running_syncs} {tr('dashboards.running')}</div>
              )}
            </Card>
          </Col>
          <Col span={6}>
            <Card hoverable style={{ borderLeft: '3px solid #722ed1' }} styles={{ body: { padding: '20px 24px' } }}>
              <Statistic title={<span style={{ color: '#666' }}>{tr('dashboards.queryCount')}</span>}
                value={stats?.query_count ?? 0} prefix={<CodeOutlined style={{ color: '#722ed1' }} />}
                valueStyle={{ color: '#333', fontWeight: 600 }} />
            </Card>
          </Col>
          <Col span={6}>
            <Card hoverable style={{ borderLeft: '3px solid #52c41a' }} styles={{ body: { padding: '20px 24px' } }}>
              <Statistic title={<span style={{ color: '#666' }}>{tr('dashboards.successRate')}</span>}
                value={stats?.success_rate ?? 100} precision={1} suffix="%"
                prefix={<CheckCircleOutlined style={{ color: '#52c41a' }} />}
                valueStyle={{ color: '#333', fontWeight: 600 }} />
            </Card>
          </Col>
        </Row>
      </Spin>
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col span={24}>
          <Card title={tr('dashboards.systemInfo')} size="small">
            <Row gutter={16}>
              <Col span={8}>
                <div style={{ padding: '8px 0', borderBottom: '1px solid #f0f0f0' }}>
                  <span style={{ color: '#999' }}>{tr('dashboards.sysVersion')}:</span>
                  <span style={{ color: '#333' }}>DBridge v1.0.0</span>
                </div>
              </Col>
              <Col span={8}>
                <div style={{ padding: '8px 0', borderBottom: '1px solid #f0f0f0' }}>
                  <span style={{ color: '#999' }}>{tr('dashboards.runStatus')}:</span>
                  <span style={{ color: '#20a53a' }}>{tr('dashboards.normal')}</span>
                </div>
              </Col>
              <Col span={8}>
                <div style={{ padding: '8px 0', borderBottom: '1px solid #f0f0f0' }}>
                  <span style={{ color: '#999' }}>{tr('dashboards.auditLogs')}:</span>
                  <span style={{ color: '#333' }}>{stats?.audit_log_count ?? 0} {tr('common.rows')}</span>
                </div>
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default Dashboard;
