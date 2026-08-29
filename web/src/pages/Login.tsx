import React, { useState, useEffect } from 'react';
import { Form, Input, Button, Card, message } from 'antd';
import { UserOutlined, LockOutlined, DatabaseOutlined } from '@ant-design/icons';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { authAPI } from '../api';
import type { LoginResponse } from '../types';

const Login: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { t } = useTranslation();

  const error = searchParams.get('error');
  useEffect(() => {
    if (error === 'missing_code') {
      message.error(t('login.failedMissingCode'));
    } else if (error === 'login_failed') {
      message.error(t('login.failedRetry'));
    }
  }, [error, t]);

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true);
    try {
      const res = await authAPI.login(values);
      const data = res.data.data as LoginResponse;
      localStorage.setItem('token', data.token);
      localStorage.setItem('user', JSON.stringify(data.user));
      message.success(t('login.success'));
      navigate('/');
    } catch {
      // Error already handled by interceptor
    } finally {
      setLoading(false);
    }
  };

  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        minHeight: '100vh',
        background: 'linear-gradient(135deg, #3a3f4a 0%, #2c3038 100%)',
      }}
    >
      <Card
        style={{
          width: 420,
          borderRadius: 8,
          boxShadow: '0 4px 24px rgba(0,0,0,0.2)',
        }}
        bordered={false}
      >
        <div style={{ textAlign: 'center', marginBottom: 32 }}>
          <DatabaseOutlined
            style={{ fontSize: 48, color: '#20a53a', marginBottom: 12 }}
          />
          <h2 style={{ margin: 0, fontSize: 22, color: '#333' }}>
            {t('login.title')}
          </h2>
          <p style={{ color: '#999', marginTop: 8, fontSize: 13 }}>
            {t('login.subtitle')}
          </p>
        </div>

        <Form name="login" onFinish={onFinish} autoComplete="off" size="large">
            <Form.Item
              name="username"
              rules={[{ required: true, message: t('login.requireUsername') }]}
            >
              <Input
                prefix={<UserOutlined style={{ color: '#999' }} />}
                placeholder={t('login.username')}
              />
            </Form.Item>

            <Form.Item
              name="password"
              rules={[{ required: true, message: t('login.requirePassword') }]}
            >
              <Input.Password
                prefix={<LockOutlined style={{ color: '#999' }} />}
                placeholder={t('login.password')}
              />
            </Form.Item>

            <Form.Item style={{ marginBottom: 0 }}>
              <Button
                type="primary"
                htmlType="submit"
                loading={loading}
                block
                style={{ height: 42 }}
              >
                {t('login.submit')}
              </Button>
            </Form.Item>
          </Form>
      </Card>
    </div>
  );
};

export default Login;
