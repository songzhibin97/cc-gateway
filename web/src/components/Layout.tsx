import React, { useEffect, useState } from 'react';
import {
  AppstoreOutlined,
  BarChartOutlined,
  BulbFilled,
  BulbOutlined,
  CloudServerOutlined,
  DashboardOutlined,
  FileTextOutlined,
  KeyOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import { Button, ConfigProvider, Input, Layout, Menu, Modal, Typography, theme } from 'antd';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';

const { Content, Footer, Header, Sider } = Layout;
const { Text } = Typography;

const AppLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(false);
  const [darkMode, setDarkMode] = useState(() => {
    const saved = localStorage.getItem('dark_mode');
    if (saved !== null) {
      return saved === 'true';
    }
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  });
  const [tokenModal, setTokenModal] = useState(false);
  const [tokenInput, setTokenInput] = useState('');

  useEffect(() => {
    if (!localStorage.getItem('admin_token')) {
      setTokenModal(true);
    }
  }, []);

  useEffect(() => {
    const handler = () => setTokenModal(true);
    window.addEventListener('auth-error', handler);
    return () => window.removeEventListener('auth-error', handler);
  }, []);

  const handleTokenSave = () => {
    localStorage.setItem('admin_token', tokenInput);
    setTokenModal(false);
    window.location.reload();
  };

  const toggleDark = () => {
    setDarkMode((prev) => {
      localStorage.setItem('dark_mode', String(!prev));
      return !prev;
    });
  };

  const menuItems = [
    { key: '/', icon: <DashboardOutlined />, label: '概览' },
    { key: '/accounts', icon: <CloudServerOutlined />, label: '源账号' },
    { key: '/groups', icon: <AppstoreOutlined />, label: '组' },
    { key: '/keys', icon: <KeyOutlined />, label: '密钥' },
    { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
    { key: '/stats', icon: <BarChartOutlined />, label: '统计' },
    { key: '/settings', icon: <SettingOutlined />, label: '设置' },
  ];

  const selectedKey =
    menuItems.find(
      (item) =>
        location.pathname === item.key ||
        (item.key !== '/' && location.pathname.startsWith(item.key)),
    )?.key || '/';

  return (
    <ConfigProvider
      theme={{ algorithm: darkMode ? theme.darkAlgorithm : theme.defaultAlgorithm }}
    >
      <Layout style={{ minHeight: '100vh' }}>
        <Sider
          collapsible
          collapsed={collapsed}
          onCollapse={setCollapsed}
          theme={darkMode ? 'dark' : 'light'}
        >
          <div
            style={{
              height: 32,
              margin: 16,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <Text strong style={{ fontSize: collapsed ? 14 : 18, whiteSpace: 'nowrap' }}>
              {collapsed ? 'GW' : 'cc-gateway'}
            </Text>
          </div>
          <Menu
            mode="inline"
            selectedKeys={[selectedKey]}
            items={menuItems}
            onClick={({ key }) => navigate(key)}
            theme={darkMode ? 'dark' : 'light'}
          />
        </Sider>
        <Layout>
          <Header
            style={{
              padding: '0 24px',
              display: 'flex',
              justifyContent: 'flex-end',
              alignItems: 'center',
              gap: 12,
              background: 'transparent',
            }}
          >
            <Button
              type="text"
              icon={darkMode ? <BulbFilled /> : <BulbOutlined />}
              onClick={toggleDark}
            />
            <Button size="small" onClick={() => setTokenModal(true)}>
              Token
            </Button>
          </Header>
          <Content style={{ margin: 24 }}>
            <Outlet />
          </Content>
          <Footer style={{ textAlign: 'center', padding: '12px 50px' }}>
            <Text type="secondary">cc-gateway Admin</Text>
          </Footer>
        </Layout>
      </Layout>

      <Modal
        title="Admin Token"
        open={tokenModal}
        onOk={handleTokenSave}
        onCancel={() => {
          if (localStorage.getItem('admin_token')) {
            setTokenModal(false);
          }
        }}
        closable={!!localStorage.getItem('admin_token')}
        maskClosable={false}
      >
        <Input.Password
          placeholder="输入 Admin Token"
          value={tokenInput}
          onChange={(e) => setTokenInput(e.target.value)}
          onPressEnter={handleTokenSave}
        />
        <Text type="secondary" style={{ marginTop: 8, display: 'block' }}>
          环境变量 ADMIN_TOKEN 的值。留空则不需要认证。
        </Text>
      </Modal>
    </ConfigProvider>
  );
};

export default AppLayout;
