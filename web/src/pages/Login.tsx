import React, { useState, useEffect } from 'react';
import { Form, Input, Button, Card, message } from 'antd';
import { UserOutlined, LockOutlined, DatabaseOutlined } from '@ant-design/icons';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { authAPI } from '../api';
import type { LoginResponse } from '../types';

const Login: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const error = searchParams.get('error');
  useEffect(() => {
    if (error === 'missing_code') {
      message.error('登录失败：缺少授权码');
    } else if (error === 'login_failed') {
      message.error('登录失败，请重试');
    }
  }, [error]);

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true);
    try {
      const res = await authAPI.login(values);
      const data = res.data.data as LoginResponse;
      localStorage.setItem('token', data.token);
      localStorage.setItem('user', JSON.stringify(data.user));
      message.success('登录成功');
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
            DBridge 数据库管理平台
          </h2>
          <p style={{ color: '#999', marginTop: 8, fontSize: 13 }}>
            安全、高效的数据同步管理工具
          </p>
        </div>

        <Form name="login" onFinish={onFinish} autoComplete="off" size="large">
            <Form.Item
              name="username"
              rules={[{ required: true, message: '请输入用户名' }]}
            >
              <Input
                prefix={<UserOutlined style={{ color: '#999' }} />}
                placeholder="用户名"
              />
            </Form.Item>

            <Form.Item
              name="password"
              rules={[{ required: true, message: '请输入密码' }]}
            >
              <Input.Password
                prefix={<LockOutlined style={{ color: '#999' }} />}
                placeholder="密码"
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
                登录
              </Button>
            </Form.Item>
          </Form>
      </Card>
    </div>
  );
};

export default Login;
