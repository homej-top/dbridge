import React, { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import {
  Layout,
  Menu,
  Button,
  Space,
  Avatar,
  Dropdown,
  message,
  Modal,
  Form,
  Input,
} from 'antd';
const { Sider, Header, Content } = Layout;
import {
  DashboardOutlined,
  LinkOutlined,
  SettingOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  UserOutlined,
  KeyOutlined,
  TranslationOutlined,
} from '@ant-design/icons';
import { authAPI } from '../api';

const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(false);
  const [pwdModalOpen, setPwdModalOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [pwdForm] = Form.useForm();
  const navigate = useNavigate();
  const location = useLocation();
  const { t, i18n } = useTranslation();

  const user = JSON.parse(localStorage.getItem('user') || '{}');
  const menuItems = useMemo(() => [
    { key: '/', icon: <DashboardOutlined />, label: t('nav.dashboard') },
    { type: 'divider' as const },
    {
      key: 'connect', icon: <LinkOutlined />, label: t('nav.dataConnect'),
      children: [
        { key: '/datasources', label: t('nav.datasourceManage') },
      ],
    },
    {
      key: 'data-ops', icon: <SettingOutlined />, label: t('nav.dataOps'),
      children: [
        { key: '/query', label: t('nav.query') },
        { key: '/compare', label: t('nav.compare') },
      ],
    },
    { type: 'divider' as const },
    {
      key: 'system', icon: <SettingOutlined />, label: t('nav.systemManage'),
      children: [
        { key: '/audit', label: t('nav.auditLogs') },
        { key: '/settings', label: t('nav.settings') },
      ],
    },
  ], [t]);

  const handleLogout = () => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    message.success(t('common.logoutSuccess'));
    navigate('/login');
  };

  const handleChangePassword = async () => {
    const values = await pwdForm.validateFields();
    if (values.new_password !== values.confirm_password) {
      message.error(t('common.passwordMismatch'));
      return;
    }
    setSubmitting(true);
    try {
      await authAPI.changePassword({
        old_password: values.old_password,
        new_password: values.new_password,
      });
      message.success(t('common.passwordChangeSuccess'));
      setPwdModalOpen(false);
      pwdForm.resetFields();
      handleLogout();
    } catch {
      // handled by interceptor
    } finally {
      setSubmitting(false);
    }
  };

  const userMenuItems = [
    ...(!user.auth_provider ? [{
      key: 'change-password',
      icon: <KeyOutlined />,
      label: t('common.changePassword'),
      onClick: () => { pwdForm.resetFields(); setPwdModalOpen(true); },
    }] : []),
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: t('common.logout'),
      onClick: handleLogout,
    },
  ];

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        trigger={null}
        collapsible
        collapsed={collapsed}
        width={200}
        collapsedWidth={60}
        style={{
          background: '#3a3f4a',
          boxShadow: '2px 0 8px rgba(0,0,0,0.15)',
        }}
      >
        <div
          style={{
            height: 50,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            fontWeight: 'bold',
            fontSize: collapsed ? 16 : 18,
            borderBottom: '1px solid rgba(255,255,255,0.1)',
            letterSpacing: 1,
          }}
        >
          {collapsed ? 'DB' : 'DBridge'}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[location.pathname]}
          items={menuItems}
          onClick={({ key }) => { if (key.startsWith('/')) navigate(key); }}
          style={{ background: '#3a3f4a', borderRight: 0 }}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            height: 50,
            lineHeight: '50px',
            padding: '0 16px',
            background: '#fff',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            borderBottom: '1px solid #e8e8e8',
            boxShadow: '0 1px 4px rgba(0,0,0,0.08)',
          }}
        >
          <Button
            type="text"
            icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            onClick={() => setCollapsed(!collapsed)}
            style={{ fontSize: 16 }}
          />
          <Space>
            <Button
              size="small"
              icon={<TranslationOutlined />}
              onClick={() => i18n.changeLanguage(i18n.language === 'zh-CN' ? 'en-US' : 'zh-CN')}
            >
              {i18n.language === 'zh-CN' ? 'EN' : '中文'}
            </Button>
            <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
            <Space style={{ cursor: 'pointer' }}>
              <Avatar
                size={30}
                src={user.avatar_url || undefined}
                icon={<UserOutlined />}
                style={{ background: '#20a53a' }}
              >
                {user.username?.[0]?.toUpperCase() || 'U'}
              </Avatar>
              <span style={{ color: '#333' }}>{user.username || t('common.user')}</span>
            </Space>
          </Dropdown>
          </Space>
        </Header>
        <Content
          style={{
            margin: 16,
            padding: 20,
            background: '#fff',
            borderRadius: 4,
            minHeight: 'calc(100vh - 82px)',
          }}
        >
          <Outlet />
        </Content>
      </Layout>

      <Modal
        title={t('common.changePassword')}
        open={pwdModalOpen}
        onCancel={() => setPwdModalOpen(false)}
        onOk={handleChangePassword}
        confirmLoading={submitting}
        okText={t('common.confirm')}
        cancelText={t('common.cancel')}
      >
        <Form form={pwdForm} layout="vertical">
          <Form.Item
            name="old_password"
            label={t('common.oldPassword')}
            rules={[{ required: true, message: t('common.requireOldPassword') }]}
          >
            <Input.Password placeholder={t('common.oldPasswordPlaceholder')} />
          </Form.Item>
          <Form.Item
            name="new_password"
            label={t('common.newPassword')}
            rules={[
              { required: true, message: t('common.requireNewPassword') },
              { min: 6, message: t('common.passwordMinLen') },
            ]}
          >
            <Input.Password placeholder={t('common.newPasswordPlaceholder')} />
          </Form.Item>
          <Form.Item
            name="confirm_password"
            label={t('common.confirmPassword')}
            rules={[{ required: true, message: t('common.requireConfirmPassword') }]}
          >
            <Input.Password placeholder={t('common.confirmPasswordPlaceholder')} />
          </Form.Item>
        </Form>
      </Modal>
    </Layout>
  );
};

export default MainLayout;
