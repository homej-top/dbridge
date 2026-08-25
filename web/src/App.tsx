import React, { useMemo } from 'react';
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
} from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import enUS from 'antd/locale/en_US';
import { useTranslation } from 'react-i18next';
import MainLayout from './layouts/MainLayout';
import Login from './pages/Login';
import Dashboard from './pages/Dashboard';
import DataSources from './pages/DataSources';
import SQLEditor from './pages/SQLEditor';
import Compare from './pages/Compare';
import SyncTasks from './pages/SyncTasks';
import AuditLogs from './pages/AuditLogs';
import Settings from './pages/Settings';

// Protected route wrapper
const ProtectedRoute: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const token = localStorage.getItem('token');
  if (!token) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
};

const App: React.FC = () => {
  const { i18n } = useTranslation();
  const antLocale = useMemo(() => i18n.language === 'en-US' ? enUS : zhCN, [i18n.language]);
  return (
    <ConfigProvider
      locale={antLocale}
      theme={{
        token: {
          colorPrimary: '#20a53a',
          colorSuccess: '#20a53a',
          colorError: '#e74c3c',
          colorWarning: '#f0ad4e',
          colorInfo: '#20a53a',
          borderRadius: 4,
          colorBgLayout: '#f0f2f5',
          fontFamily: "system-ui, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
          fontSize: 14,
          colorText: '#333',
          colorTextSecondary: '#666',
          controlHeight: 32,
        },
        components: {
          Table: {
            headerBg: '#fafafa',
            headerColor: '#333',
            borderColor: '#e8e8e8',
            rowHoverBg: '#f5f5f5',
          },
          Button: {
            primaryShadow: 'none',
            defaultShadow: 'none',
          },
          Card: {
            paddingLG: 16,
          },
          Modal: {
            titleFontSize: 16,
            headerBg: '#fff',
          },
          Menu: {
            darkItemBg: '#3a3f4a',
            darkItemColor: '#ccc',
            darkItemHoverColor: '#fff',
            darkItemSelectedBg: '#20a53a',
            darkItemSelectedColor: '#fff',
            darkSubMenuItemBg: '#33383f',
          },
          Layout: {
            siderBg: '#3a3f4a',
            headerBg: '#fff',
            bodyBg: '#f0f2f5',
            headerHeight: 50,
            headerPadding: '0 16px',
          },
        },
      }}
    >
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <MainLayout />
              </ProtectedRoute>
            }
          >
            <Route index element={<Dashboard />} />
            <Route path="datasources" element={<DataSources />} />
            <Route path="query" element={<SQLEditor />} />
            <Route path="compare" element={<Compare />} />
            <Route path="sync" element={<SyncTasks />} />
            <Route path="audit" element={<AuditLogs />} />
            <Route path="settings" element={<Settings />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ConfigProvider>
  );
};

export default App;
